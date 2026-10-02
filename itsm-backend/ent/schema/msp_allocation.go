package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MSPAllocation holds the schema definition for the MSPAllocation entity.
type MSPAllocation struct {
	ent.Schema
}

// Fields of the MSPAllocation.
func (MSPAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int("msp_user_id").
			Comment("MSP 员工ID（属于MSP租户）"),
		field.Int("customer_tenant_id").
			Comment("客户租户ID（支持单客户模式）"),
		field.Int("provider_tenant_id").
			Optional().
			Comment("服务商租户ID（IP-P2-1：= MSP 员工 home provider 且 == customer.msp_provider_id）"),
		field.String("role").
			Comment("分配角色: primary|backup|specialist").
			Default("primary"),
		field.Time("assigned_at").
			Comment("分配时间").
			Default(time.Now),
		field.Time("deassigned_at").
			Comment("解除分配时间").
			Optional(),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
	}
}

// Indexes of the MSPAllocation：活跃分配唯一（IP-P0-2 §3.0-B2，与迁移 022 同源）。
func (MSPAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("msp_user_id", "customer_tenant_id").
			Unique().
			StorageKey("uk_msp_allocation_active").
			Annotations(entsql.IndexWhere("deassigned_at IS NULL")),
		index.Fields("provider_tenant_id").
			StorageKey("idx_msp_allocations_provider").
			Annotations(entsql.IndexWhere("deassigned_at IS NULL")),
	}
}

// Edges of the MSPAllocation.
func (MSPAllocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("msp_user", User.Type).
			Ref("msp_allocations").
			Field("msp_user_id").
			Required().
			Unique(),
		edge.From("customer_tenant", Tenant.Type).
			Field("customer_tenant_id").
			Ref("msp_customer_allocations").
			Unique().
			Required(),
		edge.From("provider_tenant", Tenant.Type).
			Field("provider_tenant_id").
			Ref("msp_provider_allocations").
			Unique(),
	}
}
