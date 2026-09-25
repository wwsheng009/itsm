package protocol_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
)

func TestOpenAIChatAdapterMetadata(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()
	assert.Equal(t, protocol.ProtocolOpenAIChatCompletions, adapter.Name())
	assert.Equal(t, "/v1/chat/completions", adapter.GetAPIPath())
}

func TestOpenAIChatAdapterIsReasoningModel(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()
	for _, model := range []string{"gpt-5", "gpt-5-mini", "o1-preview", "o3-mini", "gpt-5-codex", "models/o4-mini"} {
		assert.True(t, adapter.IsReasoningModel(model), model)
	}
	for _, model := range []string{"gpt-4o-mini", "deepseek-chat", "qwen2.5-72b", ""} {
		assert.False(t, adapter.IsReasoningModel(model), model)
	}
}

func TestOpenAIChatAdapterBuildRequest(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()

	body := adapter.BuildRequest(protocol.RequestConfig{
		Model:       "gpt-4o-mini",
		Messages:    []protocol.Message{{Role: "user", Content: "hi"}},
		MaxTokens:   4096,
		Temperature: 0.3,
	})
	assert.Equal(t, "gpt-4o-mini", body["model"])
	assert.Equal(t, []map[string]any{{"role": "user", "content": "hi"}}, body["messages"])
	assert.Equal(t, 4096, body["max_tokens"])
	assert.Equal(t, 0.3, body["temperature"])
	assert.NotContains(t, body, "stream", "非流式不下发 stream（对齐 go-openai omitempty）")
	assert.NotContains(t, body, "tools")
	assert.NotContains(t, body, "tool_choice")

	streamBody := adapter.BuildRequest(protocol.RequestConfig{
		Model:          "gpt-5",
		Messages:       []protocol.Message{{Role: "user", Content: "hi"}},
		Stream:         true,
		MaxTokens:      1024,
		Temperature:    0.3,
		ReasoningModel: true,
		Tools:          []protocol.Tool{{Name: "get_time", Description: "查询时间", Parameters: map[string]any{"type": "object"}}},
		ToolChoice:     "auto",
	})
	assert.Equal(t, true, streamBody["stream"])
	assert.NotContains(t, streamBody, "temperature", "推理模型抑制 temperature（参考实现口径）")
	assert.Equal(t, "auto", streamBody["tool_choice"])
	tools, ok := streamBody["tools"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	function, ok := tools[0]["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "get_time", function["name"])
	assert.Equal(t, "查询时间", function["description"])

	// 消息携带的工具与显式工具合并：消息在前、显式在后（对齐既有实现合并口径）
	merged := adapter.BuildRequest(protocol.RequestConfig{
		Model:    "gpt-4o-mini",
		Messages: []protocol.Message{{Role: "user", Content: "hi", Tools: []protocol.Tool{{Name: "from_message"}}}},
		Tools:    []protocol.Tool{{Name: "from_config"}},
	})
	mergedTools, ok := merged["tools"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, mergedTools, 2)
	assert.Equal(t, "from_message", mergedTools[0]["function"].(map[string]any)["name"])
	assert.Equal(t, "from_config", mergedTools[1]["function"].(map[string]any)["name"])

	// 工具调用消息（assistant → tool 回传）的线上字段
	callBody := adapter.BuildRequest(protocol.RequestConfig{
		Model: "gpt-4o-mini",
		Messages: []protocol.Message{
			{Role: "assistant", ToolCalls: []protocol.ToolCall{{ID: "call_1", Name: "get_time", Arguments: `{"tz":"UTC"}`}}},
			{Role: "tool", Content: "12:00", ToolCallID: "call_1"},
		},
	})
	callMessages, ok := callBody["messages"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, callMessages, 2)
	assert.NotContains(t, callMessages[0], "content", "空 content 不下发（对齐 omitempty）")
	wireCalls, ok := callMessages[0]["tool_calls"].([]map[string]any)
	require.True(t, ok)
	assert.Equal(t, "call_1", wireCalls[0]["id"])
	assert.Equal(t, "function", wireCalls[0]["type"])
	assert.Equal(t, "call_1", callMessages[1]["tool_call_id"])
	assert.Equal(t, "12:00", callMessages[1]["content"])
}

func TestOpenAIChatAdapterBuildHeaders(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()

	headers := adapter.BuildHeaders(protocol.AdapterConfig{APIKey: "test-key"})
	assert.Equal(t, "application/json", headers["Content-Type"])
	assert.Equal(t, "Bearer test-key", headers["Authorization"])

	overridden := adapter.BuildHeaders(protocol.AdapterConfig{
		APIKey:  "test-key",
		Headers: map[string]string{"authorization": "Bearer override", "x-trace-id": "t-1"},
	})
	assert.Equal(t, "Bearer override", headerValue(overridden, "Authorization"), "附加 headers 大小写不敏感覆盖")
	assert.Equal(t, "t-1", overridden["x-trace-id"])
}

func TestOpenAIChatAdapterProcessResponse(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()

	result := adapter.ProcessResponse(map[string]any{
		"choices": []any{
			map[string]any{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":              "assistant",
					"content":           "",
					"reasoning_content": "先查时区",
					"tool_calls": []any{
						map[string]any{
							"id":       "call_1",
							"type":     "function",
							"function": map[string]any{"name": "get_time", "arguments": `{"tz":"UTC"}`},
						},
					},
				},
			},
		},
	})
	assert.Equal(t, "先查时区", result.Reasoning)
	assert.Equal(t, "tool_calls", result.FinishReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "call_1", Name: "get_time", Arguments: `{"tz":"UTC"}`}, result.ToolCalls[0])
	assert.True(t, result.HasToolCalls())

	empty := adapter.ProcessResponse(map[string]any{"choices": []any{}})
	assert.Equal(t, protocol.ProcessResult{}, empty)
}

