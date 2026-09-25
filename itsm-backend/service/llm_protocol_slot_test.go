package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLLMProtocolSlotMapping 逐行锁定主计划 §3.1.4 映射表：
// 4 种协议各有用例（含变体细分），未实现协议（槽位）一律 ErrProtocolNotImplemented（422 错误码）。
func TestLLMProtocolSlotMapping(t *testing.T) {
	cases := []struct {
		name         string
		protocol     string
		variant      string
		wantProvider string
		wantErr      error
	}{
		{
			name:         "openai_chat_completions/标准变体映射到 openai",
			protocol:     LLMProtocolOpenAIChatCompletions,
			variant:      LLMVariantDefault,
			wantProvider: "openai",
		},
		{
			name:         "openai_chat_completions/azure 映射到 azure",
			protocol:     LLMProtocolOpenAIChatCompletions,
			variant:      LLMVariantAzure,
			wantProvider: "azure",
		},
		{
			name:         "openai_chat_completions/ollama 映射到 local",
			protocol:     LLMProtocolOpenAIChatCompletions,
			variant:      LLMVariantOllama,
			wantProvider: "local",
		},
		{
			name:         "anthropic_messages/标准变体映射到 minimax",
			protocol:     LLMProtocolAnthropicMessages,
			variant:      LLMVariantDefault,
			wantProvider: "minimax",
		},
		{
			name:         "anthropic_messages/minimax 变体映射到 minimax",
			protocol:     LLMProtocolAnthropicMessages,
			variant:      LLMVariantMiniMax,
			wantProvider: "minimax",
		},
		{
			name:     "openai_responses 标准形态无旧分支可回退（空映射值）",
			protocol: LLMProtocolOpenAIResponses,
			variant:  LLMVariantDefault,
		},
		{
			name:     "google_gemini 是槽位，返回 AI_PROTOCOL_NOT_IMPLEMENTED",
			protocol: LLMProtocolGoogleGemini,
			wantErr:  ErrProtocolNotImplemented,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := MapProtocolToLegacyProvider(testCase.protocol, testCase.variant)
			if testCase.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, testCase.wantErr)
				assert.Empty(t, provider)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantProvider, provider)
			// 校验路径与映射路径必须同源同判定。
			assert.NoError(t, ValidateLLMProtocolVariant(testCase.protocol, testCase.variant))
		})
	}
}

// TestLLMProtocolSlotMappingNormalization 锁定去空白 + 大小写归一（管理 API 入参容错）。
func TestLLMProtocolSlotMappingNormalization(t *testing.T) {
	provider, err := MapProtocolToLegacyProvider(" OpenAI_Chat_Completions ", " AZURE ")
	require.NoError(t, err)
	assert.Equal(t, "azure", provider)

	provider, err = MapProtocolToLegacyProvider("Anthropic_Messages", "MiniMax")
	require.NoError(t, err)
	assert.Equal(t, "minimax", provider)
}

