package protocol_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
	"itsm-backend/service"
)

// chatStub 是 OpenAI 兼容桩服务：记录每个请求，按 mode 返回固定响应。
// 同一份桩服务同时服务"适配器路径"与"既有 OpenAIProvider 路径"，
// 供 BE-9 验收要求的对照测试逐项比对（请求/流式 chunk 序列/工具调用/错误映射）。
type chatStub struct {
	server     *httptest.Server
	streamBody string

	mu       sync.Mutex
	mode     string
	requests []recordedRequest
}

type recordedRequest struct {
	Path          string
	Authorization string
	Body          map[string]any
}

func newChatStub(t *testing.T) *chatStub {
	t.Helper()
	stub := &chatStub{streamBody: buildStubStreamBody(t)}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		stub.mu.Lock()
		stub.requests = append(stub.requests, recordedRequest{
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			Body:          payload,
		})
		mode := stub.mode
		stub.mu.Unlock()

		switch mode {
		case "401":
			writeStubJSON(w, http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		case "500":
			writeStubJSON(w, http.StatusInternalServerError, `{"error":{"message":"upstream exploded","type":"server_error","code":"internal_error"}}`)
			return
		}

		if stream, _ := payload["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, stub.streamBody)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}

		writeStubJSON(w, http.StatusOK, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"你好，世界"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *chatStub) setMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *chatStub) snapshotRequests() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func writeStubJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// buildStubStreamBody 生成固定 SSE 序列：文本两段 + 分片工具调用 + finish_reason。
func buildStubStreamBody(t *testing.T) string {
	t.Helper()
	sseFrame := func(delta map[string]any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}
	}
	chunks := []map[string]any{
		sseFrame(map[string]any{"role": "assistant", "content": "你好"}),
		sseFrame(map[string]any{"content": "，世界"}),
		sseFrame(map[string]any{"tool_calls": []any{
			map[string]any{
				"index":    0,
				"id":       "call_1",
				"type":     "function",
				"function": map[string]any{"name": "get_time", "arguments": `{"tz":`},
			},
		}}),
		sseFrame(map[string]any{"tool_calls": []any{
			map[string]any{"index": 0, "function": map[string]any{"arguments": `"UTC"}`}},
		}}),
		{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}},
	}
	var builder strings.Builder
	for _, chunk := range chunks {
		data, err := json.Marshal(chunk)
		require.NoError(t, err)
		builder.WriteString("data: ")
		builder.Write(data)
		builder.WriteString("\n\n")
	}
	builder.WriteString("data: [DONE]\n\n")
	return builder.String()
}

// doAdapterRequest 走适配器路径：BuildRequest → HTTP → HandleResponse。
// 非 2xx 由调用方按协议层 helper 映射为 *ProtocolError（与未来 service 接线口径一致）。
func doAdapterRequest(
	t *testing.T,
	ctx context.Context,
	baseURL string,
	adapter protocol.ProtocolAdapter,
	adapterCfg protocol.AdapterConfig,
	requestCfg protocol.RequestConfig,
	isStream bool,
	callbacks protocol.StreamCallbacks,
) (protocol.ProcessResult, error) {
	t.Helper()
	requestBody, err := json.Marshal(adapter.BuildRequest(requestCfg))
	require.NoError(t, err)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+adapter.GetAPIPath(), bytes.NewReader(requestBody))
	require.NoError(t, err)
	for key, value := range adapter.BuildHeaders(adapterCfg) {
		httpRequest.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return protocol.ProcessResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(response.Body)
		return protocol.ProcessResult{}, protocol.NewHTTPError(adapter.Name(), response, responseBody)
	}
	return adapter.HandleResponse(isStream, response.Body, callbacks)
}

