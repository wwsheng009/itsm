package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"itsm-backend/mcp/client"
	"itsm-backend/mcp/transport"
	"itsm-backend/metrics"
)

const (
	defaultMaxParallelCalls = 4
	defaultHealthInterval   = 30 * time.Second
	defaultConnectTimeout   = 10 * time.Second
	defaultCallTimeout      = 30 * time.Second
	defaultDisableGrace     = 30 * time.Second
	// defaultHealthFailureThreshold：连续健康检查失败告警阈值（M1-08，§9.1）。
	defaultHealthFailureThreshold = 3
	// defaultReadRetry：读工具默认重试上限（§5.4：读至多 1 次；写不重试，恒定 0）。
	defaultReadRetry = 1
	// defaultToolBudget：单租户有效工具数预算（M2-03，分析报告 §9.1 第一行）。
	defaultToolBudget = 40
	// defaultContextTokens：工具面 token 占比判定使用的上下文预算（M2-03）。
	defaultContextTokens = 128000
	// defaultToolTokenShare：工具面 token 占比上限（M2-03）。
	defaultToolTokenShare = 0.30
)

var (
	// ErrServerNotFound：服务器未注册到 manager。
	ErrServerNotFound = errors.New("MCP 服务器未注册")
	// ErrServerDisabled：服务器未启用（管理位 false）。
	ErrServerDisabled = errors.New("MCP 服务器未启用")
	// ErrServerUnavailable：服务器无可用会话（未连接/已置 error）。
	ErrServerUnavailable = errors.New("MCP 服务器当前不可用")
	// errDisableGraceTimeout：禁用宽限期内仍有在途调用。
	errDisableGraceTimeout = errors.New("禁用宽限期结束仍有在途调用")
)

// ServerConfig 是 manager 所需的服务器配置。
// 凭据（Headers）必须已由装配层（M0-06 + M0-08）解密注入；manager 不接触密文。
type ServerConfig struct {
	ID               int
	TenantID         int
	Name             string // 稳定标识（投影名主体，^[a-z0-9_-]{1,32}$）
	Transport        transport.Kind
	URL              string
	Headers          map[string]string
	TimeoutMS        int
	MaxParallelCalls int
	MaxRetry         int
	Enabled          bool
	TrustLevel       string
	Version          int
}

// StatusPatch 是运行态回写内容（不含凭据；M0-08 落库到 ent `mcp_servers`）。
type StatusPatch struct {
	Status          ServerStatus
	LastError       string
	ProtocolVersion string
	ServerInfo      string
	ConnectedAt     time.Time
}

// StatusWriter 回写服务器运行态（M0-08 提供 ent 实现）。
type StatusWriter interface {
	UpdateServerStatus(ctx context.Context, serverID int, patch StatusPatch) error
}

// Options 是 manager 选项（零值均有默认）。
type Options struct {
	Guard          transport.Guard // 出站安全（必填才能真实建连；缺省时默认 dial 会 fail-closed）
	Dial           DialFunc        // 建连实现（测试注入；默认 transport + client）
	StatusWriter   StatusWriter    // 状态回写（可为 nil）
	Events         EventSink       // 事件出口（默认丢弃）
	ToolCache      ToolCache       // 工具缓存（默认内存）
	Backoff        BackoffPolicy   // 退避（默认 1s/2x/1min）
	HealthInterval time.Duration   // 健康检查间隔（默认 30s）
	ConnectTimeout time.Duration   // 连接超时（默认 10s）
	CallTimeout    time.Duration   // 调用超时默认值（服务器 TimeoutMS 优先，默认 30s）
	DisableGrace   time.Duration   // 禁用 in-flight 宽限（默认 30s）
	// HealthFailureThreshold：连续健康检查失败告警阈值（默认 3）；<=0 用默认值。
	HealthFailureThreshold int
	// ReadRetry：读工具重试上限（平台级上限，默认 1；负值=禁用重试）。
	// 写工具恒不重试（防重复副作用）；服务器侧 MaxRetry=0 亦可单独关闭该服务器的读重试。
	ReadRetry     int
	Now           func() time.Time
	ClientName    string
	ClientVersion string
	// ToolBudget：单租户有效工具数预算（M2-03，默认 40）；<=0 用默认值。
	ToolBudget int
	// ContextTokens：工具面 token 占比判定使用的上下文预算（默认 128000）；<=0 用默认值。
	ContextTokens int
	// ToolTokenShare：工具面 token 占比上限（0-1，默认 0.30）；<=0 用默认值。
	ToolTokenShare float64
}

