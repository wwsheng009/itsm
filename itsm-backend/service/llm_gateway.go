package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"itsm-backend/common/tenantctx"
)

// LLMGateway abstracts multiple providers and basic observability/limits
type LLMGateway struct {
	provider     LLMProvider
	limiter      TokenLimiter
	observer     Observer
	providerName string

	// resolver 多 Provider 解析器（BE-3，主计划 §3.2）：bootstrap 期 WithResolver 注入，运行期只读。
	// nil = 未启用多 Provider，全部请求继续走静态 provider（现状行为）。
	resolver ProviderResolver
	// userResolver 个人默认解析器（§3.3 第二级）：WithResolver 对实现了 UserDefaultResolver
	// 的解析器自动接线；nil = 不支持个人默认（退化为 租户默认 → 静态 两级）。
	userResolver UserDefaultResolver
	// boundRequest 绑定的解析输入（BE-7）：WithProviderRequest 产生的门关副本携带；
	// nil = 未绑定，旧四方法走既有静态快速路径（开关关闭时逐字节不变）。
	boundRequest *ProviderRequest
}

// ProviderResolver 网关侧窄解析接口（主计划 §3.2 第 1 条）：BE-2 的 *LLMProviderRegistry 天然满足。
type ProviderResolver interface {
	Resolve(ctx context.Context, tenantID int, override string) (ProviderSlot, string, error)
}

// UserDefaultResolver 可选能力：个人默认解析（§3.3 第二级）。
//
// 独立于 ProviderResolver：仅当调用方显式给出 userID（ProviderRequest.UserID > 0）时才被调用；
// BE-2 的 *LLMProviderRegistry 已实现，WithResolver 会自动识别接线。
type UserDefaultResolver interface {
	ResolveUserDefault(ctx context.Context, tenantID, userID int) (ProviderSlot, string, error)
}

// ProviderRequest 一次 LLM 调用的 provider 选择输入（主计划 §3.3 解析链）。
type ProviderRequest struct {
	// Key 请求级显式覆盖（系统管理员在 /ai/chat 传的 provider 参数）：
	// 非空 = 严格解析、失败可见地失败，绝不静默回退。
	Key string
	// TenantID 租户 id；≤0 时取 ctx 中的 tenantctx.TenantID。
	TenantID int
	// UserID > 0 时启用第二级「个人默认」；≤0 跳过（后台任务/工具路径的默认行为）。
	UserID int
}

// ProviderResolution 一次调用实际生效的 provider 标注（§3.3/§3.4：响应回带 provider/providerSource）。
type ProviderResolution struct {
	// Key 生效实例 key；静态回退为静态 provider 名（provider 名为空时用 "static"）。
	Key string
	// Source 解析链来源：ProviderSourceRequest / ProviderSourceUser / ProviderSourceTenant / ProviderSourceStatic。
	Source string
	// SupportsTools 生效 provider 是否实现 ToolCallingStreamProvider（按真实实现探测）。
	SupportsTools bool
	// Protocol 生效 provider 的协议形态值（§3.7：ai_llm_calls.provider 自 v1.2 起写 4 值枚举）。
	// 静态回退按 staticProviderProtocolSpec 归一；未识别的自定义 provider 名保持原样。
	Protocol string
}

type LLMProvider interface {
	Chat(ctx context.Context, model string, messages []LLMMessage) (string, error)
}

// StreamingLLMProvider is an optional capability. Providers that implement
// ChatStream will be used for token-level streaming; otherwise the gateway
// falls back to a single-shot Chat call.
type StreamingLLMProvider interface {
	ChatStream(ctx context.Context, model string, messages []LLMMessage, callback func(string)) error
}

// ToolCallingStreamProvider is an optional capability: providers that support
// declaring tools (function calling) AND streaming. The provider receives the
// declared tools, streams text deltas through callback, and reports any tool
// calls the model requested through onToolCalls. Providers that do not
// implement this interface degrade gracefully: the gateway's
// ChatStreamWithTools falls back to a plain ChatStream with no tools declared.
type ToolCallingStreamProvider interface {
	ChatStreamWithTools(ctx context.Context, model string, messages []LLMMessage, tools []LLMTool, callback func(string), onToolCalls func([]LLMToolCall)) error
}