func TestEquivalenceNonStreamChat(t *testing.T) {
	stub := newChatStub(t)
	ctx := context.Background()

	provider := service.NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	providerText, err := provider.Chat(ctx, "", []service.LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", providerText)

	adapter := protocol.NewOpenAIChatAdapter()
	result, err := doAdapterRequest(t, ctx, stub.server.URL, adapter, protocol.AdapterConfig{APIKey: "test-key"}, protocol.RequestConfig{
		Model:       "gpt-4o-mini",
		Messages:    []protocol.Message{{Role: "user", Content: "hi"}},
		MaxTokens:   4096,
		Temperature: 0.3,
	}, false, protocol.StreamCallbacks{})
	require.NoError(t, err)
	assert.Equal(t, providerText, result.Content, "非流式正文与既有实现一致")
	assert.Empty(t, result.ToolCalls)
	assert.Equal(t, "stop", result.FinishReason)

	requests := stub.snapshotRequests()
	require.Len(t, requests, 2)
	providerRequest, adapterRequest := requests[0], requests[1]
	assert.Equal(t, "/v1/chat/completions", providerRequest.Path)
	assert.Equal(t, providerRequest.Path, adapterRequest.Path, "命中同一端点")
	assert.Equal(t, "Bearer test-key", providerRequest.Authorization)
	assert.Equal(t, providerRequest.Authorization, adapterRequest.Authorization, "鉴权头一致")

	assert.Equal(t, providerRequest.Body["model"], adapterRequest.Body["model"])
	assert.Equal(t, providerRequest.Body["messages"], adapterRequest.Body["messages"], "消息数组逐字段一致")
	assert.Equal(t, providerRequest.Body["max_tokens"], adapterRequest.Body["max_tokens"])
	assert.Equal(t, providerRequest.Body["temperature"], adapterRequest.Body["temperature"])
	assert.NotContains(t, adapterRequest.Body, "stream", "非流式不下发 stream（与既有实现一致）")
}

func TestEquivalenceStreamWithTools(t *testing.T) {
	stub := newChatStub(t)
	ctx := context.Background()

	parameters := map[string]any{
		"type":       "object",
		"properties": map[string]any{"tz": map[string]any{"type": "string"}},
		"required":   []string{"tz"},
	}

	// 既有实现路径：OpenAIProvider.ChatStreamWithTools
	provider := service.NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	var providerDeltas []string
	var providerCalls []service.LLMToolCall
	err := provider.ChatStreamWithTools(
		ctx, "",
		[]service.LLMMessage{{Role: "user", Content: "现在几点？"}},
		[]service.LLMTool{{Name: "get_time", Description: "查询时间", Parameters: parameters}},
		func(delta string) { providerDeltas = append(providerDeltas, delta) },
		func(calls []service.LLMToolCall) { providerCalls = calls },
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"你好", "，世界"}, providerDeltas)
	require.Len(t, providerCalls, 1)
	assert.Equal(t, "call_1", providerCalls[0].ID)
	assert.Equal(t, "get_time", providerCalls[0].Name)
	assert.Equal(t, `{"tz":"UTC"}`, providerCalls[0].Arguments)

	// 适配器路径
	adapter := protocol.NewOpenAIChatAdapter()
	var adapterDeltas []string
	result, err := doAdapterRequest(t, ctx, stub.server.URL, adapter, protocol.AdapterConfig{APIKey: "test-key"}, protocol.RequestConfig{
		Model:       "gpt-4o-mini",
		Messages:    []protocol.Message{{Role: "user", Content: "现在几点？"}},
		Stream:      true,
		MaxTokens:   4096,
		Temperature: 0.3,
		Tools:       []protocol.Tool{{Name: "get_time", Description: "查询时间", Parameters: parameters}},
	}, true, protocol.StreamCallbacks{OnText: func(delta string) { adapterDeltas = append(adapterDeltas, delta) }})
	require.NoError(t, err)

	assert.Equal(t, providerDeltas, adapterDeltas, "流式 chunk 序列（文本增量）逐项一致")
	assert.Equal(t, "你好，世界", result.Content)
	assert.Equal(t, "tool_calls", result.FinishReason)
	require.Len(t, result.ToolCalls, 1, "工具调用分片累积口径一致")
	assert.Equal(t, providerCalls[0].ID, result.ToolCalls[0].ID)
	assert.Equal(t, providerCalls[0].Name, result.ToolCalls[0].Name)
	assert.Equal(t, providerCalls[0].Arguments, result.ToolCalls[0].Arguments)

	requests := stub.snapshotRequests()
	require.Len(t, requests, 2)
	providerRequest, adapterRequest := requests[0], requests[1]
	assert.Equal(t, true, providerRequest.Body["stream"])
	assert.Equal(t, true, adapterRequest.Body["stream"])
	assert.Equal(t, providerRequest.Body["messages"], adapterRequest.Body["messages"])
	assert.Equal(t, providerRequest.Body["tools"], adapterRequest.Body["tools"], "工具声明逐字段一致")
	assert.Equal(t, providerRequest.Body["model"], adapterRequest.Body["model"])
}

