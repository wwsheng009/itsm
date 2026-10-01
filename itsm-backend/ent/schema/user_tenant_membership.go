package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserTenantMembership holds the schema definition for the UserTenantMembership entity.
//
// 权威定义：目标架构 §3.2 / 主方案 §6.1（IP-P1-1 契约冻结）。
// 「账号（users）≠ 成员身份」：本表是账号在某租户内的角色/组织/生效期唯一载体；
// IP-P1-1 只建表 + 回填（读路径仍回退 home+allocation 计算），切换读取归 IP-P1-2。
type UserTenantMembership struct {
	ent.Schema
}

// Fields of the UserTenantMembership.
func (UserTenantMembership) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").
			Comment("账号ID（users.id）"),
		field.Int("tenant_id").
			Comment("作用域租户ID（tenants.id）"),
		field.Enum("account_kind").
			Comment("账号类型快照（目标架构 §3.1）：customer|provider|platform；由服务层与 users 判定同步 + 巡检").
			Values("customer", "provider", "platform").
			Default("customer"),
		field.Enum("subject_type").
			Comment("主体类型：第一期仅 user；service_account P2 扩展").
			Values("user").
			Default("user"),
		field.Enum("source").
			Comment("来源：home|allocation|platform|invite|migration（可追溯）").
			Values("home", "allocation", "platform", "invite", "migration"),
		field.Int("role_id").
			Comment("作用域内角色（roles.id，同租户）；映射失败可为空并进入巡检清单").
			Optional().
			Nillable(),
		field.String("msp_role").
			Comment("服务方角色词表（D10：msp_viewer/msp_tech/msp_specialist/msp_manager/msp_admin）；客户方为空").
			Optional().
			Nillable(),
		field.Int("allocation_id").
			Comment("服务方作用域来源分配（msp_allocations.id，可追溯）").
			Optional().
			Nillable(),
		field.Enum("status").
			Comment("成员状态：active|suspended（邀请态由邀请流承载；移除=软删）").
			Values("active", "suspended").
			Default("active"),
		field.Bool("is_default").
			Comment("主作用域标记：客户方恒 true；provider 的 provider 作用域 true").
			Default(false),
		field.Time("expires_at").
			Comment("作用域到期（IP-P1-3 起生效）").
			Optional().
			Nillable(),
		field.Int("invited_by").
			Comment("邀请人（source=invite）").
			Optional().
			Nillable(),
		field.Time("joined_at").
			Comment("加入时间（source=invite 时）").
			Optional().
			Nillable(),
		field.Time("deassigned_at").
			Comment("分配撤销时间（allocation 软删同步）").
			Optional().
			Nillable(),
		field.Time("deleted_at").
			Comment("软删时间（保留历史，回收即失效）").
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

// Indexes of the UserTenantMembership：3 个部分唯一索引（§3.2 关键约束）+ 复合 FK 目标。
func (UserTenantMembership) Indexes() []ent.Index {
	return []ent.Index{
		// 1. 同一账号在同一租户仅一条存活成员行。
		index.Fields("user_id", "tenant_id").
			Unique().
			StorageKey("uq_membership_live").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		// 2. 唯一默认作用域（active）。
		index.Fields("user_id", "is_default", "status").
			Unique().
			StorageKey("uq_membership_default").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND is_default AND status = 'active'")),
		// 3. 客户方单作用域（DB 级强约束，canon P2/§3.1）。
		index.Fields("user_id", "account_kind", "status").
			Unique().
			StorageKey("uq_customer_single_scope").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND status = 'active' AND account_kind = 'customer'")),
		// 4. 复合 FK 目标（user_tenant_membership_orgs 子表，§4.0-A）。
		index.Fields("id", "tenant_id").
			Unique().
			StorageKey("uq_membership_id_tenant"),
		// 查询辅助。
		index.Fields("tenant_id", "status").
			StorageKey("idx_membership_tenant_status"),
		index.Fields("user_id", "status").
			StorageKey("idx_membership_user_status"),
		index.Fields("allocation_id").
			StorageKey("idx_membership_allocation"),
	}
}

// Edges of the UserTenantMembership.
func (UserTenantMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("tenant_memberships").
			Field("user_id").
			Required().
			Unique(),
		edge.From("tenant", Tenant.Type).
			Ref("memberships").
			Field("tenant_id").
			Required().
			Unique(),
		edge.To("orgs", UserTenantMembershipOrg.Type).
			Comment("组织归属（IP-P1-3：多组织/生效期以 membership 为唯一载体）"),
	}
}
