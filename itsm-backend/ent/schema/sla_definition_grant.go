package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SLADefinitionGrant holds the schema definition for the SLADefinitionGrant entity.
//
// SLA 定义下发授权（provider → customer）。设计依据：
// docs/multi-tenant/plan/sla-contract-multitenant-plan-2026-10-14.md §4.1（D-SLA-1，Accepted v1.11）。
//
// 关键语义：
//   - tenant_id = provider 租户（授权是 provider 的资产，写入走 provider ctx）；
//   - customer 端只读：经 RLS 显式授信通道（database/rls/migrations/011），
//     读取条件 customer_tenant_id = 当前租户 AND is_active；
//   - ticket_type_id：customer 租户内的工单类型 ID；0 = 类型无关授权（匹配低优先级），
//     非 0 = 仅对该客户的此类型生效（匹配高优先级，L1 类型维度由 grant 承载）；
//   - 同一 customer + 同一类型（0 视作一种类型）同时仅一条生效授权：
//     partial unique（uk_sla_definition_grants_active，WHERE is_active），
//     应用层配套 deactivate-then-activate 事务（方案 §4.1 Blocker-1 修复）。
type SLADefinitionGrant struct {
	ent.Schema
}

// Fields of the SLADefinitionGrant.
func (SLADefinitionGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int("sla_definition_id").
			Comment("SLA定义ID（provider 租户内的源头定义）").
			Positive(),
		field.Int("customer_tenant_id").
			Comment("被授权客户租户ID").
			Positive(),
		field.Int("provider_tenant_id").
			Comment("服务商租户ID").
			Positive(),
		field.Int64("ticket_type_id").
			Comment("customer 租户内的工单类型ID；0=类型无关授权").
			Default(0),
		field.Time("valid_until").
			Comment("有效期（NULL=长期有效；完整合同期由 P1 msp_contract 承载）").
			Optional().
			Nillable(),
		field.Bool("is_active").
			Comment("是否生效").
			Default(true),
		field.Int("tenant_id").
			Comment("归属租户ID（=provider_tenant_id；RLS 收窄依据）").
			Positive(),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the SLADefinitionGrant.
func (SLADefinitionGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("sla_definition", SLADefinition.Type).
			Ref("grants").
			Field("sla_definition_id").
			Unique().
			Required(),
	}
}

// Indexes of the SLADefinitionGrant.
// 与迁移 SQL `migrations/20261014_sla_grant_ownership_and_name_unique.sql` 同源（幂等双轨）。
func (SLADefinitionGrant) Indexes() []ent.Index {
	return []ent.Index{
		// 方案 §4.1 Blocker-1：同一 customer + 同一类型（0=类型无关）仅一条生效授权。
		index.Fields("customer_tenant_id", "ticket_type_id").
			Unique().
			StorageKey("uk_sla_definition_grants_active").
			Annotations(entsql.IndexWhere("is_active")),
		index.Fields("provider_tenant_id").
			StorageKey("idx_sla_definition_grants_provider"),
		index.Fields("sla_definition_id").
			StorageKey("idx_sla_definition_grants_definition"),
	}
}
