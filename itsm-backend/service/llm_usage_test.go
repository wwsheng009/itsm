// O2-02/§13.4 契约测试：token 估算的单一算法与请求级上报通道。
//
// 覆盖：
//  1. 估算口径 = 字符数 ÷ 4（与网关限流门禁同源；空串/负数安全）；
//  2. 网关三条调用路径在限流判定前把估算上报给请求级 sink（含被限流的一次）；
//  3. 未注入 sink 时为 no-op；sink 实现 panic 不得影响主链路（观测通道隔离）。
package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type usageStubProvider struct{ reply string }

func (s usageStubProvider) Chat(context.Context, string, []LLMMessage) (string, error) {
	return s.reply, nil
}

func TestEstimateTextTokens_Heuristic(t *testing.T) {
	assert.Equal(t, 0, EstimateTextTokens(""), "空串 = 0")
	assert.Equal(t, 0, EstimateTextTokens("abc"), "不足 4 字符向下取整为 0")
	assert.Equal(t, 1, EstimateTextTokens("abcd"))
	assert.Equal(t, 5, EstimateTextTokens(strings.Repeat("a", 20)))
	// 多字节字符按 rune 计数（中文 1 字符 = 1 rune，不被字节长度放大）。
	assert.Equal(t, 1, EstimateTextTokens("你好世界"), "4 个汉字 = 4 rune / 4 = 1")
}

func TestEstimatePromptTokens_SumsMessages(t *testing.T) {
	messages := []LLMMessage{
		{Role: "system", Content: strings.Repeat("a", 40)},
		{Role: "user", Content: strings.Repeat("b", 20)},
		{Role: "assistant", Content: ""},
	}
	// 40/4 + 20/4 + 0 = 15（与网关限流的逐条求和口径一致）。
	assert.Equal(t, 15, EstimatePromptTokens(messages))
	assert.Equal(t, 0, EstimatePromptTokens(nil))
}

func TestChat_ReportsUsageEstimateToSink(t *testing.T) {
	gw := NewLLMGateway(usageStubProvider{reply: "ok"}, nil, nil, "stub")
	messages := []LLMMessage{
		{Role: "system", Content: strings.Repeat("a", 40)},
		{Role: "user", Content: strings.Repeat("b", 20)},
	}

	var got []int
	ctx := WithUsageSink(context.Background(), func(promptTokens int) { got = append(got, promptTokens) })
	out, err := gw.Chat(ctx, "m", messages)
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	require.Len(t, got, 1, "一次调用上报一次估算")
	assert.Equal(t, EstimatePromptTokens(messages), got[0], "上报值与限流估算同源")
	assert.Equal(t, 15, got[0])
}

func TestReportUsageEstimate_NoSinkAndPanicContainment(t *testing.T) {
	// 未注入 sink：no-op，不 panic。
	assert.NotPanics(t, func() { reportUsageEstimate(context.Background(), 5) })
	assert.NotPanics(t, func() { reportUsageEstimate(nil, 5) })

	// sink 异常：观测通道隔离，绝不冒泡进主链路。
	ctx := WithUsageSink(context.Background(), func(int) { panic("sink boom") })
	assert.NotPanics(t, func() { reportUsageEstimate(ctx, 5) })

	// nil sink：不注入（避免下游多一次无意义的 Value 查找 + 语义歧义）。
	assert.Nil(t, WithUsageSink(context.Background(), nil).Value(usageSinkContextKey{}))
}
