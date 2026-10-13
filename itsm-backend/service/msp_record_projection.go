// msp_record_projection.go：变更/问题/服务请求接入工作台（读面；跨租户单据流转 P2-③ / IP-P2-12）。
//
// 设计约束（方案 §5 操作矩阵 + XF10 接入五件套；与工单读面同口径）：
//  1. 允许集合一律来自服务端（actor.AllowedCustomers）；customerTenantIds 仅作筛选，越界即 403 + 审计；
//  2. 逐租户查询 + 内存合并（RLS 友好）：按目标客户租户重绑定 ctx，updated_at 降序 + 逐租户游标；
//  3. 条目返回 allowedActions[]（本期读面 = view；条目级写面见 IP-P2-13 登记）；
//  4. 详情：声明租户一致性（400 RESOURCE_TENANT_MISMATCH）→ allocation（403）→ 名称解析；
//  5. 暂停/过期客户仍可读（只读，镜像工单读面）；服务请求软删行不投影。
package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	entChange "itsm-backend/ent/change"
	entProblem "itsm-backend/ent/problem"
	entSR "itsm-backend/ent/servicerequest"
	"itsm-backend/ent/user"
)

// 记录类型（与路由资源一一对应）。
const (
	MSPRecordChange         = "change"
	MSPRecordProblem        = "problem"
	MSPRecordServiceRequest = "service_request"
)

func isMSPRecordType(t string) bool {
	switch t {
	case MSPRecordChange, MSPRecordProblem, MSPRecordServiceRequest:
		return true
	default:
		return false
	}
}

// mspRecordRow 三类单据的公共投影行。
type mspRecordRow struct {
	tenantID    int
	id          int
	number      string
	title       string
	status      string
	priority    string
	ownerID     int
	assigneeID  int
	requesterID int
	createdAt   time.Time
	updatedAt   time.Time
}

// mspRecordViewActions 读面动作契约（XF5）：本期仅 view；条目级写面（状态迁移/回复/审批）随 IP-P2-13。
func mspRecordViewActions() []dto.WorkbenchAllowedAction {
	return []dto.WorkbenchAllowedAction{{Action: "view", Allowed: true}}
}

