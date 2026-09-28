package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MCP（外部工具接入）指标（M2-06；分析报告 §5.10）。
//
// 口径与命名遵循既有约定：`itsm_<域>_<对象>_<单位>`；labels 只放低基数维度
// （server/tool/state/outcome），**不含参数内容或用户输入**，避免指标成为泄露面。
//
// 与工具面预算（M2-03）的关系：`itsm_mcp_tool_face_tools` / `_tokens` 由 manager 的
// 预算判定顺带写出，用于看板呈现「逼近预算」的趋势；超限事件本身走
// `mcp.tools.budget_exceeded`（事件流），指标只做趋势。
var (
	// MCPServerConnected：服务器当前连接状态（1=healthy，0=其它；状态机口径见 M0-07）。
	MCPServerConnected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_server_connected",
			Help: "Whether the MCP server connection is healthy (1) or not (0)",
		},
		[]string{"server"},
	)

	// MCPHandshakeDuration：建连+握手耗时（秒；含工具发现前的一次协议协商）。
	MCPHandshakeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "itsm_mcp_handshake_duration_seconds",
			Help:    "MCP connect and initialize handshake duration in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"server"},
	)

	// MCPToolCalls：工具调用计数（outcome=ok|error|timeout|canceled|retry）。
	//
	// 说明：`retry` 只统计「读工具重试后的成功」，写工具恒不重试（M1-08 重试分治）。
	MCPToolCalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "itsm_mcp_tool_calls_total",
			Help: "Total MCP tool calls by outcome",
		},
		[]string{"server", "tool", "outcome"},
	)

	// MCPToolCallDuration：工具调用耗时（秒）——P50/P95 由 Prometheus 侧 histogram_quantile 计算。
	MCPToolCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "itsm_mcp_tool_call_duration_seconds",
			Help:    "MCP tool call duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"server", "tool"},
	)

	// MCPOutputTruncated：结果超上限被截断的次数（M0-09 的 256KB 上限）。
	MCPOutputTruncated = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "itsm_mcp_output_truncated_total",
			Help: "Total MCP tool results truncated by the output size limit",
		},
		[]string{"server", "tool"},
	)

	// MCPTools：工具治理状态计数（state=enabled|disabled|quarantined）。
	MCPTools = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_tools",
			Help: "Discovered MCP tools by governance state",
		},
		[]string{"server", "state"},
	)

	// MCPConcurrencyInUse / MCPConcurrencyLimit：每服务器并发使用率（in_use/limit）。
	MCPConcurrencyInUse = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_concurrency_in_use",
			Help: "In-flight MCP calls per server",
		},
		[]string{"server"},
	)
	MCPConcurrencyLimit = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_concurrency_limit",
			Help: "Configured per-server MCP concurrency limit",
		},
		[]string{"server"},
	)

	// MCPToolFaceTools / MCPToolFaceTokens：租户工具面规模（M2-03 预算判定的观测面）。
	MCPToolFaceTools = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_tool_face_tools",
			Help: "Effective MCP tools exposed to a tenant",
		},
		[]string{"tenant_id"},
	)
	MCPToolFaceTokens = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "itsm_mcp_tool_face_tokens",
			Help: "Estimated MCP tool face tokens for a tenant",
		},
		[]string{"tenant_id"},
	)
)

// MCPCallOutcome 是工具调用结果分类（与 `itsm_mcp_tool_calls_total{outcome}` 对齐）。
const (
	MCPCallOutcomeOK       = "ok"
	MCPCallOutcomeError    = "error"
	MCPCallOutcomeTimeout  = "timeout"
	MCPCallOutcomeCanceled = "canceled"
)