// Manager 管理 MCP 服务器连接生命周期（连接池/退避/发现/并发治理）。
type Manager struct {
	opts  Options
	dial  DialFunc
	event EventSink
	cache ToolCache

	mu      sync.Mutex
	servers map[int]*conn

	// budgetMu/budgetLast：工具面预算告警的边沿触发状态（M2-03），与 servers 锁分离。
	budgetMu   sync.Mutex
	budgetLast map[int]budgetState

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	stopped     chan struct{}
}

// New 创建 manager。
func New(opts Options) *Manager {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HealthInterval <= 0 {
		opts.HealthInterval = defaultHealthInterval
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = defaultConnectTimeout
	}
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = defaultCallTimeout
	}
	if opts.DisableGrace <= 0 {
		opts.DisableGrace = defaultDisableGrace
	}
	if opts.HealthFailureThreshold <= 0 {
		opts.HealthFailureThreshold = defaultHealthFailureThreshold
	}
	if opts.ToolBudget <= 0 {
		opts.ToolBudget = defaultToolBudget
	}
	if opts.ContextTokens <= 0 {
		opts.ContextTokens = defaultContextTokens
	}
	if opts.ToolTokenShare <= 0 {
		opts.ToolTokenShare = defaultToolTokenShare
	}
	if opts.ReadRetry < 0 {
		opts.ReadRetry = 0
	}
	if opts.ReadRetry == 0 {
		opts.ReadRetry = defaultReadRetry
	}
	opts.Backoff = opts.Backoff.WithDefaults()
	if opts.Events == nil {
		opts.Events = DiscardEvents()
	}
	if opts.ToolCache == nil {
		opts.ToolCache = NewMemoryToolCache()
	}
	if opts.ClientName == "" {
		opts.ClientName = "itsm-backend"
	}
	if opts.ClientVersion == "" {
		opts.ClientVersion = "dev"
	}

	manager := &Manager{
		opts:    opts,
		dial:    opts.Dial,
		event:   opts.Events,
		cache:   opts.ToolCache,
		servers: map[int]*conn{},
	}
	if manager.dial == nil {
		manager.dial = defaultDial(opts)
	}
	return manager
}

// defaultDial 使用 transport + client 建立真实连接。
func defaultDial(opts Options) DialFunc {
	return func(ctx context.Context, cfg ServerConfig) (ToolSession, error) {
		tr, err := transport.New(ctx, transport.Config{
			Kind:           cfg.Transport,
			URL:            cfg.URL,
			Headers:        cfg.Headers,
			ConnectTimeout: opts.ConnectTimeout,
			Guard:          opts.Guard,
		})
		if err != nil {
			return nil, err
		}
		session, err := client.New(client.Options{
			Name:    opts.ClientName,
			Version: opts.ClientVersion,
			Timeouts: client.Timeouts{
				Connect: opts.ConnectTimeout,
				Call:    callTimeout(cfg, opts.CallTimeout),
			},
		}).Connect(ctx, tr)
		if err != nil {
			return nil, err
		}
		return session, nil
	}
}

// Upsert 注册/更新服务器配置（不建连；启停由 Enable/Disable/Reload 驱动）。
func (m *Manager) Upsert(cfg ServerConfig) *conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.servers[cfg.ID]; ok {
		existing.mu.Lock()
		if cfg.MaxParallelCalls > 0 && cfg.MaxParallelCalls != cap(existing.sem) {
			existing.sem = make(chan struct{}, cfg.MaxParallelCalls)
		}
		existing.cfg = cfg
		existing.mu.Unlock()
		return existing
	}
	created := newConn(cfg, m.dial)
	m.servers[cfg.ID] = created
	return created
}

