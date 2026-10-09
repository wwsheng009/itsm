// msp_task_projection.go：跨租户任务投影（IP-P2-11；跨租户单据流转 §7.3 / §8.5）。
//
// 设计约束：
//  1. 任务行始终属客户租户（owner）；provider 只做投影（读）与受控写回（认领/完成），不复制任务行；
//  2. 投影 = Allocation ∩ 客户租户 RBAC（task:read/update，C2 基线）∩ 分配规则：
//     显式指派/候选给我 → assigned；保留标记 msp_provider（"服务商处理"节点）→ 待处理池（queue）；
//  3. 写回在客户租户 tenantctx 内执行（RLS GUC=客户 + BPMN 租户上下文），逐条审计 source=workbench；
//  4. 标记任务的 assignee 在任务创建时（bpmn executor，客户租户内）按 allocation
//     primary→backup→specialist 解析（resolveProviderTaskAssignee）；未解析到保持未分配（池兜底）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	entTenant "itsm-backend/ent/tenant"
	"itsm-backend/ent/processtask"
	"itsm-backend/middleware"
	"itsm-backend/service/bpmn"

	"go.uber.org/zap"
)

// MSPProviderTaskMarker 客户 BPMN 用户任务 candidateGroups 中的保留标记（"服务商处理"节点）。
// 命中时：任务创建按 allocation 解析 provider 员工为 assignee；未解析到则进入 provider 待办池。
const MSPProviderTaskMarker = "msp_provider"

const (
	mspTaskLimitDefault = 50
	mspTaskLimitMax     = 200
)

// ErrMSPTaskNotFound 任务不存在或不在可见范围（防枚举：与"无权访问"同语义）。
var ErrMSPTaskNotFound = errors.New("任务不存在或不在可见范围")

// MSPTaskProjectionService 跨租户任务投影服务。
type MSPTaskProjectionService struct {
	client *ent.Client
	engine taskCompletionEngine
	logger *zap.SugaredLogger
}

// taskCompletionEngine 完成任务所需的最小引擎能力（测试注入替身）。
type taskCompletionEngine interface {
	CompleteTask(ctx context.Context, taskID string, variables map[string]interface{}) error
}

// NewMSPTaskProjectionService 构造任务投影服务。
func NewMSPTaskProjectionService(client *ent.Client, logger *zap.SugaredLogger) *MSPTaskProjectionService {
	return &MSPTaskProjectionService{client: client, engine: NewCustomProcessEngine(client, logger), logger: logger}
}

// newMSPTaskProjectionServiceWithEngine 测试注入：替换完成引擎。
func newMSPTaskProjectionServiceWithEngine(client *ent.Client, engine taskCompletionEngine, logger *zap.SugaredLogger) *MSPTaskProjectionService {
	return &MSPTaskProjectionService{client: client, engine: engine, logger: logger}
}

type mspTaskTenantMeta struct {
	name   string
	status string
}

func (m mspTaskTenantMeta) active() bool { return m.status == "active" }

// ListTasks 跨客户"待我处理"任务投影（逐租户查询 + 内存合并，RLS 友好）。
func (s *MSPTaskProjectionService) ListTasks(ctx context.Context, actor MSPWorkbenchActor, req dto.MSPTaskQuery) (*dto.MSPTaskListResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp task projection")
	}
	tenantIDs, err := s.normalizeTaskTenantSet(req.CustomerTenantIDs, actor.AllowedCustomers)
	if err != nil {
		if ae, ok := AsCustomerAccessError(err); ok && ae.Code == CodeMSPAllocationRequired {
			s.recordScopeDenied(ctx, actor, req.CustomerTenantIDs, "GET")
		}
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = mspTaskLimitDefault
	}
	if limit > mspTaskLimitMax {
		limit = mspTaskLimitMax
	}
	meta, err := s.loadTaskTenantMeta(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	uidStr := strconv.Itoa(actor.UserID)
	items := make([]dto.MSPTaskItem, 0)
	for _, tid := range tenantIDs {
		m := meta[tid]
		if m.status == "" {
			continue // 未知租户（允许集合来自 allocation，正常不出现）
		}
		qctx := tenantctx.WithTenantID(ctx, tid)
		rows, err := s.client.ProcessTask.Query().
			Where(
				processtask.TenantIDEQ(tid),
				processtask.StatusIn("created", "assigned", "started"),
			).
			Order(ent.Desc(processtask.FieldCreatedTime), ent.Desc(processtask.FieldID)).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("task projection query tenant %d: %w", tid, err)
		}
		canWrite := m.active() && s.canUpdateTask(qctx, actor, tid)
		for _, t := range rows {
			source, visible := mspTaskVisibility(t, uidStr)
			if !visible {
				continue
			}
			items = append(items, mspTaskItem(t, tid, m, source, canWrite, actor.UserID))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedTime.Equal(items[j].CreatedTime) {
			return items[i].TaskID > items[j].TaskID
		}
		return items[i].CreatedTime.After(items[j].CreatedTime)
	})
	total := len(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return &dto.MSPTaskListResponse{Items: items, Total: total}, nil
}

