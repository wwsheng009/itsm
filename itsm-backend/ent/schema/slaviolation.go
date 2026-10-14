package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type SLAViolation struct{ ent.Schema }

func (SLAViolation) Fields() []ent.Field {
	return []ent.Field{
		field.Int("created_by").Comment("创建人ID").Default(0),
		field.Int("sla_definition_id").Comment("SLA定义ID").Positive(),
		field.String("ticket_type").Comment("工单类型").Default("ticket"),
		field.String("sla_name").Comment("SLA名称").Default("Default SLA"),
		field.Int("ticket_id").Comment("工单ID").Positive(),
		field.String("violation_type").Comment("违规类型").NotEmpty(),
		field.Time("violation_time").Comment("违规时间").Default(time.Now),
		field.Time("violation_occurred_at").Comment("违规发生时间").Default(time.Now),
		field.Int("expected_time").Comment("期望时间(时间戳)").Optional(),
		field.Int("actual_time").Comment("实际时间(时间戳)").Optional(),
		field.Int("overdue_minutes").Comment("超时分钟数").Default(0),
		field.String("status").Comment("状态").Default("open"),
		field.Text("description").Comment("违规描述").Optional(),
		field.String("severity").Comment("严重程度").Default("medium"),
		field.Bool("is_resolved").Comment("是否已解决").Default(false),
		field.Time("resolved_at").Comment("解决时间").Optional(),
		field.Text("resolution_notes").Comment("解决说明").Optional(),
		// D-SLA-8（方案 §13.1）：违约归责状态机——"解决了"≠"认账了"。
		// 终态不可逆（accepted/waived）；settled_flag=true 后随结算快照锁定，禁止任何变更。
		field.String("resolution_state").
			Comment("归责状态: pending|accepted|disputed|waived").
			Default("pending").
			MaxLen(20),
		field.Text("dispute_reason").Comment("争议理由（disputed 时必填）").Optional(),
		field.Text("waived_reason").Comment("豁免理由（waived 时必填）").Optional(),
		field.Int("waived_by").Comment("豁免操作人ID").Optional(),
		field.Time("waived_at").Comment("豁免时间").Optional(),
		field.Bool("settled_flag").Comment("已进结算快照（终态锁定；D-SLA-9 §13.3）").Default(false),
		// D-SLA-9（方案 §13.6，P2-2）：匹配层级快照（来自工单建单时快照；L1=合同域 / L3*=客户内部域）。
		// 结算查询按 matched_layer 过滤 + 合同基准重算双保险（客户自建违约不进合同结算）。
		field.String("matched_layer").
			Comment("匹配层级快照: L1/L2/L3a/L3b/L3c/L4（来源=工单）").
			Optional().
			MaxLen(8),
		field.Int("tenant_id").Comment("租户ID").Positive(),
		field.Time("created_at").Comment("创建时间").Default(time.Now),
		field.Time("updated_at").Comment("更新时间").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SLAViolation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("sla_definition", SLADefinition.Type).
			Ref("violations").
			Field("sla_definition_id").
			Unique().
			Required().
			Comment("SLA定义"),
		edge.From("ticket", Ticket.Type).
			Ref("sla_violations").
			Field("ticket_id").
			Unique().
			Required().
			Comment("工单"),
	}
}