func TestEquivalenceToolCallConsistency(t *testing.T) {
	// 工具调用分片在两条路径上都必须完整累积，不允许"降级丢失"
	stub := newChatStub(t)
	ctx := context.Background()
	parameters := map[string]any{"type": "object"}

	provider := service.NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	var providerCalls []service.LLMToolCall
	var providerDeltas []string
	err := provider.ChatStreamWithTools(
		ctx, "",
		[]service.LLMMessage{{Role: "user", Content: "现在几点？"}},
		[]service.LLMTool{{Name: "get_time", Parameters: parameters}},
		func(delta string) { providerDeltas = append(providerDeltas, delta) },
		func(calls []service.LLMToolCall) { providerCalls = append(providerCalls, calls...) },
	)
	require.NoError(t, err)

	adapter := protocol.NewOpenAIChatAdapter()
	result, err := doAdapterRequest(t, ctx, stub.server.URL, adapter, protocol.AdapterConfig{APIKey: "test-key"}, protocol.RequestConfig{
		Model:     "gpt-4o-mini",
		Messages:  []protocol.Message{{Role: "user", Content: "现在几点？"}},
		Stream:    true,
		MaxTokens: 4096,
		Tools:     []protocol.Tool{{Name: "get_time", Parameters: parameters}},
	}, true, protocol.StreamCallbacks{OnText: func(delta string) {}})
	require.NoError(t, err)

	// 桩服务固定返回工具调用分片，两条路径都必须解析出同一结果（无"降级丢失"）
	require.Len(t, providerCalls, 1)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, providerCalls[0].Name, result.ToolCalls[0].Name)
}

func TestEquivalenceErrorMapping(t *testing.T) {
	ctx := context.Background()
	adapter := protocol.NewOpenAIChatAdapter()
	requestCfg := protocol.RequestConfig{
		Model:       "gpt-4o-mini",
		Messages:    []protocol.Message{{Role: "user", Content: "hi"}},
		MaxTokens:   4096,
		Temperature: 0.3,
	}

	t.Run("401 鉴权失败不可重试", func(t *testing.T) {
		stub := newChatStub(t)
		stub.setMode("401")

		provider := service.NewOpenAIProvider("bad-key", stub.server.URL+"/v1", "gpt-4o-mini")
		_, providerErr := provider.Chat(ctx, "", []service.LLMMessage{{Role: "user", Content: "hi"}})
		require.Error(t, providerErr)
		var apiErr *openai.APIError
		require.ErrorAs(t, providerErr, &apiErr)

		_, adapterErr := doAdapterRequest(t, ctx, stub.server.URL, adapter, protocol.AdapterConfig{APIKey: "bad-key"}, requestCfg, false, protocol.StreamCallbacks{})
		require.Error(t, adapterErr)
		var protocolErr *protocol.ProtocolError
		require.ErrorAs(t, adapterErr, &protocolErr)

		assert.Equal(t, apiErr.HTTPStatusCode, protocolErr.StatusCode, "状态码分类与既有实现一致")
		assert.Equal(t, http.StatusUnauthorized, protocolErr.StatusCode)
		assert.Equal(t, "invalid_api_key", protocolErr.Code)
		assert.False(t, protocolErr.Retryable(), "401 不重试（与网关 isTransientLLMError 口径一致）")
	})

	t.Run("500 服务端错误可重试", func(t *testing.T) {
		stub := newChatStub(t)
		stub.setMode("500")

		provider := service.NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
		_, providerErr := provider.Chat(ctx, "", []service.LLMMessage{{Role: "user", Content: "hi"}})
		require.Error(t, providerErr)
		var apiErr *openai.APIError
		require.ErrorAs(t, providerErr, &apiErr)
		assert.Equal(t, http.StatusInternalServerError, apiErr.HTTPStatusCode)

		_, adapterErr := doAdapterRequest(t, ctx, stub.server.URL, adapter, protocol.AdapterConfig{APIKey: "test-key"}, requestCfg, false, protocol.StreamCallbacks{})
		require.Error(t, adapterErr)
		var protocolErr *protocol.ProtocolError
		require.ErrorAs(t, adapterErr, &protocolErr)

		assert.Equal(t, apiErr.HTTPStatusCode, protocolErr.StatusCode)
		assert.True(t, protocolErr.Retryable(), "5xx 可重试（与网关 isTransientLLMError 口径一致）")
	})
}
