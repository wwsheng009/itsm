package protocol_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
)

// ---- PA-4：google_gemini 适配器（Gemini API v1beta，generateContent / streamGenerateContent） ----

func TestGoogleGeminiAdapterMetadata(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)
	assert.Equal(t, protocol.ProtocolGoogleGemini, adapter.Name())
	assert.Equal(t, "/v1beta", adapter.GetAPIPath())
	assert.False(t, adapter.IsReasoningModel("gemini-2.5-pro"), "Gemini 请求侧不抑制采样参数（§4.3 PA-4）")
	assert.False(t, adapter.IsReasoningModel(""))

	// 资源路径含模型名与操作：非流式 / 流式（?alt=sse）/ models 前缀 / 空模型回退版本段。
	assert.Equal(t, "/v1beta/models/gemini-2.0-flash:generateContent", adapter.APIPathFor("gemini-2.0-flash", false))
	assert.Equal(t,
		"/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
		adapter.APIPathFor(" models/gemini-2.0-flash ", true))
	assert.Equal(t, "/v1beta", adapter.APIPathFor("   ", true))

	// endpoint 缺省回退官方地址（协议包登记值）。
	assert.Equal(t, "https://generativelanguage.googleapis.com",
		protocol.DefaultEndpoint(protocol.ProtocolGoogleGemini, protocol.VariantDefault))
	assert.Equal(t, "https://generativelanguage.googleapis.com",
		protocol.DefaultEndpoint(" Google_Gemini ", " "))

	// 注册表：标准变体命中，其余变体不命中（一协议一实现 + 变体白名单）。
	registry := protocol.NewDefaultRegistry()
	assert.True(t, registry.Supports(protocol.ProtocolGoogleGemini, protocol.VariantDefault))
	assert.True(t, registry.Supports(protocol.ProtocolGoogleGemini, "  "))
	assert.False(t, registry.Supports(protocol.ProtocolGoogleGemini, protocol.VariantAzure))
	built, err := registry.NewAdapter(protocol.ProtocolGoogleGemini, protocol.VariantDefault)
	require.NoError(t, err)
	assert.IsType(t, &protocol.GoogleGeminiAdapter{}, built)

	// 鉴权：x-goog-api-key 请求头（密钥不进 URL）；调用方 headers 大小写不敏感覆盖。
	headers := adapter.BuildHeaders(protocol.AdapterConfig{APIKey: " goog-key ", Headers: map[string]string{"X-Trace": "t1", "x-goog-api-key": "override"}})
	assert.Equal(t, "application/json", headers["Content-Type"])
	assert.Equal(t, "override", headers["x-goog-api-key"])
	assert.Equal(t, "t1", headers["X-Trace"])
	assert.NotContains(t, adapter.BuildHeaders(protocol.AdapterConfig{}), "x-goog-api-key")
}

