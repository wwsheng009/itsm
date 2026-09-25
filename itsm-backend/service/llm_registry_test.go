package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/internal/llm/protocol"
	"itsm-backend/middleware"
)

// ---- 桩服务（同一份服务同时承载适配器路径与既有 OpenAIProvider 路径） ----

type protocolStub struct {
	server     *httptest.Server
	streamBody string

	mu       sync.Mutex
	mode     string
	requests []protocolStubRequest
}

type protocolStubRequest struct {
	Path          string
	Authorization string
	Body          map[string]any
}

func newProtocolStub(t *testing.T) *protocolStub {
	t.Helper()
	stub := &protocolStub{streamBody: buildProtocolStubStream(t)}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		stub.mu.Lock()
		stub.requests = append(stub.requests, protocolStubRequest{
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			Body:          payload,
		})
		mode := stub.mode
		stub.mu.Unlock()

		switch mode {
		case "401":
			writeProtocolStubJSON(w, http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		case "500":
			writeProtocolStubJSON(w, http.StatusInternalServerError, `{"error":{"message":"upstream exploded","type":"server_error","code":"internal_error"}}`)
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
		writeProtocolStubJSON(w, http.StatusOK, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"你好，世界"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *protocolStub) setMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *protocolStub) snapshot() []protocolStubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocolStubRequest(nil), s.requests...)
}

func writeProtocolStubJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// buildProtocolStubStream 固定 SSE 序列：文本两段 + 分片工具调用 + finish_reason。
func buildProtocolStubStream(t *testing.T) string {
	t.Helper()
	frame := func(delta map[string]any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}
	}
	chunks := []map[string]any{
		frame(map[string]any{"role": "assistant", "content": "你好"}),
		frame(map[string]any{"content": "，世界"}),
		frame(map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"id":       "call_1",
			"type":     "function",
			"function": map[string]any{"name": "get_time", "arguments": `{"tz":`},
		}}}),
		frame(map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"function": map[string]any{"arguments": `"UTC"}`},
		}}}),
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

func newAdapterBackedProvider(t *testing.T, endpoint string) LLMProvider {
	t.Helper()
	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolOpenAIChatCompletions,
		APIKey:   "test-key",
		Endpoint: endpoint,
		Model:    "gpt-4o-mini",
	})
	require.NoError(t, err)
	return provider
}

// ---- 开关与分派 ----

func TestProtocolAdapterEnabledReadsEnvOverride(t *testing.T) {
	t.Setenv(LLMProtocolAdapterEnabledEnv, "true")
	assert.True(t, protocolAdapterEnabled())

	t.Setenv(LLMProtocolAdapterEnabledEnv, "1")
	assert.True(t, protocolAdapterEnabled(), "ParseBool 兼容 1")

	t.Setenv(LLMProtocolAdapterEnabledEnv, "false")
	assert.False(t, protocolAdapterEnabled())

	t.Setenv(LLMProtocolAdapterEnabledEnv, "not-a-bool")
	assert.False(t, protocolAdapterEnabled(), "非法值按关闭（零破坏优先）")
}

func TestNewProviderFromConfigSwitchOffKeepsLegacyProviders(t *testing.T) {
	t.Setenv(LLMProtocolAdapterEnabledEnv, "")
	cases := []struct {
		provider string
		want     any
	}{
		{"openai", &OpenAIProvider{}},
		{"azure", &AzureProvider{}},
		{"local", &LocalProvider{}},
		{"minimax", &MiniMaxProvider{}},
		{"", &OpenAIProvider{}},
		{"unknown-vendor", &OpenAIProvider{}}, // 既有兜底：未识别值默认 OpenAI
	}
	for _, tc := range cases {
		t.Run("off/"+tc.provider, func(t *testing.T) {
			got := NewProviderFromConfig(ProviderConfig{Provider: tc.provider, Model: "gpt-4o-mini"})
			assert.IsType(t, tc.want, got, "开关关闭时构建类型与现状一致")
		})
	}
}

func TestNewProviderFromConfigSwitchOnUsesAdapterForOpenAIVariants(t *testing.T) {
	stub := newProtocolStub(t)
	cases := []struct {
		provider string
		want     any
	}{
		{"azure", &protocolProvider{}},        // openai_chat_completions/azure：与默认变体同一适配器
		{"local", &protocolProvider{}},        // openai_chat_completions/ollama：同上（OpenAI 兼容端点）
		{"unknown-vendor", &OpenAIProvider{}}, // 未识别 provider 保持既有兜底
	}
	for _, tc := range cases {
		t.Run("on/"+tc.provider, func(t *testing.T) {
			got := NewProviderFromConfig(ProviderConfig{
				Provider:               tc.provider,
				Model:                  "gpt-4o-mini",
				APIKey:                 "test-key",
				Endpoint:               stub.server.URL + "/v1",
				ProtocolAdapterEnabled: true,
			})
			assert.IsType(t, tc.want, got, "一协议一实现：已注册协议走适配器，未识别 provider 回退既有兜底")
		})
	}
}