// ClaimTask 认领任务（客户租户内写回；条件更新防并发重复认领）。
func (s *MSPTaskProjectionService) ClaimTask(ctx context.Context, actor MSPWorkbenchActor, taskID string, declaredTenantID int) (*dto.MSPTaskItem, error) {
	t, tid, err := s.authorizeTaskAction(ctx, actor, taskID, declaredTenantID)
	if err != nil {
		return nil, err
	}
	uidStr := strconv.Itoa(actor.UserID)
	if _, visible := mspTaskVisibility(t, uidStr); !visible {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "任务未分配给你且不在服务商待处理池")
	}
	if t.Status == "completed" || t.Status == "cancelled" {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "任务已结束")
	}
	qctx := tenantctx.WithTenantID(ctx, tid)
	if t.Assignee != uidStr {
		if t.Assignee != "" {
			return nil, NewCustomerAccessError(CodeActionNotAllowed, "任务已被他人认领")
		}
		now := time.Now()
		affected, err := s.client.ProcessTask.Update().
			Where(
				processtask.ID(t.ID),
				processtask.StatusIn("created", "assigned", "started"),
				// Optional 字段未设置时落 NULL：空串与 NULL 都要匹配（否则条件更新 0 行）。
				processtask.Or(processtask.AssigneeIsNil(), processtask.AssigneeEQ("")),
			).
			SetAssignee(uidStr).
			SetStatus("assigned").
			SetAssignedTime(now).
			SetUpdatedAt(now).
			Save(qctx)
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			return nil, NewCustomerAccessError(CodeActionNotAllowed, "任务已被他人认领")
		}
	}
	s.recordTaskAudit(ctx, actor, "task.claim", tid, taskID, "success", "POST")

	refreshed, err := s.client.ProcessTask.Query().Where(processtask.ID(t.ID)).Only(qctx)
	if err != nil {
		return nil, err
	}
	meta, err := s.loadTaskTenantMeta(ctx, []int{tid})
	if err != nil {
		return nil, err
	}
	canWrite := meta[tid].active() && s.canUpdateTask(qctx, actor, tid)
	item := mspTaskItem(refreshed, tid, meta[tid], "assigned", canWrite, actor.UserID)
	return &item, nil
}

// CompleteTask 完成任务并推进客户流程（写回客户租户 tenantctx）。
func (s *MSPTaskProjectionService) CompleteTask(ctx context.Context, actor MSPWorkbenchActor, taskID string, declaredTenantID int, variables map[string]interface{}) error {
	t, tid, err := s.authorizeTaskAction(ctx, actor, taskID, declaredTenantID)
	if err != nil {
		return err
	}
	if t.Assignee != strconv.Itoa(actor.UserID) {
		return NewCustomerAccessError(CodeActionNotAllowed, "仅任务负责人可完成（可先认领）")
	}
	if t.Status == "completed" || t.Status == "cancelled" {
		return NewCustomerAccessError(CodeActionNotAllowed, "任务已结束")
	}
	if s.engine == nil {
		return fmt.Errorf("bpmn engine not available for msp task projection")
	}
	bctx := context.WithValue(tenantctx.WithTenantID(ctx, tid), bpmn.BPMNTenantIDContextKey, tid)
	bctx = context.WithValue(bctx, bpmn.BPMNUserIDContextKey, actor.UserID)
	if err := s.engine.CompleteTask(bctx, taskID, variables); err != nil {
		s.recordTaskAudit(ctx, actor, "task.complete", tid, taskID, "failed", "POST")
		return err
	}
	s.recordTaskAudit(ctx, actor, "task.complete", tid, taskID, "success", "POST")
	return nil
}