// Remove 关闭连接并移除条目（删除服务器时调用）。
func (m *Manager) Remove(serverID int) {
	m.mu.Lock()
	target, ok := m.servers[serverID]
	delete(m.servers, serverID)
	m.mu.Unlock()
	if !ok {
		return
	}
	target.closeSession()
	target.markDisabled()
	m.cache.Replace(serverID, nil)
}

// Status 返回单服务器状态快照。
func (m *Manager) Status(serverID int) (StatusSnapshot, error) {
	target, ok := m.lookup(serverID)
	if !ok {
		return StatusSnapshot{}, ErrServerNotFound
	}
	return target.snapshot(), nil
}

// Snapshot 返回全部服务器状态（M0-12 健康摘要）。
func (m *Manager) Snapshot() []StatusSnapshot {
	connections := m.connections()
	snapshots := make([]StatusSnapshot, 0, len(connections))
	for _, c := range connections {
		snapshots = append(snapshots, c.snapshot())
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ServerID < snapshots[j].ServerID })
	return snapshots
}

// Enable 异步启用服务器：立即置 connecting 并返回，后台完成建连与发现（D8）。
func (m *Manager) Enable(ctx context.Context, serverID int) error {
	target, ok := m.lookup(serverID)
	if !ok {
		return ErrServerNotFound
	}
	target.mu.Lock()
	target.cfg.Enabled = true
	target.mu.Unlock()

	if !target.beginAttempt() {
		return nil // 已有建连任务在途（幂等）：不得改写其状态，否则可能卡在 connecting
	}
	target.markConnecting()
	m.notifyStatus(ctx, target)
	go func() {
		defer target.endAttempt()
		_, _ = m.connectAndDiscover(context.Background(), target)
	}()
	return nil
}

// Disable 异步禁用服务器：等待在途调用（宽限期内）后关闭连接（D8）。
func (m *Manager) Disable(ctx context.Context, serverID int) error {
	target, ok := m.lookup(serverID)
	if !ok {
		return ErrServerNotFound
	}
	target.mu.Lock()
	target.cfg.Enabled = false
	target.mu.Unlock()

	go func() {
		graceCtx, cancel := context.WithTimeout(context.Background(), m.opts.DisableGrace)
		defer cancel()
		m.waitInFlightAndClose(target, graceCtx, "服务器已禁用")
		target.markDisabled()
		m.notifyStatus(context.Background(), target)
	}()
	return nil
}

// Retire 停用并**从连接表移除**（删除服务器）：立即拒绝新调用（lookup 失败），
// 在途调用等待 ≤ DisableGrace 后强制断开（宽限超时会发出审计事件），随后清空工具缓存。
func (m *Manager) Retire(ctx context.Context, serverID int) error {
	m.mu.Lock()
	target, ok := m.servers[serverID]
	delete(m.servers, serverID)
	m.mu.Unlock()
	if !ok {
		return ErrServerNotFound
	}
	target.mu.Lock()
	target.cfg.Enabled = false
	target.mu.Unlock()

	go func() {
		graceCtx, cancel := context.WithTimeout(context.Background(), m.opts.DisableGrace)
		defer cancel()
		m.waitInFlightAndClose(target, graceCtx, "服务器已删除")
		target.markDisabled()
		m.cache.Replace(serverID, nil)
	}()
	return nil
}

// waitInFlightAndClose 等待在途调用结束（≤ 宽限期）后关闭会话；超时则强制断开并留下审计信号。
func (m *Manager) waitInFlightAndClose(target *conn, graceCtx context.Context, reason string) {
	err := target.waitIdle(graceCtx)
	if err == nil || errors.Is(err, context.Canceled) {
		target.closeSessionWithEvent(m, reason)
		return
	}
	// 宽限期结束仍有在途调用：记 last_error（管理页可见）+ 事件（审计可查），再强制断开。
	cfg := target.config()
	target.setLastError(errDisableGraceTimeout.Error())
	m.emit(Event{
		Type:     EventServerDisableGraceExpired,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   reason + "：宽限期结束仍有在途调用，已强制断开（" + errDisableGraceTimeout.Error() + "）",
	})
	target.closeSessionWithEvent(m, reason+"（强制断开）")
}