// LLMTool 声明可供模型调用的工具（OpenAI function calling 风格）。
type LLMTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// LLMToolCall 模型发起的一次工具调用。
type LLMToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 编码的参数
}

type LLMMessage struct {
	Role    string
	Content string
	// Tools 请求侧：本消息声明可用工具（通常挂载在 user/system 消息上）。
	Tools []LLMTool
	// ToolCalls 响应侧：assistant 消息携带模型请求调用的工具列表。
	ToolCalls []LLMToolCall
	// ToolCallID 工具结果消息：对应被执行的 LLMToolCall.ID（OpenAI "tool" role）。
	ToolCallID string
}

type TokenLimiter interface {
	Allow(nTokens int) bool
}

type Observer interface {
	Observe(provider string, model string, tokens int, latency time.Duration, err error)
}

// ProviderKeyObserver 可选观测能力（BE-6，主计划 §3.7）：除既有字段外，把本次调用生效的
// DB 实例 key 一并落库（ai_llm_calls.provider_key）。静态回退传空串（写 NULL）。
//
// 独立于 Observer 是为了零破坏：既有实现（NoopObserver / MockObserver / 测试桩）无需改动，
// 网关按类型断言择优调用。
type ProviderKeyObserver interface {
	ObserveWithProviderKey(providerKey, provider, model string, tokens int, latency time.Duration, err error)
}

func NewLLMGateway(p LLMProvider, l TokenLimiter, o Observer, providerName string) *LLMGateway {
	return &LLMGateway{provider: p, limiter: l, observer: o, providerName: providerName}
}

// observe 统一观测出口：observer 实现 ProviderKeyObserver 时回带 providerKey，
// 否则退回旧签名（不丢失任何既有观测语义）。
func (g *LLMGateway) observe(providerKey, provider, model string, tokens int, latency time.Duration, err error) {
	if g == nil || g.observer == nil {
		return
	}
	if pko, ok := g.observer.(ProviderKeyObserver); ok {
		pko.ObserveWithProviderKey(providerKey, provider, model, tokens, latency, err)
		return
	}
	g.observer.Observe(provider, model, tokens, latency, err)
}

// WithProviderRequest 返回绑定了 §3.3 解析输入的网关副本（浅拷贝；原网关与其它调用方不受影响）。
//
// 用途（BE-7）：/ai/chat 与 /ai/chat/stream 在开关开启时按请求解析 provider，
// 通过副本把解析输入传给下游（RAG 流式/工具路径）而不改变任何既有方法签名。
// 绑定后 Chat/ChatStream/ChatStreamWithTools/SupportsToolCalling 自动按解析链路由；
// 未绑定（默认）时行为与接线前完全一致（QA-3 开关回归门禁）。
func (g *LLMGateway) WithProviderRequest(req ProviderRequest) *LLMGateway {
	if g == nil {
		return g
	}
	bound := req
	clone := *g
	clone.boundRequest = &bound
	return &clone
}

// observationKey 返回应写入 ai_llm_calls.provider_key 的值：
// 仅 DB 实例（请求级/个人默认/租户默认）写 key；静态回退写空串（落库为 NULL，§3.7）。
func observationKey(resolution ProviderResolution) string {
	if resolution.Source == ProviderSourceStatic {
		return ""
	}
	return resolution.Key
}

// llmRetryPolicy 控制瞬时错误的自动重试。
//
// 背景（2026-09-08 UAT Q-2 根因）：prod 33 次 LLM 调用 6 次失败（18%），
// 其中 openai 3 次为"未配置 API Key 时Embedder/Provider 回退到 openai"的 401
// （不可重试，已在配置层修复），minimax 3 次为网络抖动/上游 5xx（可重试）。
// 网关层对可重试错误做有限退避重试，把演示/验收场景的失败率压到 5% 以内；
// 不可重试错误（401/403/400）立即返回，避免拖长用户等待。
const (
	llmMaxRetries       = 2                      // 首次调用外最多重试 2 次（共 3 次尝试）
	llmRetryBaseBackoff = 300 * time.Millisecond // 指数退避基值：300ms, 600ms
	llmRetryMaxLatency  = 45 * time.Second       // 累计耗时超阈值（慢失败）不再重试
)

