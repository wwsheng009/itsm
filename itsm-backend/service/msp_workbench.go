// msp_workbench.go：MSP 跨客户工作台（IP-P0-7；WB1–WB6）。
//
// 设计约束（工作台方案 §3）：
//  1. 不切换会话即可看+做多客户单据；允许集合一律来自服务端（MSPAllocation → middleware.MSPContext），
//     不接受客户端传入集合作为授权依据；
//  2. P0 采用"逐租户查询 + 内存合并"（对 RLS 天然友好）；跨租户写走条目级端点，
//     逐条：资源租户 ∈ 分配 ∧ 目标租户 RBAC ∧ 租户 active ∧ 资源状态；
//  3. 每个条目返回 allowedActions[]（前端仅按该数组渲染，禁止前端推断权限）；
//  4. 跨租户写逐条审计（source=workbench + target_tenant_id 写入 request_body；
//     独立列随 IP-P0-10 审计统一落库）。
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/predicate"
	entTenant "itsm-backend/ent/tenant"
	entTicket "itsm-backend/ent/ticket"
	"itsm-backend/ent/user"
	usertenantmembership "itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	"itsm-backend/repository/ticket"

	"go.uber.org/zap"
)

// 工作台错误码（§3.0-A 注册表；经 CustomerAccessError 通道返回给 handler）。
const (
	CodeInvalidCursor      = "INVALID_CURSOR"
	CodeActionNotAllowed   = "ACTION_NOT_ALLOWED"
	CodeTooManyTenants     = "TOO_MANY_TENANTS"
	CodeBatchLimitExceeded = "BATCH_LIMIT_EXCEEDED"
	CodeBatchRateLimited   = "BATCH_RATE_LIMITED"
)

// workbench 常量。
const (
	workbenchLimitDefault = 50
	workbenchLimitMax     = 200
	workbenchTenantMax    = 50
	workbenchSummaryTTL   = 30
	workbenchBatchMax     = 100 // IP-P1-6：单批上限（工作台方案 §3.3）
	workbenchBatchPerMin  = 20  // 每租户每分钟批量次数（防单客户风暴）
	// IP-P2-4b："临近 SLA" 窗口（24h 内到期且未关闭）——与前端看板展示口径一致。
	workbenchSLADueSoonWindow = 24 * time.Hour
	// IP-P2-4c：用量窗口（近 N 天新建工单）——硬配额无数据源，先交付 usage-only 口径。
	workbenchUsageWindowDays = 30
)

// MSPWorkbenchActor 工作台调用方身份快照（由 handler 从 MSPContext 转换）。
type MSPWorkbenchActor struct {
	UserID           int
	Username         string
	HomeTenantID     int
	MSPRole          string
	AllowedCustomers []int
	// BatchID 非空表示本次调用来自批量入口（IP-P1-6）：逐条审计带 batch_id。
	BatchID string
}

// MSPWorkbenchService 跨客户工作台服务。
type MSPWorkbenchService struct {
	client     *ent.Client
	ticketSvc  *TicketService
	commentSvc *TicketCommentService
	logger     *zap.SugaredLogger
	batchMu    sync.Mutex
	batchHits  map[int][]time.Time
}

// NewMSPWorkbenchService 构造工作台服务（依赖均可为 nil，方法内 fail-closed）。
func NewMSPWorkbenchService(client *ent.Client, ticketSvc *TicketService, commentSvc *TicketCommentService, logger *zap.SugaredLogger) *MSPWorkbenchService {
	return &MSPWorkbenchService{client: client, ticketSvc: ticketSvc, commentSvc: commentSvc, logger: logger}
}

type workbenchCursor struct {
	V         int                  `json:"v"`
	Sort      string               `json:"sort"`
	PerTenant []workbenchCursorPos `json:"perTenant"`
}

type workbenchCursorPos struct {
	TenantID int    `json:"t"`
	LastSort string `json:"s,omitempty"` // updated: RFC3339Nano；sla: RFC3339Nano 或空=null 截止时间
	LastID   int    `json:"id"`
}

