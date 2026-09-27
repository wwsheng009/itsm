package admin

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/pkg/redact"
)

// EntAuditSink 把 MCP 管理操作审计写入既有 `audit_logs` 表（M0-11，不新建表）。
//
// 列映射（复用平台审计检索/导出链路）：
//   - resource    = "mcp"（治理面统一资源位）
//   - action      = entry.Action（如 server.create / tool.disable，与事件流命名对齐）
//   - path        = /api/v1/ai/mcp-servers[/<objectID>]
//   - method      = "MCP_ADMIN"（非 HTTP 语义；便于过滤管理面写操作）
//   - status_code = 200 成功 / 500 失败
//   - request_body= JSON{object_type,object_id,before,after,result,error_code}（redact 脱敏 + 限额）
//
// 硬约束：Before/After 只接受**已脱敏**快照（服务层保证），本 sink 再做一次纵深脱敏
// （敏感键一律掩码、长值截断），凭据明文/密文永不落库。
type EntAuditSink struct {
	client *ent.Client
	limit  int
}

// NewEntAuditSink 构造 DB 审计出口；client 为 nil 时退化为 DiscardAudit（fail-safe，
// 避免 typed-nil 接口导致调用方 panic）。
func NewEntAuditSink(client *ent.Client) AuditSink {
	if client == nil {
		return DiscardAudit()
	}
	return &EntAuditSink{client: client, limit: redact.DefaultArgsLimit}
}

// RecordMCPAudit 实现 AuditSink。
func (s *EntAuditSink) RecordMCPAudit(ctx context.Context, entry AuditEntry) error {
	body := redact.BodyJSON(map[string]interface{}{
		"object_type": entry.ObjectType,
		"object_id":   entry.ObjectID,
		"before":      entry.Before,
		"after":       entry.After,
		"result":      entry.Result,
		"error_code":  entry.ErrorCode,
	}, s.limit)

	path := "/api/v1/ai/mcp-servers"
	if entry.ObjectID != "" {
		path += "/" + entry.ObjectID
	}
	status := 200
	if entry.Result == "failure" {
		status = 500
	}

	create := s.client.AuditLog.Create().
		SetTenantID(entry.TenantID).
		SetUserID(entry.ActorID).
		SetIP(entry.IP).
		SetResource("mcp").
		SetAction(entry.Action).
		SetPath(path).
		SetMethod("MCP_ADMIN").
		SetStatusCode(status).
		SetRequestBody(body)
	if !entry.At.IsZero() {
		create.SetCreatedAt(entry.At)
	}
	_, err := create.Save(ctx)
	return err
}