func TestNewProviderFromConfigSwitchOnUsesProtocolAdapter(t *testing.T) {
	t.Setenv(LLMProtocolAdapterEnabledEnv, "")
	stub := newProtocolStub(t)
	ctx := context.Background()

	provider := NewProviderFromConfig(ProviderConfig{
		Provider:               "openai",
		Model:                  "gpt-4o-mini",
		APIKey:                 "test-key",
		Endpoint:               stub.server.URL + "/v1",
		ProtocolAdapterEnabled: true,
	})
	_, ok := provider.(*protocolProvider)
	require.True(t, ok, "开关开启 + openai 应命中原生适配器 provider")
	_, ok = provider.(StreamingLLMProvider)
	assert.True(t, ok, "保留流式能力")
	_, ok = provider.(ToolCallingStreamProvider)
	assert.True(t, ok, "保留工具调用能力")

	adapterText, err := provider.Chat(ctx, "", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", adapterText)

	legacy := NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	legacyText, err := legacy.Chat(ctx, "", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, legacyText, adapterText, "非流式正文与既有实现一致")

	requests := stub.snapshot()
	require.Len(t, requests, 2)
	adapterRequest, legacyRequest := requests[0], requests[1]
	assert.Equal(t, legacyRequest.Path, adapterRequest.Path, "命中同一端点")
	assert.Equal(t, legacyRequest.Authorization, adapterRequest.Authorization, "鉴权头一致")
	for _, key := range []string{"model", "messages", "max_tokens", "temperature"} {
		assert.Equal(t, legacyRequest.Body[key], adapterRequest.Body[key], "请求字段 %s 一致", key)
	}
	assert.NotContains(t, adapterRequest.Body, "stream")
}

// ---- 语义等价（流式 / 工具调用 / 错误映射） ----

func TestProtocolProviderStreamWithToolsEquivalence(t *testing.T) {
	stub := newProtocolStub(t)
	ctx := context.Background()
	parameters := map[string]any{
		"type":       "object",
		"properties": map[string]any{"tz": map[string]any{"type": "string"}},
		"required":   []string{"tz"},
	}
	// 多轮工具链：assistant 工具调用消息 + tool 结果消息，锁定消息数组逐字段等价
	messages := []LLMMessage{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "现在几点？"},
		{Role: "assistant", ToolCalls: []LLMToolCall{{ID: "call_0", Name: "get_time", Arguments: `{"tz":"UTC"}`}}},
		{Role: "tool", ToolCallID: "call_0", Content: `{"time":"12:00"}`},
	}
	tools := []LLMTool{{Name: "get_time", Description: "查询时间", Parameters: parameters}}

	legacy := NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	var legacyDeltas []string
	var legacyCalls []LLMToolCall
	require.NoError(t, legacy.ChatStreamWithTools(ctx, "", messages, tools,
		func(delta string) { legacyDeltas = append(legacyDeltas, delta) },
		func(calls []LLMToolCall) { legacyCalls = calls },
	))

	adapter := newAdapterBackedProvider(t, stub.server.URL+"/v1")
	var adapterDeltas []string
	var adapterCalls []LLMToolCall
	require.NoError(t, adapter.(ToolCallingStreamProvider).ChatStreamWithTools(ctx, "", messages, tools,
		func(delta string) { adapterDeltas = append(adapterDeltas, delta) },
		func(calls []LLMToolCall) { adapterCalls = calls },
	))

	assert.Equal(t, []string{"你好", "，世界"}, legacyDeltas)
	assert.Equal(t, legacyDeltas, adapterDeltas, "流式增量序列逐项一致")
	require.Len(t, legacyCalls, 1)
	require.Len(t, adapterCalls, 1)
	assert.Equal(t, legacyCalls[0], adapterCalls[0], "工具调用累积结果一致（含分片合并）")

	requests := stub.snapshot()
	require.Len(t, requests, 2)
	legacyRequest, adapterRequest := requests[0], requests[1]
	assert.Equal(t, true, adapterRequest.Body["stream"])
	assert.Equal(t, legacyRequest.Body["messages"], adapterRequest.Body["messages"], "多轮工具链消息数组逐字段一致")
	assert.Equal(t, legacyRequest.Body["tools"], adapterRequest.Body["tools"], "工具声明逐字段一致")
	assert.Equal(t, legacyRequest.Path, adapterRequest.Path)
}

func TestProtocolProviderPlainStreamEquivalence(t *testing.T) {
	stub := newProtocolStub(t)
	ctx := context.Background()
	messages := []LLMMessage{{Role: "user", Content: "打个招呼"}}

	legacy := NewOpenAIProvider("test-key", stub.server.URL+"/v1", "gpt-4o-mini")
	var legacyDeltas []string
	require.NoError(t, legacy.ChatStream(ctx, "", messages, func(delta string) { legacyDeltas = append(legacyDeltas, delta) }))

	adapter := newAdapterBackedProvider(t, stub.server.URL+"/v1")
	var adapterDeltas []string
	require.NoError(t, adapter.(StreamingLLMProvider).ChatStream(ctx, "", messages, func(delta string) { adapterDeltas = append(adapterDeltas, delta) }))

	// 既有实现的 ChatStream 会把文本增量逐条下发，工具调用分片不影响文本序列
	assert.Equal(t, legacyDeltas, adapterDeltas)
	assert.Equal(t, []string{"你好", "，世界"}, adapterDeltas)
}

func TestProtocolProviderErrorMappingMatchesGatewayRetrySemantics(t *testing.T) {
	ctx := context.Background()
	messages := []LLMMessage{{Role: "user", Content: "hi"}}

	t.Run("401 鉴权失败：状态码一致且不可重试", func(t *testing.T) {
		stub := newProtocolStub(t)
		stub.setMode("401")

		legacy := NewOpenAIProvider("bad-key", stub.server.URL+"/v1", "gpt-4o-mini")
		_, legacyErr := legacy.Chat(ctx, "", messages)
		require.Error(t, legacyErr)

		adapter := newAdapterBackedProvider(t, stub.server.URL+"/v1")
		_, adapterErr := adapter.Chat(ctx, "", messages)
		require.Error(t, adapterErr)

		var apiErr *openai.APIError
		require.ErrorAs(t, adapterErr, &apiErr)
		assert.Equal(t, http.StatusUnauthorized, apiErr.HTTPStatusCode, "网关分类依赖 *openai.APIError 状态码")
		var protocolErr *protocol.ProtocolError
		require.ErrorAs(t, adapterErr, &protocolErr)
		assert.Equal(t, "invalid_api_key", protocolErr.Code)
		assert.False(t, isTransientLLMError(adapterErr), "401 不重试（与既有实现一致）")
		assert.False(t, isTransientLLMError(legacyErr))
		assert.True(t, strings.HasPrefix(adapterErr.Error(), "OpenAI API error: "), "错误文案前缀与既有实现一致")
	})

	t.Run("500 上游故障：可重试（网关退避重试生效）", func(t *testing.T) {
		stub := newProtocolStub(t)
		stub.setMode("500")

		adapter := newAdapterBackedProvider(t, stub.server.URL+"/v1")
		_, adapterErr := adapter.Chat(ctx, "", messages)
		require.Error(t, adapterErr)

		var apiErr *openai.APIError
		require.ErrorAs(t, adapterErr, &apiErr)
		assert.Equal(t, http.StatusInternalServerError, apiErr.HTTPStatusCode)
		assert.True(t, isTransientLLMError(adapterErr), "5xx 可重试（与既有实现一致）")

		// 网关层端到端：可重试错误触发退避重试（既有口径：首次 + llmMaxRetries 次重试）
		baseline := len(stub.snapshot())
		gateway := NewLLMGateway(adapter, nil, nil, "openai")
		_, gatewayErr := gateway.Chat(ctx, "gpt-4o-mini", messages)
		require.Error(t, gatewayErr)
		assert.Len(t, stub.snapshot(), baseline+1+llmMaxRetries, "网关按既有口径退避重试")
	})
}