func encodeWorkbenchCursor(c *workbenchCursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeWorkbenchCursor(raw, wantSort string) (*workbenchCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return &workbenchCursor{V: 1, Sort: wantSort}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, NewCustomerAccessError(CodeInvalidCursor, "游标格式非法")
	}
	var c workbenchCursor
	if err := json.Unmarshal(data, &c); err != nil || c.V != 1 || c.Sort != wantSort {
		return nil, NewCustomerAccessError(CodeInvalidCursor, "游标非法或与排序不匹配")
	}
	return &c, nil
}

// ListTickets 跨客户工单列表（逐租户查询 + 内存合并，P0）。
func (s *MSPWorkbenchService) ListTickets(ctx context.Context, actor MSPWorkbenchActor, req dto.WorkbenchTicketQuery) (*dto.WorkbenchTicketListResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	tenantIDs, err := s.normalizeTenantSet(req.CustomerTenantIDs, actor.AllowedCustomers)
	if err != nil {
		if ae, ok := AsCustomerAccessError(err); ok && ae.Code == CodeMSPAllocationRequired {
			s.recordScopeDenied(ctx, actor, req.CustomerTenantIDs)
		}
		return nil, err
	}
	sortKey := req.Sort
	if sortKey == "" {
		sortKey = "updated"
	}
	if sortKey != "updated" && sortKey != "sla" {
		return nil, NewCustomerAccessError(CodeInvalidCursor, "不支持的排序: %s", sortKey)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = workbenchLimitDefault
	}
	if limit > workbenchLimitMax {
		limit = workbenchLimitMax
	}
	cur, err := decodeWorkbenchCursor(req.Cursor, sortKey)
	if err != nil {
		return nil, err
	}
	posByTenant := make(map[int]workbenchCursorPos, len(cur.PerTenant))
	for _, p := range cur.PerTenant {
		posByTenant[p.TenantID] = p
	}

	tenantMeta, err := s.loadTenantMeta(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	assigneeNames, err := s.loadAssigneeNames(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}

	type mergedRow struct {
		ent   *ent.Ticket
		order time.Time
	}

	merged := make([]mergedRow, 0, limit+1)
	moreByTenant := make(map[int]bool, len(tenantIDs))
	for _, tid := range tenantIDs {
		// IP-P2-2：授权边界内按目标客户租户重绑定 ctx（RLS enforce 下 GUC 单值 = 查询语义）。
		qctx := tenantctx.WithTenantID(ctx, tid)
		q := s.client.Ticket.Query().Where(entTicket.TenantIDEQ(tid))
		q = applyWorkbenchFilters(q, req)
		if p, ok := posByTenant[tid]; ok {
			q = applyWorkbenchCursor(q, sortKey, p)
		}
		if sortKey == "sla" {
			q = q.Order(ent.Asc(entTicket.FieldSLAResolutionDeadline), ent.Asc(entTicket.FieldID))
		} else {
			q = q.Order(ent.Desc(entTicket.FieldUpdatedAt), ent.Desc(entTicket.FieldID))
		}
		rows, err := q.Limit(limit + 1).All(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench query tenant %d: %w", tid, err)
		}
		if len(rows) > limit {
			moreByTenant[tid] = true
		}
		for _, r := range rows {
			merged = append(merged, mergedRow{ent: r, order: workbenchOrderKey(r, sortKey)})
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if sortKey == "sla" {
			if merged[i].order.Equal(merged[j].order) {
				return merged[i].ent.ID < merged[j].ent.ID
			}
			return merged[i].order.Before(merged[j].order)
		}
		if merged[i].order.Equal(merged[j].order) {
			return merged[i].ent.ID > merged[j].ent.ID
		}
		return merged[i].order.After(merged[j].order)
	})

	trimmed := len(merged) > limit
	if trimmed {
		merged = merged[:limit]
	}

	items := make([]dto.WorkbenchTicketItem, 0, len(merged))
	lastReturned := make(map[int]workbenchCursorPos)
	rbacCache := make(map[int]map[string]bool)
	for _, row := range merged {
		tid := row.ent.TenantID
		meta := tenantMeta[tid]
		perms, ok := rbacCache[tid]
		if !ok {
			perms = s.tenantActionPermissions(ctx, actor, tid, meta.active())
			rbacCache[tid] = perms
		}
		item := dto.WorkbenchTicketItem{
			ID:               row.ent.ID,
			CustomerTenantID: tid,
			CustomerName:     meta.name,
			TicketNumber:     row.ent.TicketNumber,
			Title:            row.ent.Title,
			Status:           row.ent.Status,
			Priority:         row.ent.Priority,
			UpdatedAt:        row.ent.UpdatedAt,
			AllowedActions:   workbenchAllowedActions(row.ent, meta.active(), perms),
		}
		if row.ent.AssigneeID > 0 {
			item.AssigneeID = row.ent.AssigneeID
			item.AssigneeName = assigneeNames[row.ent.AssigneeID]
		}
		if !row.ent.SLAResolutionDeadline.IsZero() {
			deadline := row.ent.SLAResolutionDeadline
			item.SLADeadline = &deadline
		}
		items = append(items, item)
		pos := workbenchCursorPos{TenantID: tid, LastID: row.ent.ID}
		if sortKey == "sla" {
			if !row.ent.SLAResolutionDeadline.IsZero() {
				// 保持读取值的时区/精度（sqlite 以文本比较时间；UTC 归一化会破坏字符串序）。
				pos.LastSort = row.ent.SLAResolutionDeadline.Format(time.RFC3339Nano)
			}
		} else {
			pos.LastSort = row.ent.UpdatedAt.Format(time.RFC3339Nano)
		}
		lastReturned[tid] = pos
	}

	next := &workbenchCursor{V: 1, Sort: sortKey}
	for _, tid := range tenantIDs {
		if p, ok := lastReturned[tid]; ok {
			next.PerTenant = append(next.PerTenant, p)
			continue
		}
		// 本页未返回该租户的行：保留既有游标位置（避免跳行/漏行）。
		if p, ok := posByTenant[tid]; ok && p.LastID > 0 {
			next.PerTenant = append(next.PerTenant, p)
		}
	}
	var nextCursor string
	if len(next.PerTenant) > 0 {
		// 仅当可能存在更多行时给出游标：任一租户本次取满 limit+1 或该租户仍有未返回的既有位置。
		hasMore := trimmed
		if !hasMore {
			for _, v := range moreByTenant {
				if v {
					hasMore = true
					break
				}
			}
		}
		if hasMore {
			nextCursor, _ = encodeWorkbenchCursor(next)
		}
	}

	return &dto.WorkbenchTicketListResponse{Items: items, NextCursor: nextCursor, Total: len(items)}, nil
}

// normalizeTenantSet 校验请求集合 ⊆ 服务端允许集合；显式请求未分配租户 → 403（防枚举：不区分"不存在"）。
func (s *MSPWorkbenchService) normalizeTenantSet(requested, allowed []int) ([]int, error) {
	allowedSet := make(map[int]bool, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = true
	}
	if len(requested) == 0 {
		out := append([]int(nil), allowed...)
		sort.Ints(out)
		return out, nil
	}
	if len(requested) > workbenchTenantMax {
		return nil, NewCustomerAccessError(CodeTooManyTenants, "单请求租户集合不得超过 %d", workbenchTenantMax)
	}
	seen := make(map[int]bool, len(requested))
	out := make([]int, 0, len(requested))
	for _, id := range requested {
		if id <= 0 {
			continue
		}
		if !allowedSet[id] {
			return nil, NewCustomerAccessError(CodeMSPAllocationRequired, "租户 %d 未分配或不可访问", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out, nil
}

type workbenchTenantMeta struct {
	name   string
	status string
}

func (m workbenchTenantMeta) active() bool { return m.status == "active" }

func (s *MSPWorkbenchService) loadTenantMeta(ctx context.Context, ids []int) (map[int]workbenchTenantMeta, error) {
	out := make(map[int]workbenchTenantMeta, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.client.Tenant.Query().Where(entTenant.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("workbench load tenants: %w", err)
	}
	for _, t := range rows {
		out[t.ID] = workbenchTenantMeta{name: t.Name, status: t.Status}
	}
	return out, nil
}

func (s *MSPWorkbenchService) loadAssigneeNames(ctx context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.client.User.Query().Where(user.TenantIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("workbench load assignees: %w", err)
	}
	for _, u := range rows {
		out[u.ID] = u.Name
	}
	return out, nil
}

func applyWorkbenchFilters(q *ent.TicketQuery, req dto.WorkbenchTicketQuery) *ent.TicketQuery {
	if strings.TrimSpace(req.Status) != "" {
		q = q.Where(entTicket.StatusEQ(strings.TrimSpace(req.Status)))
	}
	if strings.TrimSpace(req.Priority) != "" {
		q = q.Where(entTicket.PriorityEQ(strings.TrimSpace(req.Priority)))
	}
	if req.AssigneeID > 0 {
		q = q.Where(entTicket.AssigneeIDEQ(req.AssigneeID))
	}
	if qq := strings.TrimSpace(req.Q); qq != "" {
		q = q.Where(entTicket.Or(
			entTicket.TicketNumberContainsFold(qq),
			entTicket.TitleContainsFold(qq),
		))
	}
	if req.UpdatedAfter != nil {
		q = q.Where(entTicket.UpdatedAtGTE(*req.UpdatedAfter))
	}
	return q
}

func applyWorkbenchCursor(q *ent.TicketQuery, sortKey string, p workbenchCursorPos) *ent.TicketQuery {
	if sortKey == "sla" {
		if p.LastSort == "" {
			// null 截止时间之后的行：仅继续 null 段（P0 简化；见方案 §5 说明）。
			return q.Where(entTicket.SLAResolutionDeadlineIsNil(), entTicket.IDGT(p.LastID))
		}
		ts, err := time.Parse(time.RFC3339Nano, p.LastSort)
		if err != nil {
			return q
		}
		return q.Where(entTicket.Or(
			entTicket.SLAResolutionDeadlineGT(ts),
			entTicket.And(entTicket.SLAResolutionDeadlineEQ(ts), entTicket.IDGT(p.LastID)),
			entTicket.SLAResolutionDeadlineIsNil(),
		))
	}
	ts, err := time.Parse(time.RFC3339Nano, p.LastSort)
	if err != nil {
		return q
	}
	return q.Where(entTicket.Or(
		entTicket.UpdatedAtLT(ts),
		entTicket.And(entTicket.UpdatedAtEQ(ts), entTicket.IDLT(p.LastID)),
	))
}

func workbenchOrderKey(t *ent.Ticket, sortKey string) time.Time {
	if sortKey == "sla" {
		return t.SLAResolutionDeadline
	}
	return t.UpdatedAt
}

// tenantActionPermissions 计算目标租户内工作台动作权限（RBAC 按目标租户的 DB 权限判定）。
func (s *MSPWorkbenchService) tenantActionPermissions(ctx context.Context, actor MSPWorkbenchActor, tenantID int, tenantActive bool) map[string]bool {
	out := map[string]bool{"reply": false, "status": false, "assign": false}
	if !tenantActive {
		return out
	}
	rbacRole := actor.MSPRole
	if !strings.HasPrefix(rbacRole, "msp_") {
		rbacRole = middleware.GetMSPRBACRole(rbacRole)
	}
	writable := middleware.HasResourcePermission(ctx, s.client, rbacRole, "msp_ticket", "write", tenantID)
	out["reply"] = writable
	out["status"] = writable
	out["assign"] = writable
	return out
}

func workbenchAllowedActions(t *ent.Ticket, tenantActive bool, perms map[string]bool) []dto.WorkbenchAllowedAction {
	terminal := t.Status == string(ticket.StatusResolved) || t.Status == string(ticket.StatusClosed) || t.Status == "cancelled"
	action := func(name string, resourceOK bool) dto.WorkbenchAllowedAction {
		if !tenantActive {
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeCustomerInactive, ReasonText: "客户租户已暂停或过期，仅可查看"}
		}
		if !perms[name] {
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeActionNotAllowed, ReasonText: "当前角色在该客户租户无对应权限"}
		}
		if terminal && name != "reply" {
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeActionNotAllowed, ReasonText: "工单处于终态，不支持该操作"}
		}
		return dto.WorkbenchAllowedAction{Action: name, Allowed: true}
	}
	return []dto.WorkbenchAllowedAction{
		action("reply", perms["reply"]),
		action("status", perms["status"]),
		action("assign", perms["assign"]),
	}
}

// Summary 每客户计数徽标（open / slaRisk 超期 / slaDueSoon 24h 内到期 / unassigned）。
func (s *MSPWorkbenchService) Summary(ctx context.Context, actor MSPWorkbenchActor) (*dto.WorkbenchSummaryResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	tenantIDs, err := s.normalizeTenantSet(nil, actor.AllowedCustomers)
	if err != nil {
		return nil, err
	}
	meta, err := s.loadTenantMeta(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]dto.WorkbenchSummaryCustomer, 0, len(tenantIDs))
	for _, tid := range tenantIDs {
		// IP-P2-2：同列表口径，按目标客户租户重绑定 ctx。
		qctx := tenantctx.WithTenantID(ctx, tid)
		base := s.client.Ticket.Query().Where(entTicket.TenantIDEQ(tid), openTicketPredicate())
		openCount, err := base.Clone().Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary open tenant %d: %w", tid, err)
		}
		slaRisk, err := s.client.Ticket.Query().
			Where(entTicket.TenantIDEQ(tid), openTicketPredicate(), entTicket.SLAResolutionDeadlineLT(now)).
			Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary sla tenant %d: %w", tid, err)
		}
		// IP-P2-4b：临近 SLA = (now, now+24h] 内到期且未关闭（不含已超期）。
		slaDueSoon, err := s.client.Ticket.Query().
			Where(
				entTicket.TenantIDEQ(tid),
				openTicketPredicate(),
				entTicket.SLAResolutionDeadlineGT(now),
				entTicket.SLAResolutionDeadlineLTE(now.Add(workbenchSLADueSoonWindow)),
			).
			Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary due-soon tenant %d: %w", tid, err)
		}
		// IP-P2-4c：用量口径（usage-only；硬配额上限无数据源）——
		// members = 该租户 active 且未删除的成员身份数（IP-P1-1 membership 单源）；
		// ticketsCreated30d = 近 30 天新建工单数。
		members, err := s.client.UserTenantMembership.Query().
			Where(
				usertenantmembership.TenantIDEQ(tid),
				usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
				usertenantmembership.DeletedAtIsNil(),
			).
			Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary members tenant %d: %w", tid, err)
		}
		ticketsCreated30d, err := s.client.Ticket.Query().
			Where(
				entTicket.TenantIDEQ(tid),
				entTicket.DeletedAtIsNil(),
				entTicket.CreatedAtGTE(now.AddDate(0, 0, -workbenchUsageWindowDays)),
			).
			Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary usage tenant %d: %w", tid, err)
		}
		unassigned, err := s.client.Ticket.Query().
			Where(entTicket.TenantIDEQ(tid), openTicketPredicate(), entTicket.AssigneeIDIsNil()).
			Count(qctx)
		if err != nil {
			return nil, fmt.Errorf("workbench summary unassigned tenant %d: %w", tid, err)
		}
		out = append(out, dto.WorkbenchSummaryCustomer{
			CustomerTenantID:  tid,
			CustomerName:      meta[tid].name,
			Open:              openCount,
			SLARisk:           slaRisk,
			SLADueSoon:        slaDueSoon,
			Members:           members,
			TicketsCreated30d: ticketsCreated30d,
			Unassigned:        unassigned,
		})
	}
	return &dto.WorkbenchSummaryResponse{
		GeneratedAt:           now,
		TTLSeconds:            workbenchSummaryTTL,
		SLADueSoonWindowHours: int(workbenchSLADueSoonWindow / time.Hour),
		UsageWindowDays:       workbenchUsageWindowDays,
		Customers:             out,
	}, nil
}

