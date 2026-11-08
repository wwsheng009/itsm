package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SLADefinitionSnapshot is the customer-tenant copy of an SLA definition term
// set. Provider owns the source definition/grant; customer reads this row only.
//
// source_kind:
//   - contract_binding: materialized from msp_contract_sla_bindings (L1 contract chain)
//   - grant: materialized from sla_definition_grants (legacy/grant channel)
type SLADefinitionSnapshot struct {
	ent.Schema
}

func (SLADefinitionSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("customer 租户；RLS 唯一作用域").
			Positive(),
		field.String("source_kind").
			Comment("contract_binding|grant").
			NotEmpty().
			MaxLen(24),
		field.Int("source_contract_id").
			Comment("provider msp_contracts.id；grant 行可空").
			Optional().
			Nillable(),
		field.String("source_contract_no").
			Comment("provider 合同号，仅展示/溯源").
			Optional().
			MaxLen(160),
		field.Int("source_binding_id").
			Comment("provider msp_contract_sla_bindings.id；grant 行可空").
			Optional().
			Nillable(),
		field.Int("source_grant_id").
			Comment("provider sla_definition_grants.id；contract_binding 行可空").
			Optional().
			Nillable(),
		field.Int("source_definition_id").
			Comment("provider sla_definitions.id，仅溯源").
			Positive(),
		field.Int("source_tenant_id").
			Comment("provider 租户，仅溯源").
			Positive(),
		field.Int64("source_version").
			Comment("provider 发布版本；单调递增").
			Positive(),
		field.String("definition_name").
			Comment("客户可见的展示名").
			NotEmpty().
			MaxLen(255),
		field.Text("description").
			Comment("客户可见描述").
			Optional(),
		field.String("service_type").
			Comment("服务类型").
			Optional().
			MaxLen(64),
		field.String("priority").
			Comment("优先级").
			Optional().
			MaxLen(32),
		field.Int64("ticket_type_id").
			Comment("customer 租户工单类型；NULL=类型无关").
			Optional().
			Nillable(),
		field.JSON("terms", map[string]interface{}{}).
			Comment("物化条款：response_time/resolution_time/business_hours/conditions 等"),
		field.Time("effective_from").
			Comment("生效起点（含）"),
		field.Time("effective_until").
			Comment("失效终点（不含）；NULL=长期有效").
			Optional().
			Nillable(),
		field.String("status").
			Comment("active|superseded|expired|revoked").
			Default("active").
			MaxLen(16),
		field.String("payload_hash").
			Comment("快照内容 SHA-256").
			NotEmpty().
			MaxLen(64),
		field.Time("synced_at").
			Comment("最后一次下发/同步时间").
			Default(time.Now),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (SLADefinitionSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "source_kind", "status", "effective_from", "effective_until").
			StorageKey("idx_sds_tenant_effective"),
		index.Fields("tenant_id", "source_contract_id", "status").
			StorageKey("idx_sds_contract"),
		index.Fields("tenant_id", "ticket_type_id", "status").
			StorageKey("idx_sds_ticket_type"),
		index.Fields("tenant_id", "source_contract_id", "source_binding_id", "source_version").
			Unique().
			StorageKey("uk_sds_contract_version").
			Annotations(entsql.IndexWhere("source_kind = 'contract_binding' AND source_contract_id IS NOT NULL AND source_binding_id IS NOT NULL")),
		index.Fields("tenant_id", "source_grant_id", "source_version").
			Unique().
			StorageKey("uk_sds_grant_version").
			Annotations(entsql.IndexWhere("source_kind = 'grant' AND source_grant_id IS NOT NULL")),
	}
}