// authorizeTaskAction 条目级授权链（§3.1）：允许集合（allocation）→ 任务存在（防枚举 404）
// → 客户租户 active → 客户租户 RBAC task:update（C2 基线）。
func (s *MSPTaskProjectionService) authorizeTaskAction(ctx context.Context, actor MSPWorkbenchActor, taskID string, declaredTenantID int) (*ent.ProcessTask, int, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, 0, ErrMSPTaskNotFound
	}
	candidates := actor.AllowedCustomers
	if declaredTenantID > 0 {
		if !containsInt(candidates, declaredTenantID) {
			s.recordScopeDenied(ctx, actor, []int{declaredTenantID}, "POST")
			return nil, 0, NewCustomerAccessError(CodeMSPAllocationRequired, "未分配到客户租户 %d", declaredTenantID)
		}
		candidates = []int{declaredTenantID}
	}
	for _, tid := range candidates {
		qctx := tenantctx.WithTenantID(ctx, tid)
		t, err := s.client.ProcessTask.Query().
			Where(processtask.TenantIDEQ(tid), processtask.TaskIDEQ(taskID)).
			Only(qctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return nil, 0, err
		}
		meta, err := s.loadTaskTenantMeta(qctx, []int{tid})
		if err != nil {
			return nil, 0, err
		}
		if !meta[tid].active() {
			return nil, 0, NewCustomerAccessError(CodeCustomerInactive, "客户租户已暂停或过期，仅可查看")
		}
		if !s.canUpdateTask(qctx, actor, tid) {
			return nil, 0, NewCustomerAccessError(CodeActionNotAllowed, "当前角色在该客户租户无任务处理权限")
		}
		return t, tid, nil
	}
	return nil, 0, ErrMSPTaskNotFound
}

func (s *MSPTaskProjectionService) canUpdateTask(ctx context.Context, actor MSPWorkbenchActor, tenantID int) bool {
	rbacRole := actor.MSPRole
	if !strings.HasPrefix(rbacRole, "msp_") {
		rbacRole = middleware.GetMSPRBACRole(rbacRole)
	}
	return middleware.HasResourcePermission(ctx, s.client, rbacRole, "task", "update", tenantID)
}

