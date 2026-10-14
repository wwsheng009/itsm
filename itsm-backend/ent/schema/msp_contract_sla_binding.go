package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MSPContractSLABinding holds the schema definition for the MSPContractSLABinding entity.
//
// 合同 ↔ SLA 定义绑定（D-SLA-4，方案 §4.4 + 实施期修正 IM-13）：
// v1.1 原设计为 (contract_id, sla_definition_id) 二列；P0 引入类型维度后
// （grant.ticket_type_id），绑定表同步携带类型维度——由合同承载 P0 grant 的完整语义：
//
//	binding =（合同 × 定义 × 客户类型），与 grant 的
//	（定义 × 客户 × 类型）在合同语境下同构。
//
// 唯一性：同合同同类型（0=类型无关）仅一条绑定（对齐 grant 的 partial unique 语义）。
// tenant_id：归属 provider 租户（RLS 收窄；IM-14——v1.1 原文未列，按仓库全表惯例补齐）。
type MSPContractSLABinding struct {
	ent.Schema
}

// Fields of the MSPContractSLABinding.
func (MSPContractSLABinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int("contract_id").
			Comment("合同ID").
			Positive(),
		field.Int("sla_definition_id").
			Comment("SLA定义ID（owner=provider）").
			Positive(),
		field.Int64("ticket_type_id").
			Comment("customer 租户内的工单类型ID；0=类型无关绑定").
			Default(0),
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

// Edges of the MSPContractSLABinding.
func (MSPContractSLABinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("contract", MSPContract.Type).
			Ref("sla_bindings").
			Field("contract_id").
			Unique().
			Required(),
		edge.From("sla_definition", SLADefinition.Type).
			Ref("contract_bindings").
			Field("sla_definition_id").
			Unique().
			Required(),
	}
}

// Indexes of the MSPContractSLABinding.
func (MSPContractSLABinding) Indexes() []ent.Index {
	return []ent.Index{
		// 同合同同类型（0=类型无关）仅一条绑定
		index.Fields("contract_id", "ticket_type_id").
			Unique().
			StorageKey("uk_msp_contract_bindings_type"),
		// 反查：某定义被哪些合同引用（结算报表需要——§4.4 M2 关联表理由）
		index.Fields("sla_definition_id").
			StorageKey("idx_msp_contract_bindings_definition"),
		index.Fields("tenant_id").
			StorageKey("idx_msp_contract_bindings_tenant"),
	}
}
