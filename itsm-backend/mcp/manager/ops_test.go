package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/client"
	"itsm-backend/mcp/transport"
)

// —— M1-08 运维收口：告警阈值、策略回读、读重试、in-flight 宽限 ——

func filterEvents(events []Event, eventType EventType) []Event {
	var out []Event
	for _, event := range events {
		if event.Type == eventType {
			out = append(out, event)
		}
	}
	return out
}

func TestHealthAlert_ThresholdEmitsAndResets(t *testing.T) {
	// 用「建连失败」驱动连续失败计数（会话存在时探活失败会关闭会话，节奏受退避影响）。
	var dialMu sync.Mutex
	dialErr := error(nil)
	dial := func(context.Context, ServerConfig) (ToolSession, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		if dialErr != nil {
			return nil, dialErr
		}
		return newFakeSession(), nil
	}
	events := CollectEvents()
	manager := New(Options{
		Dial:                   dial,
		Events:                 events,
		StatusWriter:           newFakeStatusWriter(),
		HealthFailureThreshold: 3,
		Backoff:                BackoffPolicy{Base: time.Millisecond, Factor: 1, Max: 2 * time.Millisecond},
		ConnectTimeout:         time.Second,
		DisableGrace:           100 * time.Millisecond,
	})
	ctx := context.Background()
	cfg := testServerConfig(1, "alert")
	cfg.Enabled = true
	manager.Upsert(cfg)

	// 第 1、2 次失败：只发 reload_failed，不发告警（未达阈值）。
	dialMu.Lock()
	dialErr = errors.New("connect refused")
	dialMu.Unlock()
	for i := 0; i < 2; i++ {
		time.Sleep(3 * time.Millisecond) // 越过退避窗口
		manager.HealthTick(ctx)
	}
	require.Equal(t, 2, events.Count(EventServerReloadFailed))
	require.Equal(t, 0, events.Count(EventServerHealthAlert), "未达阈值不得告警")
	status, _, _ := manager.lookupState(cfg.ID)
	require.Equal(t, StatusError, status, "失败后服务器应置 error")

	// 第 3 次失败：达到阈值 → 告警一次，事件带失败次数与最后错误。
	time.Sleep(3 * time.Millisecond)
	manager.HealthTick(ctx)
	require.Equal(t, 1, events.Count(EventServerHealthAlert))
	alerts := filterEvents(events.Events(), EventServerHealthAlert)
	require.Len(t, alerts, 1)
	assert.Equal(t, 3, alerts[0].Failures)
	assert.Contains(t, alerts[0].Detail, "连续 3 次健康检查失败")

	// 建连成功：计数复位（再失败一次不得立即告警）。
	dialMu.Lock()
	dialErr = nil
	dialMu.Unlock()
	time.Sleep(3 * time.Millisecond)
	manager.HealthTick(ctx)
	dialMu.Lock()
	dialErr = errors.New("connect refused again")
	dialMu.Unlock()
	time.Sleep(3 * time.Millisecond)
	manager.HealthTick(ctx)
	assert.Equal(t, 1, events.Count(EventServerHealthAlert), "成功后必须复位告警基线")
}

// TestPolicy_DefaultsMatchAnalysisTable 断言有效策略回读与 §5.4 默认表一致。
func TestPolicy_DefaultsMatchAnalysisTable(t *testing.T) {
	manager := New(Options{Dial: func(context.Context, ServerConfig) (ToolSession, error) {
		return newFakeSession(), nil
	}})

	// 未注册的服务器：平台默认（调用 30s / 连接 10s / 并发 4 / 读重试 1 / 宽限 30s / 阈值 3）。
	policy := manager.Policy(999)
	assert.Equal(t, 30000, policy.TimeoutMS)
	assert.Equal(t, 10000, policy.ConnectTimeoutMS)
	assert.Equal(t, 4, policy.MaxParallelCalls)
	assert.Equal(t, 1, policy.MaxRetry)
	assert.Equal(t, 1, policy.ReadRetryCap)
	assert.Equal(t, 30000, policy.DisableGraceMS)
	assert.Equal(t, 3, policy.HealthFailureThreshold)

	// 服务器覆盖超时/并发；未配置重试（0）→ 沿用平台上限。
	cfg := testServerConfig(7, "policy")
	cfg.TimeoutMS = 5000
	cfg.MaxParallelCalls = 2
	cfg.MaxRetry = 5 // 服务器放开到 5，但平台上限 1 收紧
	manager.Upsert(cfg)
	policy = manager.Policy(7)
	assert.Equal(t, 5000, policy.TimeoutMS)
	assert.Equal(t, 2, policy.MaxParallelCalls)
	assert.Equal(t, 1, policy.MaxRetry)

	// 服务器把重试收紧为 0 → 该服务器读重试关闭。
	cfg.MaxRetry = 0
	manager.Upsert(cfg)
	assert.Equal(t, 0, manager.Policy(7).MaxRetry)
}

