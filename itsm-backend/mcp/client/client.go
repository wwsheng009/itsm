package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"itsm-backend/mcp/transport"
)

// EventType 是 MCP 连接生命周期事件类型（manager 消费；与 §5.3 事件清单对齐）。
type EventType string

const (
	// EventConnected：握手成功（含协商后的协议版本与服务器信息）。
	EventConnected EventType = "connected"
	// EventDisconnected：会话正常关闭。
	EventDisconnected EventType = "disconnected"
	// EventError：连接/握手失败（Err 已按 transport 分层码包装）。
	EventError EventType = "error"
)

// Event 是生命周期事件。Observer 实现不得阻塞、不得 panic。
type Event struct {
	Type            EventType
	ProtocolVersion string
	ServerName      string
	ServerVersion   string
	Err             error
}

// Observer 是生命周期观察钩子（可选）。
type Observer interface {
	OnMCPEvent(event Event)
}

// Options 是客户端选项；零值使用默认（Name=itsm-backend，超时见 timeouts.go）。
type Options struct {
	Name     string
	Version  string
	Timeouts Timeouts
	Observer Observer
}

// Client 是单连接 MCP 客户端：负责建连、握手与生命周期事件。
type Client struct {
	opts Options
}

// New 创建客户端。
func New(opts Options) *Client {
	if opts.Name == "" {
		opts.Name = "itsm-backend"
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	return &Client{opts: opts}
}

// Connect 建立连接并完成 initialize 握手。
//
// 超时语义（重要）：SDK 的 Connect(ctx, ...) 中的 ctx 会绑定**整个会话生命周期**
// （例如 SSE 的挂起 GET 流），因此这里不向 SDK 传递带 deadline 的 ctx；
// 握手时限由独立计时器控制，超时通过 cancel 中止（并等待 SDK 收尾），
// 成功后会话继续持有派生自调用方 ctx 的取消函数（Close 时释放）。
//
// 错误一律以 *transport.Error 返回，分层码见 transport.Code：
// connect_timeout / tls_error / auth_required / protocol_mismatch / unreachable / ...
// 协议版本不匹配由 SDK 拒绝（**不降级**），此处分类为 protocol_mismatch。
func (c *Client) Connect(ctx context.Context, t *transport.Transport) (*Session, error) {
	if t == nil {
		return nil, &transport.Error{Code: transport.CodeInvalidTransport, Op: "connect", Err: errors.New("nil transport")}
	}

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	inner := mcp.NewClient(&mcp.Implementation{Name: c.opts.Name, Version: c.opts.Version}, nil)

	type connectResult struct {
		session *mcp.ClientSession
		err     error
	}
	resultCh := make(chan connectResult, 1)
	go func() {
		session, err := inner.Connect(sessionCtx, t.SDK(), nil)
		resultCh <- connectResult{session: session, err: err}
	}()

	timeout := c.opts.Timeouts.connect()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		if result.err != nil {
			sessionCancel()
			wrapped := wrapError(result.err, "connect")
			c.notify(Event{Type: EventError, Err: wrapped})
			return nil, wrapped
		}
		s := &Session{
			inner:    result.session,
			observer: c.opts.Observer,
			timeouts: c.opts.Timeouts,
			cancel:   sessionCancel,
		}
		if initResult := result.session.InitializeResult(); initResult != nil {
			s.protocolVersion = initResult.ProtocolVersion
			if initResult.ServerInfo != nil {
				s.serverName = initResult.ServerInfo.Name
				s.serverVersion = initResult.ServerInfo.Version
			}
		}

		c.notify(Event{
			Type:            EventConnected,
			ProtocolVersion: s.protocolVersion,
			ServerName:      s.serverName,
			ServerVersion:   s.serverVersion,
		})
		return s, nil

	case <-timer.C:
		sessionCancel() // 中止握手（同时释放 SSE 挂起流等资源）
		<-resultCh      // 等待 SDK 收尾，避免 goroutine 泄漏
		wrapped := &transport.Error{
			Code: transport.CodeConnectTimeout,
			Op:   "connect",
			Err:  fmt.Errorf("建连/握手超过 %s", timeout),
		}
		c.notify(Event{Type: EventError, Err: wrapped})
		return nil, wrapped
	}
}

func (c *Client) notify(event Event) {
	if c.opts.Observer != nil {
		c.opts.Observer.OnMCPEvent(event)
	}
}

// wrapError 把底层错误包装为带分层码的 *transport.Error。
func wrapError(err error, op string) error {
	if err == nil {
		return nil
	}
	code := transport.Classify(err)
	if code == "" {
		code = transport.CodeTransportError
	}
	return &transport.Error{Code: code, Op: op, Err: err}
}
