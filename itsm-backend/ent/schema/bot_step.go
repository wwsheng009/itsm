package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotStep 是一次运行内的步骤（B1-01；type=llm|tool|confirm，阶段一报告 §5.3(b)）。
//
// `(run_id, step_index)` 唯一：步骤序号在运行内单调，重试/重放不得覆盖既有步骤
// （写入方按「序号冲突 → 新步骤」处理，不允许 UPDATE 既有行）。
type BotStep struct{ ent.Schema }

func (BotStep) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("run_id"),
		field.Int("step_index").NonNegative(),
		field.String("type").MaxLen(16).Comment("llm|tool|confirm"),
		field.String("payload_ref").Default("").MaxLen(256).Comment("载荷引用（tool_invocation ID / 消息 ID 等，不落原文）"),
		field.Int("duration_ms").Default(0),
		field.String("error_code").Default("").MaxLen(64),
		field.Time("created_at").Default(time.Now),
	}
}

func (BotStep) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", BotRun.Type).Ref("steps").Unique().Field("run_id").Required(),
	}
}

func (BotStep) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id", "step_index").Unique(),
		index.Fields("tenant_id", "run_id"),
	}
}