// isTransientLLMError 判断错误是否值得重试：
//   - 网络层错误（连接拒绝/DNS/超时）——url.Error、context deadline（非调用方取消）
//   - HTTP 429（限流）与 5xx（上游故障）
//
// 不可重试：400（请求错误）、401/403（鉴权）——重试只会重复失败并拖长延迟。
func isTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	// go-openai APIError：按状态码分类（OpenAI/Azure/兼容网关都走这个类型）
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch code := apiErr.HTTPStatusCode; {
		case code == 429:
			return true
		case code >= 500:
			return true
		default:
			return false
		}
	}
	// MiniMax/Local provider 的错误消息里带 "status %d"（无结构化类型），按文本分类。
	// 注意顺序：先匹配 5xx/429，再排除 4xx 明确不可重试的状态。
	msg := err.Error()
	if strings.Contains(msg, "status 429") || strings.Contains(msg, "status 5") {
		return true
	}
	for _, code := range []string{"status 400", "status 401", "status 403", "status 404"} {
		if strings.Contains(msg, code) {
			return false
		}
	}
	// 网络/超时类：url.Error 包装（provider 层 %w 包装了 http 错误）或
	// connection reset/refused、EOF、broken pipe、timeout 等网络层关键词
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	for _, kw := range []string{
		"connection reset", "connection refused", "broken pipe",
		"EOF", "timeout", "TLS handshake", "no such host", "i/o timeout",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// retryableChat 执行一次带退避重试的 provider.Chat。
// 观测语义：只在「最终结果」上 Observe 一次（重试中间态不写 ai_llm_calls，
// 保持调用计数与业务请求一一对应，避免重试把失败率/延迟统计进一步打乱）。
func (g *LLMGateway) retryableChat(ctx context.Context, model string, messages []LLMMessage) (string, error) {
	return g.retryableChatWith(ctx, g.provider, model, messages)
}

// retryableChatWith 与 retryableChat 同策略（同 provider 内退避重试、不跨 provider），
// 作用于指定 provider，供 BE-3 的显式选择/默认解析路径复用（重试与观测口径与旧路径一致）。
func (g *LLMGateway) retryableChatWith(ctx context.Context, provider LLMProvider, model string, messages []LLMMessage) (string, error) {
	start := time.Now()
	var (
		out string
		err error
	)
	for attempt := 0; attempt <= llmMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := llmRetryBaseBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		out, err = provider.Chat(ctx, model, messages)
		if err == nil {
			return out, nil
		}
		// 调用方取消/超时：尊重 ctx，不重试
		if ctx.Err() != nil {
			return "", err
		}
		if !isTransientLLMError(err) {
			return "", err
		}
		// 慢失败（如 30s+ 超时）重试会成倍拖长用户等待：已耗时超阈值直接放弃
		if time.Since(start) > llmRetryMaxLatency {
			return "", err
		}
	}
	return "", err
}

func (g *LLMGateway) Chat(ctx context.Context, model string, messages []LLMMessage) (string, error) {
	if g.boundRequest != nil {
		// BE-7：绑定了解析输入的副本按 §3.3 解析链路由（未绑定时零行为变化）。
		out, _, err := g.ChatWithRequest(ctx, *g.boundRequest, model, messages)
		return out, err
	}
	start := time.Now()
	// naive tokens estimation
	tokens := 0
	for _, m := range messages {
		tokens += len([]rune(m.Content)) / 4
	}
	if g.limiter != nil && !g.limiter.Allow(tokens) {
		if g.observer != nil {
			g.observe("", g.providerName, model, tokens, time.Since(start), ErrRateLimited)
		}
		return "", ErrRateLimited
	}
	out, err := g.retryableChat(ctx, model, messages)
	if g.observer != nil {
		g.observe("", g.providerName, model, tokens, time.Since(start), err)
	}
	return out, err
}

