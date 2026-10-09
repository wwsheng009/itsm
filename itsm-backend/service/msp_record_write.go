// msp_record_write.go：跨租户单据写面（P2-③ 写面首批 / IP-P2-13）。
//
// 首批范围：问题状态推进（处理类动作 investigating|identified|resolved|closed）。
// 授权链（镜像工单写路径 §8.3 七步，逐条 fail-closed）：
//  1. 以声明租户读取资源（RLS 友好）→ 404 CUSTOMER_TENANT_NOT_FOUND（防枚举）；
//  2. 资源租户一致性 → 400 RESOURCE_TENANT_MISMATCH；
//  3. Allocation（服务端允许集合）→ 403 MSP_ALLOCATION_REQUIRED；
//  4. 客户租户 active → 403 CUSTOMER_INACTIVE（写面比读面更严：暂停即禁写）；
//  5. provider 侧写门禁：映射 MSP 角色 ∩ 目标租户 `msp_ticket:write` → 403 ACTION_NOT_ALLOWED；
//  6. 终态/目标状态前置校验（closed 不可再流转；目标状态词表白名单）→ 403/400；
//  7. 域服务裁决（客户租户 ctx + actorRole=家租户 users.role，与深切换语义一致）：
//     状态机 + 行级守卫（datascope 同口径）+ 时间戳派生 → 403/409（域错误翻译见 adapter）。
//     审计：成功/拒绝（通过 allocation 之后）逐条 `workbench.action`（source=workbench + target_tenant_id）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
)

// CodeInvalidTransition 域状态机拒绝（409 语义；跨租户写面统一翻译）。
const CodeInvalidTransition = "INVALID_TRANSITION"

// CodeInvalidParam 域参数校验拒绝（400 语义；跨租户写面统一翻译）。
const CodeInvalidParam = "INVALID_PARAM"

// 问题写面允许的目标状态（域状态机仍为最终裁决；此处仅做词表前置校验）。
var mspProblemWritableStatuses = map[string]bool{
	"investigating": true,
	"identified":    true,
	"resolved":      true,
	"closed":        true,
}

// MSPProblemStatusWriter 问题状态写入器（由 wiring 层适配 handlers/problem.Service；
// 接口仅回传 error，避免 service 包反向依赖 handlers/*）。
type MSPProblemStatusWriter interface {
	UpdateProblemStatus(ctx context.Context, tenantID, id, actorID int, actorRole, status, resolution string) error
}

// SetProblemStatusWriter 注入问题写入器（nil 时写面 fail-closed 返回内部错误）。
func (s *MSPWorkbenchService) SetProblemStatusWriter(w MSPProblemStatusWriter) {
	s.problemWriter = w
}

