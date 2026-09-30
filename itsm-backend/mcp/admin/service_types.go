package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mcpserver"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"
)

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
	// Policy 是**有效执行策略**回读（M1-08，A1-08）：平台默认 + 服务器覆盖后的最终值。
	Policy    ServerPolicyView `json:"policy"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// ServerPolicyView 是执行策略回读视图（超时/并发/重试/宽限/告警阈值）。
// 输出上限（256KB）与截断标记由 provider 层（`provider.DefaultMaxResultBytes`）保证。
type ServerPolicyView struct {
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

// policyView 计算**有效策略**：平台默认（manager）× 服务器配置（ent）逐字段取「更严」方向。
//
// 为什么不用 manager.Policy 直接回读：服务器未启用时不会注册到 manager（D7 默认关闭），
// 此时 manager 只能给平台默认值，回读会与库里配置不一致——管理页看到的策略必须与配置一致。
func (s *Service) policyView(entity *ent.MCPServer) ServerPolicyView {
	policy := s.manager.Policy(entity.ID) // 平台默认（含连接超时/宽限/告警阈值/健康间隔）
	if entity.TimeoutMs > 0 {
		policy.TimeoutMS = entity.TimeoutMs
	}
	if entity.MaxParallelCalls > 0 {
		policy.MaxParallelCalls = entity.MaxParallelCalls
	}
	// 读重试：服务器配置（ent 默认 1，0=关闭）与平台上限取小。
	if entity.MaxRetry < policy.ReadRetryCap {
		policy.MaxRetry = entity.MaxRetry
	} else {
		policy.MaxRetry = policy.ReadRetryCap
	}
	return ServerPolicyView{
		TimeoutMS:              policy.TimeoutMS,
		ConnectTimeoutMS:       policy.ConnectTimeoutMS,
		MaxParallelCalls:       policy.MaxParallelCalls,
		MaxRetry:               policy.MaxRetry,
		ReadRetryCap:           policy.ReadRetryCap,
		DisableGraceMS:         policy.DisableGraceMS,
		HealthFailureThreshold: policy.HealthFailureThreshold,
		HealthIntervalMS:       policy.HealthIntervalMS,
		ToolBudget:             policy.ToolBudget,
		ContextTokens:          policy.ContextTokens,
		ToolTokenShare:         policy.ToolTokenShare,
	}
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
	// Capabilities 是 M2 能力开关的展示块（页面渲染与门禁同源；未注入能力源时为 nil）。
	Capabilities *CapabilityView `json:"capabilities,omitempty"`
}

// CapabilityView 是 M2 能力开关的展示块（snake_case，与 MCP 管理 API 契约一致）。
type CapabilityView struct {
	MCPEnabled      bool `json:"mcp_enabled"`
	MCPWriteEnabled bool `json:"mcp_write_enabled"`
	BotEnabled      bool `json:"bot_enabled"`
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
