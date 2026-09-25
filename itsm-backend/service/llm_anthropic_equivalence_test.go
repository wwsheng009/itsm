package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/internal/llm/protocol"
)

// anthropicStub 是 MiniMax/Anthropic 兼容桩服务：记录请求（路径 / 鉴权头 / 请求体），
// 按 stream 字段返回非流式消息或事件流。同一份桩服务同时服务"适配器路径"与
// "旧 MiniMaxProvider 路径"，供 PA-1 的等价性对照测试逐项比对。
type anthropicStub struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []anthropicRecordedRequest
}

type anthropicRecordedRequest struct {
	Path             string
	APIKey           string
	AnthropicVersion string
	Body             map[string]any
}

func newAnthropicStub(t *testing.T) *anthropicStub {
	t.Helper()
	stub := &anthropicStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)

		stub.mu.Lock()
		stub.requests = append(stub.requests, anthropicRecordedRequest{
			Path:             r.URL.Path,
			APIKey:           r.Header.Get("x-api-key"),
			AnthropicVersion: r.Header.Get("anthropic-version"),
			Body:             payload,
		})
		stub.mu.Unlock()

		if stream, _ := payload["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, anthropicStubStreamBody)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"msg_stub_1","type":"message","role":"assistant",`+
			`"content":[{"type":"text","text":"你好，世界"}],"stopReason":"end_turn"}`)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *anthropicStub) snapshot() []anthropicRecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]anthropicRecordedRequest(nil), s.requests...)
}

// TestAnthropicAdapterMatchesLegacyMiniMaxProvider 是 PA-1 的核心等价性对照：
// 同一桩服务、同一请求消息下，适配器路径与旧 MiniMaxProvider 的
// 请求路径 / 鉴权头 / 请求体字段 / 返回文本必须一致（temperature 变体默认值 1.0）。
func TestAnthropicAdapterMatchesLegacyMiniMaxProvider(t *testing.T) {
	ctx := context.Background()
	stub := newAnthropicStub(t)
	endpoint := stub.server.URL + "/anthropic/v1"
	messages := []LLMMessage{
		{Role: "system", Content: "你是 IT 助手"},
		{Role: "user", Content: "你好"},
	}

	legacy := &MiniMaxProvider{
		apiKey:  "sk-minimax",
		model:   "MiniMax-M2",
		baseURL: endpoint,
		client:  &http.Client{},
	}
	legacyContent, err := legacy.Chat(ctx, "", messages)
	require.NoError(t, err)

	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolAnthropicMessages,
		Variant:  protocol.VariantMiniMax,
		APIKey:   "sk-minimax",
		Endpoint: endpoint,
		Model:    "MiniMax-M2",
	})
	require.NoError(t, err)
	adapterContent, err := provider.Chat(ctx, "", messages)
	require.NoError(t, err)

	assert.Equal(t, legacyContent, adapterContent, "返回文本一致")
	assert.Equal(t, "你好，世界", adapterContent)

	requests := stub.snapshot()
	require.Len(t, requests, 2)
	legacyRequest, adapterRequest := requests[0], requests[1]

	assert.Equal(t, "/anthropic/v1/messages", legacyRequest.Path)
	assert.Equal(t, legacyRequest.Path, adapterRequest.Path, "请求路径一致")
	assert.Equal(t, legacyRequest.APIKey, adapterRequest.APIKey, "x-api-key 一致")
	assert.Equal(t, legacyRequest.AnthropicVersion, adapterRequest.AnthropicVersion, "anthropic-version 一致")

	for _, key := range []string{"model", "maxTokens", "system", "temperature", "messages"} {
		assert.Equal(t, legacyRequest.Body[key], adapterRequest.Body[key], "请求体字段 %s 一致", key)
	}
	assert.NotContains(t, adapterRequest.Body, "max_tokens", "minimax 变体使用 camelCase 字段")
	assert.NotContains(t, adapterRequest.Body, "stream", "非流式请求体不出现 stream（旧分支无该字段）")
	assert.Equal(t, 1.0, adapterRequest.Body["temperature"], "沿用旧分支的 temperature 默认值 1.0")
	assert.Equal(t, 4096.0, adapterRequest.Body["maxTokens"])
}

