package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TicketTypeDistribution holds the schema definition for the TicketTypeDistribution entity.
//
// D-TT-1 工单类型受控下发（方案 §4.5a，Accepted v1.11，P2-3）：provider → customer 的
// 类型下发追踪表。语义对齐 workflow_template_distributions 先例（P2-① 模板分发）：
//   - 行归属 **customer 租户**（tenant_id=副本租户）——副本就是普通客户租户行，零跨租户读；
//   - provider 侧访问 = mspguard 授权（allocation + 客户 active）+ 以客户租户作用域重绑定 ctx；
//   - 每次分发一行（历史可追溯）；解绑/源删除不 DELETE（转 unbound/orphaned，INV-4）。
type TicketTypeDistribution struct {
	ent.Schema
}

// Fields of the TicketTypeDistribution.
func (TicketTypeDistribution) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("tenant_id").
			Comment("副本租户ID（customer；RLS 收窄依据）"),
		field.Int64("provider_tenant_id").
			Comment("下发源服务商租户ID"),
		field.Int64("source_type_id").
			Comment("provider 源类型行ID"),
		field.Int64("copy_type_id").
			Comment("customer 副本类型行ID（ticket_types.id）"),
		field.String("sync_status").
			Comment("同步状态: synced|dirty|failed|orphaned|unbound").
			Default("synced").
			MaxLen(16),
		field.Time("last_synced_at").
			Comment("最近一次发布同步时间").
			Optional(),
		field.Int64("distributed_by").
			Comment("分发操作人ID（provider 员工）"),
		field.Time("distributed_at").
			Comment("分发时间").
			Default(time.Now),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the TicketTypeDistribution.
func (TicketTypeDistribution) Indexes() []ent.Index {
	return []ent.Index{
		// 一个副本行只归属一条分发记录（解绑/再分发走新副本新行：INV-1 INSERT-only）。
		index.Fields("copy_type_id").
			Unique().
			StorageKey("uk_ticket_type_distributions_copy"),
		// provider 侧按客户/源类型收窄查询。
		index.Fields("tenant_id", "provider_tenant_id").
			StorageKey("idx_ticket_type_distributions_tenant_provider"),
		index.Fields("source_type_id").
			StorageKey("idx_ticket_type_distributions_source"),
	}
}
