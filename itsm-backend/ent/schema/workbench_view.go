package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WorkbenchView 工作台自定义视图（IP-P2-4a）：保存的过滤器组合，支持同 provider 内分享。
//
// 作用域：tenant_id = provider 租户（视图归属域）；owner_user_id = 创建者。
// 可见性：owner 全部可见；is_shared=true 对同 provider 全体可见（只读他人视图）。
// 过滤器：filters JSON（与 WorkbenchTicketQuery 对齐：customerTenantIds/status/priority/assigneeId/q/sort；
//
//	空 customerTenantIds = 全部客户）。
type WorkbenchView struct{ ent.Schema }

// Fields of the WorkbenchView.
func (WorkbenchView) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive().Comment("provider 租户ID（视图归属域）"),
		field.Int("owner_user_id").Positive().Comment("创建者用户ID"),
		field.String("name").NotEmpty().MaxLen(60),
		field.JSON("filters", map[string]any{}).Comment("过滤器组合（customerTenantIds/status/priority/assigneeId/q/sort）"),
		field.Bool("is_shared").Default(false),
		field.Bool("is_default").Default(false).Comment("每 owner 至多一个默认视图（部分唯一索引 + 服务层事务）"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the WorkbenchView.
func (WorkbenchView) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "owner_user_id", "name").Unique(),
		index.Fields("tenant_id", "is_shared"),
	}
}