func openTicketPredicate() predicate.Ticket {
	return entTicket.StatusNotIn(
		string(ticket.StatusResolved),
		string(ticket.StatusClosed),
		"cancelled",
	)
}

// Reply 条目级回复：资源租户一致 → 分配 → 租户 active → 目标租户 RBAC → 写评论 + 审计。
func (s *MSPWorkbenchService) Reply(ctx context.Context, actor MSPWorkbenchActor, ticketID int, req dto.WorkbenchReplyRequest) (*dto.TicketCommentResponse, error) {
	t, err := s.authorizeTicketAction(ctx, actor, ticketID, req.CustomerTenantID)
	if err != nil {
		return nil, err
	}
	if s.commentSvc == nil {
		return nil, fmt.Errorf("ticket comment service unavailable")
	}
	// IP-P2-2：评论写路径按目标客户租户重绑定（enforce 下写侧 GUC 同源）。
	comment, err := s.commentSvc.CreateTicketComment(tenantctx.WithTenantID(ctx, t.TenantID), ticketID, &dto.CreateTicketCommentRequest{
		Content: req.Content,
	}, actor.UserID, t.TenantID)
	if err != nil {
		s.recordWorkbenchAudit(ctx, actor, "reply", t.TenantID, ticketID, "failed")
		return nil, err
	}
	s.recordWorkbenchAudit(ctx, actor, "reply", t.TenantID, ticketID, "success")
	return comment, nil
}

