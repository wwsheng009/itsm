// B1-03 契约测试：SSE 单一事件注册表、v2 信封、新旧兼容与运行事件映射。
//
// 覆盖：
//  1. 注册表完整性：发送点使用的事件名全部已登记；名字唯一；版本/家族/别名自洽；
//  2. v2 信封：对象载荷附加 `v:2`，v1 载荷字节不变（兼容承诺）；
//  3. 运行事件映射：run_started/step 外发；tool_call/run_finished 不外发（避免双份）；
//  4. error 载荷：预算超限与外部错误码 → errorCode；
//  5. 旧客户端模拟：只认 v1 名字的解析器在混入 v2 事件时行为不变（未知事件忽略）。
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/service/bot"
)

func TestSSERegistry_AllEmittedEventsRegistered(t *testing.T) {
	// 发送点清单（新增事件必须同步登记，否则本测试失败）。
	emitted := []string{
		SSEEventSources, SSEEventDelta, SSEEventDone, SSEEventError,
		SSEEventToolCallStarted, SSEEventToolCallFinished, SSEEventToolCallFailed,
		SSEEventApprovalPending, SSEEventConfirmationRequired,
		SSEEventRunStarted, SSEEventStep,
	}
	for _, name := range emitted {
		assert.True(t, IsKnownSSEEvent(name), "事件 %s 必须已登记", name)
	}
	// 未登记的名字不得被误判为已知（未知事件忽略策略的前提）。
	assert.False(t, IsKnownSSEEvent("no_such_event"))
	assert.False(t, IsKnownSSEEvent(""))
}

func TestSSERegistry_NamesUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range SSEEventSpecs() {
		require.NotEmpty(t, spec.Name)
		require.Contains(t, []int{1, 2}, spec.Version, "%s 版本必须是 1 或 2", spec.Name)
		require.NotEmpty(t, spec.Payload, "%s 必须声明载荷字段（注册表即文档）", spec.Name)
		assert.False(t, seen[spec.Name], "事件名 %s 重复登记", spec.Name)
		seen[spec.Name] = true
		for _, alias := range spec.Aliases {
			assert.False(t, seen[alias], "别名 %s 与已登记事件名冲突", alias)
			seen[alias] = true
		}
	}
	// 家族自洽：tool_call 三态同族；confirmation 双名同族且互为别名。
	families := map[string][]string{}
	for _, spec := range SSEEventSpecs() {
		if spec.Family != "" {
			families[spec.Family] = append(families[spec.Family], spec.Name)
		}
	}
	assert.ElementsMatch(t, []string{SSEEventToolCallStarted, SSEEventToolCallFinished, SSEEventToolCallFailed}, families[SSEFamilyToolCall])
	// confirmation 家族：v2 正名登记 + v1 旧名作为别名（同一语义不重复登记）。
	assert.ElementsMatch(t, []string{SSEEventConfirmationRequired}, families[SSEFamilyConfirmation])
	assert.Contains(t, sseAliasesOf(SSEEventConfirmationRequired), SSEEventApprovalPending)
	assert.True(t, IsKnownSSEEvent(SSEEventApprovalPending), "旧名必须仍被识别为已知事件")
	// 排序列出（含别名）不 panic 且数量一致。
	assert.Len(t, sortedSSEEventNames(), len(seen))
}

func TestSSEEnvelopeV2_AddsVersionAndPreservesFields(t *testing.T) {
	envelope := sseEnvelopeV2(map[string]any{"stepIndex": 3, "type": "llm"})
	assert.Equal(t, SSEProtocolVersionV2, envelope["v"])
	// JSON 往返后数值统一为 float64（v2 信封的实现细节，前端按 number 处理）。
	assert.Equal(t, float64(3), envelope["stepIndex"])
	assert.Equal(t, "llm", envelope["type"])

	// 结构体载荷同样可包装（tool 事件双发路径）。
	envelope = sseEnvelopeV2(ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: "pending"})
	assert.Equal(t, SSEProtocolVersionV2, envelope["v"])
	assert.Equal(t, "list_tickets", envelope["tool"])
	assert.Equal(t, "pending", envelope["status"])

	// 非对象载荷回落为 {v, value}，不丢原始值。
	envelope = sseEnvelopeV2("plain")
	assert.Equal(t, SSEProtocolVersionV2, envelope["v"])
	assert.Equal(t, `"plain"`, envelope["value"])

	// nil 载荷只带版本字段。
	assert.Equal(t, map[string]interface{}{"v": SSEProtocolVersionV2}, sseEnvelopeV2(nil))
}