// ChatStream streams tokens through the callback. If the underlying provider
// does not implement StreamingLLMProvider, it falls back to a single Chat call
// and emits the full response as one chunk. Callbacks may be invoked with
// empty strings; consumers should handle them gracefully.
func (g *LLMGateway) ChatStream(ctx context.Context, model string, messages []LLMMessage, callback func(string)) error {
	if callback == nil {
		callback = func(string) {}
	}
	if g.boundRequest != nil {
		// BE-7：绑定副本按解析链路由（未绑定时零行为变化）。
		_, err := g.ChatStreamWithRequest(ctx, *g.boundRequest, model, messages, callback)
		return err
	}
	start := time.Now()
	tokens := 0
	for _, m := range messages {
		tokens += len([]rune(m.Content)) / 4
	}
	if g.limiter != nil && !g.limiter.Allow(tokens) {
		if g.observer != nil {
			g.observe("", g.providerName, model, tokens, time.Since(start), ErrRateLimited)
		}
		return ErrRateLimited
	}

	if streamer, ok := g.provider.(StreamingLLMProvider); ok {
		// 流式调用不做自动重试：部分 token 可能已通过 callback 下发，
		// 重试会造成内容重复。瞬时错误交由调用方（前端断线重连）兜底。
		err := streamer.ChatStream(ctx, model, messages, callback)
		if g.observer != nil {
			g.observe("", g.providerName, model, tokens, time.Since(start), err)
		}
		return err
	}

	// Fallback: run a normal Chat and emit the whole response as one chunk.
	out, err := g.retryableChat(ctx, model, messages)
	if g.observer != nil {
		g.observe("", g.providerName, model, tokens, time.Since(start), err)
	}
	if err != nil {
		return err
	}
	if out != "" {
		callback(out)
	}
	return nil
}

// ChatStreamWithTools declares tools and streams the reply. Text deltas are
// delivered through callback; if the model requests tool calls they are
// reported through onToolCalls (so the caller can execute them and continue
// the conversation). Providers without tool-calling support degrade to a plain
// ChatStream with no tools declared. Token limiting/observability behave the
// same as ChatStream.
func (g *LLMGateway) ChatStreamWithTools(ctx context.Context, model string, messages []LLMMessage, tools []LLMTool, callback func(string), onToolCalls func([]LLMToolCall)) error {
	if g.boundRequest != nil {
		// BE-7：绑定副本按解析链路由（未绑定时零行为变化）。
		_, err := g.ChatStreamWithToolsRequest(ctx, *g.boundRequest, model, messages, tools, callback, onToolCalls)
		return err
	}
	if p, ok := g.provider.(ToolCallingStreamProvider); ok {
		// 修复（2026-09-08）：原实现直接透传 provider，绕过 limiter 与 observer——
		// 工具调用路径的 LLM 用量既不受 token_cap 约束，也不进 ai_llm_calls，
		// 导致 /ai/metrics 的调用数/失败率/平均延迟全部失真（UAT Q-1/Q-2 的 33 次
		// 采样若混入工具路径则不可信）。现补齐与 ChatStream 相同的门禁与观测。
		start := time.Now()
		tokens := 0
		for _, m := range messages {
			tokens += len([]rune(m.Content)) / 4
		}
		if g.limiter != nil && !g.limiter.Allow(tokens) {
			if g.observer != nil {
				g.observe("", g.providerName, model, tokens, time.Since(start), ErrRateLimited)
			}
			return ErrRateLimited
		}
		err := p.ChatStreamWithTools(ctx, model, messages, tools, callback, onToolCalls)
		if g.observer != nil {
			g.observe("", g.providerName, model, tokens, time.Since(start), err)
		}
		return err
	}
	// 退化：忽略工具声明，走普通流式；模型不会返回工具调用。
	return g.ChatStream(ctx, model, messages, callback)
}

// SupportsToolCalling reports whether the bound provider can declare tools and
// return real tool_calls in the response. Callers (e.g. AI ChatStream) use this
// to decide whether to inject tool-driven system prompt instructions: when the
// provider returns false, telling the model "you must call tools" causes it to
// fabricate text like "正在调用 list_tickets 工具..." without ever executing
// anything, which is exactly the bug we are guarding against.
//
// 注意（BE-7）：本方法无 ctx，无法解析租户维度；绑定副本（WithProviderRequest）上的
// 绑定路径请改用 SupportsToolCallingRequest，否则返回的仍是静态 provider 的能力。
func (g *LLMGateway) SupportsToolCalling() bool {
	if g == nil || g.provider == nil {
		return false
	}
	_, ok := g.provider.(ToolCallingStreamProvider)
	return ok
}

// ==================== 多 Provider 覆盖解析（BE-3，主计划 §3.2 / §3.3） ====================

