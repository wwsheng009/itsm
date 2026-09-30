// M1-03 契约测试：工具事件的 SSE 帧、字段与脱敏口径。
//
// 覆盖：
//  1. 四个事件名与 status 一一映射（未知状态不吞事件）；
//  2. 线上帧格式（`event:` / `data:` 前缀）与 JSON 字段名（前端解析依赖）；
//  3. summary 脱敏 + 截断（敏感键掩码 `****`、超限追加 `…(truncated)`）；
//  4. 错误码映射（Gate2 拒绝 / 未知工具 / 依赖不可得 / 队列不可用 / 外部工具码）。
//
// 说明：模型真实触发工具的端到端事件序列由 M1-10 联调覆盖（本包无法脱离 RAG 主链路
// 起真实工具循环）；此处锁定的是对外契约本身。
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedFrame 记录一次 writeEvent 调用的原始帧（模拟 handler 的帧格式）。
type capturedFrame struct {
	event   string
	payload interface{}
}

func collectToolEvents(events ...ToolStreamEvent) []capturedFrame {
	var frames []capturedFrame
	write := func(event string, payload interface{}) {
		frames = append(frames, capturedFrame{event: event, payload: payload})
	}
	for _, ev := range events {
		writeToolEvent(write, ev)
	}
	return frames
}

func TestWriteToolEvent_NameMapping(t *testing.T) {
	base := ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read"}

	frames := collectToolEvents(
		ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: ToolEventStatusStarted},
		ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: ToolEventStatusDone},
		ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: ToolEventStatusFailed},
		ToolStreamEvent{Tool: "mcp__mock__create_issue", Provider: "mcp", Server: "mock", Phase: "write", Status: ToolEventStatusPending},
		ToolStreamEvent{Tool: "odd", Provider: "builtin", Phase: "read", Status: "weird-status"},
	)
	// B1-03：pending 双发（approval_pending 旧名 + confirmation_required v2 新名），其余保持单发。
	require.Len(t, frames, 6)
	assert.Equal(t, "tool_call_started", frames[0].event)
	assert.Equal(t, "tool_call_finished", frames[1].event)
	assert.Equal(t, "tool_call_failed", frames[2].event)
	assert.Equal(t, SSEEventApprovalPending, frames[3].event, "旧名先发（旧客户端命中后忽略未知事件）")
	assert.Equal(t, SSEEventConfirmationRequired, frames[4].event, "v2 新名随发")
	assert.Equal(t, "tool_call_started", frames[5].event, "未知状态不得被静默吞掉")

	// 载荷必须原样透传（事件名映射不改写字段，避免前端看到两套口径）。
	assert.Equal(t, base.Tool, frames[0].payload.(ToolStreamEvent).Tool)

	// 双发语义：旧名载荷保持 v1 原样（无 v 字段）；新名载荷是同一事件 + `v:2`。
	legacy, ok := frames[3].payload.(ToolStreamEvent)
	require.True(t, ok, "旧名载荷必须是原事件结构体（v1 契约不变）")
	assert.Equal(t, "mcp__mock__create_issue", legacy.Tool)
	v2, ok := frames[4].payload.(map[string]interface{})
	require.True(t, ok, "v2 事件载荷必须是对象信封")
	assert.Equal(t, SSEProtocolVersionV2, v2["v"])
	assert.Equal(t, "mcp__mock__create_issue", v2["tool"])
	assert.Equal(t, "pending", v2["status"])
}

