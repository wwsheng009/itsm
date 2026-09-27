package ai

// M1-03：工具事件 → SSE 事件名映射。
//
// 独立成函数便于契约测试直接断言「事件名 + 载荷帧」，也保证事件名只在唯一处定义：
//
//	status=started → tool_call_started
//	status=done    → tool_call_finished
//	status=failed  → tool_call_failed
//	status=pending → approval_pending
//
// 未知状态按 `tool_call_started` 下发（宁可多发也不静默吞掉），旧客户端忽略未知事件即可。
func writeToolEvent(write func(event string, payload interface{}), ev ToolStreamEvent) {
	switch ev.Status {
	case ToolEventStatusDone:
		write("tool_call_finished", ev)
	case ToolEventStatusFailed:
		write("tool_call_failed", ev)
	case ToolEventStatusPending:
		write("approval_pending", ev)
	default:
		write("tool_call_started", ev)
	}
}