// UpdateProblemStatus 跨租户问题状态推进（P2-③ 写面首批）。
func (s *MSPWorkbenchService) UpdateProblemStatus(ctx context.Context, actor MSPWorkbenchActor, id, declaredTenantID int, status, resolution string) (*dto.MSPRecordDetailResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	if s.problemWriter == nil {
		return nil, fmt.Errorf("msp problem writer not configured")
	}
	if declaredTenantID <= 0 {
		return nil, NewCustomerAccessError(CodeCustomerTenantNotFound, "缺少目标客户租户声明")
	}
	status = strings.TrimSpace(status)
	if !mspProblemWritableStatuses[status] {
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "不支持的目标状态: %s", status)
	}

	// 1+2. 声明租户读取 + 资源租户一致性（enforce 下跨租户探测 fail-closed）。
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

	// 3. Allocation。
	if err := s.ensureAllocated(actor, row.TenantID); err != nil {
		s.recordScopeDenied(ctx, actor, []int{row.TenantID})
		return nil, err
	}

	// 4. 客户租户 active（写面：暂停/过期一律禁写）。
	meta, err := s.loadTenantMeta(ctx, []int{row.TenantID})
	if err != nil {
		return nil, err
	}
	if !meta[row.TenantID].active() {
		s.recordRecordAudit(ctx, actor, "problem.status", MSPRecordProblem, row.TenantID, row.ID, "denied")
		return nil, NewCustomerAccessError(CodeCustomerInactive, "客户租户已暂停或过期，仅可查看")
	}

	// 5. 目标租户写门禁（客户侧业务码 problem:write；IP-P2-14 口径修复，与客户侧问题路由守卫同码）。
	if !s.recordWriteAllowed(ctx, actor, row.TenantID) {
		s.recordRecordAudit(ctx, actor, "problem.status", MSPRecordProblem, row.TenantID, row.ID, "denied")
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "当前角色在该客户租户无处理权限")
	}

	// 6. 终态前置校验（closed 不可再流转；目标=closed 由域状态机校验来路）。
	if row.Status == "closed" {
		s.recordRecordAudit(ctx, actor, "problem.status", MSPRecordProblem, row.TenantID, row.ID, "denied")
		return nil, NewCustomerAccessError(CodeActionNotAllowed, "问题已关闭，不支持该操作")
	}

	// 7. 域服务裁决：客户租户 ctx + actorRole=家租户 users.role（深切换语义）。
	homeRole, err := s.loadActorHomeRole(ctx, actor)
	if err != nil {
		return nil, err
	}
	writeCtx := tenantctx.WithTenantID(ctx, row.TenantID)
	if err := s.problemWriter.UpdateProblemStatus(writeCtx, row.TenantID, row.ID, actor.UserID, homeRole, status, strings.TrimSpace(resolution)); err != nil {
		outcome := "denied"
		if mapped := translateRecordWriteError(err); mapped != nil {
			s.recordRecordAudit(ctx, actor, "problem.status", MSPRecordProblem, row.TenantID, row.ID, outcome)
			return nil, mapped
		}
		if s.logger != nil {
			s.logger.Warnw("msp problem status write failed", "error", err, "problem_id", row.ID, "tenant_id", row.TenantID)
		}
		return nil, err
	}

	s.recordRecordAudit(ctx, actor, "problem.status", MSPRecordProblem, row.TenantID, row.ID, "success")
	return s.GetRecord(ctx, actor, MSPRecordProblem, row.ID, row.TenantID)
}

// recordWriteAllowed 目标租户写门禁：映射 MSP 角色 ∩ 目标租户客户侧业务码 problem:write
// （与客户侧问题路由守卫同码；IP-P2-14 起与工单写面口径一致——目标租户内计算、客户侧词表）。
func (s *MSPWorkbenchService) recordWriteAllowed(ctx context.Context, actor MSPWorkbenchActor, tenantID int) bool {
	if s.client == nil {
		return false
	}
	rbacRole := actor.MSPRole
	if !strings.HasPrefix(rbacRole, "msp_") {
		rbacRole = middleware.GetMSPRBACRole(rbacRole)
	}
	return middleware.HasResourcePermission(ctx, s.client, rbacRole, "problem", "write", tenantID)
}

// loadActorHomeRole 读取调用方家租户 users.role（深切换下 JWT 主角色=users.role；
// 域行级守卫以该角色判定，避免把客户租户 RBAC 角色误当业务角色）。
func (s *MSPWorkbenchService) loadActorHomeRole(ctx context.Context, actor MSPWorkbenchActor) (string, error) {
	if s.client == nil || actor.UserID <= 0 {
		return "", nil
	}
	scope := actor.HomeTenantID
	if scope <= 0 {
		return "", nil
	}
	u, err := s.client.User.Query().
		Where(user.IDEQ(actor.UserID)).
		Only(tenantctx.WithTenantID(ctx, scope))
	if err != nil {
		if ent.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("msp records load actor role: %w", err)
	}
	return string(u.Role), nil
}

// canWriteRecord 与 handlers/common/datascope.CanWriteResource 同口径（service 不可反向依赖
// handlers/*；一致性由 service/msp_record_write_test.go 的 parity 用例锁定）：
// 管理角色全租户可写；其余仅 owner（创建人）或当前受理人可写；actorID<=0 拒绝。
func canWriteRecord(actorID int, actorRole string, ownerID, assigneeID int) bool {
	switch strings.ToLower(strings.TrimSpace(actorRole)) {
	case "super_admin", "admin", "manager", "sysadmin":
		return true
	}
	if actorID <= 0 {
		return false
	}
	if actorID == ownerID {
		return true
	}
	return assigneeID > 0 && actorID == assigneeID
}

