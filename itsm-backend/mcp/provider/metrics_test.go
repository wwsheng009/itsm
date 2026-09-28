package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	mcpclient "itsm-backend/mcp/client"
	"itsm-backend/mcp/manager"
	"itsm-backend/metrics"
)

// TestProvider_MetricsOnCall（M2-06）校验工具调用指标接线：
// 成功/失败分别计入 outcome，截断单独计数；标签只用 server/tool（不含参数）。
func TestProvider_MetricsOnCall(t *testing.T) {
	entClient := newTestClient(t)
	ctx := context.Background()
	serverID := createServer(t, entClient, 1, "metrics-server", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(serverID, 1, "metrics-server", "list_issues", true, `{"type":"object"}`),
	}}
	provider := New(entClient, source, Options{Enabled: true, MaxResultBytes: 128})

	const callable = "mcp__metrics-server__list_issues"
	okBefore := testutil.ToFloat64(metrics.MCPToolCalls.WithLabelValues("metrics-server", callable, metrics.MCPCallOutcomeOK))

	// 成功调用。
	source.result = &mcpclient.CallResult{Content: []mcpclient.Content{{Type: "text", Text: "ok"}}}
	_, err := provider.Execute(ctx, 1, callable, nil)
	require.NoError(t, err)
	okAfter := testutil.ToFloat64(metrics.MCPToolCalls.WithLabelValues("metrics-server", callable, metrics.MCPCallOutcomeOK))
	require.Equal(t, okBefore+1, okAfter, "成功调用应计入 outcome=ok")

	// 超限截断：截断计数 +1。
	truncatedBefore := testutil.ToFloat64(metrics.MCPOutputTruncated.WithLabelValues("metrics-server", callable))
	source.result = &mcpclient.CallResult{Content: []mcpclient.Content{{Type: "text", Text: strings.Repeat("x", 4096)}}}
	_, err = provider.Execute(ctx, 1, callable, nil)
	require.NoError(t, err)
	truncatedAfter := testutil.ToFloat64(metrics.MCPOutputTruncated.WithLabelValues("metrics-server", callable))
	require.Equal(t, truncatedBefore+1, truncatedAfter, "截断结果应计入 output_truncated")

	// 失败调用：计入 outcome=error。
	errBefore := testutil.ToFloat64(metrics.MCPToolCalls.WithLabelValues("metrics-server", callable, metrics.MCPCallOutcomeError))
	source.err = errors.New("boom")
	_, err = provider.Execute(ctx, 1, callable, nil)
	require.Error(t, err)
	errAfter := testutil.ToFloat64(metrics.MCPToolCalls.WithLabelValues("metrics-server", callable, metrics.MCPCallOutcomeError))
	require.Equal(t, errBefore+1, errAfter, "失败调用应计入 outcome=error")

	// 取消/超时分类（指标口径，不依赖具体错误码映射）。
	require.Equal(t, metrics.MCPCallOutcomeTimeout, metricsCallOutcome(context.DeadlineExceeded))
	require.Equal(t, metrics.MCPCallOutcomeCanceled, metricsCallOutcome(context.Canceled))
	require.Equal(t, metrics.MCPCallOutcomeOK, metricsCallOutcome(nil))
}
