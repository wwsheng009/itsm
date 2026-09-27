package manager

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/client"
	"itsm-backend/mcp/transport"
)

// —— 假实现（注入 dialer，保证状态机/退避/并发节奏确定可测） ——

type fakeSession struct {
	mu              sync.Mutex
	closed          bool
	tools           []client.Tool
	listToolsErr    error
	callDelay       time.Duration
	callErr         error
	pingErr         error
	active          int
	maxActive       int
	lastOutcome     string
	protocolVersion string
	serverName      string
	serverVersion   string
}

func newFakeSession(rawNames ...string) *fakeSession {
	session := &fakeSession{
		protocolVersion: "2025-06-18",
		serverName:      "fake-server",
		serverVersion:   "v1",
	}
	for _, name := range rawNames {
		session.tools = append(session.tools, client.Tool{
			RawName:     name,
			Description: name + " description",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		})
	}
	return session
}

func (s *fakeSession) ID() string              { return "fake-session" }
func (s *fakeSession) ProtocolVersion() string { return s.protocolVersion }
func (s *fakeSession) ServerName() string      { return s.serverName }
func (s *fakeSession) ServerVersion() string   { return s.serverVersion }

func (s *fakeSession) ListTools(context.Context) ([]client.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listToolsErr != nil {
		return nil, s.listToolsErr
	}
	return append([]client.Tool(nil), s.tools...), nil
}

func (s *fakeSession) CallTool(ctx context.Context, _ string, _ map[string]any) (*client.CallResult, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	delay, callErr := s.callDelay, s.callErr
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	if delay > 0 {
		select {
		case <-time.After(delay):
			s.setOutcome("delay")
		case <-ctx.Done():
			s.setOutcome("canceled")
			return nil, ctx.Err()
		}
	} else {
		s.setOutcome("immediate")
	}
	if callErr != nil {
		return nil, callErr
	}
	return &client.CallResult{Content: []client.Content{{Type: "text", Text: "ok"}}}, nil
}

func (s *fakeSession) Ping(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session closed")
	}
	return s.pingErr
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *fakeSession) peakConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

func (s *fakeSession) setOutcome(outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastOutcome = outcome
}

func (s *fakeSession) outcome() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastOutcome
}

type fakeDialer struct {
	mu       sync.Mutex
	sessions map[int]*fakeSession
	errs     map[int]error
	attempts map[int]int
	block    chan struct{}
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{
		sessions: map[int]*fakeSession{},
		errs:     map[int]error{},
		attempts: map[int]int{},
	}
}

func (d *fakeDialer) Dial(_ context.Context, cfg ServerConfig) (ToolSession, error) {
	d.mu.Lock()
	d.attempts[cfg.ID]++
	err := d.errs[cfg.ID]
	session, ok := d.sessions[cfg.ID]
	block := d.block
	d.mu.Unlock()

	if block != nil {
		<-block // 测试闸门：用于断言 Enable 的"先 connecting 后建连"时序
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("fake dialer: 未配置会话")
	}
	return session, nil
}

func (d *fakeDialer) setBlock(block chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.block = block
}

func (d *fakeDialer) setSession(id int, session *fakeSession) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessions[id] = session
}

func (d *fakeDialer) setError(id int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.errs[id] = err
}

func (d *fakeDialer) attemptCount(id int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.attempts[id]
}

type fakeStatusWriter struct {
	mu      sync.Mutex
	patches map[int][]StatusPatch
}

func newFakeStatusWriter() *fakeStatusWriter {
	return &fakeStatusWriter{patches: map[int][]StatusPatch{}}
}

func (w *fakeStatusWriter) UpdateServerStatus(_ context.Context, serverID int, patch StatusPatch) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.patches[serverID] = append(w.patches[serverID], patch)
	return nil
}

func (w *fakeStatusWriter) last(serverID int) (StatusPatch, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	patches := w.patches[serverID]
	if len(patches) == 0 {
		return StatusPatch{}, false
	}
	return patches[len(patches)-1], true
}

func (w *fakeStatusWriter) first(serverID int) (StatusPatch, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	patches := w.patches[serverID]
	if len(patches) == 0 {
		return StatusPatch{}, false
	}
	return patches[0], true
}

// —— 测试脚手架 ——

func newTestManager(dialer *fakeDialer) (*Manager, *MemoryEventSink, *fakeStatusWriter) {
	events := CollectEvents()
	statuses := newFakeStatusWriter()
	manager := New(Options{
		Dial:           dialer.Dial,
		Events:         events,
		StatusWriter:   statuses,
		Backoff:        BackoffPolicy{Base: 20 * time.Millisecond, Factor: 2, Max: 80 * time.Millisecond},
		HealthInterval: 10 * time.Millisecond,
		ConnectTimeout: time.Second,
		CallTimeout:    time.Second,
		DisableGrace:   50 * time.Millisecond,
	})
	return manager, events, statuses
}

