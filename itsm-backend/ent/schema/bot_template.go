package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotTemplate 是 Bot/助手模板（B2-01；字段清单见阶段一报告 §5.3(b)）。
//
// 语义：
//   - `slug` 在**租户内唯一**（多租户下不同租户可同名；全局唯一的做法会让 SaaS 无法自定义）；
//   - `status` 三态：draft（草稿，不可用）→ pilot（试点）→ ga（正式）；
//   - `entrypoints_json` 声明适用入口（chat|ticket|incident|ci|...），B2-04 选择器与 B3 场景共用；
//   - `risk_limit` 为该模板允许的最高风险（B2-02 交集门禁：授权 ∩ RBAC ∩ 风险上限 ∩ 入口）。
//
// 兼容默认：内置「默认助手」种子模板与既有行为等价（B2-01 的 SeedDefault）。
type BotTemplate struct{ ent.Schema }

func (BotTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.String("slug").NotEmpty().MaxLen(64).Comment("租户内唯一的模板标识（ASCII 短名）"),
		field.String("name").NotEmpty().MaxLen(128),
		field.String("audience").Default("internal").MaxLen(32).Comment("受众：internal|agent|customer|..."),
		field.String("risk_limit").Default("act_low").MaxLen(32).Comment("风险上限：read|plan|act_low|act_medium|act_high"),
		field.Text("entrypoints_json").Default("[]").Comment("适用入口白名单 JSON 数组"),
		field.String("system_prompt_ref").Default("").MaxLen(255).Comment("系统提示引用（技能/文件标识，不内联长文本）"),
		field.String("status").Default("draft").MaxLen(16).Comment("draft|pilot|ga"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BotTemplate) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("grants", BotToolGrant.Type),
	}
}

func (BotTemplate) Indexes() []ent.Index {
	return []ent.Index{
		// 租户维度前置（阶段一报告 §7-4）；slug 租户内唯一。
		index.Fields("tenant_id", "slug").Unique(),
		index.Fields("tenant_id", "status"),
	}
}
