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

// NewDefaultRegistry 构建 P0 默认注册表：只注册 openai_chat_completions 的默认变体。
//
// azure/ollama 等变体与其余 3 协议按主计划 v1.4 仍走既有分支或置灰（422），
// 由独立计划 docs/plan/llm-protocol-adapter-plan.md 逐个适配器化；
// 因此这里不注册它们，Get 会返回 ErrAdapterNotFound 以触发回退路径。
func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(ProtocolOpenAIChatCompletions, VariantDefault, NewOpenAIChatAdapter())
	return registry
}

func newRegistryKey(protocol, variant string) registryKey {
	return registryKey{
		protocol: strings.ToLower(strings.TrimSpace(protocol)),
		variant:  strings.ToLower(strings.TrimSpace(variant)),
	}
}
