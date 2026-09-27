package mockserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolSpec 描述一个夹具工具：schema + 行为。
type toolSpec struct {
	tool    *mcp.Tool
	text    string
	errText string
	// slow 标记慢工具（调用时额外 sleep，配合 FaultSlowCall 或短超时用例）。
	slow bool
	// oversizeBytes > 0 时返回该长度的文本（超长输出注入）。
	oversizeBytes int
}

// result 构造默认成功结果。
func (s toolSpec) result() *mcp.CallToolResult {
	switch {
	case s.oversizeBytes > 0:
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", s.oversizeBytes)}}}
	case s.text != "":
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s.text}}}
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(`{"tool":%q,"ok":true}`, s.tool.Name)}},
		}
	}
}

// fixtureToolNames 记录每个夹具的工具名（切换时需要移除旧集合）。
var fixtureToolNames = map[FixtureSet][]string{
	FixtureDefault: {"list_issues", "create_issue", "huge_output", "bad name!", "slow_tool", "schema_probe"},
	FixtureMinimal: {"ping_tool"},
	FixtureSchemaA: {"schema_probe"},
	FixtureSchemaB: {"schema_probe"},
}

// fixturesFor 返回夹具的工具定义。
//
// 夹具覆盖（任务卡要求）：
//   - 只读工具（list_issues）与写工具（create_issue）；
//   - 超长输出（huge_output，长度可配）；
//   - 非法字符名（`bad name!`：空格与感叹号，投影应隔离）；
//   - 慢响应（slow_tool）；
//   - schema 变更对照（schema-a / schema-b 的 schema_probe 输入 schema 不同）；
//   - 重名场景：由**两个 mock 实例**各暴露同名工具实现（单实例内 MCP 不允许同名）。
func fixturesFor(set FixtureSet, oversizeBytes int) []toolSpec {
	objectSchema := json.RawMessage(`{"type":"object"}`)
	switch set {
	case FixtureMinimal:
		return []toolSpec{{
			tool: &mcp.Tool{Name: "ping_tool", Description: "noop", InputSchema: objectSchema},
			text: "pong",
		}}
	case FixtureSchemaA:
		return []toolSpec{{
			tool: &mcp.Tool{
				Name:        "schema_probe",
				Description: "schema v1",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			},
			text: `{"schema":"v1"}`,
		}}
	case FixtureSchemaB:
		return []toolSpec{{
			tool: &mcp.Tool{
				Name:        "schema_probe",
				Description: "schema v2（新增必填 limit）",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"},"limit":{"type":"integer"}},"required":["limit"]}`),
			},
			text: `{"schema":"v2"}`,
		}}
	default:
		return []toolSpec{
			{
				tool: &mcp.Tool{
					Name:        "list_issues",
					Description: "List issues (read-only)",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"state":{"type":"string"}}}`),
					Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
				},
				text: `{"issues":[]}`,
			},
			{
				tool: &mcp.Tool{
					Name:        "create_issue",
					Description: "Create an issue (write)",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
				},
				text: `{"created":true}`,
			},
			{
				tool: &mcp.Tool{
					Name:        "huge_output",
					Description: "Returns an oversized payload",
					InputSchema: objectSchema,
				},
				oversizeBytes: oversizeBytes,
			},
			{
				// 非法字符名：注册表应把该工具隔离（ReasonInvalidServerName/空名以外的非法字符
				// 由 canonical 投影处理），任何情况下都不得进入可执行工具面。
				tool: &mcp.Tool{
					Name:        "bad name!",
					Description: "Illegal characters in tool name",
					InputSchema: objectSchema,
				},
				text: `{"bad":true}`,
			},
			{
				tool: &mcp.Tool{
					Name:        "slow_tool",
					Description: "Slow tool (for call-timeout injection)",
					InputSchema: objectSchema,
				},
				slow: true,
				text: `{"slow":true}`,
			},
			{
				tool: &mcp.Tool{
					Name:        "schema_probe",
					Description: "schema v1",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
				},
				text: `{"schema":"v1"}`,
			},
		}
	}
}

// SlowDelay 返回夹具中慢工具的延迟（供用例设置客户端超时）。
func SlowDelay(set FixtureSet, delay time.Duration) time.Duration {
	for _, spec := range fixturesFor(set, 0) {
		if spec.slow {
			return delay
		}
	}
	return 0
}