// ChangeStatus 条目级改状态：授权链 + 状态机（委托 TicketService.UpdateTicketStatus）。
func (s *MSPWorkbenchService) ChangeStatus(ctx context.Context, actor MSPWorkbenchActor, ticketID int, req dto.WorkbenchStatusRequest) (*ticket.Ticket, error) {
	t, err := s.authorizeTicketAction(ctx, actor, ticketID, req.CustomerTenantID)
	if err != nil {
		return nil, err
	}
	if s.ticketSvc == nil {
		return nil, fmt.Errorf("ticket service unavailable")
	}
	updated, err := s.ticketSvc.UpdateTicketStatus(tenantctx.WithTenantID(ctx, t.TenantID), ticketID, strings.TrimSpace(req.Status), t.TenantID, actor.UserID)
	if err != nil {
		s.recordWorkbenchAudit(ctx, actor, "status", t.TenantID, ticketID, "failed")
		return nil, err
	}
	s.recordWorkbenchAudit(ctx, actor, "status", t.TenantID, ticketID, "success")
	// A12 通知双投递：工作台条目级改状态触发状态通知（客户侧 requester/assignee + provider 侧）。
	s.ticketSvc.NotifyTicketStatusChanged(tenantctx.WithTenantID(ctx, t.TenantID), ticketID, t.Status, strings.TrimSpace(req.Status), t.TenantID)
	return updated, nil
}

