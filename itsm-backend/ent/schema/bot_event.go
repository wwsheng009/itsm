package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotEvent 是一次运行内的事件（B1-01；SSE v2 事件的落库同源，B1-03 单一注册表）。
//
// `(run_id, seq)` 唯一：事件序号在运行内单调递增；写入方「先落库后广播」，
// 保证 SSE 重放与审计同源（B1-02）。
type BotEvent struct{ ent.Schema }

func (BotEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("run_id"),
		field.Int("seq").NonNegative(),
		field.String("type").MaxLen(64).Comment("事件名（注册表口径：run_started/step/tool_call/...）"),
		field.Text("payload_json").Default("").Comment("事件载荷（脱敏后 JSON）"),
		field.Time("created_at").Default(time.Now),
	}
}

func (BotEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BotRun.Type).Ref("events").Unique().Field("run_id").Required(),
	}
}

func (BotEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id", "seq").Unique(),
		index.Fields("tenant_id", "run_id"),
	}
}
