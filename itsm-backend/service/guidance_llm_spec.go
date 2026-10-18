package service

// 本文件是「应用内（DB）LLM 配置 → guidance sidecar」的请求级下发接线层（F2，2026-10-18）。
//
// 背景：guidance sidecar（itsm-ai-service）是单进程单配置服务，只读自身 env/config.yaml；
// 而应用内配置（llm_provider_configs 表，逐租户 + AES-GCM 密文）的解析链只存在于 Go 注册表。
// 二者此前完全隔离：sidecar 未配置 LLM_* 时在 /triage 静默降级为关键词分类（F1 已识别该降级，
// 后端回落网关），应用内配置永远到不了 sidecar。
//
// 方案：请求级下发——TriageService 调用 sidecar 前解析本租户默认实例，把「最终请求 URL /
// 模型 / 密钥」随 POST /triage 的 llm 字段一并下发；sidecar 收到 llm 时不再读自身配置。
// URL 由后端按与协议适配器完全相同的规则（resolveProtocolURL + adapter.GetAPIPath() +
// protocol.DefaultEndpoint）解析——凡网关能调通的实例，sidecar 与网关逐字节同址，无口径漂移。
//
// 能力边界（sidecar 仅实现这两种调用形态；其余形态返回 ok=false，调用方跳过 sidecar 走网关）：
//   - openai_chat_completions（标准 / ollama 变体）；
//   - anthropic_messages（官方 / minimax 变体；azure 因 api-key 头与部署路径差异不支持）。
//
// 安全：spec 含明文密钥，仅存在于进程内存与内网请求体；禁止进日志（§3.5 同口径）。
// 生命周期与租户快照一致（TTL 30s + 管理写操作 Invalidate 即时失效）。
//
// 消费方：service.NewTriageServiceWithGuidance* 装配的 TriageService（bootstrap 在多 Provider
// 开启时经 SetGuidanceSpecResolver 接线）。

import (
	"context"
	"strings"

	"itsm-backend/internal/llm/protocol"
)

// GuidanceLLMSpec 是一次 sidecar 调用下发的 LLM 目标（请求体 llm 字段，F2）。
type GuidanceLLMSpec struct {
	// Protocol 调用形态：openai_chat_completions | anthropic_messages（sidecar 据此选择请求体与鉴权头）。
	Protocol string `json:"protocol"`
	// Variant 兼容变体（minimax / ollama；空 = 标准实现）。
	Variant string `json:"variant,omitempty"`
	// URL 最终请求地址：后端按与协议适配器相同的规则解析，sidecar 直接 POST，不再拼接。
	URL string `json:"url"`
	// Model 传给 provider 的模型名。
	Model string `json:"model"`
	// APIKey 明文密钥（仅内存/内网传输，禁止落日志）。
	APIKey string `json:"apiKey,omitempty"`
}

// GuidanceSpecResolver 解析「下发给 sidecar 的租户级 LLM 目标」的窄接口
// （*LLMProviderRegistry 天然满足；测试可注入桩）。
type GuidanceSpecResolver interface {
	// ResolveGuidanceSpec 返回租户默认实例对应的 sidecar 目标。
	// ok=false 表示没有可下发的目标（无租户默认 / 实例不可用 / 形态 sidecar 不支持），
	// 调用方应跳过 sidecar 直接走网关；error 表示解析失败（同样应跳过 sidecar）。
	ResolveGuidanceSpec(ctx context.Context, tenantID int) (GuidanceLLMSpec, bool, error)
}

// buildGuidanceSpec 预计算实例对应的 sidecar 目标；不支持的协议/变体返回 nil。
//
// 参数与 buildEntry 内的构建口径一一对应（model 传 llmProviderModelForVariant 的结果），
// 保证 sidecar 收到的 URL/模型与网关实际调用完全一致。
func buildGuidanceSpec(protocolName, variant, model, endpoint string, endpointFullPath bool, apiKey string) *GuidanceLLMSpec {
	protocolKey := strings.ToLower(strings.TrimSpace(protocolName))
	variantKey := strings.ToLower(strings.TrimSpace(variant))

	switch {
	case protocolKey == protocol.ProtocolOpenAIChatCompletions &&
		(variantKey == protocol.VariantDefault || variantKey == protocol.VariantOllama):
	case protocolKey == protocol.ProtocolAnthropicMessages &&
		(variantKey == protocol.VariantDefault || variantKey == protocol.VariantMiniMax):
	default:
		return nil
	}

	adapter, err := protocolAdapters.NewAdapter(protocolKey, variantKey)
	if err != nil {
		return nil
	}
	fallback := protocol.DefaultEndpoint(protocolKey, variantKey)
	if fallback == "" {
		fallback = protocolOpenAICompatibleDefaultEndpoint
	}
	return &GuidanceLLMSpec{
		Protocol: protocolKey,
		Variant:  variantKey,
		URL:      resolveProtocolURL(endpoint, adapter.GetAPIPath(), fallback, endpointFullPath),
		Model:    model,
		APIKey:   apiKey,
	}
}

// ResolveGuidanceSpec 解析租户默认实例对应的 sidecar 调用目标（§3.3 解析链的租户级）。
//
// 语义：
//   - 无 DB 客户端 / 非法租户 / 无租户默认 / 实例不可用（含形态不支持）→ (zero, false, nil)：
//     调用方应跳过 sidecar 直接走网关——不静默回退 sidecar 自身旧配置（单一事实源）；
//   - 快照加载失败 → error：调用方同样应跳过 sidecar（可见地落到网关）。
//
// 静态回退（env/config.yaml）不在此接口暴露：静态配置的单一事实源就是 sidecar/网关各自的
// 进程配置，本接口只负责「应用内（DB）配置」的下发。
func (r *LLMProviderRegistry) ResolveGuidanceSpec(ctx context.Context, tenantID int) (GuidanceLLMSpec, bool, error) {
	if r.client == nil || tenantID <= 0 {
		return GuidanceLLMSpec{}, false, nil
	}
	snap, err := r.snapshot(ctx, tenantID)
	if err != nil {
		return GuidanceLLMSpec{}, false, err
	}
	if snap.defaultKey == "" {
		return GuidanceLLMSpec{}, false, nil
	}
	entry, ok := snap.entries[snap.defaultKey]
	if !ok || entry.guidanceSpec == nil {
		return GuidanceLLMSpec{}, false, nil
	}
	return *entry.guidanceSpec, true, nil
}