// ---- 未实现协议（422 哨兵）与 URL 拼接 ----

func TestNewProtocolProviderUnregisteredReturnsSentinel(t *testing.T) {
	// PA-3 收敛后 openai_responses 已由 Responses 适配器承载（不再是 422）：标准形态直接命中。
	provider, err := NewProtocolProvider(ProtocolProviderOptions{Protocol: protocol.ProtocolOpenAIResponses})
	require.NoError(t, err)
	require.IsType(t, &protocolProvider{}, provider)

	// PA-4 收敛后 google_gemini 已由 Gemini 适配器承载（不再是 422）：标准形态直接命中。
	provider, err = NewProtocolProvider(ProtocolProviderOptions{Protocol: protocol.ProtocolGoogleGemini})
	require.NoError(t, err)
	require.IsType(t, &protocolProvider{}, provider)

	// 枚举外协议（未注册）仍落到 422 哨兵。
	_, err = NewProtocolProvider(ProtocolProviderOptions{Protocol: "openai_completions"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProtocolNotImplemented)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)

	// PA-2 收敛后 azure / ollama 已由 openai_chat_completions 适配器承载（不再是 422）；
	// 不在适配器支持范围内的变体仍落到 422 哨兵（DB 实例路径由 BE-4/BE-8 映射）。
	_, err = NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant:  "unknown-variant",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProtocolNotImplemented)
	assert.ErrorIs(t, err, protocol.ErrAdapterNotFound)
}

func TestResolveProtocolURL(t *testing.T) {
	const apiPath = "/v1/chat/completions"
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"默认地址", "", "https://api.openai.com/v1/chat/completions"},
		{"endpoint 含 /v1（既有 config.yaml 写法）", "https://api.deepseek.com/v1", "https://api.deepseek.com/v1/chat/completions"},
		{"endpoint 尾斜杠", "https://api.deepseek.com/v1/", "https://api.deepseek.com/v1/chat/completions"},
		{"endpoint 不含版本段", "http://127.0.0.1:8080", "http://127.0.0.1:8080/v1/chat/completions"},
		{"endpoint 含多级前缀 + 版本段", "https://gw.example.com/openai/v1", "https://gw.example.com/openai/v1/chat/completions"},
		{"endpoint 已含完整路径", "https://gw.example.com/v1/chat/completions", "https://gw.example.com/v1/chat/completions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveProtocolURL(tc.endpoint, apiPath, ""))
		})
	}
}

// TestResolveProtocolURLEndpointFallback 锁定变体级默认地址回退（PA-1）：
// anthropic 官方与 minimax 兼容端点在 endpoint 缺省时使用协议包登记地址，
// 其余协议维持 BE-9 的 OpenAI 兼容默认地址（零破坏）。
func TestResolveProtocolURLEndpointFallback(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		apiPath  string
		fallback string
		want     string
	}{
		{
			name:     "minimax 缺省 endpoint：与旧 MiniMaxProvider baseURL 同址",
			apiPath:  "/v1/messages",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolAnthropicMessages, protocol.VariantMiniMax),
			want:     "https://api.minimaxi.com/anthropic/v1/messages",
		},
		{
			name:     "anthropic 官方缺省 endpoint",
			apiPath:  "/v1/messages",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolAnthropicMessages, protocol.VariantDefault),
			want:     "https://api.anthropic.com/v1/messages",
		},
		{
			name:     "minimax 显式 endpoint 已带 /v1：只补资源段",
			endpoint: "https://api.minimaxi.com/anthropic/v1",
			apiPath:  "/v1/messages",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolAnthropicMessages, protocol.VariantMiniMax),
			want:     "https://api.minimaxi.com/anthropic/v1/messages",
		},
		{
			name:     "未登记协议回退 OpenAI 兼容默认地址",
			apiPath:  "/v1/chat/completions",
			fallback: protocol.DefaultEndpoint("openai_completions", protocol.VariantDefault),
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			// PA-4：gemini 登记官方默认地址；资源段含模型名与操作（由 ModelPathAdapter 给出）。
			name:     "google_gemini 缺省 endpoint：官方地址 + 模型资源路径",
			apiPath:  "/v1beta/models/gemini-2.0-flash:generateContent",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolGoogleGemini, protocol.VariantDefault),
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			name:     "google_gemini endpoint 已带 /v1beta：只补资源段",
			endpoint: "https://gemini-proxy.example.com/v1beta",
			apiPath:  "/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolGoogleGemini, protocol.VariantDefault),
			want:     "https://gemini-proxy.example.com/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
		},
		{
			// PA-3：responses 不登记默认 endpoint（与标准 chat 形态同址），资源段由适配器给出。
			name:     "openai_responses 不登记默认地址：回退 OpenAI 兼容默认地址 + /v1/responses",
			apiPath:  "/v1/responses",
			fallback: protocol.DefaultEndpoint(protocol.ProtocolOpenAIResponses, protocol.VariantDefault),
			want:     "https://api.openai.com/v1/responses",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveProtocolURL(tc.endpoint, tc.apiPath, tc.fallback))
		})
	}
}

