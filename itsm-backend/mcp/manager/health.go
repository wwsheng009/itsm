package manager

import (
	"context"
	"fmt"
	"time"
)

// HealthTick 执行一轮健康检查与重连（由后台协程周期调用；测试可直接调用）。
//
// 行为：
//   - 未启用 / 进行中（connecting）：跳过；
//   - 持有会话：Ping 探活，失败 → 关闭会话、置 error 并进入退避（发出 reload_failed）；
//   - 无会话且退避到点：尝试重连（connectAndDiscover，含工具发现）。
func (m *Manager) HealthTick(ctx context.Context) {
	for _, target := range m.connections() {
		if ctx.Err() != nil {
			return
		}
		m.healthCheckConn(ctx, target)
	}
}

func (m *Manager) healthCheckConn(ctx context.Context, target *conn) {
	target.mu.Lock()
	enabled := target.cfg.Enabled
	status := target.status
	hasSession := target.session != nil
	due := target.nextAttemptAt
	target.mu.Unlock()

	if !enabled || status == StatusConnecting {
		return
	}

	if hasSession {
		if err := m.ping(ctx, target); err != nil {
			m.handleHealthFailure(ctx, target, err)
			return
		}
		// 探活成功：复位连续失败计数与退避（M1-08）。
		target.markHealthOK(m.now())
		target.resetFailures()
		return
	}
	if !due.IsZero() && m.now().Before(due) {
		return // 退避未到
	}
	if _, err := m.connectAndDiscover(ctx, target); err == nil {
		target.resetFailures()
	}
}

// ping 对现有会话做一次探活（受调用超时约束）。
func (m *Manager) ping(ctx context.Context, target *conn) error {
	session := target.currentSession()
	if session == nil {
		return ErrServerUnavailable
	}
	target.mu.Lock()
	timeout := callTimeout(target.cfg, m.opts.CallTimeout)
	target.mu.Unlock()

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return session.Ping(pingCtx)
}

// handleHealthFailure 处理健康检查失败：断连 + 置 error + 退避 + 事件。
func (m *Manager) handleHealthFailure(ctx context.Context, target *conn, err error) {
	target.closeSessionWithEvent(m, "健康检查失败")
	target.markFailure(err, m.opts.Backoff, m.now())
	cfg := target.config()
	m.emit(Event{
		Type:     EventServerReloadFailed,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   "健康检查失败：" + summarize(err),
	})
	// M1-08：连续失败达阈值 → 服务器已置 error（markFailure）+ 发出告警事件。
	m.emitHealthAlertIfNeeded(target, err)
	m.notifyStatus(ctx, target)
}

// emitHealthAlertIfNeeded 在连续失败达到阈值时发出告警（建连失败与探活失败共用同一条计数）。
// 每满一个阈值倍数发一次（3/6/9…）：保证告警不漏，同时避免每轮健康检查刷屏。
func (m *Manager) emitHealthAlertIfNeeded(target *conn, err error) {
	failures := target.failures()
	threshold := m.opts.HealthFailureThreshold
	if failures < threshold || failures%threshold != 0 {
		return
	}
	cfg := target.config()
	m.emit(Event{
		Type:     EventServerHealthAlert,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   fmt.Sprintf("连续 %d 次健康检查失败，服务器已置 error：%s", failures, summarize(err)),
		Failures: failures,
	})
}

// waitIdle 等待并发额度全部归还（禁用宽限）。
func (c *conn) waitIdle(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if len(c.sem) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// closeSessionWithEvent 关闭会话；若关闭前确实持有会话，则发出 disconnected 事件。
// 判定口径用「是否存在会话」而非当前状态：Reload 会先置 connecting，仍需报告旧连接断开。
func (c *conn) closeSessionWithEvent(m *Manager, reason string) {
	c.mu.Lock()
	hadSession := c.session != nil
	c.mu.Unlock()

	c.closeSession()

	if hadSession {
		cfg := c.config()
		m.emit(Event{
			Type:     EventServerDisconnected,
			TenantID: cfg.TenantID,
			ServerID: cfg.ID,
			Server:   cfg.Name,
			Detail:   reason,
		})
	}
}

// beginAttempt 标记一次建连任务开始（已在途则返回 false）。
func (c *conn) beginAttempt() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connecting {
		return false
	}
	c.connecting = true
	return true
}

// endAttempt 标记建连任务结束。
func (c *conn) endAttempt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connecting = false
}
