package manager

import (
	"context"
	"math"
	"sync"
	"time"

	"itsm-backend/mcp/client"
	"itsm-backend/metrics"
)

// ServerStatus 是服务器运行位（与 ent `mcp_servers.status` 取值一致）。
// 卡片 M0-07 的 "connected" 即本实现的 `healthy`（运行位）。
type ServerStatus string

const (
	StatusDisabled   ServerStatus = "disabled"
	StatusConfigured ServerStatus = "configured"
	StatusConnecting ServerStatus = "connecting"
	StatusHealthy    ServerStatus = "healthy"
	StatusError      ServerStatus = "error"
)

// StatusSnapshot 是运行态只读快照（M0-12 健康摘要复用）。
type StatusSnapshot struct {
	ServerID         int
	Name             string
	Status           ServerStatus
	LastError        string
	ProtocolVersion  string
	ServerInfo       string
	ConnectedAt      time.Time
	Attempts         int
	NextAttemptAt    time.Time
	ToolCount        int
	QuarantinedCount int
}

// ToolSession 抽象 `*client.Session`（测试注入假实现，保证节奏与并发确定性）。
type ToolSession interface {
	ID() string
	ProtocolVersion() string
	ServerName() string
	ServerVersion() string
	ListTools(ctx context.Context) ([]client.Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*client.CallResult, error)
	Ping(ctx context.Context) error
	Close() error
}

// DialFunc 建立到服务器的会话（默认实现走 transport + client；测试可注入）。
type DialFunc func(ctx context.Context, cfg ServerConfig) (ToolSession, error)

// BackoffPolicy 是指数退避策略。
type BackoffPolicy struct {
	Base   time.Duration
	Factor float64
	Max    time.Duration
}

// WithDefaults 填充默认值：1s 起、2 倍增长、上限 1min。
func (p BackoffPolicy) WithDefaults() BackoffPolicy {
	if p.Base <= 0 {
		p.Base = time.Second
	}
	if p.Factor < 1 {
		p.Factor = 2
	}
	if p.Max <= 0 {
		p.Max = time.Minute
	}
	return p
}

// Delay 返回第 attempt 次连续失败后的等待时长（attempt 从 1 开始）。
func (p BackoffPolicy) Delay(attempt int) time.Duration {
	p = p.WithDefaults()
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(p.Base) * math.Pow(p.Factor, float64(attempt-1))
	if delay > float64(p.Max) {
		return p.Max
	}
	return time.Duration(delay)
}

// conn 是单服务器连接槽：会话 + 并发信号量 + 退避状态。
type conn struct {
	cfg  ServerConfig
	dial DialFunc
	sem  chan struct{}

	mu              sync.Mutex
	session         ToolSession
	status          ServerStatus
	lastError       string
	protocolVersion string
	serverInfo      string
	connectedAt     time.Time
	attempts        int
	// consecutiveFailures：连续失败计数（M1-08 告警阈值判定；成功即清零）。
	consecutiveFailures int
	nextAttemptAt       time.Time
	lastHealthAt        time.Time
	discovered          []ToolRecord
	connecting          bool // 建连任务在途标记（防止并发重复建连）
}

func newConn(cfg ServerConfig, dial DialFunc) *conn {
	limit := cfg.MaxParallelCalls
	if limit <= 0 {
		limit = defaultMaxParallelCalls
	}
	status := StatusConfigured
	if !cfg.Enabled {
		status = StatusDisabled
	}
	return &conn{
		cfg:    cfg,
		dial:   dial,
		sem:    make(chan struct{}, limit),
		status: status,
	}
}

// acquire 获取一个并发额度（受 ctx 取消约束）。
func (c *conn) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		c.observeConcurrency()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release 归还并发额度。
func (c *conn) release() {
	select {
	case <-c.sem:
		c.observeConcurrency()
	default:
	}
}

// observeConcurrency 写出并发使用率指标（M2-06：in_use / limit）。
func (c *conn) observeConcurrency() {
	metrics.MCPConcurrencyInUse.WithLabelValues(c.cfg.Name).Set(float64(len(c.sem)))
	metrics.MCPConcurrencyLimit.WithLabelValues(c.cfg.Name).Set(float64(cap(c.sem)))
}

func (c *conn) snapshot() StatusSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	quarantined := 0
	for _, tool := range c.discovered {
		if tool.Quarantined {
			quarantined++
		}
	}
	return StatusSnapshot{
		ServerID:         c.cfg.ID,
		Name:             c.cfg.Name,
		Status:           c.status,
		LastError:        c.lastError,
		ProtocolVersion:  c.protocolVersion,
		ServerInfo:       c.serverInfo,
		ConnectedAt:      c.connectedAt,
		Attempts:         c.attempts,
		NextAttemptAt:    c.nextAttemptAt,
		ToolCount:        len(c.discovered),
		QuarantinedCount: quarantined,
	}
}

// currentSession 返回当前会话（可能为 nil）。
func (c *conn) currentSession() ToolSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// setSession 安装新会话并标记 healthy。
func (c *conn) setSession(session ToolSession, protocolVersion, serverInfo string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = session
	c.status = StatusHealthy
	c.lastError = ""
	c.protocolVersion = protocolVersion
	c.serverInfo = serverInfo
	c.connectedAt = at
	c.attempts = 0
	c.consecutiveFailures = 0
	c.nextAttemptAt = time.Time{}
}

// closeSession 关闭并清空会话（幂等）。
func (c *conn) closeSession() {
	c.mu.Lock()
	session := c.session
	c.session = nil
	c.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}

// markConnecting 置 connecting 并清空上一轮错误展示。
func (c *conn) markConnecting() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = StatusConnecting
	c.lastError = ""
}

// markDisabled 置 disabled（并清空退避）。
func (c *conn) markDisabled() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = StatusDisabled
	c.attempts = 0
	c.nextAttemptAt = time.Time{}
}

// markFailure 记录失败并按退避策略安排下次重试。
func (c *conn) markFailure(err error, policy BackoffPolicy, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = StatusError
	if err != nil {
		c.lastError = err.Error()
	}
	c.attempts++
	c.consecutiveFailures++
	c.nextAttemptAt = now.Add(policy.WithDefaults().Delay(c.attempts))
}

// failures 返回当前连续失败次数（告警阈值判定用）。
func (c *conn) failures() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consecutiveFailures
}

// resetFailures 清零连续失败计数（探活/建连成功时调用）。
func (c *conn) resetFailures() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures = 0
}

// setLastError 记录最近一次错误（不改状态位）。
func (c *conn) setLastError(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastError = message
}

// maxRetryBudget 返回该服务器允许的读重试次数（0 表示不重试）。
func (c *conn) maxRetryBudget() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.MaxRetry
}

// markHealthOK 记录一次健康检查成功。
func (c *conn) markHealthOK(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = StatusHealthy
	c.lastHealthAt = at
	c.attempts = 0
	c.nextAttemptAt = time.Time{}
}

// state 返回 (status, attempts, lastError) 快照。
func (c *conn) state() (ServerStatus, int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.attempts, c.lastError
}

// config 返回配置快照（建连与事件组装使用，避免无锁读取 Upsert 的并发写）。
func (c *conn) config() ServerConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}
