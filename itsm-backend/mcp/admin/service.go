package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mcpserver"
	"itsm-backend/ent/mcpservertool"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"
)

// 管理服务（M0-08）：CRUD / 测试连接 / 异步启停 / 工具治理 / 健康与事件 / 审计。
//
// 设计约束：
//   - 凭据只写不读回：读接口一律 `Masked` 投影（M0-06）；
//   - 写路径先落库再异步重连（§5.10）；工具开关只翻转治理位、**不触发重连**（D6）；
//   - 审计字段 actor/tenant/action/object/before-after(脱敏)/result/ip/ts（§5.5）。

// Actor 是管理操作发起者（由 handler 从身份上下文提取）。
type Actor struct {
	TenantID int
	UserID   int
	IP       string
}

// Config 是 Service 依赖（除 Client 外均可选，缺省有安全默认）。
type Config struct {
	Client      *ent.Client
	Credentials *CredentialService
	Manager     *manager.Manager
	Guard       transport.Guard
	Store       *EntStore
	Audit       AuditSink
	Events      *EventBuffer
	Now         func() time.Time
}

// Service 是 MCP 管理服务。
type Service struct {
	client      *ent.Client
	credentials *CredentialService
	manager     *manager.Manager
	guard       transport.Guard
	store       *EntStore
	audit       AuditSink
	events      *EventBuffer
	now         func() time.Time
}

// NewService 构造管理服务。
func NewService(cfg Config) (*Service, error) {
	if cfg.Client == nil {
		return nil, errors.New("MCP 管理服务需要 ent client")
	}
	if cfg.Credentials == nil {
		return nil, errors.New("MCP 管理服务需要凭据服务（M0-06）")
	}
	if cfg.Manager == nil {
		return nil, errors.New("MCP 管理服务需要 manager（M0-07）")
	}
	store := cfg.Store
	if store == nil {
		created, err := NewEntStore(cfg.Client)
		if err != nil {
			return nil, err
		}
		store = created
	}
	audit := cfg.Audit
	if audit == nil {
		audit = DiscardAudit()
	}
	events := cfg.Events
	if events == nil {
		events = NewEventBuffer(0)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		client:      cfg.Client,
		credentials: cfg.Credentials,
		manager:     cfg.Manager,
		guard:       cfg.Guard,
		store:       store,
		audit:       audit,
		events:      events,
		now:         now,
	}, nil
}

// Store 返回 ent store（供 bootstrap 装配 manager 的 StatusWriter/ToolCache）。
func (s *Service) Store() *EntStore { return s.store }

// Events 返回事件缓冲（供 bootstrap 装配 manager.Options.Events）。
func (s *Service) Events() *EventBuffer { return s.events }

// Manager 返回连接管理器（bootstrap 用于注册 provider 与生命周期控制）。
func (s *Service) Manager() *manager.Manager { return s.manager }

// Startup 在进程启动时拉起已启用服务器（幂等；不修改治理位）：
// 逐条装配运行态配置（含解密后的请求头，仅存在于内存）并异步建连 + 工具发现。
// 单条失败只记录并继续（运行态由 manager 退避与状态回写自愈），不阻断启动。
func (s *Service) Startup(ctx context.Context) error {
	servers, err := s.client.MCPServer.Query().Where(mcpserver.EnabledEQ(true)).All(ctx)
	if err != nil {
		return WrapAdminError(http.StatusInternalServerError, CodeInternal, "加载已启用 MCP 服务器失败", err)
	}
	for _, entity := range servers {
		s.upsertManager(entity)
		if enableErr := s.manager.Enable(ctx, entity.ID); enableErr != nil {
			s.recordAudit(ctx, Actor{TenantID: entity.TenantID, IP: "startup"}, "startup_enable_failed",
				"mcp_server", entity.Name, nil, map[string]any{"error": shortError(enableErr)}, enableErr)
		}
	}
	return nil
}

// —— DTO ——

