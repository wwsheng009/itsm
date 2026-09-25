package protocol_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
)

// ---- PA-3：openai_responses 适配器（官方 Responses API 载体） ----

func TestOpenAIResponsesAdapterMetadata(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()
	assert.Equal(t, protocol.ProtocolOpenAIResponses, adapter.Name())
	assert.Equal(t, "/v1/responses", adapter.GetAPIPath())

	// 推理模型口径与 openai_chat_completions 同源（codex / gpt-5 / o1-o5）。
	for _, model := range []string{"gpt-5-codex", "gpt-5", "o3-mini", "models/o4-mini"} {
		assert.True(t, adapter.IsReasoningModel(model), model)
	}
	for _, model := range []string{"gpt-4o-mini", "deepseek-chat", ""} {
		assert.False(t, adapter.IsReasoningModel(model), model)
	}
}

func TestOpenAIResponsesAdapterBuildHeaders(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	headers := adapter.BuildHeaders(protocol.AdapterConfig{APIKey: "  sk-test  "})
	assert.Equal(t, "application/json", headers["Content-Type"])
	assert.Equal(t, "Bearer sk-test", headers["Authorization"])

	// apiKey 为空：不下发 Authorization（本机/网关代理场景）。
	assert.NotContains(t, adapter.BuildHeaders(protocol.AdapterConfig{}), "Authorization")

	// 调用方附加 headers 大小写不敏感覆盖，且可覆写鉴权头（企业网关常换 x-api-key）。
	headers = adapter.BuildHeaders(protocol.AdapterConfig{
		APIKey:  "sk-test",
		Headers: map[string]string{"authorization": "Bearer sk-override", "x-trace": "t-1"},
	})
	assert.Equal(t, "Bearer sk-override", headers["Authorization"])
	assert.Equal(t, "t-1", headers["x-trace"])
}