// translateRecordWriteError 把域服务错误翻译为跨租户稳定语义（400/403/404/409）。
// 支持两类域错误：*common.AppError（HTTPStatus）与 *common.BusinessError（业务码）。
func translateRecordWriteError(err error) error {
	var appErr *common.AppError
	if errors.As(err, &appErr) {
		status := appErr.HTTPStatus
		if status == 0 {
			switch appErr.Code {
			case common.ErrCodeForbidden:
				status = http.StatusForbidden
			case common.ErrCodeConflict:
				status = http.StatusConflict
			case common.ErrCodeBadRequest, common.ErrCodeValidation:
				status = http.StatusBadRequest
			case common.ErrCodeNotFound:
				status = http.StatusNotFound
			}
		}
		switch status {
		case http.StatusForbidden:
			return NewCustomerAccessError(CodeActionNotAllowed, "%s", appErr.Message)
		case http.StatusConflict:
			return NewCustomerAccessError(CodeInvalidTransition, "%s", appErr.Message)
		case http.StatusBadRequest:
			return NewCustomerAccessError(CodeInvalidParam, "%s", appErr.Message)
		case http.StatusNotFound:
			return NewCustomerAccessError(CodeCustomerTenantNotFound, "%s", appErr.Message)
		}
		return nil
	}
	var bizErr *common.BusinessError
	if errors.As(err, &bizErr) {
		switch bizErr.Code {
		case common.ConflictCode:
			return NewCustomerAccessError(CodeInvalidTransition, "%s", bizErr.Message)
		case common.ParamErrorCode:
			return NewCustomerAccessError(CodeInvalidParam, "%s", bizErr.Message)
		case common.ForbiddenCode:
			return NewCustomerAccessError(CodeActionNotAllowed, "%s", bizErr.Message)
		case common.NotFoundErrorCode:
			return NewCustomerAccessError(CodeCustomerTenantNotFound, "%s", bizErr.Message)
		}
	}
	return nil
}

// recordRecordAudit 跨租户单据写审计（事件 workbench.action；source/target_tenant_id/actor_account 落列）。
func (s *MSPWorkbenchService) recordRecordAudit(ctx context.Context, actor MSPWorkbenchActor, op, recordType string, tenantID, recordID int, outcome string) {
	if s.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":               "workbench",
		"op":                   op,
		"record_type":          recordType,
		"actor_user_id":        actor.UserID,
		"actor_home_tenant_id": actor.HomeTenantID,
		"target_tenant_id":     tenantID,
		"record_id":            recordID,
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
		SetResource(recordType).
		SetAction("workbench.action").
		SetPath("/api/v1/msp/" + recordType + "s/" + fmt.Sprint(recordID) + "/status").
		SetMethod("POST").
		SetStatusCode(map[bool]int{true: 200, false: 403}[outcome == "success"]).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("msp record audit write failed", "error", err, "op", op, "record_id", recordID)
	}
}

// problemAllowedActions 问题条目动作契约（XF5；与写路径前置校验逐条同口径）：
// view 恒允许；status 按 客户 active ∩ provider 写门禁 ∩ 非终态 ∩ 行级守卫 计算。
func problemAllowedActions(actor MSPWorkbenchActor, tenantActive, writeAllowed bool, homeRole string, row mspRecordRow) []dto.WorkbenchAllowedAction {
	statusAction := dto.WorkbenchAllowedAction{Action: "status", Allowed: false}
	switch {
	case !tenantActive:
		statusAction.ReasonCode = CodeCustomerInactive
		statusAction.ReasonText = "客户租户已暂停或过期，仅可查看"
	case !writeAllowed:
		statusAction.ReasonCode = CodeActionNotAllowed
		statusAction.ReasonText = "当前角色在该客户租户无处理权限"
	case row.status == "closed":
		statusAction.ReasonCode = CodeActionNotAllowed
		statusAction.ReasonText = "问题已关闭，不支持该操作"
	case !canWriteRecord(actor.UserID, homeRole, row.ownerID, row.assigneeID):
		statusAction.ReasonCode = CodeActionNotAllowed
		statusAction.ReasonText = "仅创建人、受理人或管理员可执行状态流转"
	default:
		statusAction.Allowed = true
	}
	return []dto.WorkbenchAllowedAction{{Action: "view", Allowed: true}, statusAction}
}

// problemTenantWriteAllowed 逐租户写门禁（列表聚合时每租户一次，避免逐行 RBAC 查询）。
func (s *MSPWorkbenchService) problemTenantWriteAllowed(ctx context.Context, actor MSPWorkbenchActor, tenantID int) bool {
	return s.recordWriteAllowed(ctx, actor, tenantID)
}
