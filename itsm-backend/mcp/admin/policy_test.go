package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/transport"
)

// TestServerView_PolicyReadback 断言管理面策略回读（M1-08，A1-08）：
// 服务器视图必须给出**有效策略**（平台默认 + 服务器覆盖），而不是只回显表里的原始字段。
func TestServerView_PolicyReadback(t *testing.T) {
	h := newHarness(t, transport.NewSSRFGuard(transport.SSRFConfig{AllowHTTP: true, AllowPrivate: true}))
	ctx := context.Background()

	created := h.createServer(t, "policy-readback")
	// 默认：ent 默认值（30s/4/1）与平台默认一致；宽限 30s；告警阈值 3。
	assert.Equal(t, 30000, created.Policy.TimeoutMS)
	assert.Equal(t, 4, created.Policy.MaxParallelCalls)
	assert.Equal(t, 1, created.Policy.MaxRetry)
	assert.Equal(t, 1, created.Policy.ReadRetryCap)
	assert.Equal(t, 30000, created.Policy.DisableGraceMS)
	assert.Equal(t, 3, created.Policy.HealthFailureThreshold)
	// 测试装配把健康检查间隔设为 1h（真实默认 30s，见 manager.Options）。
	assert.Equal(t, 3600000, created.Policy.HealthIntervalMS)

	// 服务器覆盖超时/并发/重试后，回读值随之变化（重试被平台上限 1 收紧）。
	timeout, parallel, retry := 5000, 2, 3
	updated, err := h.service.UpdateServer(ctx, h.actor(), created.ID, UpdateServerRequest{
		Version:          created.Version,
		TimeoutMS:        &timeout,
		MaxParallelCalls: &parallel,
		MaxRetry:         &retry,
	})
	require.NoError(t, err)
	assert.Equal(t, 5000, updated.Policy.TimeoutMS)
	assert.Equal(t, 2, updated.Policy.MaxParallelCalls)
	assert.Equal(t, 1, updated.Policy.MaxRetry, "平台读重试上限为 1，服务器放开到 3 也应收紧为 1")

	// 服务器显式关闭重试（0）→ 回读为 0。
	zero := 0
	updated, err = h.service.UpdateServer(ctx, h.actor(), created.ID, UpdateServerRequest{
		Version:  updated.Version,
		MaxRetry: &zero,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, updated.Policy.MaxRetry, "服务器可单独关闭读重试")
}