// ============================================================================
// BE-2：LLMProviderRegistry 单测（租户快照 / TTL / 失效 / 解密 / 构建 / 降级）
// ============================================================================
//
// 逐条锁定主计划 §3.2/§3.3 语义：
// override 严格命中且不回退、租户默认 → 静态回退链、跨租户 NOT_FOUND、缓存 TTL 与 Invalidate、
// 解密失败不 panic（broken decrypter）、个人默认命中/失效降级、日志无明文密钥、并发安全。

// ---- 测试基建 ----

// llmRegistryTestInstance 一条测试用 provider 实例（APIKey 为明文，写库前加密）。
type llmRegistryTestInstance struct {
	TenantID   int
	Name       string
	Protocol   string
	Variant    string
	Model      string
	Endpoint   string
	Deployment string
	// APIKey 明文；空串表示不写 encrypted_api_key（模拟无密钥实例）。
	APIKey     string
	Enabled    bool
	IsDefault  bool
	SoftDelete bool
	Options    map[string]interface{}
}

func newLLMRegistryTestEnv(t *testing.T) (*ent.Client, *middleware.EncryptionService) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { _ = client.Close() })
	return client, middleware.NewEncryptionService("llm-registry-test-secret")
}

func createLLMRegistryTestInstance(t *testing.T, client *ent.Client, encrypter *middleware.EncryptionService, cfg llmRegistryTestInstance) *ent.LLMProviderConfig {
	t.Helper()
	builder := client.LLMProviderConfig.Create().
		SetTenantID(cfg.TenantID).
		SetName(cfg.Name).
		SetDisplayName(cfg.Name).
		SetProtocol(cfg.Protocol).
		SetVariant(cfg.Variant).
		SetModel(cfg.Model).
		SetEndpoint(cfg.Endpoint).
		SetDeployment(cfg.Deployment).
		SetEnabled(cfg.Enabled).
		SetIsDefault(cfg.IsDefault)
	if cfg.APIKey != "" {
		ciphertext, err := encrypter.Encrypt(cfg.APIKey)
		require.NoError(t, err)
		builder = builder.SetEncryptedAPIKey(ciphertext)
	}
	if len(cfg.Options) > 0 {
		builder = builder.SetAdapterOptions(cfg.Options)
	}
	if cfg.SoftDelete {
		builder = builder.SetDeletedAt(time.Now())
	}
	record, err := builder.Save(context.Background())
	require.NoError(t, err)
	return record
}

// brokenLLMSecretDecrypter 模拟解密服务故障（§3.2 第 4 条：不 panic、该 slot 标记不可用）。
type brokenLLMSecretDecrypter struct{ err error }

func (d brokenLLMSecretDecrypter) Decrypt(string) (string, error) { return "", d.err }

// llmRegistryLogText 把 observer 捕获的日志（含结构化字段）拼成可断言的文本。
func llmRegistryLogText(logs *observer.ObservedLogs) string {
	var builder strings.Builder
	for _, entry := range logs.All() {
		builder.WriteString(entry.Message)
		for key, value := range entry.ContextMap() {
			builder.WriteString(" ")
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(fmt.Sprintf("%v", value))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

// ---- override（请求级显式选择）：严格命中、不回退 ----

func TestLLMProviderRegistryOverrideHit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()
	stub := newProtocolStub(t)

	const plaintextKey = "sk-deepseek-abcdefgh-1234"
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1,
		Name:     "deepseek-prod",
		Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model:    "deepseek-chat",
		Endpoint: stub.server.URL + "/v1",
		APIKey:   plaintextKey,
		Enabled:  true,
	})

	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())
	slot, source, err := registry.Resolve(ctx, 1, "deepseek-prod")
	require.NoError(t, err)
	assert.Equal(t, ProviderSourceRequest, source)
	assert.Equal(t, "deepseek-prod", slot.Key)
	assert.Equal(t, protocol.ProtocolOpenAIChatCompletions, slot.Protocol)
	assert.Equal(t, protocol.VariantDefault, slot.Variant)
	assert.Equal(t, "deepseek-chat", slot.Model)
	require.NotNil(t, slot.Provider)
	_, adapterBacked := slot.Provider.(*protocolProvider)
	assert.True(t, adapterBacked, "openai_chat_completions 默认变体由 BE-9 协议适配器承载")
	assert.True(t, slot.SupportsTools, "适配器 provider 具备工具调用能力")

	// 端到端：解析出的 slot 可直接调用（endpoint / model / 鉴权头来自实例记录）。
	text, err := slot.Provider.Chat(ctx, "", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", text)
	requests := stub.snapshot()
	require.Len(t, requests, 1)
	assert.Equal(t, "Bearer "+plaintextKey, requests[0].Authorization)
	assert.Equal(t, "deepseek-chat", requests[0].Body["model"])

	// 跨租户不可见：同 key 对租户 2 是 NOT_FOUND（§3.2 第 5 条租户隔离）。
	otherSlot, otherSource, otherErr := registry.Resolve(ctx, 2, "deepseek-prod")
	assert.Nil(t, otherSlot.Provider)
	assert.Empty(t, otherSource)
	assert.ErrorIs(t, otherErr, ErrProviderNotFound)
}

func TestLLMProviderRegistryOverrideNotFoundDisabledAndSoftDeleted(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "live", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-live-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "turned-off", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-off-abcdefgh-1234", Enabled: false,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "gone", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-gone-abcdefgh-1234", Enabled: true, SoftDelete: true,
	})

	// 静态配置有效：override 路径一律不回退，避免"以为切了其实没切"（§3.2/§3.3）。
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{
		Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234",
	}, zap.NewNop().Sugar())

	t.Run("override 不存在 → NOT_FOUND", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 1, "missing")
		assert.ErrorIs(t, err, ErrProviderNotFound)
		assert.Nil(t, slot.Provider)
		assert.Empty(t, source)
	})

	t.Run("override 已禁用 → DISABLED", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 1, "turned-off")
		assert.ErrorIs(t, err, ErrProviderDisabled)
		assert.Nil(t, slot.Provider)
		assert.Empty(t, source)
	})

	t.Run("override 已软删 → NOT_FOUND", func(t *testing.T) {
		_, _, err := registry.Resolve(ctx, 1, "gone")
		assert.ErrorIs(t, err, ErrProviderNotFound)
	})

	t.Run("override 命中可用实例", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 1, "live")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceRequest, source)
		assert.Equal(t, "live", slot.Key)
	})

	t.Run("快照加载后新建的实例可立即解析（定向查询覆盖）", func(t *testing.T) {
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 1, Name: "created-later", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "gpt-4o", Endpoint: "https://api.example.com/v1", APIKey: "sk-later-abcdefgh-1234", Enabled: true,
		})
		slot, source, err := registry.Resolve(ctx, 1, "created-later")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceRequest, source)
		assert.Equal(t, "created-later", slot.Key)
	})
}

