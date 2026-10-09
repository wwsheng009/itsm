// record_writers.go：跨租户单据写面适配器（P2-③：IP-P2-13 问题 / IP-P2-15 变更与服务请求）。
//
// service 包不可反向依赖 handlers/*，故以窄接口（service.MSP*StatusWriter）+ 本适配器解耦；
// 域错误由 service 层 translateRecordWriteError 统一翻译（400/403/404/409），本层保持透传。
package msp

import (
	"context"
	"errors"

	"itsm-backend/common"
	changeHandler "itsm-backend/handlers/change"
	problemHandler "itsm-backend/handlers/problem"
	srHandler "itsm-backend/handlers/service_request"
	"itsm-backend/service"
)

// problemStatusWriter 适配 handlers/problem.Service → service.MSPProblemStatusWriter。
type problemStatusWriter struct {
	svc *problemHandler.Service
}

// NewProblemStatusWriter 构造问题状态写入器（wiring 层注入工作台服务；nil 服务 fail-closed）。
func NewProblemStatusWriter(svc *problemHandler.Service) service.MSPProblemStatusWriter {
	return problemStatusWriter{svc: svc}
}

func (w problemStatusWriter) UpdateProblemStatus(ctx context.Context, tenantID, id, actorID int, actorRole, status, resolution string) error {
	if w.svc == nil {
		return service.NewCustomerAccessError(service.CodeActionNotAllowed, "问题写服务未启用")
	}
	_, err := w.svc.Update(ctx, tenantID, id, &problemHandler.Problem{Status: status, Resolution: resolution}, actorID, actorRole)
	return err
}

// changeStatusWriter 适配 handlers/change.Service → service.MSPChangeStatusWriter。
type changeStatusWriter struct {
	svc *changeHandler.Service
}

// NewChangeStatusWriter 构造变更状态写入器（wiring 层注入工作台服务；nil 服务 fail-closed）。
func NewChangeStatusWriter(svc *changeHandler.Service) service.MSPChangeStatusWriter {
	return changeStatusWriter{svc: svc}
}

func (w changeStatusWriter) TransitionChangeStatus(ctx context.Context, tenantID, id, actorID int, targetStatus, comment, actorRole string) error {
	if w.svc == nil {
		return service.NewCustomerAccessError(service.CodeActionNotAllowed, "变更写服务未启用")
	}
	_, err := w.svc.TransitionStatus(ctx, id, tenantID, actorID, targetStatus, comment, actorRole)
	return translateChangeDomainError(err)
}

// translateChangeDomainError 与客户侧 handler 同一分流口径（handlers/change/handler.go TransitionStatus 错误映射）：
// 域哨兵错误 → BusinessError，交 service 层 translateRecordWriteError 统一翻译（409/403/404）；
// 否则状态机拒绝会掉入 500（错误映射铁律）。
func translateChangeDomainError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, changeHandler.ErrInvalidTransition), errors.Is(err, changeHandler.ErrConcurrentModification):
		return common.NewBusinessError(common.ConflictCode, err.Error(), "")
	case errors.Is(err, changeHandler.ErrNotApprover):
		return common.NewBusinessError(common.ForbiddenCode, err.Error(), "")
	case errors.Is(err, changeHandler.ErrChangeNotFound):
		return common.NewBusinessError(common.NotFoundErrorCode, err.Error(), "")
	}
	return err
}

// requestStatusWriter 适配 handlers/service_request.Service → service.MSPRequestStatusWriter。
type requestStatusWriter struct {
	svc *srHandler.Service
}

// NewRequestStatusWriter 构造服务请求履约状态写入器（wiring 层注入；nil 服务 fail-closed）。
func NewRequestStatusWriter(svc *srHandler.Service) service.MSPRequestStatusWriter {
	return requestStatusWriter{svc: svc}
}

func (w requestStatusWriter) UpdateRequestStatus(ctx context.Context, tenantID, id, actorID int, actorRole, status string) error {
	if w.svc == nil {
		return service.NewCustomerAccessError(service.CodeActionNotAllowed, "服务请求写服务未启用")
	}
	return w.svc.UpdateStatus(ctx, id, tenantID, actorID, actorRole, status)
}