func TestRetryableCallError(t *testing.T) {
	assert.True(t, retryableCallError(&transport.Error{Code: transport.CodeUnreachable, Err: errors.New("down")}))
	assert.True(t, retryableCallError(context.DeadlineExceeded))
	// 业务错误/认证失败/出站被拦/主动取消不得重试（防重复副作用与无意义重试）。
	assert.False(t, retryableCallError(&transport.Error{Code: transport.CodeServerError, Err: errors.New("tool failed")}))
	assert.False(t, retryableCallError(&transport.Error{Code: transport.CodeAuthRequired, Err: errors.New("401")}))
	assert.False(t, retryableCallError(&transport.Error{Code: transport.CodeSSRFBlocked, Err: errors.New("blocked")}))
	assert.False(t, retryableCallError(context.Canceled))
}

// TestCallToolWithPolicy_ReadRetriesOnceWriteNever 断言读工具至多重试 1 次、写工具不重试。
func TestCallToolWithPolicy_ReadRetriesOnceWriteNever(t *testing.T) {
	session := newFakeSession("list_issues")
	manager := New(Options{
		Dial:        func(context.Context, ServerConfig) (ToolSession, error) { return session, nil },
		CallTimeout: 2 * time.Second,
	})
	ctx := context.Background()
	cfg := testServerConfig(3, "retry")
	cfg.Enabled = true
	cfg.MaxRetry = 1 // 与 ent `max_retry` 默认值一致（装配层从库读取）
	manager.Upsert(cfg)
	require.NoError(t, manager.ConnectNow(ctx, cfg.ID))

	// 读工具 + 瞬时错误：重试 1 次（共 2 次调用）后仍失败 → 返回错误。
	transient := &transport.Error{Code: transport.CodeUnreachable, Err: errors.New("dial tcp: refused")}
	session.callErr = transient
	_, err := manager.CallToolWithPolicy(ctx, cfg.ID, "list_issues", nil, true)
	require.Error(t, err)
	assert.Equal(t, 2, session.callCount(), "读工具只重试 1 次（共 2 次调用）")

	// 读工具 + 业务错误：不重试（重试无法解决且可能重复副作用）。
	session.resetCallCount()
	session.callErr = &transport.Error{Code: transport.CodeServerError, Err: errors.New("tool boom")}
	_, err = manager.CallToolWithPolicy(ctx, cfg.ID, "list_issues", nil, true)
	require.Error(t, err)
	assert.Equal(t, 1, session.callCount(), "业务错误不得重试")

	// 写工具 + 瞬时错误：不重试。
	session.resetCallCount()
	session.callErr = transient
	_, err = manager.CallToolWithPolicy(ctx, cfg.ID, "list_issues", nil, false)
	require.Error(t, err)
	assert.Equal(t, 1, session.callCount(), "写工具不得自动重试")

	// 读工具重试后成功：返回结果。
	session.resetCallCount()
	session.callErr = nil
	result, err := manager.CallToolWithPolicy(ctx, cfg.ID, "list_issues", nil, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, session.callCount())

	// 服务器侧关闭重试（max_retry=0）：读工具也不重试。
	cfg.MaxRetry = 0
	manager.Upsert(cfg)
	session.resetCallCount()
	session.callErr = transient
	_, err = manager.CallToolWithPolicy(ctx, cfg.ID, "list_issues", nil, true)
	require.Error(t, err)
	assert.Equal(t, 1, session.callCount(), "服务器关闭重试后读工具不得重试")
}