func TestLLMProviderRegistryOverrideKeyMissingDoesNotPanicAndSkipsPlaintextLogs(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	// 形态一：解密服务整体故障（broken decrypter）；形态二：实例未配置密钥。
	broken := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "broken-decrypt", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-broken-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "no-key", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", Enabled: true,
	})
	brokenCiphertext := broken.EncryptedAPIKey
	require.NotEmpty(t, brokenCiphertext)

	staticCfg := ProviderConfig{Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234"}
	core, logs := observer.New(zap.DebugLevel)
	registry := NewLLMProviderRegistry(client, brokenLLMSecretDecrypter{err: fmt.Errorf("decrypt service down")},
		staticCfg, zap.New(core).Sugar())

	// broken decrypter：不 panic，错误是 ErrProviderKeyMissing。
	slot, source, err := registry.Resolve(ctx, 1, "broken-decrypt")
	assert.ErrorIs(t, err, ErrProviderKeyMissing)
	assert.Nil(t, slot.Provider)
	assert.Empty(t, source)
	assert.NotContains(t, err.Error(), brokenCiphertext, "错误信息不得回带密文")

	// 无密钥实例同样按 KEY_MISSING 处理。
	_, _, noKeyErr := registry.Resolve(ctx, 1, "no-key")
	assert.ErrorIs(t, noKeyErr, ErrProviderKeyMissing)

	text := llmRegistryLogText(logs)
	assert.Contains(t, text, "broken-decrypt", "告警必须带上实例 name 便于定位")
	assert.Contains(t, text, "no-key")
	assert.NotContains(t, text, brokenCiphertext, "日志不得出现密文")
	assert.NotContains(t, text, "sk-broken-abcdefgh-1234", "日志不得出现明文密钥")

	// 形态三（失败隔离）：坏密文只让该实例不可用，同租户其它实例照常可用。
	const corruptedCiphertext = "not-a-valid-ciphertext"
	corrupted := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "corrupted-key", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-corrupted-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "usable", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-usable-abcdefgh-1234", Enabled: true,
	})
	_, err = client.LLMProviderConfig.UpdateOneID(corrupted.ID).SetEncryptedAPIKey(corruptedCiphertext).Save(ctx)
	require.NoError(t, err)

	healthyCore, healthyLogs := observer.New(zap.DebugLevel)
	healthyRegistry := NewLLMProviderRegistry(client, encrypter, staticCfg, zap.New(healthyCore).Sugar())

	_, _, corruptedErr := healthyRegistry.Resolve(ctx, 1, "corrupted-key")
	assert.ErrorIs(t, corruptedErr, ErrProviderKeyMissing)

	usableSlot, usableSource, usableErr := healthyRegistry.Resolve(ctx, 1, "usable")
	require.NoError(t, usableErr, "同租户其它实例不受坏密文影响")
	assert.Equal(t, ProviderSourceRequest, usableSource)
	assert.Equal(t, "usable", usableSlot.Key)
	require.NotNil(t, usableSlot.Provider)

	healthyText := llmRegistryLogText(healthyLogs)
	assert.Contains(t, healthyText, "corrupted-key")
	assert.Contains(t, healthyText, "usable")
	assert.NotContains(t, healthyText, corruptedCiphertext, "日志不得出现密文")
	assert.NotContains(t, healthyText, "sk-corrupted-abcdefgh-1234", "日志不得出现明文密钥")
	assert.NotContains(t, text, "sk-usable-abcdefgh-1234", "日志不得出现明文密钥")
}

// ---- 空 override：租户默认 → 静态回退（§3.3 第三/四级） ----

func TestLLMProviderRegistryTenantDefaultAndStaticFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	staticCfg := ProviderConfig{Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234"}
	registry := NewLLMProviderRegistry(client, encrypter, staticCfg, zap.NewNop().Sugar())

	t.Run("租户无默认 → 静态回退", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 9, "")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceStatic, source)
		assert.Equal(t, "openai", slot.Key)
		assert.Equal(t, "gpt-4o-mini", slot.Model)
		require.NotNil(t, slot.Provider)
		switch slot.Provider.(type) {
		case *OpenAIProvider, *protocolProvider:
		default:
			t.Fatalf("静态回退 provider 类型异常: %T", slot.Provider)
		}
	})

	record := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 9, Name: "tenant-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "qwen-plus", Endpoint: "https://api.example.com/v1", APIKey: "sk-tenant-abcdefgh-1234",
		Enabled: true, IsDefault: true,
	})
	// 他租户的同名默认实例：验证默认解析同样受 tenant_id 约束。
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 10, Name: "other-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "glm-4", Endpoint: "https://api.example.com/v1", APIKey: "sk-other-abcdefgh-1234",
		Enabled: true, IsDefault: true,
	})
	// 前一个用例已缓存"无默认"快照：管理写操作后调用 Invalidate 立即收敛（生产同口径）。
	registry.Invalidate(9)
	registry.Invalidate(10)

	t.Run("租户默认命中 → source=tenant", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 9, "")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceTenant, source)
		assert.Equal(t, "tenant-default", slot.Key)
		assert.Equal(t, "qwen-plus", slot.Model)
		require.NotNil(t, slot.Provider)
	})

	t.Run("租户默认只在自身租户生效", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 10, "")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceTenant, source)
		assert.Equal(t, "other-default", slot.Key)
		assert.NotEqual(t, "qwen-plus", slot.Model)
	})

	t.Run("override 优先于租户默认", func(t *testing.T) {
		slot, source, err := registry.Resolve(ctx, 9, "tenant-default")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceRequest, source)
		assert.Equal(t, "tenant-default", slot.Key)
		assert.Equal(t, "qwen-plus", slot.Model)
	})

	t.Run("默认实例被禁用 → 视为无默认并回退静态（§3.3 只认 enabled 默认）", func(t *testing.T) {
		_, err := client.LLMProviderConfig.UpdateOneID(record.ID).SetEnabled(false).Save(ctx)
		require.NoError(t, err)
		registry.Invalidate(9)

		slot, source, err := registry.Resolve(ctx, 9, "")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceStatic, source)
		assert.Equal(t, "openai", slot.Key)
		// 显式选择该禁用实例仍可见地失败（不回退）。
		_, _, explicitErr := registry.Resolve(ctx, 9, "tenant-default")
		assert.ErrorIs(t, explicitErr, ErrProviderDisabled)
	})
}