// authorizeTicketAction 条目级授权链（§3.1）：资源租户一致性 → 分配 → 租户 active → 目标租户 RBAC → 资源状态。
func (s *MSPWorkbenchService) authorizeTicketAction(ctx context.Context, actor MSPWorkbenchActor, ticketID, declaredTenantID int) (*ent.Ticket, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	// IP-P2-2：以声明租户为 RLS GUC 读取（enforce 下跨租户探测 fail-closed → not-found）。
	t, err := s.client.Ticket.Get(tenantctx.WithTenantID(ctx, declaredTenantID), ticketID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "工单不存在")
		}
		return nil, err
	}
	if t.TenantID != declaredTenantID {
		return nil, NewCustomerAccessError(CodeResourceTenantMismatch, "工单租户与请求声明不一致")
	}
	if err := s.ensureAllocated(actor, t.TenantID); err != nil {
		return nil, err
	}
	meta, err := s.loadTenantMeta(ctx, []int{t.TenantID})
	if err != nil {
		return nil, err
	}
	if !meta[t.TenantID].active() {
		return nil, NewCustomerAccessError(CodeCustomerInactive, "客户租户已暂停或过期，仅可查看")
	}
	if t.Status == string(ticket.StatusResolved) || t.Status == string(ticket.StatusClosed) || t.Status == "cancelled" {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "工单处于终态，不支持该操作")
	}
	if !s.tenantActionPermissions(ctx, actor, t.TenantID, true)["reply"] {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "当前角色在该客户租户无工单写权限")
	}
	return t, nil
}

