package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MSPContract holds the schema definition for the MSPContract entity.
//
// MSP SLA 合同（D-SLA-4，方案 §4.4，Accepted v1.11）：
// 把"SLA 条款 + 合同期 + 罚则"从 SLA 定义解耦，是结算口径的完整载体。
//
// 关键语义：
//   - 归属 provider 租户（tenant_id = provider_tenant_id；RLS 收窄依据）；
//   - customer 侧只读授信（RLS 012：customer_tenant_id = 当前租户可 SELECT）；
//   - 与既有 contract.go（vendor 合同）/ support_contract.go（支持合同）为不同域实体；
//   - 表名经 MSPAllocation → msp_allocations 先例验证（ent 默认复数化正确）。
type MSPContract struct {
	ent.Schema
}

// Fields of the MSPContract.
func (MSPContract) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_tenant_id").
			Comment("签订方（服务商租户）").
			Positive(),
		field.Int("customer_tenant_id").
			Comment("被服务方（客户租户）").
			Positive(),
		field.String("contract_no").
			Comment("合同编号（tenant 内唯一，对齐 support_contract 惯例）").
			NotEmpty().
			MaxLen(160),
		field.String("name").
			Comment("合同名称").
			NotEmpty().
			MaxLen(200),
		field.Time("effective_from").
			Comment("生效时间").
			Default(time.Now),
		field.Time("effective_until").
			Comment("失效时间（NULL=长期有效）").
			Optional().
			Nillable(),
		field.String("status").
			Comment("状态：draft|active|expired|terminated").
			Default("active").
			MaxLen(32),
		field.JSON("penalties", map[string]interface{}{}).
			Comment("违约条款（值对象）：{metric, threshold, credit_rate, cap}").
			Optional(),
		field.String("settlement_cycle").
			Comment("结算周期：monthly|quarterly|on_demand").
			Default("monthly").
			MaxLen(32),
		field.String("currency").
			Comment("结算币种（默认取 customer.currency）").
			Optional().
			MaxLen(16),
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

// Edges of the MSPContract.
func (MSPContract) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sla_bindings", MSPContractSLABinding.Type).
			Comment("合同引用的 SLA 定义集（关联表，非 JSON 数组——§4.4 v1.1）"),
	}
}

// Indexes of the MSPContract.
func (MSPContract) Indexes() []ent.Index {
	return []ent.Index{
		// tenant 内合同编号唯一（对齐 support_contract.go:39-43 惯例）
		index.Fields("tenant_id", "contract_no").
			Unique().
			StorageKey("uk_msp_contracts_tenant_no"),
		// 结算/匹配查询：按客户 + 状态收窄
		index.Fields("tenant_id", "customer_tenant_id", "status").
			StorageKey("idx_msp_contracts_customer_status"),
	}
}
