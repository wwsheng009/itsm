package manager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/transport"
)

// allowAllGuard 是集成用例的出站守卫（放行一切；真实 SSRF 校验见 transport/ssrf.go）。
type allowAllGuard struct{ calls atomic.Int32 }

func (g *allowAllGuard) Validate(context.Context, string) error {
	g.calls.Add(1)
	return nil
}

// TestManager_Integration_ConnectDiscoverCallAndReconnect 覆盖 M0-07 的集成证据要求：
// 真实 transport + client + manager 装配；mock 服务器注入"断开/恢复"；
// 断言降级（工具面塌缩）与恢复（重连 + 重新发现）。
func TestManager_Integration_ConnectDiscoverCallAndReconnect(t *testing.T) {
	ctx := context.Background()

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

	var down atomic.Bool
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	guard := &allowAllGuard{}
	events := CollectEvents()
	manager := New(Options{
		Guard:          guard,
		Events:         events,
		Backoff:        BackoffPolicy{Base: 20 * time.Millisecond, Factor: 2, Max: 80 * time.Millisecond},
		HealthInterval: 10 * time.Millisecond,
		ConnectTimeout: 2 * time.Second,
		CallTimeout:    1 * time.Second,
		DisableGrace:   200 * time.Millisecond,
	})

	// 测试收尾（defer 为 LIFO）：先关闭会话再关服务器——
	// 否则挂起的 SSE 流会让 httpServer.Close 永久阻塞。
	defer manager.Remove(1)

	manager.Upsert(ServerConfig{
		ID:               1,
		TenantID:         1,
		Name:             "mock",
		Transport:        transport.KindStreamableHTTP,
		URL:              httpServer.URL,
		MaxParallelCalls: 2,
		TimeoutMS:        1000,
		Enabled:          true,
	})

	// 建连（真实 transport + client；Guard 参与出站校验）。
	require.NoError(t, manager.ConnectNow(ctx, 1))
	require.Greater(t, guard.calls.Load(), int32(0), "SSRF Guard 必须参与构造期/请求期校验")

	snapshot, err := manager.Status(1)
	require.NoError(t, err)
	require.Equal(t, StatusHealthy, snapshot.Status)
	require.NotEmpty(t, snapshot.ProtocolVersion)
	require.Contains(t, snapshot.ServerInfo, "itsm-mock")

	// 建连已包含首次发现（connectAndDiscover）；缓存中应有投影后的工具。
	records := manager.CachedTools(1)
	require.Len(t, records, 1)
	require.Equal(t, "mcp__mock__echo", records[0].CallableName)
	require.False(t, records[0].Enabled, "新工具默认不启用（D7）")

	// 再次发现：内容一致 → 差分为空（幂等）。
	discovery, err := manager.DiscoverNow(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, discovery.Delta.Added)
	require.Zero(t, discovery.Delta.Changed())
	require.Equal(t, 1, discovery.Delta.Unchanged)

	result, err := manager.CallTool(ctx, 1, "echo", map[string]any{"message": "itsm"})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	require.Equal(t, "echo:itsm", result.Content[0].Text)
	require.Len(t, manager.CachedTools(1), 1)

	// 注入断开：健康检查失败 → error + 工具面塌缩（降级只影响自身）。
	down.Store(true)
	require.Eventually(t, func() bool {
		manager.HealthTick(ctx)
		current, _ := manager.Status(1)
		return current.Status == StatusError
	}, 8*time.Second, 20*time.Millisecond)
	require.Empty(t, manager.EffectiveTools(), "MCP 故障时工具面必须塌缩为空")

	// 注入恢复：退避到点后重连成功 → healthy + 重新发现。
	down.Store(false)
	require.Eventually(t, func() bool {
		manager.HealthTick(ctx)
		current, _ := manager.Status(1)
		return current.Status == StatusHealthy
	}, 8*time.Second, 20*time.Millisecond)
	waitCount(t, events, EventServerConnected, 2)
	require.Eventually(t, func() bool { return events.Count(EventServerReloadFailed) >= 1 },
		time.Second, 10*time.Millisecond, "等待重连失败事件")
	require.Len(t, manager.CachedTools(1), 1, "恢复后缓存仍持有工具且重新发现")

	// 停用：真实关闭会话。
	require.NoError(t, manager.Disable(ctx, 1))
	require.Eventually(t, func() bool {
		current, _ := manager.Status(1)
		return current.Status == StatusDisabled
	}, 3*time.Second, 10*time.Millisecond)
}