// WithResolver 注入多 Provider 解析器（bootstrap 期调用一次；返回自身便于链式拼接）。
// resolver 同时实现 UserDefaultResolver 时自动接线个人默认（§3.3 第二级）。
func (g *LLMGateway) WithResolver(resolver ProviderResolver) *LLMGateway {
	if g == nil {
		return g
	}
	g.resolver = resolver
	if userResolver, ok := resolver.(UserDefaultResolver); ok {
		g.userResolver = userResolver
	}
	return g
}

// ChatWithProvider 按 §3.3 解析 providerKey 后执行一次非流式对话（主计划 §3.2 方法名）。
func (g *LLMGateway) ChatWithProvider(ctx context.Context, providerKey, model string, messages []LLMMessage) (string, error) {
	out, _, err := g.ChatWithProviderInfo(ctx, providerKey, model, messages)
	return out, err
}

// ChatWithProviderInfo 同 ChatWithProvider，并回带生效 provider 标注（供 /ai/chat 回带 provider/providerSource）。
func (g *LLMGateway) ChatWithProviderInfo(ctx context.Context, providerKey, model string, messages []LLMMessage) (string, ProviderResolution, error) {
	return g.ChatWithRequest(ctx, ProviderRequest{Key: providerKey}, model, messages)
}

// ChatStreamWithProvider 按 §3.3 解析 providerKey 后流式输出（主计划 §3.2 方法名）。
func (g *LLMGateway) ChatStreamWithProvider(ctx context.Context, providerKey, model string, messages []LLMMessage, callback func(string)) error {
	_, err := g.ChatStreamWithProviderInfo(ctx, providerKey, model, messages, callback)
	return err
}

// ChatStreamWithProviderInfo 同 ChatStreamWithProvider，并回带生效 provider 标注（SSE done 事件消费）。
func (g *LLMGateway) ChatStreamWithProviderInfo(ctx context.Context, providerKey, model string, messages []LLMMessage, callback func(string)) (ProviderResolution, error) {
	return g.ChatStreamWithRequest(ctx, ProviderRequest{Key: providerKey}, model, messages, callback)
}

// ChatWithRequest 按完整解析链（§3.3：请求级 → 个人默认 → 租户默认 → 静态）执行非流式对话。
//
// 解析失败语义（§3.2）：
//   - 请求级显式选择（req.Key 非空）失败：原样透出哨兵错误，不回退；
//   - 隐式链路（个人默认/租户默认）不可用：回落静态 provider（现状行为）；静态也不可用才报错。
//
// 重试、限流与观测口径与旧 Chat 一致（同 provider 内重试；只 Observe 最终结果一次）。
func (g *LLMGateway) ChatWithRequest(ctx context.Context, req ProviderRequest, model string, messages []LLMMessage) (string, ProviderResolution, error) {
	provider, resolution, err := g.resolveProvider(ctx, req)
	if err != nil {
		return "", resolution, err
	}
	start := time.Now()
	tokens := estimateTokens(messages)
	if g.limiter != nil && !g.limiter.Allow(tokens) {
		g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), ErrRateLimited)
		return "", resolution, ErrRateLimited
	}
	out, err := g.retryableChatWith(ctx, provider, model, messages)
	g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), err)
	return out, resolution, err
}

// ChatStreamWithRequest 同 ChatWithRequest 的流式版本。
// 流式不做自动重试（与 ChatStream 一致：部分 token 可能已下发，重试会造成内容重复）。
func (g *LLMGateway) ChatStreamWithRequest(ctx context.Context, req ProviderRequest, model string, messages []LLMMessage, callback func(string)) (ProviderResolution, error) {
	provider, resolution, err := g.resolveProvider(ctx, req)
	if err != nil {
		return resolution, err
	}
	if callback == nil {
		callback = func(string) {}
	}
	return g.chatStreamWithProvider(ctx, provider, resolution, model, messages, callback)
}