func (s *MSPTaskProjectionService) normalizeTaskTenantSet(requested, allowed []int) ([]int, error) {
	allowedSet := make(map[int]bool, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = true
	}
	if len(requested) == 0 {
		out := append([]int(nil), allowed...)
		sort.Ints(out)
		return out, nil
	}
	seen := make(map[int]bool, len(requested))
	out := make([]int, 0, len(requested))
	for _, id := range requested {
		if !allowedSet[id] {
			return nil, NewCustomerAccessError(CodeMSPAllocationRequired, "未分配到客户租户 %d", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}

func (s *MSPTaskProjectionService) loadTaskTenantMeta(ctx context.Context, ids []int) (map[int]mspTaskTenantMeta, error) {
	out := make(map[int]mspTaskTenantMeta, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.client.Tenant.Query().Where(entTenant.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task projection load tenants: %w", err)
	}
	for _, t := range rows {
		out[t.ID] = mspTaskTenantMeta{name: t.Name, status: t.Status}
	}
	return out, nil
}

// mspTaskVisibility 分配规则判定：显式（assignee/候选用户=我）或待处理池（保留标记 + 已分配客户）。
func mspTaskVisibility(t *ent.ProcessTask, uidStr string) (string, bool) {
	if t.Assignee == uidStr {
		return "assigned", true
	}
	if csvTokenContains(t.CandidateUsers, uidStr) {
		return "assigned", true
	}
	if csvTokenContains(t.CandidateGroups, MSPProviderTaskMarker) {
		return "queue", true
	}
	return "", false
}

func mspTaskItem(t *ent.ProcessTask, tenantID int, m mspTaskTenantMeta, source string, canWrite bool, uid int) dto.MSPTaskItem {
	uidStr := strconv.Itoa(uid)
	open := t.Status != "completed" && t.Status != "cancelled"
	claimBase := open && t.Assignee == ""
	claimReason := ""
	if open && t.Assignee == uidStr {
		claimReason = "任务已由你认领"
	} else if open && t.Assignee != "" {
		claimReason = "任务已被他人认领"
	}
	completeBase := open && t.Assignee == uidStr
	completeReason := "仅任务负责人可完成（可先认领）"

	action := func(name string, base bool, reason string) dto.WorkbenchAllowedAction {
		if !m.active() {
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeCustomerInactive, ReasonText: "客户租户已暂停或过期，仅可查看"}
		}
		if !canWrite {
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeActionNotAllowed, ReasonText: "当前角色在该客户租户无任务处理权限"}
		}
		if !base {
			text := reason
			if text == "" {
				text = "任务已结束，不支持该操作"
			}
			return dto.WorkbenchAllowedAction{Action: name, Allowed: false, ReasonCode: CodeActionNotAllowed, ReasonText: text}
		}
		return dto.WorkbenchAllowedAction{Action: name, Allowed: true}
	}

	var due *time.Time
	if !t.DueDate.IsZero() {
		d := t.DueDate
		due = &d
	}
	return dto.MSPTaskItem{
		TaskID:               t.TaskID,
		TaskName:             t.TaskName,
		TaskDefinitionKey:    t.TaskDefinitionKey,
		ProcessDefinitionKey: t.ProcessDefinitionKey,
		ProcessInstanceID:    t.ProcessInstanceID,
		Status:               t.Status,
		Priority:             t.Priority,
		Assignee:             t.Assignee,
		DueDate:              due,
		CreatedTime:          t.CreatedTime,
		CustomerTenantID:     tenantID,
		CustomerTenantName:   m.name,
		Source:               source,
		AllowedActions: []dto.WorkbenchAllowedAction{
			action("claim", claimBase, claimReason),
			action("complete", completeBase, completeReason),
		},
	}
}

// recordTaskAudit 跨租户任务写逐条审计（事件 workbench.action；source/target_tenant_id/actor 落列）。
// ctx 必须携带 actor 家租户（enforce 下审计行 tenant_id = 当前租户作用域，fail-closed）。
func (s *MSPTaskProjectionService) recordTaskAudit(ctx context.Context, actor MSPWorkbenchActor, op string, tenantID int, taskID, outcome, method string) {
	if s.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":               "workbench",
		"op":                   op,
		"actor_user_id":        actor.UserID,
		"actor_home_tenant_id": actor.HomeTenantID,
		"target_tenant_id":     tenantID,
		"task_id":              taskID,
		"outcome":              outcome,
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
		SetResource("task").
		SetAction("workbench.action").
		SetPath("/api/v1/msp/workbench/tasks").
		SetMethod(method).
		SetStatusCode(map[bool]int{true: 200, false: 403}[outcome == "success"]).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("task projection audit write failed", "error", err, "op", op, "task_id", taskID)
	}
}

// recordScopeDenied 显式请求未分配客户 → 审计 tenant.scope_denied（防枚举：逐租户记录）。
func (s *MSPTaskProjectionService) recordScopeDenied(ctx context.Context, actor MSPWorkbenchActor, requested []int, method string) {
	if s.client == nil || len(requested) == 0 {
		return
	}
	allowed := make(map[int]bool, len(actor.AllowedCustomers))
	for _, id := range actor.AllowedCustomers {
		allowed[id] = true
	}
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = requested[0]
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, id := range requested {
		if allowed[id] {
			continue
		}
		if _, err := s.client.AuditLog.Create().
			SetCreatedAt(time.Now()).
			SetTenantID(rowTenant).
			SetUserID(actor.UserID).
			SetActorAccount(actor.Username).
			SetTargetTenantID(id).
			SetSource(middleware.AuditSourceWorkbench).
			SetResource("task").
			SetAction("tenant.scope_denied").
			SetPath("/api/v1/msp/workbench/tasks").
			SetMethod(method).
			SetStatusCode(403).
			Save(auditCtx); err != nil && s.logger != nil {
			s.logger.Warnw("task projection scope-denied audit write failed", "error", err, "target_tenant_id", id)
		}
	}
}

// resolveProviderTaskAssignee 在客户租户内解析"服务商处理"任务的具体负责人：
// 读取客户归属 provider（tenants.msp_provider_id），按 allocation role 优先级
// （primary → backup → specialist → 其他）取第一个有效分配的员工。
// 只读跨租户登记表（msp_allocations 行归属=客户租户，RLS 同域），运行时零跨租户定义引用。
func resolveProviderTaskAssignee(ctx context.Context, client *ent.Client, customerTenantID int) (string, int, string, bool) {
	if client == nil || customerTenantID <= 0 {
		return "", 0, "", false
	}
	t, err := client.Tenant.Get(ctx, customerTenantID)
	if err != nil || t.MspProviderID <= 0 {
		return "", 0, "", false
	}
	rows, err := client.MSPAllocation.Query().
		Where(
			mspallocation.CustomerTenantIDEQ(customerTenantID),
			mspallocation.ProviderTenantIDEQ(t.MspProviderID),
			mspallocation.DeassignedAtIsNil(),
		).
		Order(ent.Asc(mspallocation.FieldID)).
		All(ctx)
	if err != nil || len(rows) == 0 {
		return "", 0, "", false
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return mspAllocationRoleRank(rows[i].Role) < mspAllocationRoleRank(rows[j].Role)
	})
	best := rows[0]
	return strconv.Itoa(best.MspUserID), t.MspProviderID, best.Role, true
}

func mspAllocationRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "primary":
		return 0
	case "backup":
		return 1
	case "specialist":
		return 2
	default:
		return 3
	}
}

// csvTokenContains 逗号分隔词表包含判定（大小写/空白容忍）。
func csvTokenContains(csv, token string) bool {
	for _, v := range strings.Split(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(v), token) {
			return true
		}
	}
	return false
}

// csvTokenRemove 从逗号分隔词表移除指定词（保留其余顺序与内容）。
func csvTokenRemove(csv, token string) string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, v := range parts {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || strings.EqualFold(trimmed, token) {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, ",")
}

func containsInt(ids []int, target int) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