// TestRegistryBuildProviderUsesAnthropicAdapterForMiniMax 锁定 DB 实例路径的承载切换（PA-1）：
// `LLMProviderRegistry.buildProvider` 对 anthropic_messages/minimax 走适配器（不再映射旧
// `MiniMaxProvider` 分支），且同一桩服务下与旧分支的请求体字段 / 返回文本一致（等价性不因承载切换而变）。
func TestRegistryBuildProviderUsesAnthropicAdapterForMiniMax(t *testing.T) {
	ctx := context.Background()
	stub := newAnthropicStub(t)
	endpoint := stub.server.URL + "/anthropic/v1"
	messages := []LLMMessage{
		{Role: "system", Content: "你是 IT 助手"},
		{Role: "user", Content: "你好"},
	}

	registry := &LLMProviderRegistry{}
	provider, err := registry.buildProvider(&ent.LLMProviderConfig{
		Name:     "mm-db",
		Protocol: protocol.ProtocolAnthropicMessages,
		Variant:  protocol.VariantMiniMax,
		Endpoint: endpoint,
		Model:    "MiniMax-M2",
	}, "sk-minimax")
	require.NoError(t, err)
	require.IsType(t, &protocolProvider{}, provider, "DB 实例路径 anthropic/minimax 由适配器承载")
	assert.True(t, supportsToolCalling(provider), "适配器路径具备工具能力（PA-1 登记的能力增益）")

	adapterContent, err := provider.Chat(ctx, "", messages)
	require.NoError(t, err)

	legacy := &MiniMaxProvider{
		apiKey:  "sk-minimax",
		model:   "MiniMax-M2",
		baseURL: endpoint,
		client:  &http.Client{},
	}
	legacyContent, err := legacy.Chat(ctx, "", messages)
	require.NoError(t, err)
	assert.Equal(t, legacyContent, adapterContent, "返回文本一致")
	assert.Equal(t, "你好，世界", adapterContent)

	requests := stub.snapshot()
	require.Len(t, requests, 2)
	adapterRequest, legacyRequest := requests[0], requests[1]
	assert.Equal(t, legacyRequest.Path, adapterRequest.Path)
	assert.Equal(t, legacyRequest.APIKey, adapterRequest.APIKey)
	for _, key := range []string{"model", "maxTokens", "system", "temperature", "messages"} {
		assert.Equal(t, legacyRequest.Body[key], adapterRequest.Body[key], "请求体字段 %s 一致", key)
	}
	assert.Equal(t, 1.0, adapterRequest.Body["temperature"], "adapter_options 未配置时按变体默认 1.0")
	assert.Equal(t, 4096.0, adapterRequest.Body["maxTokens"])
}

// TestNewProviderFromConfigSwitchOnUsesAnthropicAdapterForMiniMax 锁定承载切换（方案 v1.0 PA-1）：
// 开关开启时静态 minimax 配置由适配器承载（旧分支保留给开关关闭与未适配变体）。
func TestNewProviderFromConfigSwitchOnUsesAnthropicAdapterForMiniMax(t *testing.T) {
	stub := newAnthropicStub(t)
	provider := NewProviderFromConfig(ProviderConfig{
		Provider:               "minimax",
		Model:                  "MiniMax-M2",
		APIKey:                 "test-key",
		Endpoint:               stub.server.URL + "/anthropic/v1",
		ProtocolAdapterEnabled: true,
	})
	require.IsType(t, &protocolProvider{}, provider, "minimax 静态配置在开关开启时由 anthropic_messages 适配器承载")

	content, err := provider.Chat(context.Background(), "", []LLMMessage{{Role: "user", Content: "你好"}})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", content)
}