// Reload 异步重载：先断后连（发现随之刷新；工具开关不触发本方法，D6）。
func (m *Manager) Reload(ctx context.Context, serverID int) error {
	target, ok := m.lookup(serverID)
	if !ok {
		return ErrServerNotFound
	}
	target.mu.Lock()
	enabled := target.cfg.Enabled
	target.mu.Unlock()
	if !enabled {
		return ErrServerDisabled
	}
	if !target.beginAttempt() {
		return nil
	}
	target.markConnecting()
	m.notifyStatus(ctx, target)
	go func() {
		defer target.endAttempt()
		target.closeSessionWithEvent(m, "重载：断开旧连接")
		_, _ = m.connectAndDiscover(context.Background(), target)
	}()
	return nil
}

// ConnectNow 同步建连（测试与健康循环使用）；成功返回 nil。
func (m *Manager) ConnectNow(ctx context.Context, serverID int) error {
	target, ok := m.lookup(serverID)
	if !ok {
		return ErrServerNotFound
	}
	target.mu.Lock()
	target.cfg.Enabled = true
	target.mu.Unlock()
	if !target.beginAttempt() {
		return nil
	}
	defer target.endAttempt()
	_, err := m.connectAndDiscover(ctx, target)
	return err
}

// DiscoverNow 同步执行一次工具发现并刷新缓存。
func (m *Manager) DiscoverNow(ctx context.Context, serverID int) (DiscoveryResult, error) {
	target, ok := m.lookup(serverID)
	if !ok {
		return DiscoveryResult{}, ErrServerNotFound
	}
	session := target.currentSession()
	if session == nil {
		return DiscoveryResult{}, ErrServerUnavailable
	}
	return m.discoverWithSession(ctx, target, session)
}

// CallTool 以并发上限与调用超时执行工具（**不重试**）。
//
// 需要读重试策略时用 CallToolWithPolicy（provider 按 read_only 选择）。
func (m *Manager) CallTool(ctx context.Context, serverID int, rawName string, args map[string]any) (*client.CallResult, error) {
	return m.callOnce(ctx, serverID, rawName, args)
}

// CallToolWithPolicy 按读/写策略执行工具：
//   - 读工具：瞬时错误（不可达/超时/传输层）至多重试 min(平台 ReadRetry, 服务器 MaxRetry) 次；
//   - 写工具：**恒不重试**（防重复副作用，§5.4）；
//   - 非瞬时错误（认证失效/协议不匹配/服务端业务错误/SSRF 拦截/调用方取消）一律不重试。
func (m *Manager) CallToolWithPolicy(
	ctx context.Context,
	serverID int,
	rawName string,
	args map[string]any,
	readOnly bool,
) (*client.CallResult, error) {
	attempts := 0
	for {
		result, err := m.callOnce(ctx, serverID, rawName, args)
		if err == nil {
			return result, nil
		}
		if !readOnly || attempts >= m.readRetryBudget(serverID) || !retryableCallError(err) || ctx.Err() != nil {
			return nil, err
		}
		attempts++
	}
}

// readRetryBudget 计算该服务器的实际读重试次数：平台上限与服务器配置取小（服务器未配置则用平台上限）。
func (m *Manager) readRetryBudget(serverID int) int {
	cap := m.opts.ReadRetry
	target, ok := m.lookup(serverID)
	if !ok {
		return 0
	}
	configured := target.maxRetryBudget()
	if configured < cap {
		// 服务器收紧：含 0=显式关闭该服务器读重试（ent 字段 max_retry 默认 1、Min(0)）。
		return configured
	}
	return cap
}

