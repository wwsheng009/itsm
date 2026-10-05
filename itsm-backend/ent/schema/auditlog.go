package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AuditLog holds the schema definition for the AuditLog entity.
type AuditLog struct {
	ent.Schema
}

// Fields of the AuditLog.
func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Default(time.Now),
		field.Int("tenant_id").Optional(),
		field.Int("user_id").Optional(),
		field.String("request_id").Optional(),
		field.String("ip").Default(""),
		field.String("resource").Default(""),
		field.String("action").Default(""),
		field.String("path"),
		field.String("method"),
		field.Int("status_code").Default(0),
		field.Text("request_body").Optional().Nillable(),
		// IP-P0-10 / §3.0-B3：审计作用域扩展列（全部可空；历史行为 NULL，读侧按 legacy 处理）。
		field.String("actor_account").Optional().MaxLen(64),
		field.Int("membership_id").Optional().Comment("P1 membership 表落地后填充（IP-P0-1/3）"),
		field.Int("target_tenant_id").Optional().Comment("跨租户操作的目标租户；同租户时等于 tenant_id"),
		field.Int("target_user_id").Optional().Comment("跨租户账号治理的目标用户（TUM-3）；NULL=legacy/非用户维度"),
		field.String("source").Optional().MaxLen(32).Comment("login|switch|header|workbench|platform_selected|job|system；NULL=legacy"),
	}
}

// Edges of the AuditLog.
func (AuditLog) Edges() []ent.Edge { return nil }
