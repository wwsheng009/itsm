package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Invitation holds the schema definition for the 邀请实体（实施方案 §4.0-C，IP-P1-4）。
//
// 生命周期：创建（角色白名单 + msp_role 通道校验）→ 投递（SMTP 或 inviteUrl）→
// 接受（一次性：status=pending ∧ now()<expires_at，事务内置 accepted + 建号/绑定）→
// 撤销（仅 pending，revoked_at）→ 过期由巡检置 expired；重发 = 新 token 且旧 token 失效。
//
// 安全：token 128-bit 随机 + sha256 存储（原始 token 不落库）；日志脱敏；
// super_admin/sysadmin/admin 等平台角色不可被邀请（F3）。
type Invitation struct {
	ent.Schema
}

// Fields of the Invitation.
func (Invitation) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("目标租户ID（tenants.id；被邀请人加入的作用域）"),
		field.String("token_hash").
			Comment("sha256(token) hex；原始 token 不落库").
			MaxLen(64).
			Unique(),
		field.String("email").
			Comment("被邀请邮箱（服务层统一小写规范化；DB 部分唯一索引按 lower(email)）").
			MaxLen(255).
			NotEmpty(),
		field.Int("target_user_id").
			Comment("可选：邀请已存在账号绑定（users.id）").
			Optional().
			Nillable(),
		field.Int("role_id").
			Comment("目标租户内的角色（roles.id；不可邀请平台角色）"),
		field.String("msp_role").
			Comment("服务方角色白名单：provider_admin/provider_agent（provider 通道）").
			MaxLen(32).
			Optional().
			Nillable(),
		field.Int("invited_by").
			Comment("邀请人（users.id）"),
		field.Enum("status").
			Comment("状态：pending|accepted|revoked|expired").
			Values("pending", "accepted", "revoked", "expired").
			Default("pending"),
		field.Time("expires_at").
			Comment("到期时间（默认 now()+72h，INVITATION_TTL_HOURS 可配）"),
		field.Time("accepted_at").
			Comment("接受时间").
			Optional().
			Nillable(),
		field.Time("revoked_at").
			Comment("撤销时间").
			Optional().
			Nillable(),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the Invitation（§4.0-C）。
func (Invitation) Indexes() []ent.Index {
	return []ent.Index{
		// 同租户同邮箱至多一条 pending（重发=旧邀请置 revoked 后新建）。
		// 说明：Postgres 迁移按契约使用 lower(email)；ent 侧依赖服务层小写规范化。
		index.Fields("tenant_id", "email").
			Unique().
			StorageKey("uq_invitation_pending").
			Annotations(entsql.IndexWhere("status = 'pending'")),
		index.Fields("tenant_id", "status").
			StorageKey("idx_invitations_tenant_status"),
		index.Fields("expires_at").
			StorageKey("idx_invitations_expiry").
			Annotations(entsql.IndexWhere("status = 'pending'")),
	}
}
