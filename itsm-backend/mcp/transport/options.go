package transport

import (
	"context"
	"time"
)

// DefaultConnectTimeout 是建连/握手的默认超时（M0-04 冻结：10s；与 config.mcp.connect_timeout_seconds 对应）。
const DefaultConnectTimeout = 10 * time.Second

// Kind 是远程传输类型（一期：Streamable HTTP 为主、SSE 兼容；stdio 为平台级，不在本包）。
type Kind string

const (
	// KindStreamableHTTP 对应 MCP 2025-03-26+ 的 Streamable HTTP 传输。
	KindStreamableHTTP Kind = "streamable"
	// KindSSE 对应 MCP 2024-11-05 的 HTTP+SSE 传输（兼容存量服务器）。
	KindSSE Kind = "sse"
)

// HeaderProvider 在每次请求前提供动态请求头（M0-06 凭据注入）。
// 返回值（含凭据）不得写入日志/审计/事件。
type HeaderProvider func(ctx context.Context) (map[string]string, error)

// Guard 是出站安全校验器（M0-05 提供默认实现：协议/网段/allowlist/DNS rebinding 防护）。
// 所有生产连接必须注入 Guard；Guard 为 nil 时 New 直接拒绝（fail-closed，M0-04 起即为硬约束）。
type Guard interface {
	Validate(ctx context.Context, rawURL string) error
}

// Config 描述一台 MCP 服务器的连接参数。
// 凭据不落在本结构中：通过 HeaderProvider 在请求时注入（避免明文驻留配置对象）。
type Config struct {
	Kind           Kind
	URL            string
	Headers        map[string]string
	HeaderProvider HeaderProvider
	ConnectTimeout time.Duration
	Guard          Guard
}

func (c Config) connectTimeout() time.Duration {
	if c.ConnectTimeout <= 0 {
		return DefaultConnectTimeout
	}
	return c.ConnectTimeout
}
