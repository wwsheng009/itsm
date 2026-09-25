package service

// BE-5（主计划 §4.1）启动硬约束矩阵 + 实例密钥主密钥解析。
//
// 本文件只放"启动期判定"所需的纯函数与只读探针，供 internal/bootstrap/app.go 调用；
// 目的：把矩阵语义从 bootstrap 的巨型装配函数里抽出来，让 §4.1 的 5 行语义逐行可单测。

import (
	"context"
	"os"
	"strings"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/llmproviderconfig"
)

// LLMProviderEncryptionKeyEnv 实例密钥加密主密钥的环境变量名（生产必须显式配置）。
const LLMProviderEncryptionKeyEnv = "LLM_PROVIDER_ENCRYPTION_KEY"

// llmProviderDerivedKeyPrefix 未显式配置时的开发回退密钥前缀（与 connector 配置加密同策略）。
const llmProviderDerivedKeyPrefix = "llm-provider-key-"

// startupProbeLimit 探针一次最多检查的实例数：判定"是否存在可用实例"无需全表扫描。
const startupProbeLimit = 20

// ResolveLLMProviderEncryptionKey 解析实例密钥加密主密钥：环境变量优先；
// 未配置时回退到派生自 JWT secret 的开发密钥（derived=true），生产应显式配置
// （调用方在 derived=true 时打 Warn，§3.5：密钥不落日志）。
func ResolveLLMProviderEncryptionKey(jwtSecret string) (key string, derived bool) {
	if explicit := strings.TrimSpace(os.Getenv(LLMProviderEncryptionKeyEnv)); explicit != "" {
		return explicit, false
	}
	return llmProviderDerivedKeyPrefix + jwtSecret, true
}

// LLMKeyStartupAction 是 §4.1 启动硬约束矩阵的判定结果。
type LLMKeyStartupAction int

const (
	// LLMKeyStartupInfo 静态密钥为真实值：Info（掩码）并正常启动。
	LLMKeyStartupInfo LLMKeyStartupAction = iota
	// LLMKeyStartupWarn 占位符/空值但允许降级启动：开发环境，或生产已有可用 DB 实例兜底。
	LLMKeyStartupWarn
	// LLMKeyStartupFatal 占位符/空值 + 生产 + 无可用 DB 实例：终止启动（保持阻断1 防护意图）。
	LLMKeyStartupFatal
)

// EvaluateLLMKeyStartup 实现主计划 §4.1 启动硬约束矩阵（D11，纯函数，app.go 与单测共用）：
//
//	静态密钥 | DB 启用实例（可解密） | 环境 | 行为
//	真实     | 任意                   | 任意 | Info，正常启动
//	占位符   | 有                     | 生产 | Warn（以 DB 实例为准），正常启动
//	占位符   | 无                     | 生产 | Fatal，终止启动
//	占位符   | 任意                   | 开发 | Warn，AI 功能按现状降级/禁用
//
// 注意：usableDBInstances 由调用方保证"仅当多 Provider 开关开启（D9）时才可能 > 0"——
// 开关关闭时 DB 实例不参与解析链，此时必须按"无兜底"处理，否则会放过一个既无静态密钥、
// 又无可用解析路径的生产启动（同时破坏 QA-3「开关关闭时与现状一致」门禁）。
func EvaluateLLMKeyStartup(apiKey string, production bool, usableDBInstances int) LLMKeyStartupAction {
	if !common.IsPlaceholderSecret(apiKey) {
		return LLMKeyStartupInfo
	}
	if production && usableDBInstances <= 0 {
		return LLMKeyStartupFatal
	}
	return LLMKeyStartupWarn
}

// CountUsableLLMProviderInstances 统计"可用 DB 实例"数（§4.1 探针）：
// 启用 + 未软删 + encrypted_api_key 非空且可解密且解密结果非空——与 BE-2 registry
// 的 slot 可用性判定（decryptAPIKey + buildErr）逐条对齐，避免"启动说可用、运行期不可用"。
//
// 保守语义：client/decrypter 为 nil 时返回 0（宁可维持静态硬约束，也不放行无兜底的启动）。
// 查询/解密异常返回 error，由调用方决定降级策略；本函数不 panic、不写任何数据。
func CountUsableLLMProviderInstances(ctx context.Context, client *ent.Client, decrypter SecretDecrypter) (int, error) {
	if client == nil || decrypter == nil {
		return 0, nil
	}
	records, err := client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.EnabledEQ(true),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Limit(startupProbeLimit).
		All(ctx)
	if err != nil {
		return 0, err
	}
	usable := 0
	for _, record := range records {
		ciphertext := strings.TrimSpace(record.EncryptedAPIKey)
		if ciphertext == "" {
			continue
		}
		plaintext, decErr := decrypter.Decrypt(ciphertext)
		if decErr != nil || strings.TrimSpace(plaintext) == "" {
			continue
		}
		usable++
	}
	return usable, nil
}
