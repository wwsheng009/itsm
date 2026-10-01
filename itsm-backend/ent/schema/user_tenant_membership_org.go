package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserTenantMembershipOrg holds the schema definition for the membership 组织关联子表。
//
// 权威定义：实施方案 §4.0-A（P1 契约冻结，IP-P1-3）。
// 「组织成员关系以 membership 为唯一载体」：本表把 department/team/group/project
// 四类多态组织归属挂到 (membership, tenant) 上，支持多组织归属与生效期；
// users 单值 FK（department_id/team_users/group_members）保留为兼容读，回填后不再作为权威。
type UserTenantMembershipOrg struct {
	ent.Schema
}

// Fields of the UserTenantMembershipOrg。
func (UserTenantMembershipOrg) Fields() []ent.Field {
	return []ent.Field{
		field.Int("membership_id").
			Comment("成员行ID（user_tenant_memberships.id；与 tenant_id 组成复合 FK）"),
		field.Int("tenant_id").
			Comment("作用域租户ID（tenants.id）；与 membership.tenant_id 永久一致（DB 复合 FK + 应用双校验）"),
		field.Enum("org_type").
			Comment("组织类型（多态）：department|team|group|project").
			Values("department", "team", "group", "project"),
		field.Int64("org_id").
			Comment("组织ID（多态指向 department/team/group/project 表；租户一致性由应用层校验 + guard 扫描）").
			Positive(),
		field.Int("role_id").
			Comment("组织内角色（roles.id，可选；同租户）").
			Optional().
			Nillable(),
		field.Bool("is_primary").
			Comment("主组织标记：同一 membership 同一 org_type 至多一个（部分唯一索引）").
			Default(false),
		field.Enum("status").
			Comment("关联状态：active|suspended（移除=软删）").
			Values("active", "suspended").
			Default("active"),
		field.Time("expires_at").
			Comment("组织归属生效期（到期即失效，巡检/读取按 now 判定）").
			Optional().
			Nillable(),
		field.Time("deleted_at").
			Comment("软删时间（保留历史）").
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

// Indexes of the UserTenantMembershipOrg（§4.0-A）。
func (UserTenantMembershipOrg) Indexes() []ent.Index {
	return []ent.Index{
		// 1. 同一 membership 对同一组织仅一条记录（含历史；软删后复挂走原行复位）。
		index.Fields("membership_id", "org_type", "org_id").
			Unique().
			StorageKey("uq_membership_org"),
		// 2. 主组织唯一（同 membership 同 org_type 至多一个存活主组织）。
		index.Fields("membership_id", "org_type").
			Unique().
			StorageKey("uq_membership_org_primary").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND is_primary")),
		// 3. 组织维度查询（反查成员）。
		index.Fields("tenant_id", "org_type", "org_id").
			StorageKey("idx_membership_org_scope"),
	}
}

// Edges of the UserTenantMembershipOrg。
//
// 说明：tenant_id 与 membership_id 的**复合 FK**（(membership_id, tenant_id) →
// user_tenant_memberships(id, tenant_id)）无法用 ent 单列边表达，由迁移
// 20260505 以 DB 约束落地；ent 侧保留两条单列边用于查询与 auto-migrate 兼容。
func (UserTenantMembershipOrg) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("membership", UserTenantMembership.Type).
			Ref("orgs").
			Field("membership_id").
			Required().
			Unique(),
		edge.From("tenant", Tenant.Type).
			Ref("membership_orgs").
			Field("tenant_id").
			Required().
			Unique(),
	}
}
