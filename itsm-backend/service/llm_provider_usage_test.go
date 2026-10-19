package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// B4-02：provider 真实 usage 上报通道（WithProviderUsageSink / reportProviderUsage）。

func TestProviderUsageSink_NilAndNoInjectionAreNoops(t *testing.T) {
	base := context.Background()
	assert.Equal(t, base, WithProviderUsageSink(base, nil), "nil sink 原样返回 ctx")

	// 未注入时不 panic、无副作用。
	assert.NotPanics(t, func() {
		reportProviderUsage(base, 5, 5)
		reportProviderUsage(nil, 1, 1)
	})
}

func TestProviderUsageSink_AccumulatesReports(t *testing.T) {
	var in, out int
	ctx := WithProviderUsageSink(context.Background(), func(promptTokens, completionTokens int) {
		in += promptTokens
		out += completionTokens
	})

	reportProviderUsage(ctx, 10, 3)
	reportProviderUsage(ctx, 4, 1)

	assert.Equal(t, 14, in, "工具循环多轮调用按次累加")
	assert.Equal(t, 4, out)
}

func TestProviderUsageSink_PanicIsolated(t *testing.T) {
	ctx := WithProviderUsageSink(context.Background(), func(int, int) { panic("sink boom") })

	// 观测通道异常不得影响主链路（与 estimate sink 同口径）。
	assert.NotPanics(t, func() { reportProviderUsage(ctx, 1, 1) })
}

func TestResolveTurnUsage(t *testing.T) {
	estimate := LLMUsageEstimate{TokenIn: 100, TokenOut: 40, Estimated: true}

	// 全覆盖：单次调用 + 一次实测 → 实测收口。
	real := ResolveTurnUsage(estimate, 33, 7, 1, 1)
	assert.Equal(t, LLMUsageEstimate{TokenIn: 33, TokenOut: 7, Estimated: false}, real)

	// 工具循环两轮均回包 → 累加后的实测。
	real = ResolveTurnUsage(estimate, 50, 12, 2, 2)
	assert.Equal(t, LLMUsageEstimate{TokenIn: 50, TokenOut: 12, Estimated: false}, real)

	// 部分覆盖（3 次调用只有 2 次回包 usage）→ 保持估算，不冒充全量。
	assert.Equal(t, estimate, ResolveTurnUsage(estimate, 50, 12, 2, 3))

	// 零实测（provider 未回包）→ 保持估算。
	assert.Equal(t, estimate, ResolveTurnUsage(estimate, 0, 0, 0, 1))

	// 零调用 + 实测（理论兜底：不得因为 llmCalls=0 而误判）→ 实测可用。
	assert.Equal(t, LLMUsageEstimate{TokenIn: 5, TokenOut: 2, Estimated: false}, ResolveTurnUsage(estimate, 5, 2, 1, 0))
}
