package service

// ============================================================================
// F2（2026-10-18）：应用内（DB）LLM 配置 → guidance sidecar 请求级目标
// ============================================================================
//
// 逐条锁定语义：
//   - 支持形态（openai_chat_completions 默认/ollama、anthropic_messages 默认/官方/minimax）
//     返回 URL/模型/明文密钥，URL 与网关同口径（resolveProtocolURL + adapter.GetAPIPath）；
//   - 不支持形态（azure / openai_responses / google_gemini）→ ok=false；
//   - 无租户默认 / 跨租户 / 禁用 / 软删 / 无密钥 → ok=false；
//   - 静态回退不进入本接口（单一事实源）；nil client / 非法租户 → (false, nil)。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/internal/llm/protocol"
)

func TestResolveGuidanceSpec_OpenAICompatibleTenantDefault(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "deepseek-prod", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "deepseek-chat", Endpoint: "https://api.deepseek.com/v1",
		APIKey: "sk-guidance-abcdefgh-1234", Enabled: true, IsDefault: true,
	})
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

	spec, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, protocol.ProtocolOpenAIChatCompletions, spec.Protocol)
	assert.Empty(t, spec.Variant)
	// 与网关同口径：endpoint 已带版本段 /v1 → 只补资源段。
	assert.Equal(t, "https://api.deepseek.com/v1/chat/completions", spec.URL)
	assert.Equal(t, "deepseek-chat", spec.Model)
	assert.Equal(t, "sk-guidance-abcdefgh-1234", spec.APIKey, "密钥须为解密后的明文（sidecar 直接使用）")
}

func TestResolveGuidanceSpec_EndpointFullPath_Passthrough(t *testing.T) {
	// 管理员把 endpoint 显式配成完整请求地址（endpoint_full_path=true）：
	// 与网关一致，原样下发、不再拼接。
	client, encrypter := newLLMRegistryTestEnv(t)
	createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
		TenantID: 1, Name: "qianfan-v2", Protocol: protocol.ProtocolOpenAIChatCompletions,
		Model: "deepseek-v4.1-flash", Endpoint: "https://qianfan.baidubce.com/v2/chat/completions",
		EndpointFullPath: true, APIKey: "sk-qianfan-abcdefgh-1234", Enabled: true, IsDefault: true,
	})
	registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

	spec, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "https://qianfan.baidubce.com/v2/chat/completions", spec.URL)
	assert.Equal(t, "deepseek-v4.1-flash", spec.Model)
}

func TestResolveGuidanceSpec_DefaultEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		variant  string
		wantURL  string
	}{
		{
			name: "openai 无 endpoint → 官方默认地址",
			protocol: protocol.ProtocolOpenAIChatCompletions,
			wantURL:  "https://api.openai.com/v1/chat/completions",
		},
		{
			name: "ollama 无 endpoint → 本地默认地址",
			protocol: protocol.ProtocolOpenAIChatCompletions, variant: protocol.VariantOllama,
			wantURL: "http://localhost:11434/v1/chat/completions",
		},
		{
			name: "anthropic 官方无 endpoint → 官方地址 + /v1/messages",
			protocol: protocol.ProtocolAnthropicMessages,
			wantURL:  "https://api.anthropic.com/v1/messages",
		},
		{
			name: "minimax 无 endpoint → anthropic 兼容默认地址 + /messages",
			protocol: protocol.ProtocolAnthropicMessages, variant: protocol.VariantMiniMax,
			wantURL: "https://api.minimaxi.com/anthropic/v1/messages",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, encrypter := newLLMRegistryTestEnv(t)
			createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
				TenantID: 1, Name: "spec-instance", Protocol: tc.protocol, Variant: tc.variant,
				Model: "model-x", APIKey: "sk-default-abcdefgh-1234", Enabled: true, IsDefault: true,
			})
			registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

			spec, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, tc.wantURL, spec.URL)
		})
	}
}