// callOnce 是单次调用：并发额度 + 调用超时 + 错误分层处理。
func (m *Manager) callOnce(ctx context.Context, serverID int, rawName string, args map[string]any) (*client.CallResult, error) {
	target, ok := m.lookup(serverID)
	if !ok {
		return nil, ErrServerNotFound
	}
	target.mu.Lock()
	enabled := target.cfg.Enabled
	timeout := callTimeout(target.cfg, m.opts.CallTimeout)
	target.mu.Unlock()
	if !enabled {
		return nil, ErrServerDisabled
	}

	session := target.currentSession()
	if session == nil {
		return nil, ErrServerUnavailable
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := target.acquire(callCtx); err != nil {
		return nil, err
	}
	defer target.release()

	result, err := session.CallTool(callCtx, rawName, args)
	if err != nil {
		m.handleCallError(ctx, target, err)
		return nil, err
	}
	return result, nil
}

// retryableCallError 判定错误是否可安全重试（仅瞬时类；业务错误重试会重复副作用）。
func retryableCallError(err error) bool {
	switch transport.CodeOf(err) {
	case transport.CodeUnreachable, transport.CodeConnectTimeout, transport.CodeTransportError:
		return true
	}
	// 调用超时：SDK/HTTP 客户端返回的 deadline exceeded（取消不算）。
	return errors.Is(err, context.DeadlineExceeded)
}

// Policy 是服务器的**有效执行策略**回读（M1-08；§5.4 默认表 + 服务器覆盖）。
// 输出上限（256KB）与工具面截断在 provider 层（`provider.DefaultMaxResultBytes`）。
type Policy struct {
	TimeoutMS              int     `json:"timeout_ms"`
	ConnectTimeoutMS       int     `json:"connect_timeout_ms"`
	MaxParallelCalls       int     `json:"max_parallel_calls"`
	MaxRetry               int     `json:"max_retry"`
	ReadRetryCap           int     `json:"read_retry_cap"`
	DisableGraceMS         int     `json:"disable_grace_ms"`
	HealthFailureThreshold int     `json:"health_failure_threshold"`
	HealthIntervalMS       int     `json:"health_interval_ms"`
	ToolBudget             int     `json:"tool_budget"`
	ContextTokens          int     `json:"context_tokens"`
	ToolTokenShare         float64 `json:"tool_token_share"`
}

// Policy 返回有效策略；服务器未注册时返回平台默认值（管理页可照常展示）。
func (m *Manager) Policy(serverID int) Policy {
	policy := Policy{
		TimeoutMS:              int(m.opts.CallTimeout / time.Millisecond),
		ConnectTimeoutMS:       int(m.opts.ConnectTimeout / time.Millisecond),
		MaxParallelCalls:       defaultMaxParallelCalls,
		MaxRetry:               defaultReadRetry,
		ReadRetryCap:           m.opts.ReadRetry,
		DisableGraceMS:         int(m.opts.DisableGrace / time.Millisecond),
		HealthFailureThreshold: m.opts.HealthFailureThreshold,
		HealthIntervalMS:       int(m.opts.HealthInterval / time.Millisecond),
		ToolBudget:             m.opts.ToolBudget,
		ContextTokens:          m.opts.ContextTokens,
		ToolTokenShare:         m.opts.ToolTokenShare,
	}
	target, ok := m.lookup(serverID)
	if !ok {
		return policy
	}
	cfg := target.config()
	if cfg.TimeoutMS > 0 {
		policy.TimeoutMS = cfg.TimeoutMS
	}
	if cfg.MaxParallelCalls > 0 {
		policy.MaxParallelCalls = cfg.MaxParallelCalls
	}
	policy.MaxRetry = m.readRetryBudget(serverID)
	return policy
}

// CachedTools 返回某服务器最近一次发现的缓存记录。
func (m *Manager) CachedTools(serverID int) []ToolRecord { return m.cache.List(serverID) }

// EffectiveTools 返回当前可暴露的工具：服务器 healthy ∧ enabled ∧ 未隔离（D6 派生位）。
// MCP 全挂时返回空集合且不影响调用方（降级语义）。
func (m *Manager) EffectiveTools() []ToolRecord {
	var tools []ToolRecord
	for _, target := range m.connections() {
		status, _, _ := target.state()
		if status != StatusHealthy {
			continue
		}
		for _, record := range m.cache.List(target.cfg.ID) {
			if record.Enabled && !record.Quarantined {
				tools = append(tools, record)
			}
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].CallableName < tools[j].CallableName })
	return tools
}