// ListRecords 跨租户聚合列表（变更/问题/服务请求）。
func (s *MSPWorkbenchService) ListRecords(ctx context.Context, actor MSPWorkbenchActor, recordType string, req dto.MSPRecordQuery) (*dto.MSPRecordListResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	if !isMSPRecordType(recordType) {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "不支持的单据类型: %s", recordType)
	}
	tenantIDs, err := s.normalizeTenantSet(req.CustomerTenantIDs, actor.AllowedCustomers)
	if err != nil {
		if ae, ok := AsCustomerAccessError(err); ok && ae.Code == CodeMSPAllocationRequired {
			s.recordScopeDenied(ctx, actor, req.CustomerTenantIDs)
		}
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = workbenchLimitDefault
	}
	if limit > workbenchLimitMax {
		limit = workbenchLimitMax
	}
	cur, err := decodeWorkbenchCursor(req.Cursor, "updated")
	if err != nil {
		return nil, err
	}
	posByTenant := make(map[int]workbenchCursorPos, len(cur.PerTenant))
	for _, p := range cur.PerTenant {
		posByTenant[p.TenantID] = p
	}

	meta, err := s.loadTenantMeta(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	nameIDs := append([]int(nil), tenantIDs...)
	if actor.HomeTenantID > 0 {
		nameIDs = append(nameIDs, actor.HomeTenantID)
	}
	names, err := s.loadAssigneeNames(ctx, nameIDs)
	if err != nil {
		return nil, err
	}

	merged := make([]mspRecordRow, 0, limit+1)
	moreByTenant := make(map[int]bool, len(tenantIDs))
	total := 0
	for _, tid := range tenantIDs {
		var pos *workbenchCursorPos
		if p, ok := posByTenant[tid]; ok {
			pos = &p
		}
		rows, n, more, err := s.listRecordRows(ctx, recordType, tid, req, pos, limit)
		if err != nil {
			return nil, err
		}
		total += n
		if more {
			moreByTenant[tid] = true
		}
		merged = append(merged, rows...)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].updatedAt.Equal(merged[j].updatedAt) {
			return merged[i].id > merged[j].id
		}
		return merged[i].updatedAt.After(merged[j].updatedAt)
	})
	trimmed := len(merged) > limit
	if trimmed {
		merged = merged[:limit]
	}

	items := make([]dto.MSPRecordItem, 0, len(merged))
	lastReturned := make(map[int]workbenchCursorPos)
	// 写面动作（P2-③：问题 IP-P2-13 / 变更·服务请求 IP-P2-15）：家租户角色 + 逐租户写门禁各计算一次。
	homeRole := ""
	writeByTenant := map[int]bool{}
	if resource, ok := mspRecordWriteResource[recordType]; ok {
		if role, roleErr := s.loadActorHomeRole(ctx, actor); roleErr == nil {
			homeRole = role
		} else if s.logger != nil {
			s.logger.Warnw("msp records load actor role failed", "error", roleErr, "user_id", actor.UserID)
		}
		for _, tid := range tenantIDs {
			writeByTenant[tid] = s.recordTenantWriteAllowed(ctx, actor, tid, resource)
		}
	}
	for _, row := range merged {
		item := dto.MSPRecordItem{
			RecordType:       recordType,
			ID:               row.id,
			CustomerTenantID: row.tenantID,
			CustomerName:     meta[row.tenantID].name,
			Number:           row.number,
			Title:            row.title,
			Status:           row.status,
			Priority:         row.priority,
			UpdatedAt:        row.updatedAt,
			AllowedActions:   mspRecordViewActions(),
		}
		switch recordType {
		case MSPRecordProblem:
			item.AllowedActions = problemAllowedActions(actor, meta[row.tenantID].active(), writeByTenant[row.tenantID], homeRole, row)
		case MSPRecordChange:
			item.AllowedActions = changeAllowedActions(actor, meta[row.tenantID].active(), writeByTenant[row.tenantID], homeRole, row)
		case MSPRecordServiceRequest:
			item.AllowedActions = requestAllowedActions(actor, meta[row.tenantID].active(), writeByTenant[row.tenantID], homeRole, row)
		}
		if row.assigneeID > 0 {
			item.AssigneeID = row.assigneeID
			item.AssigneeName = names[row.assigneeID]
		}
		if row.requesterID > 0 {
			item.RequesterID = row.requesterID
			item.RequesterName = names[row.requesterID]
		}
		items = append(items, item)
		lastReturned[row.tenantID] = workbenchCursorPos{
			TenantID: row.tenantID,
			LastSort: row.updatedAt.Format(time.RFC3339Nano),
			LastID:   row.id,
		}
	}

	next := &workbenchCursor{V: 1, Sort: "updated"}
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

	return &dto.MSPRecordListResponse{Items: items, NextCursor: nextCursor, Total: total}, nil
}