func TestGoogleGeminiBuildRequestContents(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)
	body := geminiRequestBody(t, adapter, protocol.RequestConfig{
		Model:       "gemini-2.0-flash",
		MaxTokens:   1024,
		Temperature: 0.3,
		Messages: []protocol.Message{
			{Role: "system", Content: "你是 ITSM 助手"},
			{Role: "system", Content: "回答要简洁"},
			{Role: "user", Content: "查一下工单 42", Tools: []protocol.Tool{
				{Name: "get_ticket", Description: "查询工单", Parameters: map[string]any{"type": "object"}},
			}},
			{Role: "assistant", Content: "好的", ToolCalls: []protocol.ToolCall{
				{ID: "call_1", Name: "get_ticket", Arguments: `{"id":42}`},
			}},
			{Role: "tool", Content: `{"status":"open"}`, ToolCallID: "call_1"},
			{Role: "system", Content: "补充约束"},
			{Role: "assistant"}, // 空消息（无文本无工具）跳过
		},
	})

	// 前导 system 合并为 systemInstruction（\n\n 连接）。
	systemInstruction := body["systemInstruction"].(map[string]any)
	assert.Equal(t, "你是 ITSM 助手\n\n回答要简洁",
		systemInstruction["parts"].([]any)[0].(map[string]any)["text"])

	contents := body["contents"].([]any)
	require.Len(t, contents, 4)
	// user 文本
	assert.Equal(t, map[string]any{"role": "user", "parts": []any{map[string]any{"text": "查一下工单 42"}}}, contents[0])
	// assistant → model：文本 + functionCall（args 为 JSON 对象）
	assert.Equal(t, map[string]any{"role": "model", "parts": []any{
		map[string]any{"text": "好的"},
		map[string]any{"functionCall": map[string]any{"name": "get_ticket", "args": map[string]any{"id": float64(42)}}},
	}}, contents[1])
	// tool → user：functionResponse（函数名由历史 functionCall 反查）
	assert.Equal(t, map[string]any{"role": "user", "parts": []any{
		map[string]any{"functionResponse": map[string]any{"name": "get_ticket", "response": map[string]any{"status": "open"}}},
	}}, contents[2])
	// 非前导 system 降级为 user 文本
	assert.Equal(t, map[string]any{"role": "user", "parts": []any{map[string]any{"text": "补充约束"}}}, contents[3])

	// 采样参数：maxOutputTokens + temperature。
	assert.Equal(t, map[string]any{"maxOutputTokens": float64(1024), "temperature": 0.3}, body["generationConfig"])

	// 工具声明：tools[].functionDeclarations[]（消息携带的 Tools 合并进声明）。
	declarations := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	require.Len(t, declarations, 1)
	assert.Equal(t, map[string]any{
		"name": "get_ticket", "description": "查询工单", "parameters": map[string]any{"type": "object"},
	}, declarations[0])
}

func TestGoogleGeminiBuildRequestEdgeCases(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)

	// 推理模型抑制 temperature；maxTokens <= 0 不下发 → 无 generationConfig。
	body := geminiRequestBody(t, adapter, protocol.RequestConfig{
		Model:          "gemini-2.5-pro",
		Temperature:    0.3,
		ReasoningModel: true,
		Messages:       []protocol.Message{{Role: "user", Content: "hi"}},
	})
	_, hasGenerationConfig := body["generationConfig"]
	assert.False(t, hasGenerationConfig, "推理模型抑制采样参数（与 chat/responses 同表口径）")
	assert.NotContains(t, body, "tools")
	assert.NotContains(t, body, "toolConfig")

	// 无参数工具补空对象 schema；工具结果解析不出函数名时用占位名；非 JSON 工具结果包成 result。
	body = geminiRequestBody(t, adapter, protocol.RequestConfig{
		Model: "gemini-2.0-flash",
		Tools: []protocol.Tool{{Name: "ping"}},
		Messages: []protocol.Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", Content: "纯文本结果", ToolCallID: "unknown-id"},
			{Role: "tool", Content: "  ", ToolCallID: ""},
		},
	})
	declaration := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{}}, declaration["parameters"])
	assert.NotContains(t, declaration, "description")

	contents := body["contents"].([]any)
	require.Len(t, contents, 3)
	assert.Equal(t, map[string]any{"functionResponse": map[string]any{
		"name": "tool", "response": map[string]any{"result": "纯文本结果"},
	}}, contents[1].(map[string]any)["parts"].([]any)[0])
	assert.Equal(t, map[string]any{"functionResponse": map[string]any{
		"name": "tool", "response": map[string]any{"result": ""},
	}}, contents[2].(map[string]any)["parts"].([]any)[0])

	// 空会话仍下发 contents（Gemini 必填字段）。
	body = geminiRequestBody(t, adapter, protocol.RequestConfig{Model: "gemini-2.0-flash"})
	assert.Equal(t, []any{}, body["contents"])
	assert.NotContains(t, body, "systemInstruction")

	// 历史里函数名非法（空名）的 functionCall 被跳过，不构造半成品工具调用。
	body = geminiRequestBody(t, adapter, protocol.RequestConfig{
		Model:    "gemini-2.0-flash",
		Messages: []protocol.Message{{Role: "assistant", Content: "x", ToolCalls: []protocol.ToolCall{{ID: "call_1", Name: "  "}}}},
	})
	parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	assert.Equal(t, []any{map[string]any{"text": "x"}}, parts)
}

