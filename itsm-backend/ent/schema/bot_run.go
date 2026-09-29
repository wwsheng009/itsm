package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotRun 是一次 Bot/助手运行的聚合根（B1-01；字段清单见阶段一报告 §5.3(b)）。
//
// 生命周期：running → completed|failed（cancelled 预留，B1-02 RunManager 落状态机）。
// 保留策略按 BQ4：runs/steps 180 天；events 90 天热存 + 归档（归档任务归 B4/运维）。
type BotRun struct{ ent.Schema }

func (BotRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("conversation_id").Optional().Comment("会话归属（聊天链路注入；B0-03 同源）"),
		field.Int("bot_id").Optional().Comment("预留：B2 Bot 模板 ID"),
		field.String("entrypoint").Default("chat").MaxLen(32).Comment("入口：chat|ticket|incident|ci|..."),
		field.String("target_type").Default("").MaxLen(32).Comment("入口目标对象类型（B3-01：ticket|incident|ci；空 = 无目标）"),
		field.Int("target_id").Optional().Comment("入口目标对象 ID（B3-01；与 target_type 成对出现）"),
		field.String("status").Default("running").MaxLen(16).Comment("running|completed|failed|cancelled"),
		field.String("model").Default("").MaxLen(128).Comment("本次运行使用的模型标识"),
		field.Text("budget_json").Default("").Comment("预算护栏快照（BP8：step/token/工具调用/超时/输出上限）"),
		field.String("error_code").Default("").MaxLen(64).Comment("失败分类码（budget_exceeded|provider_error|...）"),
		field.Time("started_at").Default(time.Now),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BotRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("steps", BotStep.Type),
		edge.To("events", BotEvent.Type),
	}
}

func (BotRun) Indexes() []ent.Index {
	return []ent.Index{
		// 租户维度前置（阶段一报告 §7-4：索引首列含租户）。
		index.Fields("tenant_id", "started_at"),
		index.Fields("tenant_id", "conversation_id"),
		index.Fields("tenant_id", "status"),
	}
}