// listRecordRows 单租户查询（updated_at 降序 + 游标 + limit+1 判更多）。
// 返回 (rows, total, more, err)：total 为当前筛选（status/q，不含游标）下的全量条数，
// 供列表响应 Total 使用（2026-10-13 契约修复：旧实现 Total=len(Items)）。
func (s *MSPWorkbenchService) listRecordRows(ctx context.Context, recordType string, tenantID int, req dto.MSPRecordQuery, pos *workbenchCursorPos, limit int) ([]mspRecordRow, int, bool, error) {
	qctx := tenantctx.WithTenantID(ctx, tenantID)
	q := strings.TrimSpace(req.Q)
	status := strings.TrimSpace(req.Status)

	var ts time.Time
	hasCursor := false
	if pos != nil && pos.LastID > 0 && pos.LastSort != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, pos.LastSort); err == nil {
			ts, hasCursor = parsed, true
		}
	}

	switch recordType {
	case MSPRecordChange:
		query := s.client.Change.Query().Where(entChange.TenantIDEQ(tenantID))
		if status != "" {
			query = query.Where(entChange.StatusEQ(status))
		}
		if q != "" {
			query = query.Where(entChange.Or(entChange.ChangeNumberContainsFold(q), entChange.TitleContainsFold(q)))
		}
		n, err := query.Clone().Count(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records count change tenant %d: %w", tenantID, err)
		}
		if hasCursor {
			query = query.Where(entChange.Or(
				entChange.UpdatedAtLT(ts),
				entChange.And(entChange.UpdatedAtEQ(ts), entChange.IDLT(pos.LastID)),
			))
		}
		rows, err := query.Order(ent.Desc(entChange.FieldUpdatedAt), ent.Desc(entChange.FieldID)).Limit(limit + 1).All(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records query change tenant %d: %w", tenantID, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		out := make([]mspRecordRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, mspRecordRow{
				tenantID: tenantID, id: r.ID, number: r.ChangeNumber, title: r.Title,
				status: r.Status, priority: r.Priority, ownerID: r.CreatedBy, assigneeID: r.AssigneeID,
				createdAt: r.CreatedAt, updatedAt: r.UpdatedAt,
			})
		}
		return out, n, more, nil

	case MSPRecordProblem:
		query := s.client.Problem.Query().Where(entProblem.TenantIDEQ(tenantID), entProblem.DeletedAtIsNil())
		if status != "" {
			query = query.Where(entProblem.StatusEQ(status))
		}
		if q != "" {
			query = query.Where(entProblem.Or(entProblem.ProblemNumberContainsFold(q), entProblem.TitleContainsFold(q)))
		}
		n, err := query.Clone().Count(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records count problem tenant %d: %w", tenantID, err)
		}
		if hasCursor {
			query = query.Where(entProblem.Or(
				entProblem.UpdatedAtLT(ts),
				entProblem.And(entProblem.UpdatedAtEQ(ts), entProblem.IDLT(pos.LastID)),
			))
		}
		rows, err := query.Order(ent.Desc(entProblem.FieldUpdatedAt), ent.Desc(entProblem.FieldID)).Limit(limit + 1).All(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records query problem tenant %d: %w", tenantID, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		out := make([]mspRecordRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, mspRecordRow{
				tenantID: tenantID, id: r.ID, number: r.ProblemNumber, title: r.Title,
				status: r.Status, priority: r.Priority, ownerID: r.CreatedBy, assigneeID: r.AssigneeID,
				createdAt: r.CreatedAt, updatedAt: r.UpdatedAt,
			})
		}
		return out, n, more, nil

	case MSPRecordServiceRequest:
		query := s.client.ServiceRequest.Query().Where(entSR.TenantIDEQ(tenantID), entSR.DeletedAtIsNil())
		if status != "" {
			query = query.Where(entSR.StatusEQ(status))
		}
		if q != "" {
			query = query.Where(entSR.TitleContainsFold(q))
		}
		n, err := query.Clone().Count(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records count service_request tenant %d: %w", tenantID, err)
		}
		if hasCursor {
			query = query.Where(entSR.Or(
				entSR.UpdatedAtLT(ts),
				entSR.And(entSR.UpdatedAtEQ(ts), entSR.IDLT(pos.LastID)),
			))
		}
		rows, err := query.Order(ent.Desc(entSR.FieldUpdatedAt), ent.Desc(entSR.FieldID)).Limit(limit + 1).All(qctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("msp records query service_request tenant %d: %w", tenantID, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		out := make([]mspRecordRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, mspRecordRow{
				tenantID: tenantID, id: r.ID, number: dto.GenerateServiceRequestNumber(r.ID, r.CreatedAt), title: r.Title,
				status: r.Status, assigneeID: r.ProcessorID, requesterID: r.RequesterID,
				createdAt: r.CreatedAt, updatedAt: r.UpdatedAt,
			})
		}
		return out, n, more, nil

	default:
		return nil, 0, false, NewCustomerAccessError(CodeActionNotAllowed, "不支持的单据类型: %s", recordType)
	}
}