func TestOpenAIChatAdapterHandleStream(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"你好"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"，世界"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"思考中"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{\"tz\":"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n\n")

	var deltas, reasons []string
	result, err := adapter.HandleResponse(true, strings.NewReader(body), protocol.StreamCallbacks{
		OnText:      func(delta string) { deltas = append(deltas, delta) },
		OnReasoning: func(delta string) { reasons = append(reasons, delta) },
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"你好", "，世界"}, deltas)
	assert.Equal(t, []string{"思考中"}, reasons)
	assert.Equal(t, "你好，世界", result.Content)
	assert.Equal(t, "思考中", result.Reasoning)
	assert.Equal(t, "tool_calls", result.FinishReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, protocol.ToolCall{ID: "call_1", Name: "get_time", Arguments: `{"tz":"UTC"}`}, result.ToolCalls[0])
}

func TestOpenAIChatAdapterHandleErrorPayload(t *testing.T) {
	adapter := protocol.NewOpenAIChatAdapter()

	_, err := adapter.HandleResponse(false, strings.NewReader(`{"error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`), protocol.StreamCallbacks{})
	require.Error(t, err)
	var protocolErr *protocol.ProtocolError
	require.ErrorAs(t, err, &protocolErr)
	assert.Equal(t, "context_length_exceeded", protocolErr.Code)
	assert.Equal(t, "context length exceeded", protocolErr.Message)
	assert.False(t, protocolErr.Retryable())

	// 流内错误事件（SSE data 带 error 对象）
	streamBody := "data: {\"error\":{\"message\":\"upstream stream failed\",\"code\":\"stream_error\"}}\n\n"
	_, err = adapter.HandleResponse(true, strings.NewReader(streamBody), protocol.StreamCallbacks{})
	require.Error(t, err)
	require.ErrorAs(t, err, &protocolErr)
	assert.Equal(t, "stream_error", protocolErr.Code)
	assert.False(t, protocolErr.Retryable())
}

func TestNewHTTPError(t *testing.T) {
	rateLimited := protocol.NewHTTPError(
		protocol.ProtocolOpenAIChatCompletions,
		&http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests"},
		[]byte(`{"error":{"message":"rate limited","code":"rate_limit_exceeded"}}`),
	)
	assert.Equal(t, http.StatusTooManyRequests, rateLimited.StatusCode)
	assert.Equal(t, "rate_limit_exceeded", rateLimited.Code)
	assert.Equal(t, "rate limited", rateLimited.Message)
	assert.True(t, rateLimited.Retryable())

	serverErr := protocol.NewHTTPError(
		protocol.ProtocolOpenAIChatCompletions,
		&http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error"},
		[]byte("<html>boom</html>"),
	)
	assert.True(t, serverErr.Retryable())
	assert.Equal(t, "500 Internal Server Error", serverErr.Message)

	unauthorized := protocol.NewHTTPError(protocol.ProtocolOpenAIChatCompletions, &http.Response{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"}, nil)
	assert.False(t, unauthorized.Retryable())
	assert.Equal(t, http.StatusUnauthorized, unauthorized.StatusCode)
}

func TestDefaultRegistryScope(t *testing.T) {
	registry := protocol.NewDefaultRegistry()

	adapter, err := registry.Get(protocol.ProtocolOpenAIChatCompletions, protocol.VariantDefault)
	require.NoError(t, err)
	assert.Equal(t, protocol.ProtocolOpenAIChatCompletions, adapter.Name())

	// BE-9 + PA-1 已适配：openai 默认变体、anthropic 默认/minimax 变体；
	// azure/ollama 变体与 openai_responses / google_gemini 未命中，
	// service 层据此回退旧分支或映射 AI_PROTOCOL_NOT_IMPLEMENTED(422)。
	anthropic, err := registry.Get(protocol.ProtocolAnthropicMessages, protocol.VariantDefault)
	require.NoError(t, err)
	assert.Equal(t, protocol.ProtocolAnthropicMessages, anthropic.Name())
	miniMax, err := registry.Get(protocol.ProtocolAnthropicMessages, protocol.VariantMiniMax)
	require.NoError(t, err)
	assert.Equal(t, protocol.ProtocolAnthropicMessages, miniMax.Name())

	_, err = registry.Get(protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)
	_, err = registry.Get(protocol.ProtocolOpenAIChatCompletions, protocol.VariantOllama)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)
	_, err = registry.Get(protocol.ProtocolOpenAIResponses, protocol.VariantDefault)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)
	_, err = registry.Get(protocol.ProtocolGoogleGemini, protocol.VariantDefault)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)

	// 显式注册后精确命中，且大小写/空白不敏感
	registry.Register(protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure, adapter)
	_, err = registry.Get(" OpenAI_Chat_Completions ", " Azure ")
	require.NoError(t, err)
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}
	return ""
}
