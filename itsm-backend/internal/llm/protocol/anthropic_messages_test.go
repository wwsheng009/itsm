package protocol_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
)

func TestAnthropicBuildRequestDefaultVariant(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:       "claude-sonnet-4",
		MaxTokens:   1024,
		Temperature: 0.3,
		Messages: []protocol.Message{
			{Role: "system", Content: "你是 IT 助手"},
			{Role: "user", Content: "你好"},
		},
	})

	assert.Equal(t, "claude-sonnet-4", body["model"])
	assert.Equal(t, 1024, body["max_tokens"], "默认变体使用官方 snake_case 字段")
	assert.NotContains(t, body, "maxTokens")
	assert.Equal(t, "你是 IT 助手", body["system"], "system 消息提升到顶层 system 字段")
	assert.Equal(t, 0.3, body["temperature"])
	assert.NotContains(t, body, "stream", "非流式请求不下发 stream")

	messages, ok := body["messages"].([]map[string]any)
	require.True(t, ok, "messages 为数组")
	require.Len(t, messages, 1, "system 消息不进 messages")
	assert.Equal(t, "user", messages[0]["role"])
	assert.Equal(t, "你好", messages[0]["content"])
}

func TestAnthropicBuildRequestMiniMaxVariant(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter("  MiniMax  ")

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:       "MiniMax-M2",
		Temperature: 1.0,
		Messages: []protocol.Message{
			{Role: "system", Content: "系统提示词"},
			{Role: "user", Content: "你好"},
		},
	})

	assert.Equal(t, 4096, body["maxTokens"], "max_tokens 缺省回填 4096（与旧 MiniMaxProvider 一致）")
	assert.NotContains(t, body, "max_tokens", "minimax 变体使用兼容端点 camelCase 字段")
	assert.Equal(t, 1.0, body["temperature"])
	assert.Equal(t, "系统提示词", body["system"])

	// 多条 system 取最后一条非空（与旧 MiniMaxProvider 覆盖口径一致）
	overridden := adapter.BuildRequest(protocol.RequestConfig{
		Model: "MiniMax-M2",
		Messages: []protocol.Message{
			{Role: "system", Content: "第一条"},
			{Role: "system", Content: "第二条"},
		},
	})
	assert.Equal(t, "第二条", overridden["system"])
}

func TestAnthropicBuildRequestToolsAndToolResults(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:     "claude-sonnet-4",
		MaxTokens: 2048,
		Stream:    true,
		Messages: []protocol.Message{
			{Role: "user", Content: "查一下工单 T-1"},
			{Role: "assistant", Content: "我来查询", ToolCalls: []protocol.ToolCall{
				{ID: "toolu_1", Name: "query_ticket", Arguments: `{"id":"T-1"}`},
			}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "工单处理中"},
		},
		Tools: []protocol.Tool{{
			Name:        "query_ticket",
			Description: "查询工单",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
		}},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "query_ticket"}},
	})

	assert.Equal(t, true, body["stream"], "流式请求下发 stream")

	messages, ok := body["messages"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, messages, 3)

	assistant := messages[1]
	blocks, ok := assistant["content"].([]map[string]any)
	require.True(t, ok, "assistant 工具调用转内容块")
	require.Len(t, blocks, 2)
	assert.Equal(t, "text", blocks[0]["type"])
	assert.Equal(t, "tool_use", blocks[1]["type"])
	assert.Equal(t, "toolu_1", blocks[1]["id"])
	assert.Equal(t, "query_ticket", blocks[1]["name"])
	assert.Equal(t, map[string]any{"id": "T-1"}, blocks[1]["input"])

	toolResult := messages[2]
	assert.Equal(t, "user", toolResult["role"], "工具结果转 user 角色")
	resultBlocks, ok := toolResult["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, resultBlocks, 1)
	assert.Equal(t, "tool_result", resultBlocks[0]["type"])
	assert.Equal(t, "toolu_1", resultBlocks[0]["tool_use_id"])
	assert.Equal(t, "工单处理中", resultBlocks[0]["content"])

	tools, ok := body["tools"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	assert.Equal(t, "query_ticket", tools[0]["name"])
	assert.Equal(t, "查询工单", tools[0]["description"])
	assert.NotNil(t, tools[0]["input_schema"], "Anthropic 使用 input_schema")

	assert.Equal(t, map[string]any{"type": "tool", "name": "query_ticket"}, body["tool_choice"],
		"OpenAI 风格 tool_choice 归一为 Anthropic 形态")
}

func TestAnthropicBuildHeaders(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	headers := adapter.BuildHeaders(protocol.AdapterConfig{
		APIKey:  "sk-test",
		Headers: map[string]string{"X-Trace-Id": "trace-1"},
	})
	assert.Equal(t, "sk-test", headerValue(headers, "x-api-key"))
	assert.Equal(t, "2023-06-01", headerValue(headers, "anthropic-version"))
	assert.Equal(t, "application/json", headerValue(headers, "content-type"))
	assert.Equal(t, "trace-1", headerValue(headers, "x-trace-id"))

	// 调用方附加 headers 大小写不敏感覆盖内置值
	overridden := adapter.BuildHeaders(protocol.AdapterConfig{
		APIKey:  "sk-test",
		Headers: map[string]string{"X-API-KEY": "sk-caller"},
	})
	assert.Equal(t, "sk-caller", headerValue(overridden, "x-api-key"))

	// 无 key 不下发鉴权头
	assert.Empty(t, headerValue(adapter.BuildHeaders(protocol.AdapterConfig{}), "x-api-key"))
}

func TestAnthropicProcessResponse(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	result := adapter.ProcessResponse(map[string]any{
		"id":   "msg_1",
		"type": "message",
		"content": []any{
			map[string]any{"type": "thinking", "thinking": "先分析"},
			map[string]any{"type": "text", "text": "你好"},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "query_ticket", "input": map[string]any{"id": "T-1"}},
		},
		"stop_reason": "tool_use",
	})

	assert.Equal(t, "你好", result.Content)
	assert.Equal(t, "先分析", result.Reasoning)
	assert.Equal(t, "tool_use", result.FinishReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "toolu_1", result.ToolCalls[0].ID)
	assert.Equal(t, "query_ticket", result.ToolCalls[0].Name)
	assert.Equal(t, `{"id":"T-1"}`, result.ToolCalls[0].Arguments)
	assert.True(t, result.HasToolCalls())

	// minimax 变体 camelCase 的 stopReason 同样识别
	miniMax := protocol.NewAnthropicMessagesAdapter(protocol.VariantMiniMax)
	camel := miniMax.ProcessResponse(map[string]any{
		"content":    []any{map[string]any{"type": "text", "text": "你好，世界"}},
		"stopReason": "end_turn",
	})
	assert.Equal(t, "你好，世界", camel.Content)
	assert.Equal(t, "end_turn", camel.FinishReason)
}