// GetRecord 跨租户单条详情（只读；声明租户一致性 → allocation → 名称解析）。
func (s *MSPWorkbenchService) GetRecord(ctx context.Context, actor MSPWorkbenchActor, recordType string, id, declaredTenantID int) (*dto.MSPRecordDetailResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	if !isMSPRecordType(recordType) {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "不支持的单据类型: %s", recordType)
	}
	if declaredTenantID <= 0 {
		return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "缺少目标客户租户声明")
	}

	var resp *dto.MSPRecordDetailResponse
	ownerID := 0
	switch recordType {
	case MSPRecordChange:
		row, err := s.client.Change.Get(tenantctx.WithTenantID(ctx, declaredTenantID), id)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "变更不存在")
			}
			return nil, err
		}
		if row.TenantID != declaredTenantID {
			return nil, NewCustomerAccessError(CodeResourceTenantMismatch, "变更租户与请求声明不一致")
		}
		ownerID = row.CreatedBy
		resp = changeRecordDetail(row)

	case MSPRecordProblem:
		row, err := s.client.Problem.Get(tenantctx.WithTenantID(ctx, declaredTenantID), id)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "问题不存在")
			}
			return nil, err
		}
		if row.TenantID != declaredTenantID {
			return nil, NewCustomerAccessError(CodeResourceTenantMismatch, "问题租户与请求声明不一致")
		}
		if row.DeletedAt != nil {
			return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "问题不存在")
		}
		ownerID = row.CreatedBy
		resp = problemRecordDetail(row)

	case MSPRecordServiceRequest:
		row, err := s.client.ServiceRequest.Get(tenantctx.WithTenantID(ctx, declaredTenantID), id)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "服务请求不存在")
			}
			return nil, err
		}
		if row.TenantID != declaredTenantID {
			return nil, NewCustomerAccessError(CodeResourceTenantMismatch, "服务请求租户与请求声明不一致")
		}
		if row.DeletedAt != nil {
			return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "服务请求不存在")
		}
		resp = serviceRequestRecordDetail(row)
	}

	if err := s.ensureAllocated(actor, resp.CustomerTenantID); err != nil {
		return nil, err
	}
	meta, err := s.loadTenantMeta(ctx, []int{resp.CustomerTenantID})
	if err != nil {
		return nil, err
	}
	resp.CustomerName = meta[resp.CustomerTenantID].name
	names, err := s.recordUserNameMap(ctx, actor, resp.CustomerTenantID)
	if err != nil {
		return nil, err
	}
	resp.AssigneeName = names[resp.AssigneeID]
	resp.RequesterName = names[resp.RequesterID]
	resp.AllowedActions = mspRecordViewActions()
	if resource, ok := mspRecordWriteResource[recordType]; ok {
		homeRole := ""
		if role, roleErr := s.loadActorHomeRole(ctx, actor); roleErr == nil {
			homeRole = role
		}
		writeAllowed := s.recordTenantWriteAllowed(ctx, actor, resp.CustomerTenantID, resource)
		row := mspRecordRow{status: resp.Status, ownerID: ownerID, assigneeID: resp.AssigneeID}
		switch recordType {
		case MSPRecordProblem:
			resp.AllowedActions = problemAllowedActions(actor, meta[resp.CustomerTenantID].active(), writeAllowed, homeRole, row)
		case MSPRecordChange:
			resp.AllowedActions = changeAllowedActions(actor, meta[resp.CustomerTenantID].active(), writeAllowed, homeRole, row)
		case MSPRecordServiceRequest:
			resp.AllowedActions = requestAllowedActions(actor, meta[resp.CustomerTenantID].active(), writeAllowed, homeRole, row)
		}
	}
	return resp, nil
}