// Start 启动健康检查协程（幂等）。
func (m *Manager) Start(ctx context.Context) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	stopped := make(chan struct{})
	m.stopped = stopped

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(m.opts.HealthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				m.HealthTick(runCtx)
			}
		}
	}()
}

// Stop 停止健康检查协程（幂等；不等待在途异步任务）。
func (m *Manager) Stop() {
	m.lifecycleMu.Lock()
	cancel, stopped := m.cancel, m.stopped
	m.cancel, m.stopped = nil, nil
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-stopped
}

// —— 内部 ——

func (m *Manager) connections() []*conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	connections := make([]*conn, 0, len(m.servers))
	for _, target := range m.servers {
		connections = append(connections, target)
	}
	return connections
}

func (m *Manager) lookup(serverID int) (*conn, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.servers[serverID]
	return target, ok
}

func (m *Manager) now() time.Time { return m.opts.Now() }

func (m *Manager) emit(event Event) {
	if event.At.IsZero() {
		event.At = m.now()
	}
	m.event.EmitMCPEvent(event)
}

func (m *Manager) notifyStatus(ctx context.Context, target *conn) {
	if m.opts.StatusWriter == nil {
		return
	}
	snapshot := target.snapshot()
	_ = m.opts.StatusWriter.UpdateServerStatus(ctx, snapshot.ServerID, StatusPatch{
		Status:          snapshot.Status,
		LastError:       snapshot.LastError,
		ProtocolVersion: snapshot.ProtocolVersion,
		ServerInfo:      snapshot.ServerInfo,
		ConnectedAt:     snapshot.ConnectedAt,
	})
}

// connectAndDiscover 建连 + 握手 +（成功后）发现。
func (m *Manager) connectAndDiscover(ctx context.Context, target *conn) (DiscoveryResult, error) {
	target.markConnecting()
	m.notifyStatus(ctx, target)

	cfg := target.config()
	handshakeStarted := time.Now()
	session, err := target.dial(ctx, cfg)
	if err != nil {
		metrics.MCPServerConnected.WithLabelValues(cfg.Name).Set(0)
		target.markFailure(err, m.opts.Backoff, m.now())
		m.emitDialFailure(target, err)
		m.emitHealthAlertIfNeeded(target, err)
		m.notifyStatus(ctx, target)
		return DiscoveryResult{}, err
	}

	target.setSession(session, session.ProtocolVersion(), serverInfoJSON(session), m.now())
	// M2-06：握手耗时与连接态（成功路径；失败路径只置 0，不污染耗时分布）。
	metrics.MCPHandshakeDuration.WithLabelValues(cfg.Name).Observe(time.Since(handshakeStarted).Seconds())
	metrics.MCPServerConnected.WithLabelValues(cfg.Name).Set(1)
	m.emit(Event{
		Type:     EventServerConnected,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   fmt.Sprintf("协议版本=%s 服务器=%s", session.ProtocolVersion(), session.ServerName()),
	})
	m.notifyStatus(ctx, target)

	result, discoverErr := m.discoverWithSession(ctx, target, session)
	if discoverErr != nil {
		target.mu.Lock()
		target.lastError = "工具发现失败：" + discoverErr.Error()
		target.mu.Unlock()
		m.notifyStatus(ctx, target)
	}
	return result, nil
}