func TestGoogleGeminiBuildRequestToolConfig(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)
	cases := []struct {
		name   string
		choice any
		want   map[string]any
	}{
		{name: "auto", choice: "auto", want: map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}},
		{name: "required", choice: "required", want: map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}},
		{name: "any", choice: " any ", want: map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}},
		{name: "none", choice: "none", want: map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}},
		{
			name:   "openai chat 风格",
			choice: map[string]any{"type": "function", "function": map[string]any{"name": "get_ticket"}},
			want: map[string]any{"functionCallingConfig": map[string]any{
				"mode": "ANY", "allowedFunctionNames": []any{"get_ticket"},
			}},
		},
		{
			name:   "gemini 原生形态透传",
			choice: map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}},
			want:   map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}},
		},
		{name: "mode 简写", choice: map[string]any{"mode": "AUTO"}, want: map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}},
		{name: "不可识别字符串", choice: "sometimes", want: nil},
		{name: "不可识别对象", choice: map[string]any{"type": "weird"}, want: nil},
		{name: "nil", choice: nil, want: nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := geminiRequestBody(t, adapter, protocol.RequestConfig{
				Model:      "gemini-2.0-flash",
				ToolChoice: testCase.choice,
				Messages:   []protocol.Message{{Role: "user", Content: "hi"}},
			})
			got, ok := body["toolConfig"].(map[string]any)
			if testCase.want == nil {
				assert.False(t, ok, "未设置/不可识别一律不下发 toolConfig")
				return
			}
			require.True(t, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// geminiRequestBody 序列化-反序列化请求体，便于用 JSON 语义断言（数组为 []any）。
func geminiRequestBody(t *testing.T, adapter *protocol.GoogleGeminiAdapter, cfg protocol.RequestConfig) map[string]any {
	t.Helper()
	raw, err := json.Marshal(adapter.BuildRequest(cfg))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	return body
}

func TestGoogleGeminiHandleResponseNonStream(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)

	payload := `{
		"candidates": [{
			"content": {"parts": [
				{"text": "思考中…", "thought": true},
				{"text": "工单是 "},
				{"text": "开放的"},
				{"functionCall": {"name": "get_ticket", "args": {"id": 42}}}
			]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 3}
	}`
	result, err := adapter.HandleResponse(false, strings.NewReader(payload), protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, "工单是 开放的", result.Content)
	assert.Equal(t, "思考中…", result.Reasoning)
	assert.Equal(t, "STOP", result.FinishReason)
	assert.True(t, result.HasToolCalls())
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, "get_ticket", result.ToolCalls[0].Name)
	assert.JSONEq(t, `{"id":42}`, result.ToolCalls[0].Arguments)

	// 带内错误：error 对象映射为 ProtocolError（无 HTTP 语义 → StatusCode=0，不重试）。
	_, err = adapter.HandleResponse(false,
		strings.NewReader(`{"error":{"code":"INVALID_ARGUMENT","message":"API key not valid"}}`),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	var protocolErr *protocol.ProtocolError
	require.True(t, errors.As(err, &protocolErr))
	assert.Equal(t, protocol.ProtocolGoogleGemini, protocolErr.Protocol)
	assert.Equal(t, 0, protocolErr.StatusCode)
	assert.Equal(t, "INVALID_ARGUMENT", protocolErr.Code)
	assert.Equal(t, "API key not valid", protocolErr.Message)
	assert.False(t, protocolErr.Retryable())

	// 安全拦截（promptFeedback.blockReason，无候选）→ 带内错误。
	_, err = adapter.HandleResponse(false,
		strings.NewReader(`{"promptFeedback":{"blockReason":"SAFETY"}}`), protocol.StreamCallbacks{})
	require.Error(t, err)
	require.True(t, errors.As(err, &protocolErr))
	assert.Equal(t, "SAFETY", protocolErr.Code)
	assert.Equal(t, "prompt blocked: SAFETY", protocolErr.Message)
	assert.False(t, protocolErr.Retryable())

	// 非法 JSON / 空 body：显式报错，不静默返回空结果。
	_, err = adapter.HandleResponse(false, strings.NewReader("{not json"), protocol.StreamCallbacks{})
	assert.ErrorContains(t, err, "decode response")
	_, err = adapter.HandleResponse(false, nil, protocol.StreamCallbacks{})
	assert.ErrorContains(t, err, "response body is required")

	// 空 payload（无 candidates）不 panic，返回空结果。
	result = adapter.ProcessResponse(map[string]any{})
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FinishReason)
	assert.False(t, result.HasToolCalls())
	assert.Empty(t, adapter.ProcessResponse(nil).Content)
}