func TestOpenAIResponsesBuildRequest(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:       "gpt-4o-mini",
		MaxTokens:   1024,
		Temperature: 0.3,
		Messages: []protocol.Message{
			{Role: "system", Content: "你是 ITSM 助手"},
			{Role: "developer", Content: "保持简洁"},
			{Role: "user", Content: "查一下工单 T-1"},
			{Role: "assistant", Content: "我来查", ToolCalls: []protocol.ToolCall{
				{ID: "call_1", Name: "query_ticket", Arguments: `{"id":"T-1"}`},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"status":"open"}`},
			{Role: "system", Content: "补充约束：只读"},
		},
	})

	assert.Equal(t, "gpt-4o-mini", body["model"])
	assert.Equal(t, false, body["store"], "itsm 每次重放完整历史，不启用服务端存储")
	assert.EqualValues(t, 1024, body["max_output_tokens"])
	assert.Equal(t, 0.3, body["temperature"])
	assert.NotContains(t, body, "stream", "非流式调用不下发 stream")

	// 前导 system/developer 合并为顶层 instructions；其后的 system 保留在 input 中。
	assert.Equal(t, "你是 ITSM 助手\n\n保持简洁", body["instructions"])

	input, ok := body["input"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, input, 5)

	assert.Equal(t, map[string]any{"type": "message", "role": "user", "content": []map[string]any{
		{"type": "input_text", "text": "查一下工单 T-1"},
	}}, input[0])

	assert.Equal(t, map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{
		{"type": "output_text", "text": "我来查"},
	}}, input[1])
	assert.Equal(t, map[string]any{
		"type": "function_call", "call_id": "call_1", "name": "query_ticket", "arguments": `{"id":"T-1"}`,
	}, input[2])
	assert.Equal(t, map[string]any{
		"type": "function_call_output", "call_id": "call_1", "output": `{"status":"open"}`,
	}, input[3])
	assert.Equal(t, map[string]any{"type": "message", "role": "developer", "content": []map[string]any{
		{"type": "input_text", "text": "补充约束：只读"},
	}}, input[4])

	// 空 messages 的 input 是空数组（合法装载），而不是 null。
	empty := adapter.BuildRequest(protocol.RequestConfig{Model: "gpt-4o-mini"})
	assert.Equal(t, []map[string]any{}, empty["input"])
	assert.NotContains(t, empty, "instructions")
	assert.NotContains(t, empty, "max_output_tokens", "max_tokens<=0 时不下发")
	assert.Equal(t, 0.0, empty["temperature"], "temperature 始终下发（0 亦为合法采样值，与 chat 适配器同口径）")
}

func TestOpenAIResponsesBuildRequestToolsAndToolChoice(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()
	parameters := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model: "gpt-4o-mini",
		Messages: []protocol.Message{
			{Role: "user", Content: "hi", Tools: []protocol.Tool{{Name: "from_message"}}},
		},
		Tools: []protocol.Tool{
			{Name: "query_ticket", Description: "查询工单", Parameters: parameters},
			{Name: "no_schema"},
		},
		// Chat 风格 tool_choice 必须展平为 Responses 形态（严格上游以 400 拒绝嵌套 function）。
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "query_ticket"}},
	})

	tools, ok := body["tools"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, tools, 3, "消息携带的工具在前、调用侧显式声明在后")
	assert.Equal(t, map[string]any{"type": "function", "name": "from_message"}, tools[0])
	assert.Equal(t, map[string]any{
		"type": "function", "name": "query_ticket", "description": "查询工单", "parameters": parameters,
	}, tools[1])
	assert.Equal(t, map[string]any{"type": "function", "name": "no_schema"}, tools[2])
	assert.NotContains(t, tools[1], "function", "Responses tools 为扁平结构")

	assert.Equal(t, map[string]any{"type": "function", "name": "query_ticket"}, body["tool_choice"])

	// Responses 风格与字符串形态原样透传；不可识别的形态不下发。
	responsesStyle := adapter.BuildRequest(protocol.RequestConfig{
		Model:      "gpt-4o-mini",
		ToolChoice: map[string]any{"type": "function", "name": "query_ticket"},
	})
	assert.Equal(t, map[string]any{"type": "function", "name": "query_ticket"}, responsesStyle["tool_choice"])

	for _, choice := range []any{"auto", "required", "none"} {
		built := adapter.BuildRequest(protocol.RequestConfig{Model: "gpt-4o-mini", ToolChoice: choice})
		assert.Equal(t, choice, built["tool_choice"])
	}

	unusable := adapter.BuildRequest(protocol.RequestConfig{
		Model:      "gpt-4o-mini",
		ToolChoice: map[string]any{"type": "function"},
	})
	assert.NotContains(t, unusable, "tool_choice")
}

func TestOpenAIResponsesBuildRequestReasoningModelSuppressesSampling(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:          "gpt-5-codex",
		Stream:         true,
		MaxTokens:      4096,
		Temperature:    0.7,
		ReasoningModel: true,
		Messages:       []protocol.Message{{Role: "user", Content: "hi"}},
	})
	assert.True(t, adapter.IsReasoningModel("gpt-5-codex"))
	assert.NotContains(t, body, "temperature", "推理模型不下发采样参数")
	assert.Equal(t, true, body["stream"])
	assert.EqualValues(t, 4096, body["max_output_tokens"])
}

func TestOpenAIResponsesProcessResponse(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	result := adapter.ProcessResponse(map[string]any{
		"id":     "resp_1",
		"status": "completed",
		"output": []any{
			map[string]any{"type": "reasoning", "summary": []any{
				map[string]any{"type": "summary_text", "text": "先查工单状态。"},
			}},
			map[string]any{"type": "message", "role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": "工单 T-1 "},
				map[string]any{"type": "output_text", "text": "处于 open 状态。"},
			}},
			map[string]any{
				"type": "function_call", "call_id": "call_7", "name": "query_ticket", "arguments": `{"id":"T-1"}`,
			},
		},
	})

	assert.Equal(t, "工单 T-1 处于 open 状态。", result.Content)
	assert.Equal(t, "先查工单状态。", result.Reasoning)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "call_7", Name: "query_ticket", Arguments: `{"id":"T-1"}`}, result.ToolCalls[0])
	assert.True(t, result.HasToolCalls())
	assert.Equal(t, "tool_calls", result.FinishReason)

	// 无工具调用的 completed → stop。
	plain := adapter.ProcessResponse(map[string]any{
		"status": "completed",
		"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "output_text", "text": "完成"},
		}}},
	})
	assert.Equal(t, "stop", plain.FinishReason)

	// 兼容上游给出的 stop_reason 优先于 status 推断。
	stopped := adapter.ProcessResponse(map[string]any{
		"status":      "completed",
		"stop_reason": "end_turn",
		"output":      []any{},
	})
	assert.Equal(t, "end_turn", stopped.FinishReason)

	// incomplete：正文按已累积部分返回，finish_reason 取 incomplete_details.reason。
	incomplete := adapter.ProcessResponse(map[string]any{
		"status":             "incomplete",
		"incomplete_details": map[string]any{"reason": "max_output_tokens"},
		"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "output_text", "text": "被截断的正文"},
		}}},
	})
	assert.Equal(t, "被截断的正文", incomplete.Content)
	assert.Equal(t, "max_output_tokens", incomplete.FinishReason)

	// 未知名/未知 type 的项被忽略，call_id 缺省回退项 id；两项都缺的 function_call 丢弃。
	edge := adapter.ProcessResponse(map[string]any{
		"status": "completed",
		"output": []any{
			map[string]any{"type": "web_search_call", "id": "ws_1"},
			map[string]any{"type": "function_call", "id": "fc_1", "name": "query_ticket"},
			map[string]any{"type": "function_call", "call_id": "call_x"},
			map[string]any{"type": "function_call", "name": "no_id"},
		},
	})
	require.Len(t, edge.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "fc_1", Name: "query_ticket", Arguments: ""}, edge.ToolCalls[0])
}

func TestOpenAIResponsesHandleResponseNonStream(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	result, err := adapter.HandleResponse(false, strings.NewReader(
		`{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant",`+
			`"content":[{"type":"output_text","text":"你好"}]}]}`),
		protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, "你好", result.Content)
	assert.Equal(t, "stop", result.FinishReason)

	// 失败态（status=failed + error 对象）→ 带内 *ProtocolError，不重试。
	_, err = adapter.HandleResponse(false, strings.NewReader(
		`{"id":"resp_2","status":"failed","error":{"code":"invalid_api_key","message":"Incorrect API key"}}`),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	protocolErr, ok := err.(*protocol.ProtocolError)
	require.True(t, ok)
	assert.Equal(t, protocol.ProtocolOpenAIResponses, protocolErr.Protocol)
	assert.Equal(t, "invalid_api_key", protocolErr.Code)
	assert.Contains(t, protocolErr.Message, "Incorrect API key")
	assert.False(t, protocolErr.Retryable(), "带内错误不可重试")

	// 顶层 error 对象（网关代理形态）同样映射。
	_, err = adapter.HandleResponse(false, strings.NewReader(
		`{"error":{"type":"rate_limit_error","message":"slow down"}}`), protocol.StreamCallbacks{})
	require.Error(t, err)

	// 非法 JSON 直接失败，不静默返回空结果。
	_, err = adapter.HandleResponse(false, strings.NewReader(`{"output":`), protocol.StreamCallbacks{})
	require.Error(t, err)
}

const openAIResponsesStreamBody = "event: response.created\n" +
	`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}` + "\n\n" +
	"event: response.reasoning_summary_text.delta\n" +
	`data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","index":0,"delta":"思考…"}` + "\n\n" +
	"event: response.output_item.added\n" +
	`data: {"type":"response.output_item.added","output_index":1,"item_id":"fc_1","item":{"type":"function_call","id":"fc_1","call_id":"call_9","name":"query_ticket","arguments":""}}` + "\n\n" +
	"event: response.function_call_arguments.delta\n" +
	`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"id\":"}` + "\n\n" +
	"event: response.function_call_arguments.delta\n" +
	`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"\"T-1\"}"}` + "\n\n" +
	"event: response.output_item.done\n" +
	`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_9","name":"query_ticket","arguments":"{\"id\":\"T-1\"}"}}` + "\n\n" +
	"event: response.output_text.delta\n" +
	`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":2,"delta":"工单 "}` + "\n\n" +
	"event: response.output_text.delta\n" +
	`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":2,"delta":"T-1 是 open"}` + "\n\n" +
	"event: response.completed\n" +
	`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[]}}` + "\n\n"

func TestOpenAIResponsesHandleResponseStream(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	var textSegments, reasoningSegments []string
	result, err := adapter.HandleResponse(true, strings.NewReader(openAIResponsesStreamBody), protocol.StreamCallbacks{
		OnText:      func(text string) { textSegments = append(textSegments, text) },
		OnReasoning: func(reasoning string) { reasoningSegments = append(reasoningSegments, reasoning) },
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"工单 ", "T-1 是 open"}, textSegments, "正文增量按事件顺序下发")
	assert.Equal(t, []string{"思考…"}, reasoningSegments)
	assert.Equal(t, "工单 T-1 是 open", result.Content)
	assert.Equal(t, "思考…", result.Reasoning)
	assert.Equal(t, "tool_calls", result.FinishReason)

	// 工具调用按增量累积：done 事件作为权威值补齐，重复内容不叠加。
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "call_9", Name: "query_ticket", Arguments: `{"id":"T-1"}`}, result.ToolCalls[0])
}

