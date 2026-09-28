package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/transport"
)

// —— 测试桩 ——

type allowGuard struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (g *allowGuard) Validate(context.Context, string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return g.err
}

func (g *allowGuard) CallCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (o *recordingObserver) OnMCPEvent(event Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
}

func (o *recordingObserver) Events() []Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Event(nil), o.events...)
}

// newEchoServer 起最小 MCP 服务（echo 工具）并记录收到的请求头。
func newEchoServer(t *testing.T) (*httptest.Server, func() http.Header) {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "itsm-mock", Version: "v0.1.0"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "echo back",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + args.Message}}}, nil
	})

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)

	var mu sync.Mutex
	var lastHeaders http.Header
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastHeaders = r.Header.Clone()
		mu.Unlock()
		mcpHandler.ServeHTTP(w, r)
	}))

	headers := func() http.Header {
		mu.Lock()
		defer mu.Unlock()
		return lastHeaders
	}
	return httpServer, headers
}

// —— 主链路 ——

func TestConnect_StreamableHTTP_FullFlow(t *testing.T) {
	ctx := context.Background()
	httpServer, seenHeaders := newEchoServer(t)
	defer httpServer.Close()

	guard := &allowGuard{}
	tr, err := transport.New(ctx, transport.Config{
		URL:     httpServer.URL,
		Headers: map[string]string{"X-Static": "static-value"},
		HeaderProvider: func(context.Context) (map[string]string, error) {
			return map[string]string{"Authorization": "Bearer dyn-token"}, nil
		},
		Guard: guard,
	})
	require.NoError(t, err)

	observer := &recordingObserver{}
	session, err := New(Options{Name: "itsm-backend-test", Version: "v0.0.1", Observer: observer}).Connect(ctx, tr)
	require.NoError(t, err)
	defer session.Close()

	require.NotEmpty(t, session.ProtocolVersion(), "必须记录协商后的协议版本")
	require.Equal(t, "itsm-mock", session.ServerName())
	require.Equal(t, "v0.1.0", session.ServerVersion())

	tools, err := session.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, "echo", tools[0].RawName)
	require.Contains(t, string(tools[0].InputSchema), `"message"`, "inputSchema 必须原样保存")

	result, err := session.CallTool(ctx, "echo", map[string]any{"message": "itsm"})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	require.Equal(t, "text", result.Content[0].Type)
	require.Equal(t, "echo:itsm", result.Content[0].Text)

	require.NoError(t, session.Ping(ctx))

	// 请求头注入（静态 + 动态凭据）。
	headers := seenHeaders()
	require.Equal(t, "static-value", headers.Get("X-Static"))
	require.Equal(t, "Bearer dyn-token", headers.Get("Authorization"))

	// Guard 至少覆盖构造期与请求期。
	require.GreaterOrEqual(t, guard.CallCount(), 2)

	// 事件：connected（握手）→ disconnected（关闭）。
	require.NoError(t, session.Close())
	events := observer.Events()
	require.NotEmpty(t, events)
	require.Equal(t, EventConnected, events[0].Type)
	require.Equal(t, session.ProtocolVersion(), events[0].ProtocolVersion)
	require.Equal(t, EventDisconnected, events[len(events)-1].Type)
}

func TestConnect_SSE_Handshake(t *testing.T) {
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "itsm-sse-mock", Version: "v0.1.0"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "ping_tool",
		Description: "noop",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil
	})

	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	tr, err := transport.New(ctx, transport.Config{Kind: transport.KindSSE, URL: httpServer.URL, Guard: &allowGuard{}})
	require.NoError(t, err)

	// 握手预算取 3s：全量并发测试（go test ./...）下 100ms 级预算会因排程抖动误报超时；
	// 本用例真正要断言的是「会话不绑定握手超时」，因此只需让后续 sleep 超过该预算即可。
	session, err := New(Options{Timeouts: Timeouts{Connect: 3 * time.Second}}).Connect(ctx, tr)
	require.NoError(t, err)
	defer session.Close()

	// 回归守卫（M0-04）：会话不得绑定握手超时——连接超时窗口过后 SSE 流仍必须存活。
	time.Sleep(3200 * time.Millisecond)

	tools, err := session.ListTools(ctx)
	require.NoError(t, err, "sse tools/list err=%v", err)
	require.Len(t, tools, 1)
	require.Equal(t, "ping_tool", tools[0].RawName)
}

// —— 错误路径 ——

func TestConnect_AuthRequired(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer httpServer.Close()

	tr, err := transport.New(context.Background(), transport.Config{URL: httpServer.URL, Guard: &allowGuard{}})
	require.NoError(t, err)

	observer := &recordingObserver{}
	_, err = New(Options{Observer: observer}).Connect(context.Background(), tr)
	require.Error(t, err)
	require.Equal(t, transport.CodeAuthRequired, transport.CodeOf(err), "err=%v", err)

	events := observer.Events()
	require.Len(t, events, 1)
	require.Equal(t, EventError, events[0].Type)
	require.Error(t, events[0].Err)
}

func TestConnect_Timeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer slow.Close()

	tr, err := transport.New(context.Background(), transport.Config{
		URL:            slow.URL,
		ConnectTimeout: 80 * time.Millisecond,
		Guard:          &allowGuard{},
	})
	require.NoError(t, err)

	_, err = New(Options{Timeouts: Timeouts{Connect: 80 * time.Millisecond}}).Connect(context.Background(), tr)
	require.Error(t, err)
	require.Equal(t, transport.CodeConnectTimeout, transport.CodeOf(err))
}

func TestConnect_ProtocolMismatch(t *testing.T) {
	// 手工 JSON-RPC 服务器：initialize 返回不受支持的协议版本。
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)

		if msg.Method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"result": map[string]any{
					"protocolVersion": "1999-01-01",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "legacy", "version": "0"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer raw.Close()

	tr, err := transport.New(context.Background(), transport.Config{URL: raw.URL, Guard: &allowGuard{}})
	require.NoError(t, err)

	_, err = New(Options{}).Connect(context.Background(), tr)
	require.Error(t, err)
	require.Equal(t, transport.CodeProtocolMismatch, transport.CodeOf(err), "协议版本不匹配必须拒绝且不降级")
}

func TestConnect_SSRFBlocked(t *testing.T) {
	httpServer, _ := newEchoServer(t)
	defer httpServer.Close()

	guard := &allowGuard{err: errors.New("blocked: private address")}
	tr, err := transport.New(context.Background(), transport.Config{URL: httpServer.URL, Guard: guard})
	// 构造期即被拒绝。
	require.Error(t, err)
	require.Equal(t, transport.CodeSSRFBlocked, transport.CodeOf(err))
	require.Nil(t, tr)
}

func TestConnect_TLSFailure(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "itsm-tls-mock", Version: "v0.1.0"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := httptest.NewTLSServer(handler)
	defer httpServer.Close()

	tr, err := transport.New(context.Background(), transport.Config{URL: httpServer.URL, Guard: &allowGuard{}})
	require.NoError(t, err)

	_, err = New(Options{Timeouts: Timeouts{Connect: 2 * time.Second}}).Connect(context.Background(), tr)
	require.Error(t, err)
	require.Equal(t, transport.CodeTLSError, transport.CodeOf(err), "自签证书必须分类为 tls_error")
}