func (s *MSPWorkbenchService) ensureAllocated(actor MSPWorkbenchActor, tenantID int) error {
	for _, id := range actor.AllowedCustomers {
		if id == tenantID {
			return nil
		}
	}
	return NewCustomerAccessError(CodeMSPAllocationRequired, "目标客户未分配或不可访问")
}

// recordWorkbenchAudit 跨租户写逐条审计（事件 workbench.action；source/target_tenant_id/actor_account 落列）。
// ctx 必须携带 actor 家租户（enforce 下审计行 tenant_id = 当前租户作用域，fail-closed）。
func (s *MSPWorkbenchService) recordWorkbenchAudit(ctx context.Context, actor MSPWorkbenchActor, op string, tenantID, ticketID int, outcome string) {
	if s.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":               "workbench",
		"op":                   op,
		"actor_user_id":        actor.UserID,
		"actor_home_tenant_id": actor.HomeTenantID,
		"target_tenant_id":     tenantID,
		"ticket_id":            ticketID,
		"outcome":              outcome,
		"batch_id":             actor.BatchID,
	})
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = tenantID
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(tenantID).
		SetSource(middleware.AuditSourceWorkbench).
		SetResource("msp_ticket").
		SetAction("workbench.action").
		SetPath("/api/v1/msp/workbench/tickets").
		SetMethod("POST").
		SetStatusCode(map[bool]int{true: 200, false: 403}[outcome == "success"]).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("workbench audit write failed", "error", err, "op", op, "ticket_id", ticketID)
	}
}