func TestLLMProviderRegistryTenantDefaultUnavailableSurfacesError(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	// 默认实例未配置密钥：设计取舍是"可见地失败"，不在 registry 内静默回退静态
	//（避免租户显式选择的 provider 被平台级静态密钥悄悄替换；是否回落由 BE-3 决定）。
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 3, Name: "default-without-key", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", Enabled: true, IsDefault: true,
	})
	registry := NewLLMProviderRegistry(client, encrypter,
		ProviderConfig{Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234"},
		zap.NewNop().Sugar())

	slot, source, err := registry.Resolve(ctx, 3, "")
	assert.ErrorIs(t, err, ErrProviderKeyMissing)
	assert.Nil(t, slot.Provider)
	assert.Empty(t, source)
}

func TestLLMProviderRegistryStaticFallbackUnavailable(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())
	slot, source, err := registry.Resolve(ctx, 4, "")
	assert.ErrorIs(t, err, ErrProviderUnavailable)
	assert.Nil(t, slot.Provider)
	assert.Empty(t, source)

	// nil client 也不 panic：无 DB 实例 → 静态配置为空 → UNAVAILABLE。
	nilClientRegistry := NewLLMProviderRegistry(nil, nil, ProviderConfig{}, zap.NewNop().Sugar())
	_, _, nilClientErr := nilClientRegistry.Resolve(ctx, 4, "")
	assert.ErrorIs(t, nilClientErr, ErrProviderUnavailable)
}

// ---- 缓存：Invalidate 立即失效、TTL 过期收敛 ----

func TestLLMProviderRegistryInvalidateAndTTLExpiry(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	record := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "cache-target", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "model-v1", Endpoint: "https://api.example.com/v1", APIKey: "sk-cache-abcdefgh-1234",
		Enabled: true,
	})
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())
	current := time.Now()
	registry.now = func() time.Time { return current }

	resolveModel := func(t *testing.T) string {
		t.Helper()
		slot, source, err := registry.Resolve(ctx, 1, "cache-target")
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceRequest, source)
		return slot.Model
	}
	updateModel := func(t *testing.T, model string) {
		t.Helper()
		_, err := client.LLMProviderConfig.UpdateOneID(record.ID).SetModel(model).Save(ctx)
		require.NoError(t, err)
	}

	assert.Equal(t, "model-v1", resolveModel(t))

	// 未失效：快照命中，他副本的写入对本实例不可见（多副本靠 TTL 收敛）。
	updateModel(t, "model-v2")
	assert.Equal(t, "model-v1", resolveModel(t), "快照未失效时继续命中缓存")

	// Invalidate 立即失效：下一次 Resolve 立刻看到新数据。
	registry.Invalidate(1)
	assert.Equal(t, "model-v2", resolveModel(t), "Invalidate 后立即生效")

	// TTL 未过期：仍然命中缓存。
	updateModel(t, "model-v3")
	current = current.Add(1 * time.Second)
	assert.Equal(t, "model-v2", resolveModel(t), "TTL 内继续命中缓存")

	// TTL 过期：重建快照后收敛到新数据。
	current = current.Add(llmRegistryCacheTTL + time.Second)
	assert.Equal(t, "model-v3", resolveModel(t), "TTL 过期后收敛到新数据")

	// TTL 过期同样覆盖"禁用"这类状态变更。
	_, err := client.LLMProviderConfig.UpdateOneID(record.ID).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	current = current.Add(llmRegistryCacheTTL + time.Second)
	_, _, err = registry.Resolve(ctx, 1, "cache-target")
	assert.ErrorIs(t, err, ErrProviderDisabled)
}

// ---- ResolveUserDefault：个人默认命中 / 失效降级 ----

