package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMCPMetrics_暴露断言 校验 MCP 指标确实出现在默认注册表的采集输出中
// （即 `/metrics` 端点可抓到；router.go 的 metricsAuth 分组直接挂 promhttp.Handler()）。
func TestMCPMetrics_暴露断言(t *testing.T) {
	// 先给每个指标写入一个样本：Prometheus 采集输出只包含「有样本」的序列，
	// 未写入的 Histogram/Counter 不会出现在 Gather() 结果里。
	MCPToolFaceTools.WithLabelValues("exposure-tenant").Set(7)
	MCPToolFaceTokens.WithLabelValues("exposure-tenant").Set(4096)
	MCPServerConnected.WithLabelValues("exposure-server").Set(1)
	MCPHandshakeDuration.WithLabelValues("exposure-server").Observe(0.1)
	MCPToolCalls.WithLabelValues("exposure-server", "list_issues", MCPCallOutcomeOK).Inc()
	MCPToolCallDuration.WithLabelValues("exposure-server", "list_issues").Observe(0.2)
	MCPOutputTruncated.WithLabelValues("exposure-server", "list_issues").Inc()
	MCPTools.WithLabelValues("exposure-server", "enabled").Set(1)
	MCPConcurrencyInUse.WithLabelValues("exposure-server").Set(1)
	MCPConcurrencyLimit.WithLabelValues("exposure-server").Set(4)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("采集默认注册表失败：%v", err)
	}
	names := map[string]bool{}
	registered := make([]string, 0, len(families))
	for _, family := range families {
		names[family.GetName()] = true
		registered = append(registered, family.GetName())
	}
	for _, want := range []string{
		"itsm_mcp_server_connected",
		"itsm_mcp_handshake_duration_seconds",
		"itsm_mcp_tool_calls_total",
		"itsm_mcp_tool_call_duration_seconds",
		"itsm_mcp_output_truncated_total",
		"itsm_mcp_tools",
		"itsm_mcp_concurrency_in_use",
		"itsm_mcp_concurrency_limit",
		"itsm_mcp_tool_face_tools",
		"itsm_mcp_tool_face_tokens",
	} {
		if !names[want] {
			t.Fatalf("指标 %s 未出现在 /metrics 采集输出中（已注册：%s）", want, strings.Join(registered, ", "))
		}
	}
}

// TestMCPMetrics_注册与写入 校验 MCP 指标（M2-06）已注册到默认注册表且可写入/读取。
//
// 说明：此处只做「指标设施」层断言；各采集点的语义断言在对应包用例中
// （如 mcp/manager 的预算指标、mcp/provider 的调用指标）。
func TestMCPMetrics_注册与写入(t *testing.T) {
	MCPServerConnected.WithLabelValues("srv-metrics-test").Set(1)
	if got := testutil.ToFloat64(MCPServerConnected.WithLabelValues("srv-metrics-test")); got != 1 {
		t.Fatalf("MCPServerConnected = %v, want 1", got)
	}
	MCPServerConnected.WithLabelValues("srv-metrics-test").Set(0)
	if got := testutil.ToFloat64(MCPServerConnected.WithLabelValues("srv-metrics-test")); got != 0 {
		t.Fatalf("MCPServerConnected = %v, want 0", got)
	}

	MCPHandshakeDuration.WithLabelValues("srv-metrics-test").Observe(0.25)
	MCPToolCallDuration.WithLabelValues("srv-metrics-test", "list_issues").Observe(0.5)

	before := testutil.ToFloat64(MCPToolCalls.WithLabelValues("srv-metrics-test", "list_issues", MCPCallOutcomeOK))
	MCPToolCalls.WithLabelValues("srv-metrics-test", "list_issues", MCPCallOutcomeOK).Inc()
	after := testutil.ToFloat64(MCPToolCalls.WithLabelValues("srv-metrics-test", "list_issues", MCPCallOutcomeOK))
	if after != before+1 {
		t.Fatalf("MCPToolCalls ok: %v -> %v, want +1", before, after)
	}

	MCPTools.WithLabelValues("srv-metrics-test", "enabled").Set(3)
	if got := testutil.ToFloat64(MCPTools.WithLabelValues("srv-metrics-test", "enabled")); got != 3 {
		t.Fatalf("MCPTools enabled = %v, want 3", got)
	}

	MCPConcurrencyInUse.WithLabelValues("srv-metrics-test").Set(2)
	MCPConcurrencyLimit.WithLabelValues("srv-metrics-test").Set(4)
	if got := testutil.ToFloat64(MCPConcurrencyInUse.WithLabelValues("srv-metrics-test")); got != 2 {
		t.Fatalf("MCPConcurrencyInUse = %v, want 2", got)
	}

	MCPToolFaceTools.WithLabelValues("1").Set(41)
	MCPToolFaceTokens.WithLabelValues("1").Set(12345)
	if got := testutil.ToFloat64(MCPToolFaceTokens.WithLabelValues("1")); got != 12345 {
		t.Fatalf("MCPToolFaceTokens = %v, want 12345", got)
	}

	MCPOutputTruncated.WithLabelValues("srv-metrics-test", "list_issues").Inc()
}

// TestMCPCallOutcome_常量 钉住结果分类取值（与看板/告警规则约定一致）。
func TestMCPCallOutcome_常量(t *testing.T) {
	want := map[string]string{
		"ok":       MCPCallOutcomeOK,
		"error":    MCPCallOutcomeError,
		"timeout":  MCPCallOutcomeTimeout,
		"canceled": MCPCallOutcomeCanceled,
	}
	for expected, actual := range want {
		if expected != actual {
			t.Fatalf("outcome 常量 %q != %q", expected, actual)
		}
	}
}
