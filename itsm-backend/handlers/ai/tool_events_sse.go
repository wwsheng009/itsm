package ai

// M1-03 + B1-03：工具事件 → SSE 事件名映射（**单一注册表**在 sse_events.go）。
//
// 事件名只在 `sse_events.go` 定义，本函数只做 status → 名字的分派：
//
//	status=started → tool_call_started      （v1，载荷不变）
//	status=done    → tool_call_finished     （v1，载荷不变）
//	status=failed  → tool_call_failed       （v1，载荷不变）
//	status=pending → approval_pending       （v1，载荷不变）
//	              +  confirmation_required  （v2，同一载荷 + `v:2`，双发灰度）
//
// 未知状态按 `tool_call_started` 下发（宁可多发也不静默吞掉），旧客户端忽略未知事件即可。
func writeToolEvent(write func(event string, payload interface{}), ev ToolStreamEvent) {
	switch ev.Status {
	case ToolEventStatusDone:
		write(SSEEventToolCallFinished, ev)
	case ToolEventStatusFailed:
		write(SSEEventToolCallFailed, ev)
	case ToolEventStatusPending:
		// 先旧名后新名：旧客户端命中旧名后即可忽略随后的未知事件（未知事件忽略策略）。
		write(SSEEventApprovalPending, ev)
		write(SSEEventConfirmationRequired, sseEnvelopeV2(ev))
	default:
		write(SSEEventToolCallStarted, ev)
	}
}