// TestDisable_InFlightGraceAndForceClose 断言禁用：新调用立即拒绝、在途等宽限、超时强断并审计。
func TestDisable_InFlightGraceAndForceClose(t *testing.T) {
	session := newFakeSession("list_issues")
	events := CollectEvents()
	manager := New(Options{
		Dial:         func(context.Context, ServerConfig) (ToolSession, error) { return session, nil },
		Events:       events,
		CallTimeout:  2 * time.Second,
		DisableGrace: 80 * time.Millisecond,
	})
	ctx := context.Background()
	cfg := testServerConfig(5, "grace")
	cfg.Enabled = true
	manager.Upsert(cfg)
	require.NoError(t, manager.ConnectNow(ctx, cfg.ID))

	// 在途长调用（200ms > 80ms 宽限）。
	session.setBlockCh(make(chan struct{})) // 阻塞至测试显式放行，宽限必然到期（不依赖墙钟相对时序）
	done := make(chan error, 1)
	go func() {
		_, err := manager.CallTool(ctx, cfg.ID, "list_issues", nil)
		done <- err
	}()
	require.Eventually(t, func() bool { return session.activeCount() == 1 }, time.Second, 5*time.Millisecond)

	// 禁用：新调用立即拒绝（不等宽限）。
	require.NoError(t, manager.Disable(ctx, cfg.ID))
	_, err := manager.CallTool(ctx, cfg.ID, "list_issues", nil)
	assert.ErrorIs(t, err, ErrServerDisabled)

	// 宽限期结束仍在途 → 强制断开 + 审计事件。
	require.Eventually(t, func() bool { return events.Count(EventServerDisableGraceExpired) == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return session.isClosed() }, 2*time.Second, 10*time.Millisecond)
	close(session.blockCh) // 放行在途调用（此时会话已强制断开）
	<-done
	assert.Equal(t, 1, events.Count(EventServerDisconnected))
	status, _, lastError := manager.lookupState(cfg.ID)
	assert.Equal(t, StatusDisabled, status)
	assert.Contains(t, lastError, "宽限期")
}

// TestRetire_RejectsNewCallsAndHonorsGrace 断言删除：立即拒绝新调用（未注册）且保留 in-flight 宽限。
func TestRetire_RejectsNewCallsAndHonorsGrace(t *testing.T) {
	session := newFakeSession("list_issues")
	events := CollectEvents()
	manager := New(Options{
		Dial:         func(context.Context, ServerConfig) (ToolSession, error) { return session, nil },
		Events:       events,
		CallTimeout:  2 * time.Second,
		DisableGrace: time.Second, // 宽限足够：在途应正常结束（不触发强断）
	})
	ctx := context.Background()
	cfg := testServerConfig(6, "retire")
	cfg.Enabled = true
	manager.Upsert(cfg)
	require.NoError(t, manager.ConnectNow(ctx, cfg.ID))

	session.setBlockCh(make(chan struct{}))
	done := make(chan error, 1)
	go func() {
		_, err := manager.CallTool(ctx, cfg.ID, "list_issues", nil)
		done <- err
	}()
	require.Eventually(t, func() bool { return session.activeCount() == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, manager.Retire(ctx, cfg.ID))
	// 删除后：新调用立即拒绝（服务器已不在连接表）。
	_, err := manager.CallTool(ctx, cfg.ID, "list_issues", nil)
	assert.ErrorIs(t, err, ErrServerNotFound)

	// 在途调用不受影响地完成；宽限内不产生强断事件。
	close(session.blockCh)
	require.NoError(t, <-done)
	assert.Equal(t, 0, events.Count(EventServerDisableGraceExpired))
	require.Eventually(t, func() bool { return session.isClosed() }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, events.Count(EventServerDisconnected))
}

// —— 测试辅助：复用 fakeSession 的调用计数与在途并发观测 ——

func (s *fakeSession) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *fakeSession) resetCallCount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = 0
}

func (s *fakeSession) activeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// lookupState 读取连接槽状态（仅测试使用；manager 未提供该粒度快照）。
func (m *Manager) lookupState(serverID int) (ServerStatus, int, string) {
	target, ok := m.lookup(serverID)
	if !ok {
		return "", 0, ""
	}
	return target.state()
}

var _ = client.Tool{} // 保持 client 导入（fakeSession 的工具构造在 manager_test.go）
