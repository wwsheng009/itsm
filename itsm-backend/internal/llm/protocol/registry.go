package protocol

import (
	"errors"
	"strings"
	"sync"
)

// openAIOllamaDefaultEndpoint 与旧 service.LocalProvider 的默认 baseURL 同值
// （Ollama 的 OpenAI 兼容资源路径 /v1/chat/completions 由 service 拼接）。
const openAIOllamaDefaultEndpoint = "http://localhost:11434"

// ErrAdapterNotFound 表示该协议没有可用的适配器实现（协议未注册，或该变体不在其支持范围）。
// service 层据此回退既有分支或映射既有错误码 AI_PROTOCOL_NOT_IMPLEMENTED(422)。
var ErrAdapterNotFound = errors.New("protocol adapter not implemented")

// AdapterSpec 描述**一个协议**的适配器实现：支持哪些变体、如何按变体构造实例。
//
// 适配器与协议一一对应（协议枚举 4 值 = 至多 4 个实现）；variant 只是同一实现的构造期
// 选项（如 anthropic_messages 的 minimax camelCase 口径、openai_chat_completions 的
// azure / ollama 部署形态），不构成独立适配器身份，也不进注册表键。
type AdapterSpec struct {
	// Variants 该协议适配器支持的变体集合（至少 1 项；VariantDefault 表示标准形态）。
	Variants []string
	// New 按变体构造适配器实例（变体差异在构造期固化，请求期不查表、不分支）。
	New func(variant string) ProtocolAdapter
}

// Registry 按协议索引适配器实现（每协议一个 spec）。
// 启动期注册、运行期并发读取，读写均加锁。
type Registry struct {
	mu    sync.RWMutex
	specs map[string]AdapterSpec
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{specs: make(map[string]AdapterSpec)}
}

// Register 注册协议适配器实现（启动期一次；重复注册以最后一次为准）。
//
// protocol 为空、Variants 为空或 New 为 nil 一律 panic：注册错误应在启动期暴露，
// 而不是运行期静默退化到旧分支。
func (r *Registry) Register(protocolName string, spec AdapterSpec) {
	key := normalizeProtocolName(protocolName)
	if key == "" {
		panic("protocol: empty protocol name")
	}
	if len(spec.Variants) == 0 {
		panic("protocol: adapter spec requires at least one variant: " + key)
	}
	if spec.New == nil {
		panic("protocol: nil adapter factory: " + key)
	}
	variants := make([]string, 0, len(spec.Variants))
	for _, variant := range spec.Variants {
		variants = append(variants, normalizeProtocolName(variant))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[key] = AdapterSpec{Variants: variants, New: spec.New}
}

// Supports 报告 (protocol, variant) 是否由协议适配器承载（不构造实例）。
func (r *Registry) Supports(protocolName, variant string) bool {
	_, ok := r.lookup(protocolName, variant)
	return ok
}

// NewAdapter 构造 (protocol, variant) 的适配器实例。
// 协议未注册或变体不在该协议的支持范围内，一律返回 ErrAdapterNotFound。
func (r *Registry) NewAdapter(protocolName, variant string) (ProtocolAdapter, error) {
	spec, ok := r.lookup(protocolName, variant)
	if !ok {
		return nil, ErrAdapterNotFound
	}
	return spec.New(normalizeProtocolName(variant)), nil
}

// NewDefaultRegistry 构建默认注册表（一协议一实现；未注册协议即"未适配"）：
//
//   - openai_chat_completions：默认形态 + azure / ollama 部署变体（同一适配器，
//     厂商/部署差异由 endpoint 与调用参数表达，不新增适配器）；
//   - anthropic_messages：官方形态 + minimax 兼容端点（camelCase 字段口径，
//     同一适配器构造期固化）;
//   - openai_responses：官方 Responses API 形态（PA-3，同一适配器；只有一个标准
//     变体，不登记默认 endpoint 以保持既有 api.openai.com 回退口径）；
//   - google_gemini：Gemini API v1beta 形态（PA-4，同一适配器；标准变体，
//     资源路径含模型名由 ModelPathAdapter 在请求期给出，默认 endpoint 为官方地址）。
func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(ProtocolOpenAIChatCompletions, AdapterSpec{
		Variants: []string{VariantDefault, VariantAzure, VariantOllama},
		New:      func(string) ProtocolAdapter { return NewOpenAIChatAdapter() },
	})
	registry.Register(ProtocolAnthropicMessages, AdapterSpec{
		Variants: []string{VariantDefault, VariantMiniMax},
		New:      func(variant string) ProtocolAdapter { return NewAnthropicMessagesAdapter(variant) },
	})
	registry.Register(ProtocolOpenAIResponses, AdapterSpec{
		Variants: []string{VariantDefault},
		New:      func(string) ProtocolAdapter { return NewOpenAIResponsesAdapter() },
	})
	registry.Register(ProtocolGoogleGemini, AdapterSpec{
		Variants: []string{VariantDefault},
		New:      func(variant string) ProtocolAdapter { return NewGoogleGeminiAdapter(variant) },
	})
	return registry
}

// DefaultEndpoint 返回 (协议, 变体) 在调用方未配置 endpoint 时的默认地址；
// 未登记组合返回空串，由 service 层回退其既有默认（OpenAI 兼容地址）。
//
// 取值口径（主计划 §3.1.1 变体表 + §3.1.4 映射表，与旧分支逐字节一致）：
//   - openai_chat_completions / ollama：与旧 service.LocalProvider 的默认 baseURL 同值；
//   - openai_chat_completions 默认与 azure：不登记（旧分支分别回退 api.openai.com 与
//     go-openai 默认 BaseURL，两者地址一致）；
//   - anthropic_messages / ""：官方 https://api.anthropic.com，路径 /v1/messages；
//   - anthropic_messages / minimax：与旧 service.MiniMaxProvider.baseURL 同值
//     （https://api.minimaxi.com/anthropic/v1）。
//   - openai_responses / ""：不登记（与 openai_chat_completions 标准形态同址回退
//     api.openai.com，路径 /v1/responses 由适配器给出）。
//   - google_gemini / ""：官方 https://generativelanguage.googleapis.com，
//     资源路径 /v1beta/models/{model}:generateContent 由适配器给出。
func DefaultEndpoint(protocolName, variant string) string {
	key := normalizeProtocolName(protocolName)
	switch key {
	case ProtocolOpenAIChatCompletions:
		if normalizeProtocolName(variant) == VariantOllama {
			return openAIOllamaDefaultEndpoint
		}
		return ""
	case ProtocolAnthropicMessages:
		if normalizeProtocolName(variant) == VariantMiniMax {
			return anthropicMiniMaxDefaultEndpoint
		}
		return anthropicDefaultEndpoint
	case ProtocolGoogleGemini:
		return geminiDefaultEndpoint
	default:
		return ""
	}
}

// lookup 精确匹配协议与变体（大小写/空白不敏感），返回注册的 spec。
func (r *Registry) lookup(protocolName, variant string) (AdapterSpec, bool) {
	key := normalizeProtocolName(protocolName)
	variantKey := normalizeProtocolName(variant)
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[key]
	if !ok {
		return AdapterSpec{}, false
	}
	for _, supported := range spec.Variants {
		if supported == variantKey {
			return spec, true
		}
	}
	return AdapterSpec{}, false
}

// normalizeProtocolName 归一协议名/变体名：去空白、转小写（枚举值本身即小写）。
func normalizeProtocolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
