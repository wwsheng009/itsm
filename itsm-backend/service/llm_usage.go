package service

import "context"

// O2-02 / §13.4：token 估算的**单一口径**与请求级上报通道。
//
// 口径（诚实声明）：字符数 ÷ 4 的启发式，与 LLMGateway 限流门禁（limiter.Allow）完全同一算法——
// 因此展示值与限流/观测所用值一致，但**不是** provider 回包的真实 usage，也不是账单口径。
// 真实 usage 需按 provider 协议解析响应体（deepseek/openai 的 `usage` 字段等），归 B4-02 专项；
// 在此之前前端展示必须同时标注「估算」，禁止当作成本账单。
//
// 通道设计：估算发生在网关内部（三条调用路径共 6 个门禁点），而消费方在聊天层
// （handlers/ai 的 chatStream）。为避免改动 RAG/网关的公开签名，采用 **context 携带 sink** 的方式：
// 聊天层 WithUsageSink 注入累加器，网关在每次门禁点 reportUsageEstimate 上报。
// 未注入 sink 时（A2UI/agent/BPMN 等既有调用方）为 no-op，零行为变化。

// LLMUsageEstimate 是一次会话回合的 token 用量：Estimated=true 时为字符/4 估算（口径见上）；
// Estimated=false 时 tokenIn/tokenOut 为 provider 回包实测（B4-02，经 WithProviderUsageSink 累加）。
type LLMUsageEstimate struct {
	TokenIn   int  `json:"tokenIn"`
	TokenOut  int  `json:"tokenOut"`
	Estimated bool `json:"estimated"`
}

// EstimateTextTokens 估算单段文本的 token 数（字符数 ÷ 4，向下取整；负数按 0）。
func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	tokens := len([]rune(text)) / 4
	if tokens < 0 {
		return 0
	}
	return tokens
}

// EstimatePromptTokens 估算消息列表的 prompt token（逐条字符数 ÷ 4 求和）。
// 与网关限流门禁同一算法——导出供聊天层与测试复用，避免口径漂移。
func EstimatePromptTokens(messages []LLMMessage) int {
	tokens := 0
	for _, m := range messages {
		tokens += EstimateTextTokens(m.Content)
	}
	return tokens
}

type usageSinkContextKey struct{}

// WithUsageSink 注入请求级 token 估算接收器（并发安全由 sink 实现负责；
// 聊天层在单请求内单 goroutine 累加）。sink 为 nil 时原样返回 ctx。
func WithUsageSink(ctx context.Context, sink func(promptTokens int)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, usageSinkContextKey{}, sink)
}

// reportUsageEstimate 向请求级 sink 上报一次 prompt 估算（未注入 sink 时为 no-op）。
// 在限流判定**之前**调用：被限流的请求同样产生成本，必须可见。
func reportUsageEstimate(ctx context.Context, promptTokens int) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(usageSinkContextKey{}).(func(int))
	if sink == nil {
		return
	}
	defer func() {
		// 观测通道绝不因 sink 实现异常影响主链路（与 observer 同口径）。
		_ = recover()
	}()
	sink(promptTokens)
}

// —— B4-02：provider 真实 usage 的上报通道 ——
//
// 通道与估算 sink 同形（context 携带；未注入 = no-op，既有调用方零行为变化）：
// 协议适配器从回包解析出 Usage（internal/llm/protocol.ProcessResult.Usage）后，
// 由接线层（protocolProvider.call）经 reportProviderUsage 上报；聊天层据此把
// 回合用量标注为「provider 实测」（estimated=false）。
//
// 口径：provider 回包原值（prompt/completion）；不做账单映射（缓存命中/推理 token 等
// 细分字段不参与），多轮工具循环按次累加。

type providerUsageSinkContextKey struct{}

// WithProviderUsageSink 注入请求级 provider 真实 usage 接收器（sink 为 nil 时原样返回 ctx）。
func WithProviderUsageSink(ctx context.Context, sink func(promptTokens, completionTokens int)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, providerUsageSinkContextKey{}, sink)
}

// reportProviderUsage 向请求级 sink 上报一次 provider 真实 usage（未注入 sink 时为 no-op）。
func reportProviderUsage(ctx context.Context, promptTokens, completionTokens int) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(providerUsageSinkContextKey{}).(func(int, int))
	if sink == nil {
		return
	}
	defer func() {
		// 观测通道绝不因 sink 实现异常影响主链路（与 estimate sink 同口径）。
		_ = recover()
	}()
	sink(promptTokens, completionTokens)
}

// ResolveTurnUsage 收口一次会话回合的用量口径（B4-02）：
// provider 实测覆盖了全部 LLM 调用（actualReports>0 且 >= llmCalls）→ 实测（estimated=false）；
// 否则保持估算——不把部分实测冒充全量。llmCalls 为网关限流门禁上报的调用次数
// （reportUsageEstimate 次数，含重试；重试不产生 provider usage 时自然回落估算）。
func ResolveTurnUsage(estimate LLMUsageEstimate, actualIn, actualOut, actualReports, llmCalls int) LLMUsageEstimate {
	if actualReports > 0 && actualReports >= llmCalls {
		return LLMUsageEstimate{TokenIn: actualIn, TokenOut: actualOut, Estimated: false}
	}
	return estimate
}
