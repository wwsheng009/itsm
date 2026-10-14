package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type SLADefinition struct{ ent.Schema }

func (SLADefinition) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Comment("SLA名称").NotEmpty(),
		field.Text("description").Comment("SLA描述").Optional(),
		field.String("service_type").Comment("服务类型").Optional(),
		field.String("priority").Comment("优先级").Optional(),
		field.Int("response_time").Comment("响应时间(分钟)").Positive().Default(30),
		field.Int("resolution_time").Comment("解决时间(分钟)").Positive().Default(240),
		field.JSON("business_hours", map[string]interface{}{}).Comment("营业时间配置").Optional(),
		field.JSON("escalation_rules", map[string]interface{}{}).Comment("升级规则").Optional(),
		field.JSON("conditions", map[string]interface{}{}).Comment("适用条件").Optional(),
		field.Bool("is_active").Comment("是否激活").Default(true),
		// v1.11 方案 D-SLA-1：归属服务商租户 ID。
		// 0=租户自用（现状行为不变）；非 0=provider 创建管理的合同 SLA，
		// 经 sla_definition_grants 授权下发，customer 侧只读（四层写保护见方案 §4.1）。
		field.Int("owner_provider_tenant_id").
			Comment("归属服务商租户ID；0=租户自用，非0=provider 合同 SLA").
			Default(0),
		field.Int("tenant_id").Comment("租户ID").Positive(),
		field.Time("created_at").Comment("创建时间").Default(time.Now),
		field.Time("updated_at").Comment("更新时间").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SLADefinition) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("violations", SLAViolation.Type).Comment("SLA违规记录"),
		edge.To("metrics", SLAMetric.Type).Comment("SLA指标"),
		edge.To("tickets", Ticket.Type).Comment("关联工单"),
		edge.To("alert_rules", SLAAlertRule.Type).Comment("SLA预警规则"),
		edge.To("grants", SLADefinitionGrant.Type).Comment("下发授权（provider 侧资产）"),
		edge.To("contract_bindings", MSPContractSLABinding.Type).Comment("合同绑定（provider 侧资产，D-SLA-4）"),
	}
}