func testServerConfig(id int, name string) ServerConfig {
	return ServerConfig{
		ID:               id,
		TenantID:         1,
		Name:             name,
		Transport:        transport.KindStreamableHTTP,
		URL:              "https://example.com/mcp",
		MaxParallelCalls: 2,
		TimeoutMS:        1000,
		Enabled:          true,
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Fail(t, "等待条件超时")
}

// waitCount 等待事件计数达到期望值（事件在状态写入之后发出，属异步可观测语义）。
func waitCount(t *testing.T, events *MemoryEventSink, eventType EventType, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return events.Count(eventType) >= want },
		time.Second, 5*time.Millisecond, "等待事件 %s 达到 %d 条", eventType, want)
}

// —— 用例 ——

func TestManager_AsyncEnable_StateMachineAndDiscovery(t *testing.T) {
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession("list_issues", "get_issue"))
	block := make(chan struct{})
	dialer.setBlock(block)
	manager, events, statuses := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "github"))

	require.NoError(t, manager.Enable(context.Background(), 1))
	snapshot, err := manager.Status(1)
	require.NoError(t, err)
	require.Equal(t, StatusConnecting, snapshot.Status, "Enable 必须异步返回（D8：先置 connecting）")
	close(block) // 放行建连

	waitFor(t, 2*time.Second, func() bool {
		current, _ := manager.Status(1)
		return current.Status == StatusHealthy
	})

	waitCount(t, events, EventServerConnected, 1)
	waitCount(t, events, EventToolsDiscovered, 1)
	require.Eventually(t, func() bool { return len(manager.CachedTools(1)) == 2 },
		time.Second, 5*time.Millisecond, "等待工具缓存刷新")

	first, ok := statuses.first(1)
	require.True(t, ok)
	require.Equal(t, StatusConnecting, first.Status)
	var last StatusPatch
	require.Eventually(t, func() bool {
		current, ok := statuses.last(1)
		last = current
		return ok && current.Status == StatusHealthy
	}, time.Second, 5*time.Millisecond, "等待状态回写 healthy")
	require.Equal(t, "2025-06-18", last.ProtocolVersion)
	require.NotContains(t, last.ServerInfo, "token")
}

func TestManager_ConnectFailureBackoffThenRecover(t *testing.T) {
	dialer := newFakeDialer()
	dialer.setError(1, errors.New("connection refused"))
	dialer.setSession(1, newFakeSession("echo"))
	manager, events, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))

	require.NoError(t, manager.Enable(context.Background(), 1))
	waitFor(t, time.Second, func() bool {
		snapshot, _ := manager.Status(1)
		return snapshot.Status == StatusError
	})
	require.Equal(t, 1, dialer.attemptCount(1))
	waitCount(t, events, EventServerReloadFailed, 1)
	snapshot, _ := manager.Status(1)
	require.Equal(t, 1, snapshot.Attempts)
	require.NotZero(t, snapshot.NextAttemptAt, "失败后必须安排退避重试")

	// 退避未到：tick 不得重试。
	manager.HealthTick(context.Background())
	require.Equal(t, 1, dialer.attemptCount(1))

	// 恢复：清除错误后，退避到点由健康循环重连成功。
	dialer.setError(1, nil)
	require.Eventually(t, func() bool {
		manager.HealthTick(context.Background())
		current, _ := manager.Status(1)
		return current.Status == StatusHealthy
	}, 2*time.Second, 15*time.Millisecond)
	waitCount(t, events, EventServerConnected, 1)
	require.Eventually(t, func() bool { return len(manager.CachedTools(1)) == 1 },
		time.Second, 5*time.Millisecond, "等待恢复后的工具缓存")
}

func TestManager_ConcurrencyLimit(t *testing.T) {
	session := newFakeSession("echo")
	session.callDelay = 40 * time.Millisecond
	dialer := newFakeDialer()
	dialer.setSession(1, session)
	manager, _, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))
	require.NoError(t, manager.ConnectNow(context.Background(), 1))

	var waitGroup sync.WaitGroup
	var failures int32
	for i := 0; i < 6; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if _, err := manager.CallTool(context.Background(), 1, "echo", map[string]any{"message": "x"}); err != nil {
				atomic.AddInt32(&failures, 1)
			}
		}()
	}
	waitGroup.Wait()

	require.Zero(t, atomic.LoadInt32(&failures))
	require.LessOrEqual(t, session.peakConcurrency(), 2, "并发不得超过 MaxParallelCalls")
	require.GreaterOrEqual(t, session.peakConcurrency(), 2, "6 路并发在 40ms 延迟下应达到上限（验证限制器生效）")
}