// chatStreamWithProvider 对已解析的 provider 执行流式（或单块回退）调用并只观测一次最终结果。
// 供 ChatStreamWithRequest（§3.3 覆盖链）与 ChatStreamWithToolsRequest 的降级分支复用，
// 保证限流/观测/回退口径与既有 ChatStream 完全一致。
func (g *LLMGateway) chatStreamWithProvider(ctx context.Context, provider LLMProvider, resolution ProviderResolution, model string, messages []LLMMessage, callback func(string)) (ProviderResolution, error) {
	start := time.Now()
	tokens := estimateTokens(messages)
	if g.limiter != nil && !g.limiter.Allow(tokens) {
		g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), ErrRateLimited)
		return resolution, ErrRateLimited
	}
	if streamer, ok := provider.(StreamingLLMProvider); ok {
		err := streamer.ChatStream(ctx, model, messages, callback)
		g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), err)
		return resolution, err
	}
	// 非流式 provider：与旧 ChatStream 相同，单块下发整段回复。
	out, err := g.retryableChatWith(ctx, provider, model, messages)
	g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), err)
	if err != nil {
		return resolution, err
	}
	if out != "" {
		callback(out)
	}
	return resolution, nil
}

// ChatStreamWithToolsRequest 按 §3.3 解析链执行工具流式调用（BE-7 绑定路径的落点）。
//
// 与 ChatStreamWithTools 的关系：解析出 provider 后按同一门禁/观测/降级口径执行；
// provider 不支持工具调用时与既有实现一致地退化为普通流式（不声明 tools）。
// 工具调用路径不读取请求级 override 的约束由调用方保证（§3.3：A2UI/agent/BPMN 不传 req.Key）。
func (g *LLMGateway) ChatStreamWithToolsRequest(ctx context.Context, req ProviderRequest, model string, messages []LLMMessage, tools []LLMTool, callback func(string), onToolCalls func([]LLMToolCall)) (ProviderResolution, error) {
	provider, resolution, err := g.resolveProvider(ctx, req)
	if err != nil {
		return resolution, err
	}
	if callback == nil {
		callback = func(string) {}
	}
	p, ok := provider.(ToolCallingStreamProvider)
	if !ok {
		// 退化：忽略工具声明，走普通流式；模型不会返回工具调用（与 ChatStreamWithTools 一致）。
		return g.chatStreamWithProvider(ctx, provider, resolution, model, messages, callback)
	}
	start := time.Now()
	tokens := estimateTokens(messages)
	if g.limiter != nil && !g.limiter.Allow(tokens) {
		g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), ErrRateLimited)
		return resolution, ErrRateLimited
	}
	err = p.ChatStreamWithTools(ctx, model, messages, tools, callback, onToolCalls)
	g.observe(observationKey(resolution), resolution.Protocol, model, tokens, time.Since(start), err)
	return resolution, err
}

// ResolveRequest 只做 §3.3 解析、不发起模型调用：供 /ai/chat（纯 RAG 检索，无 LLM 调用）
// 回带 provider/providerSource，以及请求级校验（BE-7）。
func (g *LLMGateway) ResolveRequest(ctx context.Context, req ProviderRequest) (ProviderResolution, error) {
	_, resolution, err := g.resolveProvider(ctx, req)
	return resolution, err
}

// SupportsToolCallingRequest 按 §3.3 解析后报告生效 provider 的工具能力（BE-7 绑定路径专用；
// 无 ctx 的 SupportsToolCalling 无法解析租户维度）。
func (g *LLMGateway) SupportsToolCallingRequest(ctx context.Context, req ProviderRequest) bool {
	if g == nil {
		return false
	}
	_, resolution, err := g.resolveProvider(ctx, req)
	return err == nil && resolution.SupportsTools
}

// SupportsToolCallingFor 解析 providerKey 后报告其是否支持工具调用（主计划 §3.2；P1 供上层探测）。
// 计划原文未带 ctx，因解析需要租户维度（§3.3）而补上；解析失败返回 false（不 panic）。
func (g *LLMGateway) SupportsToolCallingFor(ctx context.Context, providerKey string) bool {
	if g == nil {
		return false
	}
	_, resolution, err := g.resolveProvider(ctx, ProviderRequest{Key: providerKey})
	return err == nil && resolution.SupportsTools
}