func TestResolveGuidanceSpec_UnsupportedForms_NotDelivered(t *testing.T) {
	// sidecar 不支持的形态必须返回 ok=false（调用方跳过 sidecar 走网关）：
	// azure（api-key 头 + 部署路径）、openai_responses、google_gemini。
	cases := []struct {
		name     string
		protocol string
		variant  string
	}{
		{name: "azure 变体", protocol: protocol.ProtocolOpenAIChatCompletions, variant: protocol.VariantAzure},
		{name: "openai_responses", protocol: protocol.ProtocolOpenAIResponses},
		{name: "google_gemini", protocol: protocol.ProtocolGoogleGemini},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, encrypter := newLLMRegistryTestEnv(t)
			createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
				TenantID: 1, Name: "unsupported-instance", Protocol: tc.protocol, Variant: tc.variant,
				Model: "model-x", Deployment: "deploy-x", Endpoint: "https://api.example.com/v1",
				APIKey: "sk-unsupported-abcdefgh-1234", Enabled: true, IsDefault: true,
			})
			registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

			spec, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
			require.NoError(t, err)
			assert.False(t, ok, "不支持形态不得下发")
			assert.Equal(t, GuidanceLLMSpec{}, spec)
		})
	}
}

func TestResolveGuidanceSpec_NoUsableTarget(t *testing.T) {
	t.Run("无租户默认 → 不下发（跨租户隔离）", func(t *testing.T) {
		client, encrypter := newLLMRegistryTestEnv(t)
		// 租户 2 的默认实例不得泄漏给租户 1；租户 1 自己的实例非默认。
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 2, Name: "tenant2-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "model-x", Endpoint: "https://api.example.com/v1",
			APIKey: "sk-t2-abcdefgh-1234", Enabled: true, IsDefault: true,
		})
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 1, Name: "tenant1-non-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "model-x", Endpoint: "https://api.example.com/v1",
			APIKey: "sk-t1-abcdefgh-1234", Enabled: true, IsDefault: false,
		})
		registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

		_, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("禁用 / 软删的默认 → 不下发", func(t *testing.T) {
		client, encrypter := newLLMRegistryTestEnv(t)
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 3, Name: "disabled-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "model-x", Endpoint: "https://api.example.com/v1",
			APIKey: "sk-disabled-abcdefgh-1234", Enabled: false, IsDefault: true,
		})
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 3, Name: "soft-deleted-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "model-x", Endpoint: "https://api.example.com/v1",
			APIKey: "sk-deleted-abcdefgh-1234", Enabled: true, IsDefault: true, SoftDelete: true,
		})
		registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

		_, ok, err := registry.ResolveGuidanceSpec(context.Background(), 3)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("无密钥默认 → 不下发（解密失败等价 slot 不可用）", func(t *testing.T) {
		client, encrypter := newLLMRegistryTestEnv(t)
		createLLMRegistryTestInstance(t, client, encrypter, llmRegistryTestInstance{
			TenantID: 4, Name: "keyless-default", Protocol: protocol.ProtocolOpenAIChatCompletions,
			Model: "model-x", Endpoint: "https://api.example.com/v1",
			Enabled: true, IsDefault: true,
		})
		registry := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())

		_, ok, err := registry.ResolveGuidanceSpec(context.Background(), 4)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("静态回退不进入本接口（单一事实源）", func(t *testing.T) {
		client, encrypter := newLLMRegistryTestEnv(t)
		registry := NewLLMProviderRegistry(client, encrypter,
			ProviderConfig{Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-static-abcdefgh-1234"},
			zap.NewNop().Sugar())

		_, ok, err := registry.ResolveGuidanceSpec(context.Background(), 9)
		require.NoError(t, err)
		assert.False(t, ok, "静态配置不属于应用内（DB）下发范围")
	})

	t.Run("nil client / 非法租户 → 安全返回", func(t *testing.T) {
		registry := NewLLMProviderRegistry(nil, nil, ProviderConfig{}, zap.NewNop().Sugar())

		_, ok, err := registry.ResolveGuidanceSpec(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, ok)

		client, encrypter := newLLMRegistryTestEnv(t)
		registry2 := NewLLMProviderRegistry(client, encrypter, ProviderConfig{}, zap.NewNop().Sugar())
		_, ok2, err2 := registry2.ResolveGuidanceSpec(context.Background(), 0)
		require.NoError(t, err2)
		assert.False(t, ok2)
	})
}
