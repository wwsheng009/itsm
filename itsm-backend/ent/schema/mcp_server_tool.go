package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MCPServerTool 缓存单个 MCP 工具的元数据与治理状态（M0-03；字段清单见分析报告 §5.2）。
//
// 三态分离（D6）：enabled=管理位、healthy=运行位、quarantined=隔离位；
// 暴露条件 = enabled && healthy && !quarantined（effective 由上层派生）。
// 安全默认（D7）：read_only=false、risk=high（未知工具按最高风险）、enabled=false。
type MCPServerTool struct{ ent.Schema }

func (MCPServerTool) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("server_id"),
		field.String("raw_name").NotEmpty().MaxLen(256).Comment("MCP 服务器返回的原始工具名"),
		field.String("callable_name").NotEmpty().MaxLen(64).Comment("投影名 mcp__<server>__<tool>（registry.CanonicalToolName）"),
		field.Text("description").Default(""),
		field.Text("input_schema").Optional().Comment("原始 inputSchema JSON"),
		field.String("schema_hash").Default("").MaxLen(64).Comment("变更检测；变化 → 隔离待复核"),
		field.Bool("read_only").Default(false),
		field.String("risk").Default("high").MaxLen(16).Comment("read|plan|act_low|act_medium|act_high（默认 high=最保守）"),
		field.String("category").Default("").MaxLen(32),
		field.Bool("enabled").Default(false).Comment("管理位：默认不启用（D7）"),
		field.Bool("healthy").Default(false).Comment("运行位：discovery / 健康检查回写"),
		field.Bool("quarantined").Default(false),
		field.String("quarantine_reason").Default("").MaxLen(500),
		field.String("last_error").Default("").MaxLen(2000),
		field.Time("discovered_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MCPServerTool) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("server", MCPServer.Type).Ref("tools").Unique().Field("server_id").Required(),
	}
}

func (MCPServerTool) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "server_id", "raw_name").Unique(),
		index.Fields("tenant_id", "callable_name").Unique(),
		index.Fields("tenant_id", "enabled"),
	}
}