// TestProtocolProviderVariantDefaults 锁定变体级默认参数与默认地址（PA-1）。
func TestProtocolProviderVariantDefaults(t *testing.T) {
	miniMax, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolAnthropicMessages,
		Variant:  " MiniMax ",
		APIKey:   "sk-test",
		Model:    "MiniMax-M2",
	})
	require.NoError(t, err)
	miniMaxProvider, ok := miniMax.(*protocolProvider)
	require.True(t, ok)
	assert.Equal(t, 1.0, miniMaxProvider.temperature, "minimax 变体沿用旧分支 temperature 1.0")
	assert.Equal(t, protocolProviderDefaultMaxTokens, miniMaxProvider.maxTokens)
	assert.Equal(t, "https://api.minimaxi.com/anthropic/v1", miniMaxProvider.defaultEndpoint())

	anthropicDefault, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolAnthropicMessages,
		APIKey:   "sk-test",
		Model:    "claude-sonnet-4",
	})
	require.NoError(t, err)
	anthropicProvider, ok := anthropicDefault.(*protocolProvider)
	require.True(t, ok)
	assert.Equal(t, protocolProviderDefaultTemperature, anthropicProvider.temperature, "官方 anthropic 沿用 BE-9 默认 0.3")
	assert.Equal(t, "https://api.anthropic.com", anthropicProvider.defaultEndpoint())

	openAI, err := NewProtocolProvider(ProtocolProviderOptions{Protocol: protocol.ProtocolOpenAIChatCompletions, Model: "gpt-4o-mini"})
	require.NoError(t, err)
	openAIProvider, ok := openAI.(*protocolProvider)
	require.True(t, ok)
	assert.Equal(t, protocolOpenAICompatibleDefaultEndpoint, openAIProvider.defaultEndpoint(), "未登记协议维持 BE-9 默认地址")
}

// TestAnthropicAdapterStreamingWithTools 覆盖旧分支不具备的流式 + 工具调用能力（能力增益）。
func TestAnthropicAdapterStreamingWithTools(t *testing.T) {
	stub := newAnthropicStub(t)
	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolAnthropicMessages,
		Variant:  protocol.VariantMiniMax,
		APIKey:   "sk-minimax",
		Endpoint: stub.server.URL + "/anthropic/v1",
		Model:    "MiniMax-M2",
	})
	require.NoError(t, err)

	streamingProvider, ok := provider.(ToolCallingStreamProvider)
	require.True(t, ok)

	var text string
	var toolCalls []LLMToolCall
	err = streamingProvider.ChatStreamWithTools(context.Background(), "", []LLMMessage{{Role: "user", Content: "查工单"}},
		[]LLMTool{{Name: "query_ticket", Description: "查询工单"}},
		func(delta string) { text += delta },
		func(calls []LLMToolCall) { toolCalls = calls })
	require.NoError(t, err)

	assert.Equal(t, "你好，世界", text)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "toolu_stub_1", toolCalls[0].ID)
	assert.Equal(t, "query_ticket", toolCalls[0].Name)
	assert.Equal(t, `{"id":"T-1"}`, toolCalls[0].Arguments)

	requests := stub.snapshot()
	require.Len(t, requests, 1)
	tools, ok := requests[0].Body["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, _ := tools[0].(map[string]any)
	assert.Equal(t, "query_ticket", tool["name"])
	assert.NotNil(t, tool["input_schema"])
}

// anthropicStubStreamBody 桩事件流：text 增量 + tool_use 增量 + 结束原因。
const anthropicStubStreamBody = `event: message_start
data: {"type":"message_start","message":{"id":"msg_stub_1","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"，世界"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_stub_1","name":"query_ticket","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"id\":\"T-1\"}"}}

event: message_delta
data: {"type":"message_delta","delta":{"stopReason":"tool_use"}}

event: message_stop
data: {"type":"message_stop"}

`
