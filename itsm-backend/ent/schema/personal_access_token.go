package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PersonalAccessToken 个人访问令牌（PAT，Personal Access Token）。
//
// 安全约定：
//   - 库中只存 sha256(明文) 与展示用前缀，明文永不落库（哈希范式先例：invitation）。
//   - 权限不冻结在本表，鉴权时按 user_id 实时解析（middleware/rbac.go）。
//   - 见 docs/plan/personal-access-token-plan-2026-10-16.md（D-PAT-2/3/6）。
type PersonalAccessToken struct {
	ent.Schema
}

// Fields of the PersonalAccessToken.
func (PersonalAccessToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").Positive().Comment("归属用户ID（自作用域过滤键）"),
		field.Int("tenant_id").Positive().Comment("归属租户ID（RLS 键）"),
		field.String("name").NotEmpty().MaxLen(64).Comment("用户可读名称"),
		field.String("description").Optional().MaxLen(255).Comment("备注"),
		field.String("token_hash").NotEmpty().MaxLen(64).Unique().
			Comment("hex(sha256(明文))；明文不落库"),
		field.String("token_prefix").NotEmpty().MaxLen(24).
			Comment("明文前 17 字符（itsm_pat_+8位），仅用于展示；maskedToken=prefix+****，不含真实后缀"),
		field.Time("expires_at").Comment("过期时间（必填，默认 90 天，上限 365 天）"),
		field.Time("last_used_at").Optional().Nillable().Comment("最近使用时间（60s 节流更新）"),
		field.String("last_used_ip").Optional().MaxLen(64).Comment("最近使用 IP"),
		field.Int("use_count").Default(0).Comment("使用次数（60s 节流更新）"),
		field.Time("revoked_at").Optional().Nillable().Comment("非空即失效"),
		field.String("revoked_reason").Optional().MaxLen(32).
			Comment("self|rotated|revoke_all|security_event"),
		field.Int("rotated_to_token_id").Optional().Nillable().Comment("轮换血缘：新令牌 ID"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the PersonalAccessToken.
func (PersonalAccessToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Fields("user_id", "revoked_at"),
		index.Fields("tenant_id", "revoked_at"),
		index.Fields("expires_at"),
	}
}