func TestOpenAIResponsesHandleResponseStreamSnapshotRecovery(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	// 上游只给出终态快照（无增量事件）：从 response.completed 的 output 补齐正文与工具调用。
	body := "event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"推理摘要"}]},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"快照正文"}]},` +
		`{"type":"function_call","call_id":"call_snap","name":"query_ticket","arguments":"{}"}]}}` + "\n\n"

	var textSegments []string
	result, err := adapter.HandleResponse(true, strings.NewReader(body), protocol.StreamCallbacks{
		OnText: func(text string) { textSegments = append(textSegments, text) },
	})
	require.NoError(t, err)
	assert.Equal(t, "快照正文", result.Content)
	assert.Equal(t, []string{"快照正文"}, textSegments, "快照补齐的正文同样下发回调")
	assert.Equal(t, "推理摘要", result.Reasoning)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "call_snap", Name: "query_ticket", Arguments: "{}"}, result.ToolCalls[0])
	assert.Equal(t, "tool_calls", result.FinishReason)

	// 增量已覆盖时快照不重复追加（正文只出现一次）。
	result, err = adapter.HandleResponse(true, strings.NewReader(
		"event: response.output_text.delta\n"+
			`data: {"type":"response.output_text.delta","index":0,"delta":"增量"}`+"\n\n"+
			"event: response.completed\n"+
			`data: {"type":"response.completed","response":{"status":"completed","output":[`+
			`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"增量"}]}]}}`+"\n\n"),
		protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, "增量", result.Content)
}

