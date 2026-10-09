// record_writers.go：跨租户单据写面适配器（P2-③ 写面首批 / IP-P2-13）。
//
// service 包不可反向依赖 handlers/*，故以窄接口（service.MSPProblemStatusWriter）+ 本适配器解耦；
// 域错误由 service 层 translateRecordWriteError 统一翻译（400/403/404/409），本层保持透传。
package msp

import (
	"context"

	problemHandler "itsm-backend/handlers/problem"
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