func TestLLMProviderRegistryResolveUserDefault(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	preferredInstance := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "preferred", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "qwen-max", Endpoint: "https://api.example.com/v1", APIKey: "sk-preferred-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 2, Name: "other-tenant", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "glm-4", Endpoint: "https://api.example.com/v1", APIKey: "sk-other-abcdefgh-1234", Enabled: true,
	})

	registry := NewLLMProviderRegistry(client, encrypter,
		ProviderConfig{Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234"},
		zap.NewNop().Sugar())

	t.Run("无偏好记录 → NOT_FOUND", func(t *testing.T) {
		slot, source, err := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, err, ErrProviderNotFound)
		assert.Nil(t, slot.Provider)
		assert.Empty(t, source)
	})

	preference, err := client.LLMUserPreference.Create().
		SetUserID(42).
		SetTenantID(1).
		SetProviderKey("preferred").
		Save(ctx)
	require.NoError(t, err)

	t.Run("命中同租户启用实例 → source=user", func(t *testing.T) {
		slot, source, err := registry.ResolveUserDefault(ctx, 1, 42)
		require.NoError(t, err)
		assert.Equal(t, ProviderSourceUser, source)
		assert.Equal(t, "preferred", slot.Key)
		assert.Equal(t, "qwen-max", slot.Model)
		require.NotNil(t, slot.Provider)
	})

	t.Run("provider_key 为空 → NOT_FOUND（跟随租户默认）", func(t *testing.T) {
		_, err := client.LLMUserPreference.UpdateOneID(preference.ID).SetProviderKey("").Save(ctx)
		require.NoError(t, err)
		_, _, resolveErr := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, resolveErr, ErrProviderNotFound)

		_, err = client.LLMUserPreference.UpdateOneID(preference.ID).SetProviderKey("preferred").Save(ctx)
		require.NoError(t, err)
	})

	t.Run("跨租户引用 → NOT_FOUND（租户隔离，不泄漏他租户实例）", func(t *testing.T) {
		_, err := client.LLMUserPreference.UpdateOneID(preference.ID).SetProviderKey("other-tenant").Save(ctx)
		require.NoError(t, err)
		slot, source, resolveErr := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, resolveErr, ErrProviderNotFound)
		assert.Nil(t, slot.Provider)
		assert.Empty(t, source)

		_, err = client.LLMUserPreference.UpdateOneID(preference.ID).SetProviderKey("preferred").Save(ctx)
		require.NoError(t, err)
	})

	t.Run("实例被禁用 → 降级 NOT_FOUND 且不改写偏好", func(t *testing.T) {
		_, err := client.LLMProviderConfig.UpdateOneID(preferredInstance.ID).
			SetEnabled(false).Save(ctx)
		require.NoError(t, err)
		registry.Invalidate(1)

		slot, source, resolveErr := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, resolveErr, ErrProviderNotFound)
		assert.Nil(t, slot.Provider)
		assert.Empty(t, source)

		stored, err := client.LLMUserPreference.Get(ctx, preference.ID)
		require.NoError(t, err)
		assert.Equal(t, "preferred", stored.ProviderKey, "失效降级不得自动改写个人偏好")
	})

	t.Run("实例被软删 → 降级 NOT_FOUND", func(t *testing.T) {
		_, err := client.LLMProviderConfig.UpdateOneID(preferredInstance.ID).
			SetDeletedAt(time.Now()).Save(ctx)
		require.NoError(t, err)
		registry.Invalidate(1)

		_, _, resolveErr := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, resolveErr, ErrProviderNotFound)
	})

	t.Run("实例启用但密钥缺失 → 透出 KEY_MISSING（不视而不见）", func(t *testing.T) {
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 1, Name: "no-key-user-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "qwen-max", Endpoint: "https://api.example.com/v1", Enabled: true,
		})
		_, err := client.LLMUserPreference.UpdateOneID(preference.ID).SetProviderKey("no-key-user-default").Save(ctx)
		require.NoError(t, err)
		registry.Invalidate(1)

		_, _, resolveErr := registry.ResolveUserDefault(ctx, 1, 42)
		assert.ErrorIs(t, resolveErr, ErrProviderKeyMissing)
	})

	t.Run("非法参数（userID<=0）→ NOT_FOUND", func(t *testing.T) {
		_, _, resolveErr := registry.ResolveUserDefault(ctx, 1, 0)
		assert.ErrorIs(t, resolveErr, ErrProviderNotFound)
	})
}

// ---- (协议, 变体) → provider 构建分派（§3.1.4 映射表 + 槽位协议） ----

func TestLLMProviderRegistryProtocolVariantMapping(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	const tenantID = 5
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "azure-instance", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant: protocol.VariantAzure, Model: "gpt-4o", Endpoint: "https://example.openai.azure.com/",
		Deployment: "gpt4o-deploy", APIKey: "sk-azure-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "azure-deployment-fallback", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant: protocol.VariantAzure, Endpoint: "https://example.openai.azure.com/",
		Deployment: "gpt4o-deploy", APIKey: "sk-azure-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "ollama-instance", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant: protocol.VariantOllama, Model: "llama3.1", Endpoint: "http://127.0.0.1:11434",
		APIKey: "ollama-local-placeholder", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "minimax-instance", Protocol: protocol.ProtocolAnthropicMessages,
		Variant: protocol.VariantMiniMax, Model: "MiniMax-M2", APIKey: "sk-minimax-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "anthropic-default", Protocol: protocol.ProtocolAnthropicMessages,
		Model: "MiniMax-M2", APIKey: "sk-anthropic-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "responses-instance", Protocol: protocol.ProtocolOpenAIResponses,
		Model: "gpt-4o", APIKey: "sk-responses-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "gemini-instance", Protocol: protocol.ProtocolGoogleGemini,
		Model: "gemini-2.0-flash", APIKey: "sk-gemini-abcdefgh-1234", Enabled: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "bogus-variant", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant: "bogus", Model: "gpt-4o", APIKey: "sk-bogus-abcdefgh-1234", Enabled: true,
	})

	core, logs := observer.New(zap.DebugLevel)
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.New(core).Sugar())

	cases := []struct {
		key       string
		want      LLMProvider
		wantTools bool
		wantErr   error
	}{
		// 一协议一实现（PA-2 收敛）：openai_chat_completions 的 azure / ollama 变体与默认变体
		// 共用同一适配器（变体差异在 endpoint / model 归一上），能力位按真实实现探测。
		{key: "azure-instance", want: &protocolProvider{}, wantTools: true},
		{key: "azure-deployment-fallback", want: &protocolProvider{}, wantTools: true},
		{key: "ollama-instance", want: &protocolProvider{}, wantTools: true},
		// PA-1 承载切换：anthropic_messages 两变体由协议适配器承载（不再映射旧 MiniMaxProvider 分支），
		// 能力位按真实实现探测（适配器实现 ToolCallingStreamProvider，属登记的能力增益）。
		{key: "minimax-instance", want: &protocolProvider{}, wantTools: true},
		{key: "anthropic-default", want: &protocolProvider{}, wantTools: true},
		// PA-3 承载切换：openai_responses 标准形态由 Responses 适配器承载（唯一分支，无旧分支回退）。
		{key: "responses-instance", want: &protocolProvider{}, wantTools: true},
		// PA-4 承载切换：google_gemini 由 Gemini 适配器承载（4/4 协议适配器化收官）。
		{key: "gemini-instance", want: &protocolProvider{}, wantTools: true},
		// 非法变体仍按 UNAVAILABLE 处理（不 panic）。
		{key: "bogus-variant", wantErr: ErrProviderUnavailable},
	}
	for _, testCase := range cases {
		t.Run(testCase.key, func(t *testing.T) {
			slot, source, err := registry.Resolve(ctx, tenantID, testCase.key)
			if testCase.wantErr != nil {
				assert.ErrorIs(t, err, testCase.wantErr, "槽位协议/非法变体必须按 UNAVAILABLE 处理（不 panic）")
				assert.Nil(t, slot.Provider)
				assert.Empty(t, source)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ProviderSourceRequest, source)
			assert.IsType(t, testCase.want, slot.Provider)
			assert.Equal(t, testCase.wantTools, slot.SupportsTools, "能力位按真实实现探测（聚合 provider 的实现能力）")
		})
	}

	// azure 变体 model 缺省时回退 deployment（旧 AzureProvider `actualModel := deploymentID` 口径）。
	azureSlot, _, err := registry.Resolve(ctx, tenantID, "azure-deployment-fallback")
	require.NoError(t, err)
	assert.Equal(t, "gpt4o-deploy", azureSlot.Provider.(*protocolProvider).model)

	// 协议名归一化：库中大写/带空白也能命中同一槽位并落库为小写枚举。
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: tenantID, Name: "normalized", Protocol: "  OpenAI_Chat_Completions  ",
		Model: "gpt-4o", Endpoint: "https://api.example.com/v1", APIKey: "sk-norm-abcdefgh-1234", Enabled: true,
	})
	slot, _, err := registry.Resolve(ctx, tenantID, "normalized")
	require.NoError(t, err)
	assert.Equal(t, protocol.ProtocolOpenAIChatCompletions, slot.Protocol)

	text := llmRegistryLogText(logs)
	assert.Contains(t, text, "protocol_unimplemented")
	assert.Contains(t, text, "gemini-instance")
	assert.Contains(t, text, "bogus-variant")
	// PA-3 后 responses 实例可构建（构建成功日志会带 provider 名），不得再有 unavailable 告警。
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "unavailable") {
			assert.NotContains(t, line, "responses-instance", "PA-3 后 responses 实例可构建，不再告警")
		}
	}
}