// ServerView 是管理面服务器视图（凭据字段恒为掩码）。
type ServerView struct {
	ID                 int               `json:"id"`
	Name               string            `json:"name"`
	DisplayName        string            `json:"display_name"`
	Transport          string            `json:"transport"`
	URL                string            `json:"url"`
	CredentialType     string            `json:"credential_type"`
	TrustLevel         string            `json:"trust_level"`
	Enabled            bool              `json:"enabled"`
	Status             string            `json:"status"`
	RunningStatus      string            `json:"running_status"`
	LastError          string            `json:"last_error"`
	ProtocolVersion    string            `json:"protocol_version"`
	ServerInfo         string            `json:"server_info"`
	TimeoutMS          int               `json:"timeout_ms"`
	MaxParallelCalls   int               `json:"max_parallel_calls"`
	MaxRetry           int               `json:"max_retry"`
	Version            int               `json:"version"`
	ToolCount          int               `json:"tool_count"`
	EnabledToolCount   int               `json:"enabled_tool_count"`
	QuarantinedToolNum int               `json:"quarantined_tool_count"`
	HeadersMasked      map[string]string `json:"headers_masked"`
	CredentialMasked   map[string]string `json:"credential_masked"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// ServerSummary 是列表/健康摘要计数。
type ServerSummary struct {
	Total            int `json:"total"`
	Enabled          int `json:"enabled"`
	Connected        int `json:"connected"`
	Error            int `json:"error"`
	AuthRequired     int `json:"auth_required"`
	Tools            int `json:"tools"`
	EnabledTools     int `json:"enabled_tools"`
	QuarantinedTools int `json:"quarantined_tools"`
}

// ServerListResult 是 GET / 的响应体。
type ServerListResult struct {
	Items   []ServerView  `json:"items"`
	Summary ServerSummary `json:"summary"`
}

// ToolView 是工具治理视图（三态分离：enabled/healthy/configured）。
type ToolView struct {
	ID                 int       `json:"id"`
	RawName            string    `json:"raw_name"`
	CallableName       string    `json:"callable_name"`
	Description        string    `json:"description"`
	InputSchema        string    `json:"input_schema"`
	SchemaHash         string    `json:"schema_hash"`
	ReadOnly           bool      `json:"read_only"`
	Risk               string    `json:"risk"`
	Category           string    `json:"category"`
	Enabled            bool      `json:"enabled"`
	Healthy            bool      `json:"healthy"`
	ConfiguredEnabled  bool      `json:"configured_enabled"`
	Quarantined        bool      `json:"quarantined"`
	QuarantineReason   string    `json:"quarantine_reason"`
	LastError          string    `json:"last_error"`
	DiscoveredAt       time.Time `json:"discovered_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	ServerRunningState string    `json:"server_running_state"`
}

// ToolPreview 是测试连接的轻量工具预览。
type ToolPreview struct {
	RawName      string `json:"raw_name"`
	CallableName string `json:"callable_name"`
	Description  string `json:"description"`
}

// ConnectionTestResult 是测试连接结果（同步 ≤10s；不落库）。
type ConnectionTestResult struct {
	OK              bool          `json:"ok"`
	ProtocolVersion string        `json:"protocol_version"`
	ServerName      string        `json:"server_name"`
	ServerVersion   string        `json:"server_version"`
	Tools           []ToolPreview `json:"tools"`
	ToolCount       int           `json:"tool_count"`
	DurationMS      int64         `json:"duration_ms"`
	ErrorCode       string        `json:"error_code,omitempty"`
	Message         string        `json:"message,omitempty"`
}

// CreateServerRequest 是新增请求（凭据只写不读回）。
type CreateServerRequest struct {
	Name             string            `json:"name"`
	DisplayName      string            `json:"display_name"`
	Transport        string            `json:"transport"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers"`
	CredentialType   string            `json:"credential_type"`
	Credential       map[string]string `json:"credential"`
	TimeoutMS        int               `json:"timeout_ms"`
	MaxParallelCalls int               `json:"max_parallel_calls"`
	MaxRetry         int               `json:"max_retry"`
	TrustLevel       string            `json:"trust_level"`
}

// UpdateServerRequest 是编辑请求（version 必填；空凭据 = 不修改）。
type UpdateServerRequest struct {
	Version          int               `json:"version"`
	DisplayName      *string           `json:"display_name"`
	URL              *string           `json:"url"`
	Transport        *string           `json:"transport"`
	CredentialType   *string           `json:"credential_type"`
	Headers          map[string]string `json:"headers"`
	Credential       map[string]string `json:"credential"`
	TimeoutMS        *int              `json:"timeout_ms"`
	MaxParallelCalls *int              `json:"max_parallel_calls"`
	MaxRetry         *int              `json:"max_retry"`
	TrustLevel       *string           `json:"trust_level"`
}

