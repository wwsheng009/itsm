package protocol

import (
	"errors"
	"strings"
	"sync"
)

// ErrAdapterNotFound 表示该 (protocol, variant) 未注册适配器。
// service 层据此回退旧分支或映射既有错误码 AI_PROTOCOL_NOT_IMPLEMENTED(422)。
var ErrAdapterNotFound = errors.New("protocol adapter not implemented")

// Registry 按 (protocol, variant) 索引协议适配器（BE-9：Registry 构建路径分派）。
// 启动期注册、运行期并发读取，读写均加锁。
type Registry struct {
	mu       sync.RWMutex
	adapters map[registryKey]ProtocolAdapter
}

type registryKey struct {
	protocol string
	variant  string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[registryKey]ProtocolAdapter)}
}

// Register 注册适配器：protocol 必填，variant 为空表示该协议的默认形态。
func (r *Registry) Register(protocol, variant string, adapter ProtocolAdapter) {
	if adapter == nil {
		panic("protocol: nil adapter")
	}
	key := newRegistryKey(protocol, variant)
	if key.protocol == "" {
		panic("protocol: empty protocol name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[key] = adapter
}

// Get 查询适配器：精确匹配 (protocol, variant)，未注册返回 ErrAdapterNotFound。
//
// 刻意不做"回退默认变体"：P0 只有 openai_chat_completions 默认变体是适配器承载，
// azure/ollama 等变体仍走既有分支（主计划 v1.4），若此处静默回退会错误地把
// 变体请求路由到适配器；是否回退由 service 层的构建分派决定。
func (r *Registry) Get(protocol, variant string) (ProtocolAdapter, error) {
	key := newRegistryKey(protocol, variant)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if adapter, ok := r.adapters[key]; ok {
		return adapter, nil
	}
	return nil, ErrAdapterNotFound
}

// NewDefaultRegistry 构建默认注册表。
//
// 已注册（BE-9 + 独立计划 v1.0 PA-1）：
//   - openai_chat_completions 默认变体；
//   - anthropic_messages 默认变体与 minimax 变体（MiniMax 兼容端点 camelCase 口径）。
//
// azure/ollama 变体与 openai_responses / google_gemini 仍走既有分支或置灰（422），
// 由独立计划 docs/plan/llm-protocol-adapter-plan.md 的后续任务逐个适配器化；
// Get 对未注册组合返回 ErrAdapterNotFound 以触发回退路径。
func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(ProtocolOpenAIChatCompletions, VariantDefault, NewOpenAIChatAdapter())
	registry.Register(ProtocolAnthropicMessages, VariantDefault, NewAnthropicMessagesAdapter(VariantDefault))
	registry.Register(ProtocolAnthropicMessages, VariantMiniMax, NewAnthropicMessagesAdapter(VariantMiniMax))
	return registry
}

// DefaultEndpoint 返回 (协议, 变体) 在调用方未配置 endpoint 时的默认地址；
// 未登记组合返回空串，由 service 层回退其既有默认（OpenAI 兼容地址）。
//
// 取值口径（主计划 §3.1.1 变体表 + §3.1.4 映射表）：
//   - anthropic_messages / ""：官方 https://api.anthropic.com，路径 /v1/messages；
//   - anthropic_messages / minimax：与旧 service.MiniMaxProvider.baseURL 同值
//     （https://api.minimaxi.com/anthropic/v1），保证开关开启时 endpoint 缺省的线上地址一致。
func DefaultEndpoint(protocolName, variant string) string {
	key := newRegistryKey(protocolName, variant)
	switch key.protocol {
	case ProtocolAnthropicMessages:
		if key.variant == VariantMiniMax {
			return anthropicMiniMaxDefaultEndpoint
		}
		return anthropicDefaultEndpoint
	default:
		return ""
	}
}

func newRegistryKey(protocol, variant string) registryKey {
	return registryKey{
		protocol: strings.ToLower(strings.TrimSpace(protocol)),
		variant:  strings.ToLower(strings.TrimSpace(variant)),
	}
}