// resolveProvider 执行 §3.3 解析链并返回可调用 provider 与标注。
func (g *LLMGateway) resolveProvider(ctx context.Context, req ProviderRequest) (LLMProvider, ProviderResolution, error) {
	requested := strings.TrimSpace(req.Key)
	if g.resolver == nil {
		if requested != "" {
			// 未启用多 Provider 的部署收到显式选择：可见地失败（不假装接受又静默忽略）。
			return nil, ProviderResolution{Key: requested, Source: ProviderSourceRequest},
				fmt.Errorf("%w: 当前部署未启用多 Provider 注册表，无法解析 provider=%q", ErrProviderUnavailable, requested)
		}
		return g.staticFallback()
	}

	tenantID := req.TenantID
	if tenantID <= 0 {
		tenantID, _ = tenantctx.TenantID(ctx)
	}

	if requested == "" && req.UserID > 0 && g.userResolver != nil {
		// §3.3 第二级：个人默认。无偏好/已禁用/已软删 → ErrProviderNotFound，降级到租户默认
		// （registry 内已记日志，见 LLMProviderRegistry.ResolveUserDefault）；密钥缺失等硬错误透出。
		slot, source, err := g.userResolver.ResolveUserDefault(ctx, tenantID, req.UserID)
		switch {
		case err == nil:
			return slotProvider(slot, source)
		case errors.Is(err, ErrProviderNotFound), errors.Is(err, ErrProviderDisabled):
			// 降级：继续走 租户默认 → 静态。
		default:
			return nil, ProviderResolution{Key: slot.Key, Source: source}, err
		}
	}

	slot, source, err := g.resolver.Resolve(ctx, tenantID, requested)
	if err != nil {
		if requested != "" {
			// §3.3：显式选择失败必须可见地失败，绝不静默回退到静态配置。
			return nil, ProviderResolution{Key: requested, Source: ProviderSourceRequest}, err
		}
		// 隐式链路（个人默认/租户默认）不可用：回落静态 provider，保持现状行为。
		return g.staticFallback()
	}
	return slotProvider(slot, source)
}

// staticFallback 返回静态 provider（config.yaml/env）及其标注；静态配置缺失时报 ErrProviderUnavailable。
func (g *LLMGateway) staticFallback() (LLMProvider, ProviderResolution, error) {
	resolution := ProviderResolution{Key: g.providerName, Source: ProviderSourceStatic}
	if resolution.Key == "" {
		resolution.Key = ProviderSourceStatic
	}
	resolution.Protocol = staticProtocol(g.providerName)
	if g.provider == nil {
		return nil, resolution, fmt.Errorf("%w: 静态 LLM provider 未配置", ErrProviderUnavailable)
	}
	resolution.SupportsTools = supportsToolCalling(g.provider)
	return g.provider, resolution, nil
}

// staticProtocol 把静态 provider 名归一为协议形态值（§3.7 的 provider 列口径）：
// 空名/openai → openai_chat_completions；azure/ollama 变体同协议；minimax → anthropic_messages；
// 未识别的自定义 provider 名保持原样（历史行按文本兼容，R-12）。
func staticProtocol(providerName string) string {
	if protocolName, _, ok := staticProviderProtocolSpec(providerName); ok {
		return protocolName
	}
	return providerName
}

// slotProvider 把注册表 slot 归一为可调用 provider；nil provider 兜底为 ErrProviderUnavailable（防御性）。
func slotProvider(slot ProviderSlot, source string) (LLMProvider, ProviderResolution, error) {
	resolution := ProviderResolution{
		Key:           slot.Key,
		Source:        source,
		Protocol:      slot.Protocol,
		SupportsTools: supportsToolCalling(slot.Provider),
	}
	if slot.Provider == nil {
		return nil, resolution, fmt.Errorf("%w: provider %q 未构建出可用实例", ErrProviderUnavailable, slot.Key)
	}
	return slot.Provider, resolution, nil
}

// estimateTokens 沿用既有口径估算输入 token（rune 数 / 4），供新增路径与旧路径保持同一限流口径。
func estimateTokens(messages []LLMMessage) int {
	tokens := 0
	for _, m := range messages {
		tokens += len([]rune(m.Content)) / 4
	}
	return tokens
}

// Simple implementations
var ErrRateLimited = &RateLimitError{Message: "rate limited"}

type RateLimitError struct{ Message string }

func (e *RateLimitError) Error() string { return e.Message }

type FixedWindowLimiter struct{ capacity int }

func NewFixedWindowLimiter(capacity int) *FixedWindowLimiter {
	return &FixedWindowLimiter{capacity: capacity}
}
func (l *FixedWindowLimiter) Allow(n int) bool { return n <= l.capacity }

type NoopObserver struct{}

func (NoopObserver) Observe(_ string, _ string, _ int, _ time.Duration, _ error) {}