// TestServerRequest 是测试连接的覆盖项（不落库；字段为空则用已存配置）。
type TestServerRequest struct {
	Transport  string            `json:"transport"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
	Credential map[string]string `json:"credential"`
}

// ClassificationRequest 是工具标注请求。
type ClassificationRequest struct {
	ReadOnly *bool  `json:"read_only"`
	Risk     string `json:"risk"`
	Category string `json:"category"`
}

// BulkToolRequest 是批量治理请求（tools 为空 = 全部）。
type BulkToolRequest struct {
	Tools   []string `json:"tools"`
	Enabled bool     `json:"enabled"`
}

// RotateCredentialRequest 是凭据轮换请求。
type RotateCredentialRequest struct {
	CredentialType string            `json:"credential_type"`
	Credential     map[string]string `json:"credential"`
	Headers        map[string]string `json:"headers"`
}

// —— 读 ——

// ListServers 返回租户服务器列表 + 摘要。
func (s *Service) ListServers(ctx context.Context, actor Actor) (ServerListResult, error) {
	rows, err := s.client.MCPServer.Query().
		Where(mcpserver.TenantIDEQ(actor.TenantID)).
		Order(ent.Asc(mcpserver.FieldName)).
		All(ctx)
	if err != nil {
		return ServerListResult{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询服务器失败", err)
	}
	result := ServerListResult{Items: make([]ServerView, 0, len(rows))}
	for _, row := range rows {
		view, err := s.serverView(ctx, actor.TenantID, row)
		if err != nil {
			return ServerListResult{}, err
		}
		result.Items = append(result.Items, view)
		if view.Enabled {
			result.Summary.Enabled++
		}
		switch manager.ServerStatus(view.RunningStatus) {
		case manager.StatusHealthy:
			result.Summary.Connected++
		case manager.StatusError:
			result.Summary.Error++
		}
		result.Summary.Tools += view.ToolCount
		result.Summary.EnabledTools += view.EnabledToolCount
		result.Summary.QuarantinedTools += view.QuarantinedToolNum
	}
	result.Summary.Total = len(result.Items)
	return result, nil
}

// GetServer 返回单个服务器详情。
func (s *Service) GetServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	return s.serverView(ctx, actor.TenantID, entity)
}

// —— 写（CRUD） ——

// CreateServer 新建服务器（默认 disabled；工具默认待治理）。
func (s *Service) CreateServer(ctx context.Context, actor Actor, req CreateServerRequest) (ServerView, error) {
	kind := transport.Kind(strings.TrimSpace(req.Transport))
	if err := ValidateServerName(req.Name); err != nil {
		return ServerView{}, err
	}
	if err := ValidateDisplayName(req.DisplayName); err != nil {
		return ServerView{}, err
	}
	if err := ValidateTransport(kind); err != nil {
		return ServerView{}, err
	}
	if err := ValidateURL(req.URL); err != nil {
		return ServerView{}, err
	}
	if err := ValidateTimeout(req.TimeoutMS); err != nil {
		return ServerView{}, err
	}
	if err := ValidateParallelCalls(req.MaxParallelCalls); err != nil {
		return ServerView{}, err
	}
	if err := ValidateRetry(req.MaxRetry); err != nil {
		return ServerView{}, err
	}
	credentialType, err := normalizeCredentialType(req.CredentialType)
	if err != nil {
		return ServerView{}, err
	}
	trustLevel, err := normalizeTrustLevel(req.TrustLevel)
	if err != nil {
		return ServerView{}, err
	}
	headers, err := NormalizeSecrets(req.Headers)
	if err != nil {
		return ServerView{}, err
	}
	credential, err := NormalizeSecrets(req.Credential)
	if err != nil {
		return ServerView{}, err
	}
	headersCipher, err := s.credentials.Encrypt(NewSecretValues(headers))
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}
	credentialCipher, err := s.credentials.Encrypt(NewSecretValues(credential))
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}

	duplicated, err := s.client.MCPServer.Query().
		Where(mcpserver.TenantIDEQ(actor.TenantID), mcpserver.NameEQ(strings.TrimSpace(req.Name))).
		Exist(ctx)
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查重失败", err)
	}
	if duplicated {
		return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "name 已存在")
	}

	create := s.client.MCPServer.Create().
		SetTenantID(actor.TenantID).
		SetName(strings.TrimSpace(req.Name)).
		SetDisplayName(req.DisplayName).
		SetTransport(string(kind)).
		SetURL(strings.TrimSpace(req.URL)).
		SetHeadersEncrypted(headersCipher).
		SetCredentialType(credentialType).
		SetCredentialEncrypted(credentialCipher).
		SetTrustLevel(trustLevel).
		SetEnabled(false).
		SetStatus(string(manager.StatusConfigured))
	if req.TimeoutMS > 0 {
		create = create.SetTimeoutMs(req.TimeoutMS)
	}
	if req.MaxParallelCalls > 0 {
		create = create.SetMaxParallelCalls(req.MaxParallelCalls)
	}
	if req.MaxRetry > 0 {
		create = create.SetMaxRetry(req.MaxRetry)
	}
	entity, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "name 已存在")
		}
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "创建服务器失败", err)
	}

	s.recordAudit(ctx, actor, "create_server", "mcp_server", entity.Name, nil,
		s.serverAuditSnapshot(entity, NewSecretValues(headers).Masked(), NewSecretValues(credential).Masked()), nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// UpdateServer 编辑服务器（乐观锁；凭据空值 = 不修改；写后异步重连）。
func (s *Service) UpdateServer(ctx context.Context, actor Actor, serverID int, req UpdateServerRequest) (ServerView, error) {
	if req.Version <= 0 {
		return ServerView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "version 必填（乐观锁）")
	}
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	beforeHeaders, beforeCredential := s.maskedSecrets(entity)

	update := s.client.MCPServer.UpdateOneID(entity.ID).
		Where(mcpserver.VersionEQ(req.Version)).
		AddVersion(1)

	if req.DisplayName != nil {
		if err := ValidateDisplayName(*req.DisplayName); err != nil {
			return ServerView{}, err
		}
		update = update.SetDisplayName(*req.DisplayName)
	}
	if req.Transport != nil {
		kind := transport.Kind(strings.TrimSpace(*req.Transport))
		if err := ValidateTransport(kind); err != nil {
			return ServerView{}, err
		}
		update = update.SetTransport(string(kind))
	}
	if req.URL != nil {
		if err := ValidateURL(*req.URL); err != nil {
			return ServerView{}, err
		}
		update = update.SetURL(strings.TrimSpace(*req.URL))
	}
	if req.TimeoutMS != nil {
		if err := ValidateTimeout(*req.TimeoutMS); err != nil {
			return ServerView{}, err
		}
		update = update.SetTimeoutMs(*req.TimeoutMS)
	}
	if req.MaxParallelCalls != nil {
		if err := ValidateParallelCalls(*req.MaxParallelCalls); err != nil {
			return ServerView{}, err
		}
		update = update.SetMaxParallelCalls(*req.MaxParallelCalls)
	}
	if req.MaxRetry != nil {
		if err := ValidateRetry(*req.MaxRetry); err != nil {
			return ServerView{}, err
		}
		update = update.SetMaxRetry(*req.MaxRetry)
	}
	if req.TrustLevel != nil {
		trustLevel, err := normalizeTrustLevel(*req.TrustLevel)
		if err != nil {
			return ServerView{}, err
		}
		update = update.SetTrustLevel(trustLevel)
	}
	if req.CredentialType != nil {
		normalized, err := normalizeCredentialType(*req.CredentialType)
		if err != nil {
			return ServerView{}, err
		}
		update = update.SetCredentialType(normalized)
	}
	if len(req.Headers) > 0 {
		normalized, err := NormalizeSecrets(req.Headers)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.HeadersEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.HeadersEncrypted {
			update = update.SetHeadersEncrypted(mergedCipher)
		}
	}
	if len(req.Credential) > 0 {
		normalized, err := NormalizeSecrets(req.Credential)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.CredentialEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.CredentialEncrypted {
			update = update.SetCredentialEncrypted(mergedCipher)
		}
	}

	if _, err := update.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeConflict, "版本冲突，请刷新后重试")
		}
		if ent.IsConstraintError(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "更新违反唯一约束")
		}
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新服务器失败", err)
	}

	updated, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	afterHeaders, afterCredential := s.maskedSecrets(updated)
	s.recordAudit(ctx, actor, "update_server", "mcp_server", updated.Name,
		s.serverAuditSnapshot(entity, beforeHeaders, beforeCredential),
		s.serverAuditSnapshot(updated, afterHeaders, afterCredential), nil)

	// 先落库再异步重连（§5.10）：启用中的服务器热重载新配置。
	if updated.Enabled {
		s.upsertManager(updated)
		_ = s.manager.Reload(ctx, updated.ID)
	}
	return s.serverView(ctx, actor.TenantID, updated)
}

// DeleteServer 删除服务器（断开连接 + 级联工具缓存）。
func (s *Service) DeleteServer(ctx context.Context, actor Actor, serverID int) error {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return err
	}
	headers, credential := s.maskedSecrets(entity)
	s.manager.Remove(entity.ID)
	if err := s.store.DeleteServerTools(ctx, actor.TenantID, entity.ID); err != nil {
		return WrapAdminError(http.StatusInternalServerError, CodeInternal, "清理工具缓存失败", err)
	}
	if err := s.client.MCPServer.DeleteOneID(entity.ID).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return NewAdminError(http.StatusNotFound, CodeNotFound, "服务器不存在")
		}
		return WrapAdminError(http.StatusInternalServerError, CodeInternal, "删除服务器失败", err)
	}
	s.recordAudit(ctx, actor, "delete_server", "mcp_server", entity.Name,
		s.serverAuditSnapshot(entity, headers, credential), nil, nil)
	return nil
}

// —— 测试连接（同步 ≤10s；不落库） ——

// testServerID 是测试连接使用的临时槽位（负值不与真实服务器冲突）。
const testServerID = -1

// TestServer 对已存服务器（可带覆盖项）做一次同步连接测试。
//
// 语义：只做一次短握手 + 工具预览，**不落库、不改运行态**；连接失败按 §5.5 返回映射错误
// （ssrf_blocked→422 / connect_timeout|tls_error|auth_required|protocol_mismatch→502 等）。
func (s *Service) TestServer(ctx context.Context, actor Actor, serverID int, req TestServerRequest) (ConnectionTestResult, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	cfg, err := s.buildServerConfig(entity)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	if strings.TrimSpace(req.Transport) != "" {
		kind := transport.Kind(strings.TrimSpace(req.Transport))
		if err := ValidateTransport(kind); err != nil {
			return ConnectionTestResult{}, err
		}
		cfg.Transport = kind
	}
	if strings.TrimSpace(req.URL) != "" {
		if err := ValidateURL(req.URL); err != nil {
			return ConnectionTestResult{}, err
		}
		cfg.URL = strings.TrimSpace(req.URL)
	}
	if len(req.Headers) > 0 {
		normalized, err := NormalizeSecrets(req.Headers)
		if err != nil {
			return ConnectionTestResult{}, err
		}
		for key, value := range normalized {
			cfg.Headers[key] = value
		}
	}
	if len(req.Credential) > 0 {
		normalized, err := NormalizeSecrets(req.Credential)
		if err != nil {
			return ConnectionTestResult{}, err
		}
		for key, value := range normalized {
			cfg.Headers[key] = value
		}
	}
	cfg.ID = testServerID
	cfg.Enabled = true

	tester := manager.New(manager.Options{
		Guard:          s.guard,
		ConnectTimeout: 10 * time.Second,
		CallTimeout:    10 * time.Second,
		HealthInterval: time.Hour,
		Now:            s.now,
	})
	defer tester.Stop()

	tester.Upsert(cfg)
	started := s.now()
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := tester.ConnectNow(testCtx, testServerID); err != nil {
		return ConnectionTestResult{}, MapTransportError(err)
	}
	discovery, discoverErr := tester.DiscoverNow(testCtx, testServerID)
	snapshot, _ := tester.Status(testServerID)
	result := ConnectionTestResult{
		OK:              true,
		ProtocolVersion: snapshot.ProtocolVersion,
		DurationMS:      s.now().Sub(started).Milliseconds(),
	}
	var info struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(snapshot.ServerInfo), &info); err == nil {
		result.ServerName = info.Name
		result.ServerVersion = info.Version
	}
	if discoverErr == nil {
		for _, record := range discovery.Records {
			result.Tools = append(result.Tools, ToolPreview{
				RawName:      record.RawName,
				CallableName: record.CallableName,
				Description:  record.Description,
			})
		}
		result.ToolCount = len(result.Tools)
	}
	tester.Remove(testServerID)
	return result, nil
}

// —— 启停 / 重载（异步：202 + 状态回读，D8） ——

// EnableServer 异步启用：落库 enabled=true，提交 manager 建连（返回时状态为 connecting）。
func (s *Service) EnableServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if !entity.Enabled {
		if _, err := s.client.MCPServer.UpdateOneID(entity.ID).SetEnabled(true).Save(ctx); err != nil {
			return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "启用失败", err)
		}
		entity.Enabled = true
	}
	s.upsertManager(entity)
	if err := s.manager.Enable(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "enable_server", "mcp_server", entity.Name, map[string]any{"enabled": false}, map[string]any{"enabled": true}, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// DisableServer 异步禁用：落库 enabled=false，后台宽限关闭连接。
func (s *Service) DisableServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if entity.Enabled {
		if _, err := s.client.MCPServer.UpdateOneID(entity.ID).SetEnabled(false).Save(ctx); err != nil {
			return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "禁用失败", err)
		}
		entity.Enabled = false
	}
	if err := s.manager.Disable(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "disable_server", "mcp_server", entity.Name, map[string]any{"enabled": true}, map[string]any{"enabled": false}, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// ReloadServer 异步重连 + 重新发现（服务器必须已启用）。
func (s *Service) ReloadServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if !entity.Enabled {
		return ServerView{}, NewAdminError(http.StatusConflict, CodeServerDisabled, "服务器未启用，无法重载")
	}
	s.upsertManager(entity)
	if err := s.manager.Reload(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "reload_server", "mcp_server", entity.Name, nil, nil, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// —— 工具治理（不触发重连，D6） ——

// ListTools 返回服务器全量工具（含禁用/隔离）。
func (s *Service) ListTools(ctx context.Context, actor Actor, serverID int) ([]ToolView, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return nil, err
	}
	rows, err := s.client.MCPServerTool.Query().
		Where(mcpservertool.TenantIDEQ(actor.TenantID), mcpservertool.ServerIDEQ(serverID)).
		Order(ent.Asc(mcpservertool.FieldRawName)).
		All(ctx)
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询工具失败", err)
	}
	runningState := s.runningState(serverID)
	views := make([]ToolView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toolView(row, runningState))
	}
	return views, nil
}

// SetToolEnabled 单工具启停（只翻转治理位，不重连）。
func (s *Service) SetToolEnabled(ctx context.Context, actor Actor, serverID int, callableName string, enabled bool) (ToolView, error) {
	row, err := s.toolEntity(ctx, actor.TenantID, serverID, callableName)
	if err != nil {
		return ToolView{}, err
	}
	updated, err := s.client.MCPServerTool.UpdateOneID(row.ID).SetEnabled(enabled).Save(ctx)
	if err != nil {
		return ToolView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新工具状态失败", err)
	}
	s.recordAudit(ctx, actor, "set_tool_enabled", "mcp_tool", updated.CallableName,
		map[string]any{"enabled": row.Enabled, "server": serverID}, map[string]any{"enabled": enabled, "server": serverID}, nil)
	return toolView(updated, s.runningState(serverID)), nil
}

// BulkSetTools 批量启停（tools 为空 = 全部工具）。
func (s *Service) BulkSetTools(ctx context.Context, actor Actor, serverID int, req BulkToolRequest) (int, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return 0, err
	}
	update := s.client.MCPServerTool.Update().
		Where(mcpservertool.TenantIDEQ(actor.TenantID), mcpservertool.ServerIDEQ(serverID))
	if len(req.Tools) > 0 {
		update = update.Where(mcpservertool.CallableNameIn(req.Tools...))
	}
	affected, err := update.SetEnabled(req.Enabled).Save(ctx)
	if err != nil {
		return 0, WrapAdminError(http.StatusInternalServerError, CodeInternal, "批量更新失败", err)
	}
	s.recordAudit(ctx, actor, "bulk_set_tools", "mcp_tool", "server:"+strconv.Itoa(serverID),
		map[string]any{"tools": len(req.Tools)}, map[string]any{"enabled": req.Enabled, "affected": affected}, nil)
	return affected, nil
}

// SetToolClassification 标注 read_only/risk/category（影响审批，必审计）。
func (s *Service) SetToolClassification(ctx context.Context, actor Actor, serverID int, callableName string, req ClassificationRequest) (ToolView, error) {
	row, err := s.toolEntity(ctx, actor.TenantID, serverID, callableName)
	if err != nil {
		return ToolView{}, err
	}
	update := s.client.MCPServerTool.UpdateOneID(row.ID)
	before := map[string]any{"read_only": row.ReadOnly, "risk": row.Risk, "category": row.Category}
	after := map[string]any{"read_only": row.ReadOnly, "risk": row.Risk, "category": row.Category}

	if req.ReadOnly != nil {
		update = update.SetReadOnly(*req.ReadOnly)
		after["read_only"] = *req.ReadOnly
	}
	if strings.TrimSpace(req.Risk) != "" {
		risk := strings.TrimSpace(req.Risk)
		if !validRisk(risk) {
			return ToolView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "risk 仅允许 read|plan|act_low|act_medium|act_high")
		}
		update = update.SetRisk(risk)
		after["risk"] = risk
	}
	if strings.TrimSpace(req.Category) != "" {
		category := strings.TrimSpace(req.Category)
		if len(category) > 32 {
			return ToolView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "category 超长（≤32）")
		}
		update = update.SetCategory(category)
		after["category"] = category
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return ToolView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新工具标注失败", err)
	}
	s.recordAudit(ctx, actor, "set_tool_classification", "mcp_tool", updated.CallableName, before, after, nil)
	return toolView(updated, s.runningState(serverID)), nil
}

// RotateCredential 轮换凭据/请求头（先落库再异步重连；旧值不再引用）。
func (s *Service) RotateCredential(ctx context.Context, actor Actor, serverID int, req RotateCredentialRequest) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	credentialType := entity.CredentialType
	if strings.TrimSpace(req.CredentialType) != "" {
		normalized, err := normalizeCredentialType(req.CredentialType)
		if err != nil {
			return ServerView{}, err
		}
		credentialType = normalized
	}
	update := s.client.MCPServer.UpdateOneID(entity.ID).AddVersion(1).SetCredentialType(credentialType)
	if len(req.Headers) > 0 {
		normalized, err := NormalizeSecrets(req.Headers)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.HeadersEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.HeadersEncrypted {
			update = update.SetHeadersEncrypted(mergedCipher)
		}
	}
	if len(req.Credential) > 0 {
		normalized, err := NormalizeSecrets(req.Credential)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.CredentialEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.CredentialEncrypted {
			update = update.SetCredentialEncrypted(mergedCipher)
		}
	}
	if _, err := update.Save(ctx); err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "轮换凭据失败", err)
	}
	updated, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	afterHeaders, afterCredential := s.maskedSecrets(updated)
	s.recordAudit(ctx, actor, "rotate_credential", "mcp_server", updated.Name, nil,
		s.serverAuditSnapshot(updated, afterHeaders, afterCredential), nil)

	if updated.Enabled {
		s.upsertManager(updated)
		_ = s.manager.Reload(ctx, updated.ID)
	}
	return s.serverView(ctx, actor.TenantID, updated)
}

// —— 健康与事件 ——

// HealthSummary 返回租户健康摘要（供管理页 Header 与告警）。
func (s *Service) HealthSummary(ctx context.Context, actor Actor) (ServerListResult, error) {
	list, err := s.ListServers(ctx, actor)
	if err != nil {
		return ServerListResult{}, err
	}
	authRequired := 0
	for _, item := range list.Items {
		if s.lastAuthRequired(item.ID) {
			authRequired++
		}
	}
	summary := list.Summary
	summary.AuthRequired = authRequired
	list.Summary = summary
	return list, nil
}

// ServerEvents 返回服务器近期生命周期事件（时间升序）。
func (s *Service) ServerEvents(ctx context.Context, actor Actor, serverID int) ([]manager.Event, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return nil, err
	}
	return s.events.List(serverID), nil
}

// —— 内部：视图与装载 ——

func (s *Service) serverEntity(ctx context.Context, tenantID, serverID int) (*ent.MCPServer, error) {
	entity, err := s.client.MCPServer.Query().
		Where(mcpserver.IDEQ(serverID), mcpserver.TenantIDEQ(tenantID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, NewAdminError(http.StatusNotFound, CodeNotFound, "服务器不存在")
	}
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询服务器失败", err)
	}
	return entity, nil
}

func (s *Service) toolEntity(ctx context.Context, tenantID, serverID int, callableName string) (*ent.MCPServerTool, error) {
	row, err := s.client.MCPServerTool.Query().
		Where(
			mcpservertool.TenantIDEQ(tenantID),
			mcpservertool.ServerIDEQ(serverID),
			mcpservertool.CallableNameEQ(strings.TrimSpace(callableName)),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, NewAdminError(http.StatusNotFound, CodeNotFound, "工具不存在")
	}
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询工具失败", err)
	}
	return row, nil
}

// serverView 组装服务器视图（凭据掩码；运行位以 manager 为准，DB 状态为回写快照）。
func (s *Service) serverView(ctx context.Context, tenantID int, entity *ent.MCPServer) (ServerView, error) {
	headers, credential := s.maskedSecrets(entity)
	total, enabledTools, quarantined, err := s.store.ToolCounts(ctx, tenantID, entity.ID)
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "统计工具失败", err)
	}
	running := s.runningState(entity.ID)
	status := entity.Status
	if running != "" {
		status = running
	}
	return ServerView{
		ID:                 entity.ID,
		Name:               entity.Name,
		DisplayName:        entity.DisplayName,
		Transport:          entity.Transport,
		URL:                entity.URL,
		CredentialType:     entity.CredentialType,
		TrustLevel:         entity.TrustLevel,
		Enabled:            entity.Enabled,
		Status:             status,
		RunningStatus:      running,
		LastError:          entity.LastError,
		ProtocolVersion:    entity.ProtocolVersion,
		ServerInfo:         entity.ServerInfo,
		TimeoutMS:          entity.TimeoutMs,
		MaxParallelCalls:   entity.MaxParallelCalls,
		MaxRetry:           entity.MaxRetry,
		Version:            entity.Version,
		ToolCount:          total,
		EnabledToolCount:   enabledTools,
		QuarantinedToolNum: quarantined,
		HeadersMasked:      headers,
		CredentialMasked:   credential,
		CreatedAt:          entity.CreatedAt,
		UpdatedAt:          entity.UpdatedAt,
	}, nil
}

// maskedSecrets 解密并掩码（解密失败按空集处理：不阻断列表，运行期自会暴露 last_error）。
func (s *Service) maskedSecrets(entity *ent.MCPServer) (map[string]string, map[string]string) {
	headers := map[string]string{}
	if entity.HeadersEncrypted != "" {
		if values, err := s.credentials.Decrypt(entity.HeadersEncrypted); err == nil {
			headers = values.Masked()
		}
	}
	credential := map[string]string{}
	if entity.CredentialEncrypted != "" {
		if values, err := s.credentials.Decrypt(entity.CredentialEncrypted); err == nil {
			credential = values.Masked()
		}
	}
	return headers, credential
}

// mergeSecrets 在既有密文上应用更新：patch 空值 = 保留原值；
// 合并结果与现值一致时**返回原密文**（避免随机 nonce 造成的无谓改写与乐观锁噪声）。
func (s *Service) mergeSecrets(existingCipher string, patch map[string]string) (string, error) {
	current, err := s.credentials.Decrypt(existingCipher)
	if err != nil {
		return "", WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据解密失败", err)
	}
	merged := current.ApplyPatch(patch)
	if sameSecretValues(current, merged) {
		return existingCipher, nil
	}
	ciphertext, err := s.credentials.Encrypt(merged)
	if err != nil {
		return "", WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}
	return ciphertext, nil
}

func sameSecretValues(left, right SecretValues) bool {
	if left.Len() != right.Len() {
		return false
	}
	leftValues := left.Values()
	rightValues := right.Values()
	for key, value := range leftValues {
		if other, ok := rightValues[key]; !ok || other != value {
			return false
		}
	}
	return true
}

// shortError 截断错误文本（审计/日志用，≤200 字符；不用于对外响应）。
func shortError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

// serverAuditSnapshot 生成审计快照（不含明文；凭据只保留掩码）。
func (s *Service) serverAuditSnapshot(entity *ent.MCPServer, headers, credential map[string]string) map[string]any {
	return map[string]any{
		"id":                entity.ID,
		"name":              entity.Name,
		"display_name":      entity.DisplayName,
		"transport":         entity.Transport,
		"url":               entity.URL,
		"credential_type":   entity.CredentialType,
		"trust_level":       entity.TrustLevel,
		"enabled":           entity.Enabled,
		"status":            entity.Status,
		"timeout_ms":        entity.TimeoutMs,
		"max_parallel":      entity.MaxParallelCalls,
		"max_retry":         entity.MaxRetry,
		"version":           entity.Version,
		"headers_masked":    toAnyMap(headers),
		"credential_masked": toAnyMap(credential),
	}
}

// buildServerConfig 构建 manager 配置（解密凭据注入请求头；凭据键覆盖同名请求头）。
func (s *Service) buildServerConfig(entity *ent.MCPServer) (manager.ServerConfig, error) {
	headers := map[string]string{}
	if entity.HeadersEncrypted != "" {
		values, err := s.credentials.Decrypt(entity.HeadersEncrypted)
		if err != nil {
			return manager.ServerConfig{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "请求头解密失败", err)
		}
		headers = values.Values()
	}
	if entity.CredentialEncrypted != "" {
		values, err := s.credentials.Decrypt(entity.CredentialEncrypted)
		if err != nil {
			return manager.ServerConfig{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据解密失败", err)
		}
		for key, value := range values.Values() {
			headers[key] = value
		}
	}
	return manager.ServerConfig{
		ID:               entity.ID,
		TenantID:         entity.TenantID,
		Name:             entity.Name,
		Transport:        transport.Kind(entity.Transport),
		URL:              entity.URL,
		Headers:          headers,
		TimeoutMS:        entity.TimeoutMs,
		MaxParallelCalls: entity.MaxParallelCalls,
		MaxRetry:         entity.MaxRetry,
		Enabled:          entity.Enabled,
		TrustLevel:       entity.TrustLevel,
		Version:          entity.Version,
	}, nil
}

// upsertManager 把最新配置装入 manager（不建连；启停/重载另行触发）。
func (s *Service) upsertManager(entity *ent.MCPServer) {
	cfg, err := s.buildServerConfig(entity)
	if err != nil {
		return // 凭据解密失败：保留旧配置，运行态由 last_error 暴露
	}
	s.manager.Upsert(cfg)
}

func (s *Service) runningState(serverID int) string {
	snapshot, err := s.manager.Status(serverID)
	if err != nil {
		return ""
	}
	return string(snapshot.Status)
}

// lastAuthRequired 判定最近一次连接结论是否为「认证失效」。
func (s *Service) lastAuthRequired(serverID int) bool {
	events := s.events.List(serverID)
	for index := len(events) - 1; index >= 0; index-- {
		switch events[index].Type {
		case manager.EventServerAuthRequired:
			return true
		case manager.EventServerConnected:
			return false
		}
	}
	return false
}

func (s *Service) recordAudit(ctx context.Context, actor Actor, action, objectType, objectID string, before, after map[string]any, cause error) {
	entry := AuditEntry{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     action,
		ObjectType: objectType,
		ObjectID:   objectID,
		Before:     before,
		After:      after,
		Result:     "success",
		IP:         actor.IP,
		At:         s.now(),
	}
	if cause != nil {
		entry.Result = "failure"
		if adminErr, ok := AsAdminError(cause); ok {
			entry.ErrorCode = string(adminErr.Code)
		} else {
			entry.ErrorCode = string(CodeInternal)
		}
	}
	_ = s.audit.RecordMCPAudit(ctx, entry)
}

func toolView(row *ent.MCPServerTool, runningState string) ToolView {
	effective := row.Enabled && row.Healthy && !row.Quarantined
	return ToolView{
		ID:                 row.ID,
		RawName:            row.RawName,
		CallableName:       row.CallableName,
		Description:        row.Description,
		InputSchema:        row.InputSchema,
		SchemaHash:         row.SchemaHash,
		ReadOnly:           row.ReadOnly,
		Risk:               row.Risk,
		Category:           row.Category,
		Enabled:            effective,
		Healthy:            row.Healthy,
		ConfiguredEnabled:  row.Enabled,
		Quarantined:        row.Quarantined,
		QuarantineReason:   row.QuarantineReason,
		LastError:          row.LastError,
		DiscoveredAt:       row.DiscoveredAt,
		UpdatedAt:          row.UpdatedAt,
		ServerRunningState: runningState,
	}
}

func normalizeCredentialType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "none":
		return "none", nil
	case "static_header":
		return "static_header", nil
	case "oauth2":
		return "oauth2", nil
	default:
		return "", NewAdminError(http.StatusBadRequest, CodeValidationFailed, "credential_type 仅允许 none|static_header|oauth2")
	}
}

func normalizeTrustLevel(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "untrusted":
		return "untrusted", nil
	case "trusted":
		return "trusted", nil
	default:
		return "", NewAdminError(http.StatusBadRequest, CodeValidationFailed, "trust_level 仅允许 trusted|untrusted")
	}
}

func validRisk(value string) bool {
	switch value {
	case "read", "plan", "act_low", "act_medium", "act_high":
		return true
	default:
		return false
	}
}
