package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Conversation holds the schema definition for the Conversation entity.
type Conversation struct {
	ent.Schema
}

// Fields of the Conversation.
func (Conversation) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Default(time.Now),
		field.Int("tenant_id").Optional(),
		field.Int("user_id").Optional(),
		field.String("title").Default(""),
		// bot_id：会话归属的 Bot 模板（B2-04）；0 = 未绑定 = 内置默认助手（兼容默认）。
		// 绑定在**创建会话时**写入并按选择器传入，切换不回溯修改历史会话归属。
		field.Int("bot_id").Default(0),
	}
}

// Edges of the Conversation.
func (Conversation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("messages", Message.Type),
		edge.To("tool_invocations", ToolInvocation.Type),
	}
}
