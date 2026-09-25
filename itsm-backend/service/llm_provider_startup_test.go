package service

// BE-5 §4.1 启动硬约束矩阵证据：逐行锁定 5 行语义 + 探针可用性判定 + 主密钥解析。

import (
	"context"
	"testing"

	"itsm-backend/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEvaluateLLMKeyStartupMatrix 逐行锁定 §4.1（D11）启动硬约束矩阵。
func TestEvaluateLLMKeyStartupMatrix(t *testing.T) {
	const (
		realKey     = "sk-live-0123456789abcdef0123456789abcdef"
		placeholder = "sk-your-api-key-here"
	)

	cases := []struct {
		name       string
		apiKey     string
		production bool
		usable     int
		want       LLMKeyStartupAction
	}{
		{"第1行 真实密钥+生产+无实例 → Info", realKey, true, 0, LLMKeyStartupInfo},
		{"第5行 真实密钥+有实例 → Info（静态配置已存在）", realKey, true, 3, LLMKeyStartupInfo},
		{"第2行 占位符+生产+有可用 DB 实例 → Warn 正常启动", placeholder, true, 1, LLMKeyStartupWarn},
		{"第3行 占位符+生产+无 DB 实例 → Fatal 终止启动", placeholder, true, 0, LLMKeyStartupFatal},
		{"占位符+生产+实例全不可解密（探针计 0）→ Fatal", placeholder, true, 0, LLMKeyStartupFatal},
		{"第4行 占位符+开发+无实例 → Warn 降级", placeholder, false, 0, LLMKeyStartupWarn},
		{"空密钥+生产+无实例 → Fatal（空值同占位符）", "", true, 0, LLMKeyStartupFatal},
		{"空密钥+开发+有实例 → Warn", "", false, 2, LLMKeyStartupWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EvaluateLLMKeyStartup(tc.apiKey, tc.production, tc.usable))
		})
	}
}

// TestResolveLLMProviderEncryptionKey 锁定主密钥解析：env 优先，缺省回退派生密钥并标记 derived。
func TestResolveLLMProviderEncryptionKey(t *testing.T) {
	t.Setenv(LLMProviderEncryptionKeyEnv, "")
	key, derived := ResolveLLMProviderEncryptionKey("jwt-secret")
	assert.True(t, derived, "未显式配置时必须标记为派生回退（调用方据此打 Warn）")
	assert.Equal(t, "llm-provider-key-jwt-secret", key)

	t.Setenv(LLMProviderEncryptionKeyEnv, "  explicit-master-key  ")
	key, derived = ResolveLLMProviderEncryptionKey("jwt-secret")
	assert.False(t, derived)
	assert.Equal(t, "explicit-master-key", key)
}

// TestCountUsableLLMProviderInstances 锁定探针口径：只有"启用 + 未软删 + 密文可解密"的实例可用；
// 与 BE-2 registry 的 slot 可用性判定一致（无密文/解密失败一律不可用）。
func TestCountUsableLLMProviderInstances(t *testing.T) {
	client, encrypter := newLLMRegistryTestEnv(t)
	ctx := context.Background()
	base := llmRegistryTestInstance{TenantID: 1, Protocol: "openai_chat_completions"}

	// 可用：启用 + 密文可解密
	okCfg := base
	okCfg.Name = "prov-ok"
	okCfg.APIKey = "sk-live-ok"
	okCfg.Enabled = true
	createLLMRegistryTestInstance(t, client, encrypter, okCfg)

	// 禁用：不可用
	disabledCfg := base
	disabledCfg.Name = "prov-disabled"
	disabledCfg.APIKey = "sk-live-disabled"
	disabledCfg.Enabled = false
	createLLMRegistryTestInstance(t, client, encrypter, disabledCfg)

	// 软删：不可用
	deletedCfg := base
	deletedCfg.Name = "prov-deleted"
	deletedCfg.APIKey = "sk-live-deleted"
	deletedCfg.Enabled = true
	deletedCfg.SoftDelete = true
	createLLMRegistryTestInstance(t, client, encrypter, deletedCfg)

	// 无密文：不可用（registry decryptAPIKey 同语义）
	noKeyCfg := base
	noKeyCfg.Name = "prov-nokey"
	noKeyCfg.Enabled = true
	createLLMRegistryTestInstance(t, client, encrypter, noKeyCfg)

	// 密文无法用当前主密钥解密：不可用
	otherEncrypter := middleware.NewEncryptionService("another-master-key")
	ciphertext, err := otherEncrypter.Encrypt("sk-live-broken")
	require.NoError(t, err)
	_, err = client.LLMProviderConfig.Create().
		SetTenantID(1).
		SetName("prov-broken").
		SetDisplayName("prov-broken").
		SetProtocol("openai_chat_completions").
		SetEnabled(true).
		SetEncryptedAPIKey(ciphertext).
		Save(ctx)
	require.NoError(t, err)

	n, err := CountUsableLLMProviderInstances(ctx, client, encrypter)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "仅 prov-ok 满足 启用+未软删+可解密")

	// 保守语义：依赖缺失一律 0，不放行无兜底启动
	n, err = CountUsableLLMProviderInstances(ctx, nil, encrypter)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nil client → 0")

	n, err = CountUsableLLMProviderInstances(ctx, client, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nil decrypter → 0")

	// 禁用/软删/坏密文实例都不得被计为可用
	n, err = CountUsableLLMProviderInstances(ctx, client, otherEncrypter)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "换用匹配另一实例密文的主密钥后，只有该实例可用")
}