func TestAnthropicHandleResponseNonStream(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	result, err := adapter.HandleResponse(false, strings.NewReader(
		`{"id":"msg_1","content":[{"type":"text","text":"你好，世界"}],"stop_reason":"end_turn"}`),
		protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", result.Content)
	assert.Equal(t, "end_turn", result.FinishReason)

	// tool_use-only 响应：正文为空但不报错（与旧 MiniMaxProvider 的有意差异，见适配器注释）
	toolOnly, err := adapter.HandleResponse(false, strings.NewReader(
		`{"content":[{"type":"tool_use","id":"toolu_1","name":"query_ticket","input":{"id":"T-1"}}],"stop_reason":"tool_use"}`),
		protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Empty(t, toolOnly.Content)
	require.Len(t, toolOnly.ToolCalls, 1)

	// 带内错误体（Anthropic 形态）映射为 *ProtocolError
	_, err = adapter.HandleResponse(false, strings.NewReader(
		`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: required"}}`),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	protocolErr, ok := err.(*protocol.ProtocolError)
	require.True(t, ok)
	assert.Equal(t, protocol.ProtocolAnthropicMessages, protocolErr.Protocol)
	assert.Equal(t, "invalid_request_error", protocolErr.Code)
	assert.Contains(t, protocolErr.Message, "max_tokens")
}

func TestAnthropicHandleResponseStream(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)

	var textSegments, reasoningSegments []string
	result, err := adapter.HandleResponse(true, strings.NewReader(anthropicStreamBody), protocol.StreamCallbacks{
		OnText:      func(text string) { textSegments = append(textSegments, text) },
		OnReasoning: func(text string) { reasoningSegments = append(reasoningSegments, text) },
	})
	require.NoError(t, err)

	assert.Equal(t, "你好，世界", result.Content)
	assert.Equal(t, "先分析", result.Reasoning)
	assert.Equal(t, "tool_use", result.FinishReason)
	assert.Equal(t, []string{"你好", "，世界"}, textSegments)
	assert.Equal(t, []string{"先分析"}, reasoningSegments)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "toolu_1", result.ToolCalls[0].ID)
	assert.Equal(t, "query_ticket", result.ToolCalls[0].Name)
	assert.Equal(t, `{"id":"T-1"}`, result.ToolCalls[0].Arguments)

	// 带内 error 事件（流内无 HTTP 语义）映射为 *ProtocolError
	_, err = adapter.HandleResponse(true, strings.NewReader(
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	protocolErr, ok := err.(*protocol.ProtocolError)
	require.True(t, ok)
	assert.Equal(t, "overloaded_error", protocolErr.Code)
	assert.False(t, protocolErr.Retryable(), "流内错误事件不重试（与网关 isTransientLLMError 口径一致）")
}

func TestAnthropicIsReasoningModelAndEndpoint(t *testing.T) {
	adapter := protocol.NewAnthropicMessagesAdapter(protocol.VariantDefault)
	assert.True(t, adapter.IsReasoningModel("claude-sonnet-4-thinking"))
	assert.False(t, adapter.IsReasoningModel("claude-sonnet-4"), "普通 claude 不抑制 temperature")
	assert.False(t, adapter.IsReasoningModel("MiniMax-M2"), "minimax 变体恒下发 temperature（旧分支口径）")

	assert.Equal(t, "/v1/messages", adapter.GetAPIPath())
	assert.Equal(t, "https://api.anthropic.com", protocol.DefaultEndpoint(protocol.ProtocolAnthropicMessages, protocol.VariantDefault))
	assert.Equal(t, "https://api.minimaxi.com/anthropic/v1", protocol.DefaultEndpoint(protocol.ProtocolAnthropicMessages, " MiniMax "))
	assert.Equal(t, "https://generativelanguage.googleapis.com",
		protocol.DefaultEndpoint(protocol.ProtocolGoogleGemini, protocol.VariantDefault), "PA-4 起 gemini 登记官方默认地址")
	assert.Empty(t, protocol.DefaultEndpoint("openai_completions", protocol.VariantDefault))
}

// anthropicStreamBody 复刻官方事件序列（thinking → text → tool_use → message_delta）。
const anthropicStreamBody = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","role":"assistant"}}

event: ping
data: {"type":"ping"}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先分析"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"，世界"}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"query_ticket","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"id\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"T-1\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

event: message_stop
data: {"type":"message_stop"}

`