// recordScopeDenied 显式请求未分配客户 → 审计 tenant.scope_denied（防枚举：逐租户记录，不泄露存在性）。
// ctx 必须携带 actor 家租户（enforce 下审计行 tenant_id = 当前租户作用域，fail-closed）。
func (s *MSPWorkbenchService) recordScopeDenied(ctx context.Context, actor MSPWorkbenchActor, requested []int) {
	if s.client == nil || len(requested) == 0 {
		return
	}
	allowed := make(map[int]bool, len(actor.AllowedCustomers))
	for _, id := range actor.AllowedCustomers {
		allowed[id] = true
	}
	for _, tid := range requested {
		if tid <= 0 || allowed[tid] {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"source":               "workbench",
			"op":                   "scope_denied",
			"actor_user_id":        actor.UserID,
			"actor_home_tenant_id": actor.HomeTenantID,
			"target_tenant_id":     tid,
		})
		auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := s.client.AuditLog.Create().
			SetCreatedAt(time.Now()).
			SetTenantID(actor.HomeTenantID).
			SetUserID(actor.UserID).
			SetActorAccount(actor.Username).
			SetTargetTenantID(tid).
			SetSource(middleware.AuditSourceWorkbench).
			SetResource("msp_customer").
			SetAction("tenant.scope_denied").
			SetPath("/api/v1/msp/workbench/tickets").
			SetMethod("GET").
			SetStatusCode(403).
			SetRequestBody(string(payload)).
			Save(auditCtx)
		cancel()
		if err != nil && s.logger != nil {
			s.logger.Warnw("workbench scope-denied audit write failed", "error", err, "target_tenant_id", tid)
		}
	}
}