// ---- 并发安全（-race 友好）：并发 Resolve + Invalidate ----

func TestLLMProviderRegistryConcurrentResolveAndInvalidate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 6, Name: "concurrent", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-concurrent-abcdefgh-1234",
		Enabled: true, IsDefault: true,
	})
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 6, Name: "concurrent-disabled", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: "sk-disabled-abcdefgh-1234",
		Enabled: false,
	})

	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

	const readers = 8
	const iterations = 30
	var wg sync.WaitGroup
	failures := make(chan error, readers*2)

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if slot, source, err := registry.Resolve(ctx, 6, "concurrent"); err != nil || slot.Provider == nil || source != ProviderSourceRequest {
					failures <- fmt.Errorf("override resolve: key=%q source=%q err=%v", slot.Key, source, err)
					return
				}
				if slot, source, err := registry.Resolve(ctx, 6, ""); err != nil || slot.Provider == nil || source != ProviderSourceTenant {
					failures <- fmt.Errorf("tenant default resolve: key=%q source=%q err=%v", slot.Key, source, err)
					return
				}
				if _, _, err := registry.Resolve(ctx, 6, "concurrent-disabled"); !errors.Is(err, ErrProviderDisabled) {
					failures <- fmt.Errorf("disabled resolve: err=%v", err)
					return
				}
				if _, _, err := registry.ResolveUserDefault(ctx, 6, 4242); !errors.Is(err, ErrProviderNotFound) {
					failures <- fmt.Errorf("user default resolve: err=%v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < iterations; j++ {
			registry.Invalidate(6)
		}
	}()
	wg.Wait()
	close(failures)

	for failure := range failures {
		t.Fatal(failure)
	}
}

// ---- 日志脱敏：只允许 name + 脱敏值 ----

func TestLLMProviderRegistryLogsNeverExposeSecret(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")

	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()

	const plaintext = "sk-super-secret-abcdefghijklmnop-9876"
	record := createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "secret-instance", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "gpt-4o-mini", Endpoint: "https://api.example.com/v1", APIKey: plaintext, Enabled: true,
	})
	ciphertext := record.EncryptedAPIKey
	require.NotEmpty(t, ciphertext)
	require.NotEqual(t, plaintext, ciphertext)

	core, logs := observer.New(zap.DebugLevel)
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.New(core).Sugar())
	slot, source, err := registry.Resolve(ctx, 1, "secret-instance")
	require.NoError(t, err)
	require.NotNil(t, slot.Provider)
	assert.Equal(t, ProviderSourceRequest, source)

	text := llmRegistryLogText(logs)
	require.NotEmpty(t, text, "确保确实产出日志，避免脱敏断言空转")
	assert.Contains(t, text, "secret-instance")
	assert.Contains(t, text, common.MaskSecret(plaintext), "日志应输出脱敏值")
	assert.NotContains(t, text, plaintext, "日志不得出现明文密钥")
	assert.NotContains(t, text, ciphertext, "日志不得出现密文")

	// 解密失败路径：错误信息与告警同样不得回带明文/密文。
	brokenCore, brokenLogs := observer.New(zap.DebugLevel)
	brokenRegistry := NewLLMProviderRegistry(client, brokenLLMSecretDecrypter{err: errors.New("decrypt service down")},
		ProviderConfig{}, zap.New(brokenCore).Sugar())
	_, _, brokenErr := brokenRegistry.Resolve(ctx, 1, "secret-instance")
	require.ErrorIs(t, brokenErr, ErrProviderKeyMissing)
	assert.NotContains(t, brokenErr.Error(), plaintext)
	assert.NotContains(t, brokenErr.Error(), ciphertext)

	brokenText := llmRegistryLogText(brokenLogs)
	require.NotEmpty(t, brokenText)
	assert.Contains(t, brokenText, "secret-instance")
	assert.NotContains(t, brokenText, plaintext)
	assert.NotContains(t, brokenText, ciphertext)
}