func TestManager_CallTimeoutAndSemaphoreRelease(t *testing.T) {
	session := newFakeSession("slow")
	session.callDelay = 300 * time.Millisecond
	dialer := newFakeDialer()
	dialer.setSession(2, session)
	manager, _, _ := newTestManager(dialer)
	config := testServerConfig(2, "srv2")
	config.TimeoutMS = 30
	manager.Upsert(config)
	require.NoError(t, manager.ConnectNow(context.Background(), 2))

	start := time.Now()
	_, err := manager.CallTool(context.Background(), 2, "slow", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, "canceled", session.outcome(), "超时必须生效（调用被 ctx 截止取消而非等待完整延迟）")
	require.Less(t, time.Since(start), 2*time.Second, "不得等待完整 callDelay")

	// 额度归还：后续调用仍可执行。
	session.mu.Lock()
	session.callDelay = 0
	session.mu.Unlock()
	_, err = manager.CallTool(context.Background(), 2, "slow", nil)
	require.NoError(t, err)
}

func TestManager_CallToolErrorPaths(t *testing.T) {
	manager, _, _ := newTestManager(newFakeDialer())

	_, err := manager.CallTool(context.Background(), 99, "x", nil)
	require.ErrorIs(t, err, ErrServerNotFound)

	manager.Upsert(ServerConfig{ID: 3, TenantID: 1, Name: "srv3", Enabled: false})
	_, err = manager.CallTool(context.Background(), 3, "x", nil)
	require.ErrorIs(t, err, ErrServerDisabled)

	manager.Upsert(ServerConfig{ID: 4, TenantID: 1, Name: "srv4", Enabled: true})
	_, err = manager.CallTool(context.Background(), 4, "x", nil)
	require.ErrorIs(t, err, ErrServerUnavailable)
}

func TestManager_AuthRequiredDegradesAndEmits(t *testing.T) {
	session := newFakeSession("echo")
	session.callErr = &transport.Error{Code: transport.CodeAuthRequired, Op: "tools/call", Err: errors.New("401")}
	dialer := newFakeDialer()
	dialer.setSession(1, session)
	manager, events, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))
	require.NoError(t, manager.ConnectNow(context.Background(), 1))

	_, err := manager.CallTool(context.Background(), 1, "echo", nil)
	require.Error(t, err)

	snapshot, _ := manager.Status(1)
	require.Equal(t, StatusError, snapshot.Status, "认证失效必须降级")
	require.True(t, session.isClosed())
	require.Equal(t, 1, events.Count(EventServerAuthRequired))
	require.Equal(t, 1, events.Count(EventServerDisconnected))
}

func TestManager_EffectiveToolsRespectsGovernanceAndHealth(t *testing.T) {
	session := newFakeSession("alpha", "beta")
	dialer := newFakeDialer()
	dialer.setSession(1, session)
	manager, _, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))
	require.NoError(t, manager.ConnectNow(context.Background(), 1))

	records := manager.CachedTools(1)
	require.Len(t, records, 2)
	require.Empty(t, manager.EffectiveTools(), "默认 enabled=false：工具面为空（D7）")

	records[0].Enabled = true
	records[1].Enabled = true
	records[1].Quarantined = true
	manager.cache.Replace(1, records)
	require.Len(t, manager.EffectiveTools(), 1, "隔离工具不得进入有效工具面")

	// MCP 故障：工具面塌缩为空（降级只影响自身）。
	session.mu.Lock()
	session.pingErr = errors.New("boom")
	session.mu.Unlock()
	manager.HealthTick(context.Background())
	snapshot, _ := manager.Status(1)
	require.Equal(t, StatusError, snapshot.Status)
	require.Empty(t, manager.EffectiveTools(), "MCP 全挂时工具面为空且不 panic")
}

func TestManager_ReloadAndDisable(t *testing.T) {
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession("echo"))
	manager, events, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))
	require.NoError(t, manager.ConnectNow(context.Background(), 1))

	require.NoError(t, manager.Reload(context.Background(), 1))
	waitFor(t, 2*time.Second, func() bool {
		snapshot, _ := manager.Status(1)
		return snapshot.Status == StatusHealthy
	})
	waitCount(t, events, EventServerConnected, 2)
	waitCount(t, events, EventServerDisconnected, 1)

	require.NoError(t, manager.Disable(context.Background(), 1))
	waitFor(t, time.Second, func() bool {
		snapshot, _ := manager.Status(1)
		return snapshot.Status == StatusDisabled
	})
	waitCount(t, events, EventServerDisconnected, 2)
	_, err := manager.CallTool(context.Background(), 1, "echo", nil)
	require.ErrorIs(t, err, ErrServerDisabled)
}

func TestManager_StartStopHealthLoop(t *testing.T) {
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession("echo"))
	manager, _, _ := newTestManager(dialer)
	manager.Upsert(testServerConfig(1, "srv1"))
	require.NoError(t, manager.ConnectNow(context.Background(), 1))

	manager.Start(context.Background())
	time.Sleep(60 * time.Millisecond) // 让健康循环执行若干轮（Ping 成功）
	snapshot, _ := manager.Status(1)
	require.Equal(t, StatusHealthy, snapshot.Status)

	manager.Stop()
	manager.Stop() // 幂等
}
