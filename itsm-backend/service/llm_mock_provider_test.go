package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// M2-05：mock LLM provider（E2E 替身）的确定性行为契约。
//
// 覆盖：默认回复流式切片 / 无工具声明不触发 / 触发词命中后调用指定工具 / 指定名不在声明列表
// 时回落第一个 / 参数非法 JSON 规整为 {} / 开关判定（仅 true|1 开启）。

func TestMockProvider_流式回复切片(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{Reply: "abcdefghij", ChunkSize: 4})
	var chunks []string
	require.NoError(t, provider.ChatStream(context.Background(), "m", nil, func(chunk string) {
		chunks = append(chunks, chunk)
	}))
	require.Equal(t, []string{"abcd", "efgh", "ij"}, chunks)
	require.Equal(t, "abcdefghij", strings.Join(chunks, ""))

	single, err := provider.Chat(context.Background(), "m", nil)
	require.NoError(t, err)
	require.Equal(t, "abcdefghij", single)
	require.Equal(t, "mock", provider.Name())
}

func TestMockProvider_无工具声明不触发(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{Trigger: DefaultMockLLMTrigger})
	var calls []LLMToolCall
	require.NoError(t, provider.ChatStreamWithTools(
		context.Background(), "m",
		[]LLMMessage{{Role: "user", Content: "请调用工具 " + DefaultMockLLMTrigger}},
		nil,
		nil,
		func(calls_ []LLMToolCall) { calls = calls_ },
	))
	require.Empty(t, calls, "未声明任何工具时不得发起工具调用")
}

func TestMockProvider_触发词命中调用指定工具(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{
		ToolName: "mcp__gitlab__list_issues",
		ToolArgs: `{"state":"opened"}`,
	})
	tools := []LLMTool{
		{Name: "list_tickets"},
		{Name: "mcp__gitlab__list_issues"},
	}

	var (
		streamed string
		calls    []LLMToolCall
	)
	require.NoError(t, provider.ChatStreamWithTools(
		context.Background(), "m",
		[]LLMMessage{{Role: "user", Content: "帮我看看未关闭的缺陷 " + DefaultMockLLMTrigger}},
		tools,
		func(chunk string) { streamed += chunk },
		func(calls_ []LLMToolCall) { calls = calls_ },
	))

	require.NotEmpty(t, streamed, "应仍有文本流式输出")
	require.Len(t, calls, 1)
	require.Equal(t, "mcp__gitlab__list_issues", calls[0].Name)
	require.Equal(t, `{"state":"opened"}`, calls[0].Arguments)
	require.Equal(t, "mock-call-1", calls[0].ID, "调用 ID 应稳定可断言")
}

func TestMockProvider_触发词未命中不调用(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{})
	var calls []LLMToolCall
	require.NoError(t, provider.ChatStreamWithTools(
		context.Background(), "m",
		[]LLMMessage{{Role: "user", Content: "普通提问，不含触发词"}},
		[]LLMTool{{Name: "list_tickets"}},
		nil,
		func(calls_ []LLMToolCall) { calls = calls_ },
	))
	require.Empty(t, calls)
}

func TestMockProvider_指定工具不在声明列表时回落第一个(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{ToolName: "not-declared"})
	var calls []LLMToolCall
	require.NoError(t, provider.ChatStreamWithTools(
		context.Background(), "m",
		[]LLMMessage{{Role: "user", Content: DefaultMockLLMTrigger}},
		[]LLMTool{{Name: "first_tool"}, {Name: "second_tool"}},
		nil,
		func(calls_ []LLMToolCall) { calls = calls_ },
	))
	require.Len(t, calls, 1)
	require.Equal(t, "first_tool", calls[0].Name)
}

func TestMockProvider_工具名回落消息挂载的声明(t *testing.T) {
	provider := NewMockProvider(MockProviderOptions{})
	var calls []LLMToolCall
	require.NoError(t, provider.ChatStreamWithTools(
		context.Background(), "m",
		[]LLMMessage{{Role: "user", Content: DefaultMockLLMTrigger, Tools: []LLMTool{{Name: "from_message"}}}},
		nil,
		nil,
		func(calls_ []LLMToolCall) { calls = calls_ },
	))
	require.Len(t, calls, 1)
	require.Equal(t, "from_message", calls[0].Name, "请求侧 tools 为空时应回落到消息挂载的 Tools")
}

func TestMockProviderOptionsFromEnv_默认值与非法参数(t *testing.T) {
	t.Setenv(MockLLMReplyEnv, "")
	t.Setenv(MockLLMTriggerEnv, "")
	t.Setenv(MockLLMToolNameEnv, "")
	t.Setenv(MockLLMToolArgsEnv, "{不是 JSON")
	options := MockProviderOptionsFromEnv()
	require.Equal(t, DefaultMockLLMReply, options.Reply)
	require.Equal(t, DefaultMockLLMTrigger, options.Trigger)
	require.Equal(t, "{}", options.ToolArgs, "非法 JSON 必须规整为 {}")
}

func TestMockLLMEnabled_仅显式真值开启(t *testing.T) {
	for _, enabled := range []string{"true", "TRUE", "1", " true "} {
		t.Setenv(MockLLMEnabledEnv, enabled)
		require.True(t, MockLLMEnabled(), "值 %q 应视为开启", enabled)
	}
	for _, disabled := range []string{"", "false", "0", "yes", "on"} {
		t.Setenv(MockLLMEnabledEnv, disabled)
		require.False(t, MockLLMEnabled(), "值 %q 不应开启", disabled)
	}
}

func TestNewProviderFromConfig_Mock需显式开关(t *testing.T) {
	// 未开启开关：即使 provider=mock 也回退默认 provider（不得隐式启用替身）。
	t.Setenv(MockLLMEnabledEnv, "")
	provider := NewProviderFromConfig(ProviderConfig{Provider: "mock"})
	require.NotEqual(t, "mock", providerName(provider), "未显式开启时不得启用 mock provider")

	// 显式开启：返回 mock provider。
	t.Setenv(MockLLMEnabledEnv, "true")
	provider = NewProviderFromConfig(ProviderConfig{Provider: "mock"})
	mock, ok := provider.(*MockProvider)
	require.True(t, ok, "显式开启后应返回 *MockProvider，实际 %T", provider)
	require.Equal(t, "mock", mock.Name())
}

// providerName 读取 provider 的可读名（OpenAI/Azure 等实现无统一 Name 接口，用类型断言兜底）。
func providerName(provider LLMProvider) string {
	if named, ok := provider.(interface{ Name() string }); ok {
		return named.Name()
	}
	return ""
}