// recordUserNameMap 详情名称解析：记录租户 + provider 家租户各自作用域读取（enforce 下 fail-closed）。
func (s *MSPWorkbenchService) recordUserNameMap(ctx context.Context, actor MSPWorkbenchActor, tenantID int) (map[int]string, error) {
	names := make(map[int]string)
	scopes := []int{tenantID}
	if actor.HomeTenantID > 0 && actor.HomeTenantID != tenantID {
		scopes = append(scopes, actor.HomeTenantID)
	}
	for _, scope := range scopes {
		rows, err := s.client.User.Query().
			Where(user.TenantIDEQ(scope)).
			All(tenantctx.WithTenantID(ctx, scope))
		if err != nil {
			return nil, fmt.Errorf("msp records load users tenant %d: %w", scope, err)
		}
		for _, u := range rows {
			names[u.ID] = u.Name
		}
	}
	return names, nil
}

func changeRecordDetail(row *ent.Change) *dto.MSPRecordDetailResponse {
	resp := &dto.MSPRecordDetailResponse{
		MSPRecordItem: dto.MSPRecordItem{
			RecordType:       MSPRecordChange,
			ID:               row.ID,
			CustomerTenantID: row.TenantID,
			Number:           row.ChangeNumber,
			Title:            row.Title,
			Status:           row.Status,
			Priority:         row.Priority,
			AssigneeID:       row.AssigneeID,
			UpdatedAt:        row.UpdatedAt,
		},
		Description:   row.Description,
		CreatedAt:     row.CreatedAt,
		Type:          row.Type,
		RiskLevel:     row.RiskLevel,
		ImpactScope:   row.ImpactScope,
		Justification: row.Justification,
	}
	if !row.PlannedStartDate.IsZero() {
		v := row.PlannedStartDate
		resp.PlannedStartDate = &v
	}
	if !row.PlannedEndDate.IsZero() {
		v := row.PlannedEndDate
		resp.PlannedEndDate = &v
	}
	return resp
}

func problemRecordDetail(row *ent.Problem) *dto.MSPRecordDetailResponse {
	resp := &dto.MSPRecordDetailResponse{
		MSPRecordItem: dto.MSPRecordItem{
			RecordType:       MSPRecordProblem,
			ID:               row.ID,
			CustomerTenantID: row.TenantID,
			Number:           row.ProblemNumber,
			Title:            row.Title,
			Status:           row.Status,
			Priority:         row.Priority,
			AssigneeID:       row.AssigneeID,
			UpdatedAt:        row.UpdatedAt,
		},
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		Category:    row.Category,
		RootCause:   row.RootCause,
		Workaround:  row.Workaround,
		Resolution:  row.Resolution,
		Impact:      row.Impact,
	}
	resp.ResolvedAt = row.ResolvedAt
	resp.ClosedAt = row.ClosedAt
	return resp
}

func serviceRequestRecordDetail(row *ent.ServiceRequest) *dto.MSPRecordDetailResponse {
	resp := &dto.MSPRecordDetailResponse{
		MSPRecordItem: dto.MSPRecordItem{
			RecordType:       MSPRecordServiceRequest,
			ID:               row.ID,
			CustomerTenantID: row.TenantID,
			Number:           dto.GenerateServiceRequestNumber(row.ID, row.CreatedAt),
			Title:            row.Title,
			Status:           row.Status,
			AssigneeID:       row.ProcessorID,
			RequesterID:      row.RequesterID,
			UpdatedAt:        row.UpdatedAt,
		},
		Description:     row.Reason,
		CreatedAt:       row.CreatedAt,
		CatalogID:       row.CatalogID,
		CurrentLevel:    row.CurrentLevel,
		TotalLevels:     row.TotalLevels,
		CurrentApprover: row.CurrentApprover,
		ApproverComment: row.ApproverComment,
		CompletionNote:  row.CompletionNote,
	}
	if !row.ApprovedAt.IsZero() {
		v := row.ApprovedAt
		resp.ApprovedAt = &v
	}
	if !row.StartedAt.IsZero() {
		v := row.StartedAt
		resp.StartedAt = &v
	}
	if !row.CompletedAt.IsZero() {
		v := row.CompletedAt
		resp.CompletedAt = &v
	}
	return resp
}
