package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotArtifact 是 plan/analysis/draft 类工具产出的**结构化产物**（B3-06）。
//
// 设计口径（阶段一报告 G11 与 §4.4 B3-06）：
//   - plan/analysis 类工具**只读不写业务库**：产物落本表，业务实体零写入；
//   - 归属与会话隔离：`owner_user_id`（发起人）+ `conversation_id`（会话）+ `tenant_id`（租户），
//     查询一律按「租户 + 归属」双条件收敛（跨租户/跨用户一律表现为不存在）；
//   - 证据引用：`evidence_json` 保存 RAG 来源 / CI 关系等可回溯线索（不内联大正文）。
type BotArtifact struct{ ent.Schema }

func (BotArtifact) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("owner_user_id").Positive().Comment("归属发起人（会话隔离：他人不可见）"),
		field.Int("conversation_id").Optional().Comment("会话归属（B0-03 同源；0/缺省 = 无会话）"),
		field.Int("run_id").Optional().Comment("运行归属（B1-01；0/缺省 = 无运行档案）"),
		field.String("kind").MaxLen(16).Comment("产物类型：plan|analysis|draft"),
		field.String("tool_name").MaxLen(64).Comment("产出该物件的工具名"),
		field.String("title").Default("").MaxLen(200),
		field.Text("content_json").Default("").Comment("结构化内容（JSON）"),
		field.Text("evidence_json").Default("").Comment("证据引用（RAG 来源 / CI 关系等，JSON）"),
		field.Time("created_at").Default(time.Now),
	}
}

func (BotArtifact) Indexes() []ent.Index {
	return []ent.Index{
		// 归属查询：租户 + 发起人 + 时间（列表默认倒序）。
		index.Fields("tenant_id", "owner_user_id", "created_at"),
		// 会话回溯与运行回溯（审计/证据面板）。
		index.Fields("tenant_id", "conversation_id"),
		index.Fields("tenant_id", "run_id"),
		// 按类型过滤（plan/analysis/draft）。
		index.Fields("tenant_id", "kind"),
	}
}