// Batch 批量操作（IP-P1-6，WB-A4）：≤100 条、仅低危动作（reply/status/assign）、
// 逐条授权 + 逐条审计（batch_id 关联）、单租户速率护栏；返回逐条结果。
func (s *MSPWorkbenchService) Batch(ctx context.Context, actor MSPWorkbenchActor, req dto.WorkbenchBatchRequest) (*dto.WorkbenchBatchResponse, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if len(req.Items) == 0 || len(req.Items) > workbenchBatchMax {
		return nil, NewCustomerAccessError(CodeBatchLimitExceeded, "批量条目数需在 1-100 之间")
	}
	switch action {
	case "reply", "status", "assign":
	default:
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "仅支持低危动作：reply/status/assign")
	}
	if !s.batchAllowed(actor.HomeTenantID) {
		return nil, NewCustomerAccessError(CodeBatchRateLimited, "批量操作过于频繁，请稍后重试")
	}
	batchID := fmt.Sprintf("wb-%d-%d", time.Now().UnixNano(), actor.UserID)
	actor.BatchID = batchID
	resp := &dto.WorkbenchBatchResponse{
		BatchID: batchID,
		Results: make([]dto.WorkbenchBatchItemResult, 0, len(req.Items)),
	}
	for _, item := range req.Items {
		res := dto.WorkbenchBatchItemResult{
			TicketID:         item.TicketID,
			CustomerTenantID: item.CustomerTenantID,
		}
		var err error
		switch action {
		case "reply":
			_, err = s.Reply(ctx, actor, item.TicketID, dto.WorkbenchReplyRequest{
				CustomerTenantID: item.CustomerTenantID,
				Content:          req.Payload.Content,
			})
		case "status":
			_, err = s.ChangeStatus(ctx, actor, item.TicketID, dto.WorkbenchStatusRequest{
				CustomerTenantID: item.CustomerTenantID,
				Status:           req.Payload.Status,
			})
		case "assign":
			if !s.tenantActionPermissions(ctx, actor, item.CustomerTenantID, true)["assign"] {
				err = NewCustomerAccessError(CodeActionNotAllowed, "当前角色在该客户租户无指派权限")
			} else if s.ticketSvc == nil {
				err = fmt.Errorf("ticket service unavailable")
			} else if _, aerr := s.ticketSvc.AssignMSPTechnician(ctx, item.TicketID, item.CustomerTenantID, actor.UserID); aerr != nil {
				err = aerr
			} else {
				s.recordWorkbenchAudit(ctx, actor, "assign", item.CustomerTenantID, item.TicketID, "success")
			}
		}
		if err != nil {
			res.OK = false
			if ae, ok := AsCustomerAccessError(err); ok {
				res.ReasonCode = ae.Code
				res.Message = ae.Message
			} else {
				res.Message = "操作失败"
			}
			resp.Failed++
		} else {
			res.OK = true
			resp.Succeeded++
		}
		resp.Results = append(resp.Results, res)
	}
	return resp, nil
}

// batchAllowed 单租户批量速率护栏（滑动窗口；内存实现，实例级）。
func (s *MSPWorkbenchService) batchAllowed(homeTenantID int) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	if s.batchHits == nil {
		s.batchHits = make(map[int][]time.Time)
	}
	hits := s.batchHits[homeTenantID]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= workbenchBatchPerMin {
		s.batchHits[homeTenantID] = kept
		return false
	}
	s.batchHits[homeTenantID] = append(kept, now)
	return true
}
