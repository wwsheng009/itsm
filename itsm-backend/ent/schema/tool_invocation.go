package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ToolInvocation stores each tool call during a conversation.
type ToolInvocation struct{ ent.Schema }

func (ToolInvocation) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Default(time.Now),
		field.Int("tenant_id"),
		field.Int("conversation_id").Optional(),
		field.String("tool_name"),
		field.Text("arguments").Default(""),
		field.Text("result").Optional().Nillable(),
		field.String("status").Default("success"),
		field.String("request_id").Optional(),
		field.Bool("needs_approval").Default(false),
		field.String("approval_state").Default("none"), // none|pending|approved|rejected
		field.String("approval_reason").Default(""),
		field.Int("approved_by").Optional(),
		field.Time("approved_at").Optional(),
		field.Bool("dry_run").Default(false),
		field.Text("error").Optional().Nillable(),
		// P2-6 AI 工具 RBAC 校验审计字段（向后兼容，所有字段均带默认值）
		field.Int("user_id").Optional().Comment("工具触发者用户 ID"),
		field.String("permission_check").Default("skipped").Comment("权限校验结果: passed|denied|skipped"),
		field.String("permission_reason").Default("").Comment("权限校验原因/拒绝原因"),
		field.String("role_snapshot").Default("").Comment("调用时角色快照，便于事后审计"),
		// —— MCP 接入字段（M0-03）——
		field.String("provider").Default("builtin").Comment("工具来源: builtin|mcp"),
		field.String("mcp_server_name").Default("").Comment("MCP 服务器标识（provider=mcp 时）"),
		field.String("mcp_raw_tool_name").Default("").Comment("MCP 原始工具名"),
		field.String("mcp_callable_name").Default("").Comment("MCP 投影名 mcp__<server>__<tool>"),
		field.Text("args_redacted").Optional().Comment("脱敏后的入参快照（展示/审计唯一来源；与 B0-02 input_redacted 统一命名）"),
		field.Text("output_summary").Optional().Comment("结果摘要（脱敏/截断）"),
		field.Int("duration_ms").Default(0).Comment("执行耗时（毫秒）"),
		field.String("error_code").Default("").Comment("错误码（分类后）"),
		// —— Bot 运行态字段（B0-02，随 M0-03 联合窗口一次加列，避免二次迁移）——
		field.Int("run_id").Optional().Comment("所属 bot_runs（B1-01 落表后关联）"),
		field.Int("step_id").Optional().Comment("所属 bot_steps"),
		field.String("risk").Default("").Comment("调用时风险快照: read|plan|act_low|act_medium|act_high"),
		field.String("category").Default("").Comment("工具分类快照"),
		field.String("target_type").Default("").Comment("目标对象类型（ticket/incident/ci/...）"),
		field.String("target_id").Default("").Comment("目标对象 ID"),
		field.String("support_ref").Default("").Comment("支撑信息引用（证据/来源）"),
		field.String("idempotency_key_hash").Optional().Comment("幂等键 hash（只存 hash；读工具为空）"),
		field.Time("expires_at").Optional().Nillable().Comment("确认单过期时间"),
		field.String("verify_state").Default("").Comment("执行后回读: pending|verified|failed|skipped"),
		field.String("verify_note").Default(""),
		field.Int("attempt_count").Default(0).Comment("队列消费尝试次数"),
		field.String("last_error_code").Default("").Comment("最近一次消费错误码"),
	}
}

func (ToolInvocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("conversation", Conversation.Type).Ref("tool_invocations").Unique().Field("conversation_id"),
		edge.From("user", User.Type).Ref("tool_invocations").Unique().Field("user_id"),
	}
}

func (ToolInvocation) Indexes() []ent.Index {
	return []ent.Index{
		// 幂等键唯一（作用域=租户；读工具为空 → 多 NULL 不冲突）。
		index.Fields("tenant_id", "idempotency_key_hash").Unique(),
		// 会话回溯（审计按会话查询）。
		index.Fields("tenant_id", "conversation_id"),
	}
}