func (m *Manager) emitDialFailure(target *conn, err error) {
	cfg := target.config()
	eventType := EventServerReloadFailed
	if transport.CodeOf(err) == transport.CodeAuthRequired {
		eventType = EventServerAuthRequired
	}
	m.emit(Event{
		Type:     eventType,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   "建连失败：" + summarize(err),
	})
}

// handleCallError 依据分层错误码更新运行位（仅认证类会立即降级）。
func (m *Manager) handleCallError(ctx context.Context, target *conn, err error) {
	if transport.CodeOf(err) != transport.CodeAuthRequired {
		return
	}
	cfg := target.config()
	target.closeSessionWithEvent(m, "调用被拒（认证失效）")
	target.markFailure(err, m.opts.Backoff, m.now())
	m.emit(Event{
		Type:     EventServerAuthRequired,
		TenantID: cfg.TenantID,
		ServerID: cfg.ID,
		Server:   cfg.Name,
		Detail:   "调用被拒（认证失效）：" + summarize(err),
	})
	m.notifyStatus(ctx, target)
}

// discoverWithSession 拉取工具列表 → 差分 → 刷新缓存 → 事件。
func (m *Manager) discoverWithSession(ctx context.Context, target *conn, session ToolSession) (DiscoveryResult, error) {
	tools, err := session.ListTools(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}
	discovered := make([]DiscoveredTool, 0, len(tools))
	for _, tool := range tools {
		discovered = append(discovered, DiscoveredTool{
			RawName:     tool.RawName,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	target.mu.Lock()
	serverName, tenantID, serverID := target.cfg.Name, target.cfg.TenantID, target.cfg.ID
	target.mu.Unlock()

	result := PlanDiscovery(serverID, tenantID, serverName, m.cache.List(serverID), discovered, m.now())
	m.cache.Replace(serverID, result.Records)
	m.observeToolStates(serverName, result.Records)

	target.mu.Lock()
	target.discovered = append([]ToolRecord(nil), result.Records...)
	target.mu.Unlock()

	m.emit(Event{
		Type:     EventToolsDiscovered,
		TenantID: tenantID,
		ServerID: serverID,
		Server:   serverName,
		Detail: fmt.Sprintf("added=%d updated=%d removed=%d quarantined=%d unchanged=%d",
			len(result.Delta.Added), len(result.Delta.Updated), len(result.Delta.Removed), len(result.Delta.Quarantined), result.Delta.Unchanged),
	})
	for _, record := range result.Records {
		if record.QuarantineReason == QuarantineReasonSchemaChanged {
			m.emit(Event{
				Type:     EventToolQuarantined,
				TenantID: tenantID,
				ServerID: serverID,
				Server:   serverName,
				Tool:     record.RawName,
				Detail:   "schema_hash 变更，隔离待复核",
			})
		}
	}
	// M2-03：一次发现刷新后判定租户工具面预算（边沿触发，超限时发 mcp.tools.budget_exceeded）。
	m.EvaluateToolBudget(tenantID)
	return result, nil
}

// observeToolStates 写出工具治理状态计数指标（M2-06）。
func (m *Manager) observeToolStates(serverName string, records []ToolRecord) {
	enabled, disabled, quarantined := 0, 0, 0
	for _, record := range records {
		switch {
		case record.Quarantined:
			quarantined++
		case record.Enabled:
			enabled++
		default:
			disabled++
		}
	}
	metrics.MCPTools.WithLabelValues(serverName, "enabled").Set(float64(enabled))
	metrics.MCPTools.WithLabelValues(serverName, "disabled").Set(float64(disabled))
	metrics.MCPTools.WithLabelValues(serverName, "quarantined").Set(float64(quarantined))
}

func serverInfoJSON(session ToolSession) string {
	payload, err := json.Marshal(map[string]string{
		"name":    session.ServerName(),
		"version": session.ServerVersion(),
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

func callTimeout(cfg ServerConfig, fallback time.Duration) time.Duration {
	if cfg.TimeoutMS > 0 {
		return time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	return fallback
}

// summarize 压缩错误文本（控制事件长度；不得含凭据——底层错误已脱敏）。
func summarize(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}