func TestWriteSSERunEvent_Mapping(t *testing.T) {
	type frame struct {
		event   string
		payload interface{}
	}
	var frames []frame
	write := func(event string, payload interface{}) { frames = append(frames, frame{event, payload}) }

	assert.True(t, writeSSERunEvent(write, "run_started", map[string]interface{}{"runId": 7, "entrypoint": "chat"}))
	assert.True(t, writeSSERunEvent(write, "step", map[string]interface{}{"runId": 7, "stepIndex": 0, "type": "llm"}))
	// 不外发：tool_call 由 tool_call_* 三态承载；run_finished 由 done/error 承载。
	assert.False(t, writeSSERunEvent(write, "tool_call", map[string]interface{}{"tool": "x"}))
	assert.False(t, writeSSERunEvent(write, "run_finished", map[string]interface{}{"status": "completed"}))

	require.Len(t, frames, 2)
	assert.Equal(t, SSEEventRunStarted, frames[0].event)
	assert.Equal(t, SSEEventStep, frames[1].event)
	for _, f := range frames {
		payload, ok := f.payload.(map[string]interface{})
		require.True(t, ok, "v2 运行事件必须是对象信封")
		assert.Equal(t, SSEProtocolVersionV2, payload["v"])
		// JSON 往返后数值为 float64（与 sseEnvelopeV2 的实现一致）。
		assert.Equal(t, float64(7), payload["runId"])
	}
}

func TestSSEErrorPayload_Codes(t *testing.T) {
	// 预算超限：稳定错误码（含 wrapped 形态）。
	budgetErr := fmt.Errorf("本次对话已超出工具调用预算: %w", bot.ErrBudgetExceeded)
	payload := sseErrorPayload(budgetErr)
	assert.Equal(t, "budget_exceeded", payload["errorCode"])
	assert.Contains(t, payload["message"], "预算")

	// 外部错误类型：透传 ErrorCode()。
	payload = sseErrorPayload(providerCodedError{code: "server_error"})
	assert.Equal(t, "server_error", payload["errorCode"])

	// 未分类错误：只有 message（不得虚构错误码）。
	payload = sseErrorPayload(errors.New("boom"))
	assert.Equal(t, "boom", payload["message"])
	assert.NotContains(t, payload, "errorCode")
}

// TestSSERegistry_LegacyClientIgnoresUnknownEvents 模拟旧客户端解析器（只认 v1 事件名），
// 在混入 v2 事件后行为必须不变：忽略未知事件、正常消费 delta/done、不抛错。
func TestSSERegistry_LegacyClientIgnoresUnknownEvents(t *testing.T) {
	legacyHandled := map[string]bool{
		SSEEventSources: true, SSEEventDelta: true, SSEEventDone: true, SSEEventError: true,
		SSEEventToolCallStarted: true, SSEEventToolCallFinished: true, SSEEventToolCallFailed: true,
		SSEEventApprovalPending: true,
	}
	type frame struct {
		event   string
		payload interface{}
	}
	stream := []frame{
		{SSEEventRunStarted, sseEnvelopeV2(map[string]interface{}{"runId": 1})},
		{SSEEventDelta, map[string]string{"content": "你好"}},
		{SSEEventApprovalPending, ToolStreamEvent{Tool: "mcp__mock__create_issue", Status: "pending"}},
		{SSEEventConfirmationRequired, sseEnvelopeV2(ToolStreamEvent{Tool: "mcp__mock__create_issue", Status: "pending"})},
		{SSEEventStep, sseEnvelopeV2(map[string]interface{}{"stepIndex": 0, "type": "llm"})},
		{"future_event_unknown_to_this_client", map[string]interface{}{"v": 3}},
		{SSEEventDone, map[string]int{"conversationId": 42}},
	}

	var text string
	var convID int
	unknown := 0
	for _, f := range stream {
		raw, err := json.Marshal(f.payload)
		require.NoError(t, err, "载荷必须可序列化（单行，无换行）")
		assert.NotContains(t, string(raw), "\n")

		if !legacyHandled[f.event] {
			unknown++ // 未知事件：静默忽略
			continue
		}
		switch f.event {
		case SSEEventDelta:
			var payload map[string]string
			require.NoError(t, json.Unmarshal(raw, &payload))
			text += payload["content"]
		case SSEEventDone:
			var payload map[string]int
			require.NoError(t, json.Unmarshal(raw, &payload))
			convID = payload["conversationId"]
		}
	}
	// 旧客户端看不到的事件：run_started / confirmation_required / step / future（approval_pending 已知）。
	assert.Equal(t, 4, unknown, "未知事件一律静默忽略，不中断、不误判为错误")
	assert.Equal(t, "你好", text)
	assert.Equal(t, 42, convID)
}
