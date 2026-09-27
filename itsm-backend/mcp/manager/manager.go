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
)

const (
	defaultMaxParallelCalls = 4
	defaultHealthInterval   = 30 * time.Second
	defaultConnectTimeout   = 10 * time.Second
	defaultCallTimeout      = 30 * time.Second
	defaultDisableGrace     = 30 * time.Second
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
	Now            func() time.Time
	ClientName     string
	ClientVersion  string
}

// Manager 管理 MCP 服务器连接生命周期（连接池/退避/发现/并发治理）。
type Manager struct {
	opts  Options
	dial  DialFunc
	event EventSink
	cache ToolCache

	mu      sync.Mutex
	servers map[int]*conn

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
		if err := target.waitIdle(graceCtx); err != nil && !errors.Is(err, context.Canceled) {
			target.mu.Lock()
			target.lastError = errDisableGraceTimeout.Error()
			target.mu.Unlock()
		}
		target.closeSessionWithEvent(m, "服务器已禁用")
		target.markDisabled()
		m.notifyStatus(context.Background(), target)
	}()
	return nil
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

// CallTool 以并发上限与调用超时执行工具（不自动重试；写工具重试策略属 M1）。
func (m *Manager) CallTool(ctx context.Context, serverID int, rawName string, args map[string]any) (*client.CallResult, error) {
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
	session, err := target.dial(ctx, cfg)
	if err != nil {
		target.markFailure(err, m.opts.Backoff, m.now())
		m.emitDialFailure(target, err)
		m.notifyStatus(ctx, target)
		return DiscoveryResult{}, err
	}

	target.setSession(session, session.ProtocolVersion(), serverInfoJSON(session), m.now())
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
	return result, nil
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
