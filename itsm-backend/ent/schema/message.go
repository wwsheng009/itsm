package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Message holds the schema definition for the Message entity.
type Message struct{ ent.Schema }

func (Message) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Default(time.Now),
		field.Int("conversation_id"),
		// IP-P2-3：租户化——写入时由请求 ctx 派生（ctx 无租户时由所属会话派生；两者冲突
		// 或均缺失时 fail-closed）。历史行由迁移回填；NOT NULL 收尾 2026-10-03
		//（migrations/20261003_messages_tenant_not_null.sql，巡检三节归零后执行）。
		field.Int("tenant_id"),
		field.String("role"), // user/assistant/system/tool
		field.Text("content").Default(""),
		field.String("request_id").Optional(),
	}
}

func (Message) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("conversation", Conversation.Type).Ref("messages").Unique().Field("conversation_id").Required(),
	}
}

// Indexes of the Message.
func (Message) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "conversation_id", "created_at"),
	}
}