func TestGoogleGeminiHandleResponseStream(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)

	body := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"，正在查询\"},{\"text\":\"…\",\"thought\":true}]}}]}\n\n" +
		": keep-alive\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"get_ticket\",\"args\":{\"id\":42}}}]}}]}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":9}}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"MAX_TOKENS\"}]}\n\n" +
		"data: [DONE]\n\n"

	var texts, reasonings []string
	result, err := adapter.HandleResponse(true, strings.NewReader(body), protocol.StreamCallbacks{
		OnText:      func(text string) { texts = append(texts, text) },
		OnReasoning: func(reasoning string) { reasonings = append(reasonings, reasoning) },
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"你好", "，正在查询"}, texts)
	assert.Equal(t, []string{"…"}, reasonings)
	assert.Equal(t, "你好，正在查询", result.Content)
	assert.Equal(t, "…", result.Reasoning)
	assert.Equal(t, "STOP", result.FinishReason, "首个 finishReason 生效，后续帧不覆盖")
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, "get_ticket", result.ToolCalls[0].Name)
	assert.JSONEq(t, `{"id":42}`, result.ToolCalls[0].Arguments)

	// 仅 [DONE]：正常结束的空结果。
	result, err = adapter.HandleResponse(true, strings.NewReader("data: [DONE]\n\n"), protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FinishReason)
}

func TestGoogleGeminiStreamInBandErrors(t *testing.T) {
	adapter := protocol.NewGoogleGeminiAdapter(protocol.VariantDefault)

	// 帧内 error（HTTP 200 + 带内错误）。
	_, err := adapter.HandleResponse(true,
		strings.NewReader("data: {\"error\":{\"code\":\"UNAVAILABLE\",\"message\":\"overloaded\"}}\n\n"),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	var protocolErr *protocol.ProtocolError
	require.True(t, errors.As(err, &protocolErr))
	assert.Equal(t, "UNAVAILABLE", protocolErr.Code)
	assert.Equal(t, "overloaded", protocolErr.Message)
	assert.Equal(t, 0, protocolErr.StatusCode)
	assert.False(t, protocolErr.Retryable())

	// 帧内 promptFeedback 拦截。
	_, err = adapter.HandleResponse(true,
		strings.NewReader("data: {\"promptFeedback\":{\"blockReason\":\"PROHIBITED_CONTENT\"}}\n\n"),
		protocol.StreamCallbacks{})
	require.Error(t, err)
	require.True(t, errors.As(err, &protocolErr))
	assert.Equal(t, "PROHIBITED_CONTENT", protocolErr.Code)

	// 坏帧：显式报错（不吞帧继续）。
	_, err = adapter.HandleResponse(true, strings.NewReader("data: {not json}\n\n"), protocol.StreamCallbacks{})
	assert.ErrorContains(t, err, "decode stream chunk")

	// 流式路径同样拒绝空 body。
	_, err = adapter.HandleResponse(true, nil, protocol.StreamCallbacks{})
	assert.ErrorContains(t, err, "response body is required")
}
