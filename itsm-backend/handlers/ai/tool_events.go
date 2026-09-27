// M1-03：对话流式链路的工具事件契约（SSE）。
//
// 设计要点（对应实施方案 §4.2 M1-03）：
//   - 事件载荷字段固定为 `id / tool / provider / server / phase / status / summary / durationMs / errorCode`；
//   - `summary` 必须是**脱敏 + 截断**的结果摘要，参数与原始输出绝不进入事件；
//   - 事件只做「过程可见」，最终答案仍走既有 `delta/done` 消息流——事件丢失时前端降级渲染，不出现空白块；
//   - 既有 `sources/delta/done/error` 语义不变，旧客户端遇到未知事件必须忽略（前端 default 分支）。
package ai

import (
	"encoding/json"
	"errors"
	"strings"

	"itsm-backend/pkg/redact"
)

// 工具事件状态（与 SSE 事件名一一映射，见 handler.writeToolEvent）。
const (
	// ToolEventStatusStarted：工具调用开始（读路径即将执行 / 写路径即将提交审批）。
	ToolEventStatusStarted = "started"
	// ToolEventStatusDone：读路径执行成功（写路径的「执行」发生在审批之后，不在本次流内）。
	ToolEventStatusDone = "done"
	// ToolEventStatusFailed：工具调用失败（Gate2 拒绝 / 未知工具 / 执行错误）。
	ToolEventStatusFailed = "failed"
	// ToolEventStatusPending：写工具已提交审批，等待人工决策（一期外置审批闭环）。
	ToolEventStatusPending = "pending"
)

// ToolEventSummaryLimit 事件 summary 的字符上限（超出追加 redact.TruncatedMarker）。
//
// 取值与审批列表的单行摘要粒度对齐：足够让用户判断「查到了什么」，又不至于把整段
// 工具输出塞进 SSE（长输出由工具自身的截断/折叠机制承担）。
const ToolEventSummaryLimit = 320

// ToolStreamEvent 是 SSE 工具事件的载荷。
//
// 字段顺序与 JSON 标签即对外契约；`omitempty` 仅用于「无意义即不出现」的字段，
// 其余字段（tool/provider/phase/status）恒在，便于前端做类型收窄与未知值兜底。
type ToolStreamEvent struct {
	// ID：tool_invocation 主键（仅写路径/审批后有值）。
	ID int `json:"id,omitempty"`
	// Tool：投影后的可调用名（MCP 为 mcp__<server>__<tool>，内置为工具名）。
	Tool string `json:"tool"`
	// Provider：来源（builtin | mcp）。
	Provider string `json:"provider"`
	// Server：MCP 服务器标识（内置工具为空）。
	Server string `json:"server,omitempty"`
	// Phase：调用性质（read | write）。
	Phase string `json:"phase"`
	// Status：started | done | failed | pending。
	Status string `json:"status"`
	// Summary：脱敏 + 截断的结果摘要（不包含原始参数）。
	Summary string `json:"summary,omitempty"`
	// DurationMs：读路径执行耗时（毫秒，≥1；写路径提交阶段不产生耗时）。
	DurationMs int64 `json:"durationMs,omitempty"`
	// ErrorCode：失败时的稳定错误码（前端据此区分「无权限」与「执行失败」）。
	ErrorCode string `json:"errorCode,omitempty"`
}

// errorCoder 由外部工具错误类型实现（如 mcp/provider.ExecuteError），
// 这里做结构化提取，避免 handlers → mcp 的反向依赖。
type errorCoder interface{ ErrorCode() string }

// toolEventErrorCode 把工具执行错误映射为稳定错误码（供 SSE failed 事件与前端分流）。
//
// 未分类错误一律 `tool_execution_failed`：既保证前端有稳定分支，也避免把底层错误串外泄。
func toolEventErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrToolPermissionDenied):
		return "tool_permission_denied"
	case errors.Is(err, ErrUnknownTool):
		return "unknown_tool"
	case errors.Is(err, ErrToolUnavailable):
		return "tool_unavailable"
	case errors.Is(err, ErrToolQueueUnavailable):
		return "tool_queue_unavailable"
	case errors.Is(err, ErrInvocationNotPending):
		return "invocation_not_pending"
	}
	var coder errorCoder
	if errors.As(err, &coder) {
		if code := strings.TrimSpace(coder.ErrorCode()); code != "" {
			return code
		}
	}
	return "tool_execution_failed"
}

// toolEventSummary 生成事件摘要：先按 `pkg/redact` 的口径做键级掩码与值截断，再整体截断。
//
// 说明：`redact.Map` 对结构体（非 JSON 容器）不做深拷贝掩码，因此非 map/slice 结果
// 只有截断保护；MCP 结果在 provider 侧已按 `MaxResultBytes` 与脱敏口径处理，
// 内置工具结果不含凭据类字段，该分层与 M0-11 的审计摘要一致。
func toolEventSummary(result interface{}, limit int) string {
	if result == nil {
		return ""
	}
	if limit <= 0 {
		limit = ToolEventSummaryLimit
	}
	encoded, err := json.Marshal(redact.Map(result))
	if err != nil {
		return ""
	}
	text := string(encoded)
	if len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + redact.TruncatedMarker
}
