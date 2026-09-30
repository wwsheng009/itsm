package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotToolGrant 是「模板 × 工具」的授权行（B2-01）。
//
// 语义：
//   - `(tenant_id, bot_id, tool_name)` 唯一；同一模板对同一工具只有一条授权；
//   - `risk_limit` 可在模板风险上限之内进一步收紧（B2-02 取交集时逐工具判定）；
//   - `args_policy_json` 预留参数策略（字段级 allow/deny，B2-02 起用）；
//   - 级联：模板删除时授权随删（**服务层事务内显式删除**，见 service/bot/admin.go；
//     不依赖驱动层 FK 行为，SQLite/Postgres 语义一致）。
type BotToolGrant struct{ ent.Schema }

func (BotToolGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("bot_id").Comment("所属模板（bot_templates.id）"),
		field.String("tool_name").NotEmpty().MaxLen(128).Comment("工具唯一名（含 mcp__<server>__<tool> 投影名）"),
		field.String("risk_limit").Default("act_low").MaxLen(32).Comment("该工具的风险上限（不超过模板上限）"),
		field.Text("args_policy_json").Default("").Comment("参数策略 JSON（预留，B2-02 起用）"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BotToolGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("bot", BotTemplate.Type).Ref("grants").Unique().Field("bot_id").Required(),
	}
}

func (BotToolGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "bot_id", "tool_name").Unique(),
		index.Fields("tenant_id", "tool_name"),
	}
}
