package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B4-02：适配器路径的真实 usage 端到端接线（协议回包 → protocolProvider → 请求级 sink）。

func TestProtocolProvider_ReportsRealUsageFromStream(t *testing.T) {
	stub := newProtocolStub(t)
	stub.streamBody = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"

	provider := newAdapterBackedProvider(t, stub.server.URL+"/v1")

	var in, out int
	ctx := WithProviderUsageSink(context.Background(), func(promptTokens, completionTokens int) {
		in += promptTokens
		out += completionTokens
	})

	var text strings.Builder
	require.NoError(t, provider.(StreamingLLMProvider).ChatStream(ctx, "gpt-4o-mini",
		[]LLMMessage{{Role: "user", Content: "hi"}}, func(delta string) { text.WriteString(delta) }))

	assert.Equal(t, "hi", text.String())
	assert.Equal(t, 17, in, "prompt_tokens 应原值上报")
	assert.Equal(t, 5, out, "completion_tokens 应原值上报")

	// 请求体已请求 usage 末帧（B4-02）。
	reqs := stub.snapshot()
	require.Len(t, reqs, 1)
	streamOptions, ok := reqs[0].Body["stream_options"].(map[string]any)
	require.True(t, ok, "流式请求必须携带 stream_options")
	assert.Equal(t, true, streamOptions["include_usage"])
}

func TestProtocolProvider_NoUsageKeepsSinkSilent(t *testing.T) {
	stub := newProtocolStub(t) // 默认桩流不含 usage 帧
	provider := newAdapterBackedProvider(t, stub.server.URL+"/v1")

	called := false
	ctx := WithProviderUsageSink(context.Background(), func(int, int) { called = true })
	require.NoError(t, provider.(StreamingLLMProvider).ChatStream(ctx, "gpt-4o-mini",
		[]LLMMessage{{Role: "user", Content: "hi"}}, nil))
	assert.False(t, called, "无 usage 回包不得触发上报（保持估算口径）")
}

func TestProtocolProvider_NonStreamReportsUsage(t *testing.T) {
	stub := newProtocolStub(t)

	provider := newAdapterBackedProvider(t, stub.server.URL+"/v1")
	var in, out int
	ctx := WithProviderUsageSink(context.Background(), func(promptTokens, completionTokens int) {
		in += promptTokens
		out += completionTokens
	})

	// 非流式桩响应当前不含 usage → sink 保持静默（零行为变化回归）。
	_, err := provider.Chat(ctx, "gpt-4o-mini", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Zero(t, in+out, "无 usage 字段的非流式回包不得上报")
}

func TestProtocolProvider_NonStreamUsageReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-x","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":9,"completion_tokens":4}}`)
	}))
	t.Cleanup(server.Close)

	provider := newAdapterBackedProvider(t, server.URL+"/v1")
	var in, out int
	ctx := WithProviderUsageSink(context.Background(), func(promptTokens, completionTokens int) {
		in += promptTokens
		out += completionTokens
	})

	answer, err := provider.Chat(ctx, "gpt-4o-mini", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "pong", answer)
	assert.Equal(t, 9, in)
	assert.Equal(t, 4, out)
}