func TestOpenAIResponsesHandleResponseStreamErrors(t *testing.T) {
	adapter := protocol.NewOpenAIResponsesAdapter()

	// response.failed：带内错误（StatusCode=0），不可重试。
	_, err := adapter.HandleResponse(true, strings.NewReader(
		"event: response.failed\n"+
			`data: {"type":"response.failed","response":{"id":"resp_3","status":"failed","error":{"code":"server_error","message":"boom"}}}`+"\n\n"),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	protocolErr, ok := err.(*protocol.ProtocolError)
	require.True(t, ok)
	assert.Equal(t, "server_error", protocolErr.Code)
	assert.Contains(t, protocolErr.Message, "boom")
	assert.Equal(t, 0, protocolErr.StatusCode)
	assert.False(t, protocolErr.Retryable())

	// 顶层 error 事件（流内无 HTTP 语义）同样映射。
	_, err = adapter.HandleResponse(true, strings.NewReader(
		"event: error\n"+`data: {"type":"error","code":"invalid_request","message":"bad input"}`+"\n\n"),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	protocolErr, ok = err.(*protocol.ProtocolError)
	require.True(t, ok)
	assert.Equal(t, "invalid_request", protocolErr.Code)

	// 坏帧（非法 JSON）直接失败。
	_, err = adapter.HandleResponse(true, strings.NewReader(
		"event: response.output_text.delta\ndata: {\"delta\":\n\n"), protocol.StreamCallbacks{})
	require.Error(t, err)

	// incomplete 不是错误：正文照常返回，finish_reason 取 details.reason。
	result, err := adapter.HandleResponse(true, strings.NewReader(
		"event: response.incomplete\n"+
			`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`+"\n\n"),
		protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, "max_output_tokens", result.FinishReason)
}
