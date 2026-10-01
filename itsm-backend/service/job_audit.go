package service

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/middleware"
)

// JobAuditEntry 后台任务执行审计（IP-P0-11；source=job，§3.0-E 枚举）。
type JobAuditEntry struct {
	TenantID   int
	Component  string // 任务组件，作为 actor_account（系统执行者标识）
	Action     string
	Resource   string
	StatusCode int
	Detail     map[string]any
}

// RecordJobAudit 记录后台任务执行：tenant_id = target_tenant_id = 任务租户，
// source=job、actor_account=组件名；写入使用独立超时，失败仅告警不阻塞任务。
func RecordJobAudit(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, e JobAuditEntry) {
	if client == nil || e.TenantID <= 0 {
		return
	}
	detail := map[string]any{"component": e.Component}
	for k, v := range e.Detail {
		detail[k] = v
	}
	payload, _ := json.Marshal(detail)
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(e.TenantID).
		SetTargetTenantID(e.TenantID).
		SetSource(middleware.AuditSourceJob).
		SetActorAccount(e.Component).
		SetResource(e.Resource).
		SetAction(e.Action).
		SetPath("/internal/jobs/" + e.Component).
		SetMethod("JOB").
		SetStatusCode(e.StatusCode).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && logger != nil {
		logger.Warnw("job audit write failed", "error", err, "component", e.Component, "action", e.Action)
	}
}
