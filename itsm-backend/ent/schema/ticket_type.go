package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TicketType holds the schema definition for the TicketType entity.
type TicketType struct {
	ent.Schema
}

// Fields of the TicketType.
func (TicketType) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").NotEmpty().MaxLen(50),
		field.String("name").NotEmpty().MaxLen(100),
		field.Text("description"),
		field.String("icon").MaxLen(50),
		field.String("color").MaxLen(20),
		field.String("status").Default("active"),
		field.Int("category_id").Optional(),
		field.String("default_priority").Default("medium").MaxLen(20),
		field.Int("sort_order").Default(0),
		field.String("workflow_definition_key").Optional().MaxLen(128),
		field.Int("assignment_rule_id").Optional(),
		field.Time("archived_at").Optional().Nillable(),
		field.Int64("archived_by").Optional(),
		field.JSON("custom_fields", map[string]interface{}{}),
		field.Bool("approval_enabled").Default(false),
		field.Int64("approval_workflow_id").Optional(),
		field.JSON("approval_chain", []interface{}{}),
		field.Bool("sla_enabled").Default(false),
		field.Int64("default_sla_id").Optional(),
		// v1.4 方案 §4.2：SLA 显式豁免（默认 false 零回归）。
		// true=该类型工单跳过全部 SLA 匹配（不进 fail-loud 告警），报表单列"豁免"桶；
		// 语义区别于 sla_enabled=false（那只是放弃 L3a 直绑，仍走属性匹配）。
		field.Bool("sla_exempt").
			Comment("SLA 显式豁免：true=跳过全部匹配，报表豁免桶").
			Default(false),
		field.Bool("auto_assign_enabled").Default(false),
		field.Bool("msp_route_enabled").
			Comment("是否启用服务商跨租户路由（类型级自动路由：建单派生 MSP 快照并标记路由）").
			Default(false),
		field.JSON("assignment_rules", []interface{}{}),
		field.JSON("notification_config", map[string]interface{}{}),
		field.JSON("permission_config", map[string]interface{}{}),
		field.Int64("tenant_id"),
		field.Int64("created_by"),
		field.Time("created_at"),
		field.Time("updated_at"),
		field.Int64("updated_by").Optional(),
		field.Int("usage_count").Default(0),
		// D-TT-1（方案 §4.5a，P2-3）：受控下发溯源 + 同步状态。
		// 委托 origin 三元组判定归属与来源；controlled_by_provider=true 时受控字段仅服务商发布可改。
		field.String("origin").
			Comment("来源: native(客户自建)|controlled(服务商下发副本)").
			Default("native").
			MaxLen(16),
		field.Int64("origin_provider_tenant_id").
			Comment("下发源服务商租户ID（0=非下发）").
			Optional(),
		field.Int64("origin_type_id").
			Comment("下发源类型行ID（provider 租户内；0=非下发）").
			Optional(),
		field.Bool("controlled_by_provider").
			Comment("受控字段锁：true=受控字段(name/description/sla_enabled/default_sla_id)仅服务商发布可改").
			Default(false),
		field.String("sync_status").
			Comment("同步状态: ''(native)|synced|dirty|failed|orphaned|unbound").
			Default("").
			MaxLen(16),
		field.Time("last_synced_at").
			Comment("最近一次发布同步时间").
			Optional(),
	}
}

// Edges of the TicketType.
func (TicketType) Edges() []ent.Edge {
	return []ent.Edge{edge.To("tickets", Ticket.Type)}
}

// Indexes of the TicketType.
func (TicketType) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code", "tenant_id").Unique(),
		index.Fields("tenant_id"),
		index.Fields("status"),
		index.Fields("tenant_id", "sort_order"),
	}
}
