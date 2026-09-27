package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MCPServer 保存 MCP 外部工具服务器配置（M0-03；字段清单见分析报告 §5.2）。
//
// 安全默认（D7 默认拒绝）：enabled=false、trust_level=untrusted；凭据只存密文。
// 运行态字段（status / last_error / last_connected_at / protocol_version / server_info）
// 由 manager 回写，不是管理员输入；version 供管理端乐观锁使用。
// 所有字段均带默认值或 nullable，保证旧库升级向后兼容。
type MCPServer struct{ ent.Schema }

func (MCPServer) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.String("name").NotEmpty().MaxLen(32).Comment("稳定标识 [a-z0-9_-]{1,32}，用于投影名 mcp__<name>__<tool>"),
		field.String("display_name").Default("").MaxLen(100),
		field.String("transport").Default("streamable").MaxLen(20).Comment("streamable|sse|stdio（一期仅远程；stdio 为平台级）"),
		field.String("url").Optional().MaxLen(2048),
		field.Text("command").Optional().Comment("stdio 命令；平台级，一期不启用"),
		field.JSON("args", []string{}).Optional(),
		field.JSON("env", map[string]string{}).Optional(),
		field.String("working_dir").Optional().MaxLen(1024),
		field.Text("headers_encrypted").Optional().Comment("自定义请求头（AES-GCM 密文，只写不读回）"),
		field.String("credential_type").Default("none").MaxLen(32).Comment("none|static_header|oauth2"),
		field.Text("credential_encrypted").Optional().Comment("凭据密文（AES-GCM，只写不读回）"),
		field.String("trust_level").Default("untrusted").MaxLen(16).Comment("trusted|untrusted"),
		field.Bool("enabled").Default(false).Comment("服务器级总开关（默认关闭）"),
		field.String("status").Default("configured").MaxLen(32).Comment("configured|connecting|healthy|error|disabled（manager 回写）"),
		field.String("last_error").Default("").MaxLen(2000),
		field.Time("last_connected_at").Optional().Nillable(),
		field.String("protocol_version").Default("").MaxLen(32),
		field.Text("server_info").Optional().Comment("initialize 结果 JSON 快照（不含凭据）"),
		field.Int("timeout_ms").Default(30000).Positive().Comment("单次 tools/call 超时（默认 30s）"),
		field.Int("max_parallel_calls").Default(4).Positive(),
		field.Int("max_retry").Default(1).Min(0),
		field.Int("version").Default(1).Comment("管理端乐观锁"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MCPServer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tools", MCPServerTool.Type),
	}
}

func (MCPServer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "enabled"),
	}
}
