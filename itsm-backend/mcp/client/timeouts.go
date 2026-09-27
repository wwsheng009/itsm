package client

import "time"

const (
	// DefaultConnectTimeout 是建连/握手默认超时（与 config.mcp.connect_timeout_seconds 默认一致）。
	DefaultConnectTimeout = 10 * time.Second
	// DefaultCallTimeout 是单次 MCP 调用（tools/list、tools/call、ping）默认超时（冻结：30s）。
	DefaultCallTimeout = 30 * time.Second
)

// Timeouts 是会话级超时；零值使用默认。
type Timeouts struct {
	Connect time.Duration
	Call    time.Duration
}

func (t Timeouts) connect() time.Duration {
	if t.Connect <= 0 {
		return DefaultConnectTimeout
	}
	return t.Connect
}

func (t Timeouts) call() time.Duration {
	if t.Call <= 0 {
		return DefaultCallTimeout
	}
	return t.Call
}