// TestLLMProtocolSlotValidation 覆盖 §3.4 的 422 分支：协议非法 / 协议未实现 / variant 非法。
func TestLLMProtocolSlotValidation(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		variant  string
		wantErr  error
	}{
		{name: "未知协议", protocol: "openai_completions", variant: "", wantErr: ErrProtocolInvalid},
		{name: "空协议", protocol: "  ", variant: "", wantErr: ErrProtocolInvalid},
		{
			name:     "openai_chat_completions 不接受 minimax 变体",
			protocol: LLMProtocolOpenAIChatCompletions,
			variant:  LLMVariantMiniMax,
			wantErr:  ErrProtocolVariantInvalid,
		},
		{
			name:     "anthropic_messages 不接受 azure 变体",
			protocol: LLMProtocolAnthropicMessages,
			variant:  LLMVariantAzure,
			wantErr:  ErrProtocolVariantInvalid,
		},
		{
			name:     "anthropic_messages 不接受 ollama 变体",
			protocol: LLMProtocolAnthropicMessages,
			variant:  LLMVariantOllama,
			wantErr:  ErrProtocolVariantInvalid,
		},
		{
			name:     "openai_responses 不接受 azure 变体",
			protocol: LLMProtocolOpenAIResponses,
			variant:  LLMVariantAzure,
			wantErr:  ErrProtocolVariantInvalid,
		},
		{
			name:     "google_gemini 即使变体非空也是槽位",
			protocol: LLMProtocolGoogleGemini,
			variant:  LLMVariantAzure,
			wantErr:  ErrProtocolNotImplemented,
		},
		{
			name:     "azure 变体合法",
			protocol: LLMProtocolOpenAIChatCompletions,
			variant:  LLMVariantAzure,
		},
		{
			name:     "minimax 变体合法",
			protocol: LLMProtocolAnthropicMessages,
			variant:  LLMVariantMiniMax,
		},
		{
			name:     "openai_responses 标准变体合法",
			protocol: LLMProtocolOpenAIResponses,
			variant:  LLMVariantDefault,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateLLMProtocolVariant(testCase.protocol, testCase.variant)
			if testCase.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

// TestLLMProtocolSlotCapabilities 能力位按 P0 现有实现填写（§3.4 available / §11.2 槽位 4）。
func TestLLMProtocolSlotCapabilities(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		variant  string
		want     LLMCapabilities
	}{
		{
			name:     "openai_chat_completions 默认变体：流式 + 工具 + 推理",
			protocol: LLMProtocolOpenAIChatCompletions,
			variant:  LLMVariantDefault,
			want: LLMCapabilities{
				SupportsStream: true, SupportsTools: true, SupportsReasoning: true, Implemented: true,
			},
		},
		{
			name:     "openai_chat_completions azure 变体：仅非流式 Chat",
			protocol: LLMProtocolOpenAIChatCompletions,
			variant:  LLMVariantAzure,
			want:     LLMCapabilities{Implemented: true},
		},
		{
			name:     "openai_chat_completions ollama 变体：仅非流式 Chat",
			protocol: LLMProtocolOpenAIChatCompletions,
			variant:  LLMVariantOllama,
			want:     LLMCapabilities{Implemented: true},
		},
		{
			name:     "anthropic_messages minimax 变体：仅非流式 Chat",
			protocol: LLMProtocolAnthropicMessages,
			variant:  LLMVariantMiniMax,
			want:     LLMCapabilities{Implemented: true},
		},
		{
			name:     "openai_responses 标准形态：适配器为唯一承载，流式 + 工具 + 推理",
			protocol: LLMProtocolOpenAIResponses,
			variant:  LLMVariantDefault,
			want: LLMCapabilities{
				SupportsStream: true, SupportsTools: true, SupportsReasoning: true, Implemented: true,
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := LLMProtocolCapabilities(testCase.protocol, testCase.variant)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}

	// 槽位：能力位零值且返回 422 哨兵。
	got, err := LLMProtocolCapabilities(LLMProtocolGoogleGemini, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProtocolNotImplemented)
	assert.Equal(t, LLMCapabilities{}, got)
}

// TestLLMProtocolOptions 锁定枚举顺序 / implemented 标记 / 变体白名单（DTO 与前端下拉的数据源）。
// 顺序口径 = 主计划 §3.1.4 映射表行序：openai_chat_completions → anthropic_messages →
// openai_responses → google_gemini（与 llmProtocolSlots 声明顺序一致）。
func TestLLMProtocolOptions(t *testing.T) {
	assert.Equal(t, []string{
		LLMProtocolOpenAIChatCompletions,
		LLMProtocolAnthropicMessages,
		LLMProtocolOpenAIResponses,
		LLMProtocolGoogleGemini,
	}, SupportedLLMProtocols())

	options := LLMProtocolOptions()
	require.Len(t, options, 4)

	assert.Equal(t, LLMProtocolOpenAIChatCompletions, options[0].Protocol)
	assert.True(t, options[0].Implemented)
	assert.Equal(t, []string{"", "azure", "ollama"}, options[0].Variants)
	assert.True(t, options[0].Capabilities.SupportsStream)

	assert.Equal(t, LLMProtocolAnthropicMessages, options[1].Protocol)
	assert.True(t, options[1].Implemented)
	assert.Equal(t, []string{"", "minimax"}, options[1].Variants)

	assert.Equal(t, LLMProtocolOpenAIResponses, options[2].Protocol)
	assert.True(t, options[2].Implemented)
	assert.Equal(t, []string{""}, options[2].Variants)
	assert.True(t, options[2].Capabilities.SupportsStream)
	assert.True(t, options[2].Capabilities.SupportsTools)
	assert.True(t, options[2].Capabilities.SupportsReasoning)

	assert.Equal(t, LLMProtocolGoogleGemini, options[3].Protocol)
	assert.False(t, options[3].Implemented)
	assert.Empty(t, options[3].Variants)

	assert.True(t, IsImplementedLLMProtocol(LLMProtocolOpenAIChatCompletions))
	assert.True(t, IsImplementedLLMProtocol(LLMProtocolAnthropicMessages))
	assert.True(t, IsImplementedLLMProtocol(LLMProtocolOpenAIResponses))
	assert.False(t, IsImplementedLLMProtocol(LLMProtocolGoogleGemini))
	assert.False(t, IsImplementedLLMProtocol("unknown"))

	assert.Equal(t, []string{""}, SupportedLLMVariants(LLMProtocolOpenAIResponses))
	assert.Empty(t, SupportedLLMVariants(LLMProtocolGoogleGemini))
	assert.Empty(t, SupportedLLMVariants("unknown"))
}

// TestValidateAdapterOptions 覆盖 §3.5 adapter_options 约束：JSON 对象、非敏感键、4KB 上限。
func TestValidateAdapterOptions(t *testing.T) {
	validCases := []struct {
		name string
		raw  string
	}{
		{name: "未设置（空串）", raw: ""},
		{name: "未设置（空白）", raw: "   \n\t"},
		{name: "未设置（null）", raw: "null"},
		{name: "空对象", raw: "{}"},
		{name: "azure api_version", raw: `{"api_version":"2024-02-15-preview"}`},
		{name: "ollama keep_alive", raw: `{"keep_alive":"5m"}`},
		{name: "max_tokens 不是敏感键", raw: `{"max_tokens":1000,"num_tokens":32}`},
		{name: "嵌套非敏感参数", raw: `{"ollama":{"keep_alive":"5m","num_ctx":4096}}`},
		{name: "数组值", raw: `{"headers":["x-trace","x-tenant"]}`},
	}

	for _, testCase := range validCases {
		t.Run("合法/"+testCase.name, func(t *testing.T) {
			assert.NoError(t, ValidateAdapterOptions(testCase.raw))
		})
	}

	deepValue := "1"
	for index := 0; index < llmAdapterOptionsMaxDepth+2; index++ {
		deepValue = `{"lvl":` + deepValue + `}`
	}

	invalidCases := []struct {
		name string
		raw  string
	}{
		{name: "非 JSON", raw: "api_version=2024"},
		{name: "顶层数组", raw: `["a","b"]`},
		{name: "顶层字符串", raw: `"api_version"`},
		{name: "顶层数字", raw: "123"},
		{name: "尾随内容", raw: `{"a":1} {"b":2}`},
		{name: "api_key 敏感键", raw: `{"api_key":"sk-xxx"}`},
		{name: "apiKey 驼峰敏感键", raw: `{"apiKey":"sk-xxx"}`},
		{name: "x-api-key 连字符敏感键", raw: `{"x-api-key":"sk-xxx"}`},
		{name: "access_token 敏感键", raw: `{"access_token":"t"}`},
		{name: "password 敏感键", raw: `{"Password":"p"}`},
		{name: "authorization 敏感键", raw: `{"authorization":"Bearer x"}`},
		{name: "嵌套敏感键", raw: `{"nested":{"client_secret":"s"}}`},
		{name: "数组内敏感键", raw: `{"items":[{"private_key":"k"}]}`},
		{name: "嵌套过深", raw: deepValue},
	}

	for _, testCase := range invalidCases {
		t.Run("非法/"+testCase.name, func(t *testing.T) {
			err := ValidateAdapterOptions(testCase.raw)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrAdapterOptionsInvalid)
		})
	}
}

// TestValidateAdapterOptionsMaxBytes 锁定 4KB 边界（含边界值本身合法、超 1 字节拒绝）。
func TestValidateAdapterOptionsMaxBytes(t *testing.T) {
	const wrapper = `{"note":""}`
	filler := strings.Repeat("a", LLMAdapterOptionsMaxBytes-len(wrapper))
	exact := `{"note":"` + filler + `"}`
	require.Len(t, exact, LLMAdapterOptionsMaxBytes)
	assert.NoError(t, ValidateAdapterOptions(exact))

	oversized := `{"note":"` + filler + `a"}`
	require.Len(t, oversized, LLMAdapterOptionsMaxBytes+1)
	err := ValidateAdapterOptions(oversized)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAdapterOptionsInvalid)
}

// TestIsSensitiveAdapterOptionKey 锁定敏感键判定：不误伤合法参数，不漏判常见密钥键名。
func TestIsSensitiveAdapterOptionKey(t *testing.T) {
	sensitive := []string{
		"api_key", "apiKey", "API-KEY", "x-api-key", "key", "token", "access_token",
		"auth_token", "secret", "client_secret", "clientSecret", "password", "passwd",
		"authorization", "Authorization", "credential", "credentials", "private_key",
		"access_key", "session_key", "bearer",
	}
	for _, name := range sensitive {
		assert.True(t, IsSensitiveAdapterOptionKey(name), "应判定为敏感键: %s", name)
	}

	benign := []string{
		"api_version", "keep_alive", "max_tokens", "num_tokens", "temperature",
		"deployment_name", "region", "monkey", "num_ctx", "top_p",
	}
	for _, name := range benign {
		assert.False(t, IsSensitiveAdapterOptionKey(name), "不应判定为敏感键: %s", name)
	}
}