func TestToolStreamEvent_JSONContract(t *testing.T) {
	// 完整事件：字段名与省略规则即对外契约。
	done := ToolStreamEvent{
		ID:         7,
		Tool:       "mcp__mock__list_issues",
		Provider:   "mcp",
		Server:     "mock",
		Phase:      "read",
		Status:     ToolEventStatusDone,
		Summary:    `{"issues":[]}`,
		DurationMs: 12,
	}
	encoded, err := json.Marshal(done)
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, float64(7), decoded["id"])
	assert.Equal(t, "mcp__mock__list_issues", decoded["tool"])
	assert.Equal(t, "mcp", decoded["provider"])
	assert.Equal(t, "mock", decoded["server"])
	assert.Equal(t, "read", decoded["phase"])
	assert.Equal(t, "done", decoded["status"])
	assert.Equal(t, `{"issues":[]}`, decoded["summary"])
	assert.Equal(t, float64(12), decoded["durationMs"])
	assert.NotContains(t, decoded, "errorCode")

	// 最小事件：无 id/server/summary/durationMs/errorCode 时这些键不出现（前端按可选处理）。
	started := ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: ToolEventStatusStarted}
	encoded, err = json.Marshal(started)
	require.NoError(t, err)
	decoded = map[string]interface{}{}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, map[string]interface{}{
		"tool": "list_tickets", "provider": "builtin", "phase": "read", "status": "started",
	}, decoded, "恒在字段仅 tool/provider/phase/status")

	// failed 事件：errorCode 恒在（前端据此分流），summary 为空则不出现。
	failed := ToolStreamEvent{Tool: "delete_ci", Provider: "builtin", Phase: "write", Status: ToolEventStatusFailed, ErrorCode: "tool_permission_denied"}
	encoded, err = json.Marshal(failed)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"errorCode":"tool_permission_denied"`)
	assert.NotContains(t, string(encoded), `"summary"`)

	// 帧安全：载荷必须单行（SSE 只写一个 `data:` 前缀），含换行的摘要只做 JSON 转义。
	multi := ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: ToolEventStatusDone, Summary: "line1\nline2"}
	encoded, err = json.Marshal(multi)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "\n", "SSE 载荷必须单行")
	assert.Contains(t, string(encoded), `\n`)
}

func TestToolEventSummary_RedactsAndTruncates(t *testing.T) {
	// 敏感键掩码：与 args_redacted / 审计摘要同一套规则。
	summary := toolEventSummary(map[string]interface{}{
		"ticket": map[string]interface{}{"title": "打印机故障", "token": "s3cr3t-value", "password": "p@ss"},
	}, ToolEventSummaryLimit)
	assert.Contains(t, summary, "打印机故障")
	assert.NotContains(t, summary, "s3cr3t-value")
	assert.NotContains(t, summary, "p@ss")
	assert.Contains(t, summary, `"token":"****"`)
	assert.Contains(t, summary, `"password":"****"`)

	// 超限截断：追加统一标记，且输出仍是可读的 JSON 前缀。
	long := strings.Repeat("x", ToolEventSummaryLimit*2)
	truncated := toolEventSummary(long, ToolEventSummaryLimit)
	assert.Contains(t, truncated, "…(truncated)")
	assert.LessOrEqual(t, len([]rune(truncated)), ToolEventSummaryLimit+len([]rune("…(truncated)")))

	// nil / 不可编码值：返回空串而不是 "null"（前端不渲染空摘要）。
	assert.Equal(t, "", toolEventSummary(nil, 0))
	assert.Equal(t, "", toolEventSummary(func() {}, 0))
}

func TestToolEventErrorCode_Mapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"gate2 拒绝", fmt.Errorf("%w: role=agent lacks mcp:write", ErrToolPermissionDenied), "tool_permission_denied"},
		{"未知工具", ErrUnknownTool, "unknown_tool"},
		{"依赖不可得", ErrToolUnavailable, "tool_unavailable"},
		{"队列不可用", ErrToolQueueUnavailable, "tool_queue_unavailable"},
		{"审批状态机", ErrInvocationNotPending, "invocation_not_pending"},
		{"外部工具码", providerCodedError{code: "server_error"}, "server_error"},
		{"未分类", fmt.Errorf("boom"), "tool_execution_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toolEventErrorCode(tc.err))
		})
	}
}

// providerCodedError 模拟外部 provider 的错误类型（结构化接口，无需反向依赖 mcp 包）。
type providerCodedError struct{ code string }

func (e providerCodedError) Error() string     { return "provider error: " + e.code }
func (e providerCodedError) ErrorCode() string { return e.code }
