package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/mcp/client"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"

	_ "github.com/mattn/go-sqlite3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const serviceTestKey = "unit-test-mcp-admin-key-1234"

// —— stub 会话（实现 manager.ToolSession）——

type stubSession struct {
	mu      sync.Mutex
	tools   []string
	pingErr error
	closed  bool
}

func newStubSession(tools ...string) *stubSession {
	return &stubSession{tools: tools}
}

func (s *stubSession) ID() string              { return "stub-session" }
func (s *stubSession) ProtocolVersion() string { return "2025-06-18" }
func (s *stubSession) ServerName() string      { return "stub-server" }
func (s *stubSession) ServerVersion() string   { return "v1" }

func (s *stubSession) ListTools(context.Context) ([]client.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tools := make([]client.Tool, 0, len(s.tools))
	for _, name := range s.tools {
		tools = append(tools, client.Tool{
			RawName:     name,
			Description: name + " description",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		})
	}
	return tools, nil
}

func (s *stubSession) CallTool(context.Context, string, map[string]any) (*client.CallResult, error) {
	return &client.CallResult{Content: []client.Content{{Type: "text", Text: "ok"}}}, nil
}

func (s *stubSession) Ping(context.Context) error { return s.pingErr }

func (s *stubSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// —— 测试脚手架 ——

type harness struct {
	t        *testing.T
	client   *ent.Client
	db       *sql.DB
	service  *Service
	manager  *manager.Manager
	audit    *MemoryAuditSink
	events   *EventBuffer
	session  *stubSession
	guard    transport.Guard
	dial     manager.DialFunc
	dialErr  error
	dialLock sync.Mutex
}

func newHarness(t *testing.T, guard transport.Guard) *harness {
	t.Helper()
	// 文件库 + WAL：manager 后台协程会并发写运行态/工具缓存；
	// 共享缓存内存库在并发下会直接报 SQLITE_LOCKED（不吃 busy_timeout），故用临时文件库。
	dsn := filepath.Join(t.TempDir(), "mcp-admin-test.db") + "?_fk=1&_busy_timeout=15000&_journal_mode=WAL"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	if guard == nil {
		guard = transport.NewSSRFGuard(transport.SSRFConfig{AllowHTTP: true, AllowPrivate: true})
	}
	credentials, err := NewCredentialService(serviceTestKey)
	require.NoError(t, err)
	audit := NewMemoryAuditSink()
	events := NewEventBuffer(0)
	session := newStubSession("list_issues", "create_issue")

	h := &harness{t: t, client: client, db: db, audit: audit, events: events, session: session, guard: guard}
	store, err := NewEntStore(client)
	require.NoError(t, err)
	dial := func(context.Context, manager.ServerConfig) (manager.ToolSession, error) {
		h.dialLock.Lock()
		err := h.dialErr
		h.dialLock.Unlock()
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	h.dial = dial
	managerInstance := manager.New(manager.Options{
		Guard:          guard,
		Dial:           dial,
		Events:         events,
		StatusWriter:   store,
		ToolCache:      store,
		HealthInterval: time.Hour,
	})
	h.manager = managerInstance
	service, err := NewService(Config{
		Client:      client,
		Credentials: credentials,
		Manager:     managerInstance,
		Guard:       guard,
		Store:       store,
		Audit:       audit,
		Events:      events,
		Now:         func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)
	h.service = service
	return h
}

func (h *harness) actor() Actor { return Actor{TenantID: 1, UserID: 7, IP: "10.1.2.3"} }

func (h *harness) createServer(t *testing.T, name string) ServerView {
	t.Helper()
	view, err := h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name:        name,
		DisplayName: "测试服务器",
		Transport:   "streamable",
		URL:         "https://mcp.example.com/mcp",
		Headers:     map[string]string{"Authorization": "Bearer super-secret-token-123"},
		Credential:  map[string]string{"X-Api-Key": "api-key-abcdef"},
	})
	require.NoError(t, err)
	return view
}

func waitForStatus(t *testing.T, h *harness, serverID int, want manager.ServerStatus) {
	t.Helper()
	require.Eventually(t, func() bool {
		snapshot, err := h.manager.Status(serverID)
		return err == nil && snapshot.Status == want
	}, 3*time.Second, 5*time.Millisecond, "等待运行态 %s", want)
}

// waitForTools 等待发现结果落库（healthy 与发现完成之间存在异步窗口）。
func waitForTools(t *testing.T, h *harness, serverID, want int) []ToolView {
	t.Helper()
	var tools []ToolView
	require.Eventually(t, func() bool {
		current, err := h.service.ListTools(context.Background(), h.actor(), serverID)
		if err != nil {
			return false
		}
		tools = current
		return len(current) == want
	}, 3*time.Second, 10*time.Millisecond, "等待工具发现落库")
	return tools
}

// —— 用例 ——

func TestService_CreateServer_SafeDefaultsMaskingAndAudit(t *testing.T) {
	h := newHarness(t, nil)
	view := h.createServer(t, "github")

	require.False(t, view.Enabled, "新服务器默认禁用（D7）")
	require.Equal(t, "untrusted", view.TrustLevel)
	require.Equal(t, "configured", view.Status)
	require.Equal(t, 1, view.Version)
	require.NotContains(t, view.HeadersMasked["Authorization"], "super-secret-token-123")
	require.Equal(t, "Bear****23", view.HeadersMasked["Authorization"])
	require.Equal(t, "api-****ef", view.CredentialMasked["X-Api-Key"])
	require.NotContains(t, view.CredentialMasked["X-Api-Key"], "api-key-abcdef")

	// DB 中无明文（密文列）。
	var storedHeaders, storedCredential string
	require.NoError(t, h.db.QueryRow(
		`SELECT headers_encrypted, credential_encrypted FROM mcp_servers WHERE name = 'github'`).
		Scan(&storedHeaders, &storedCredential))
	require.NotContains(t, storedHeaders, "super-secret-token-123")
	require.NotContains(t, storedCredential, "api-key-abcdef")

	// 审计：actor/tenant/action/object/ip/ts + 脱敏快照。
	entry, ok := h.audit.Find("create_server")
	require.True(t, ok)
	require.Equal(t, 1, entry.TenantID)
	require.Equal(t, 7, entry.ActorID)
	require.Equal(t, "mcp_server", entry.ObjectType)
	require.Equal(t, "github", entry.ObjectID)
	require.Equal(t, "success", entry.Result)
	require.Equal(t, "10.1.2.3", entry.IP)
	require.False(t, entry.At.IsZero())
	require.NotContains(t, fmt.Sprint(entry.After), "super-secret-token-123")
	require.Contains(t, fmt.Sprint(entry.After), "Bear****23")
}

func TestService_CreateServer_ValidationAndDuplicate(t *testing.T) {
	h := newHarness(t, nil)
	h.createServer(t, "github")

	_, err := h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name: "github", Transport: "streamable", URL: "https://mcp.example.com/mcp",
	})
	adminErr, ok := AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusConflict, adminErr.Status)
	require.Equal(t, CodeDuplicateName, adminErr.Code)

	_, err = h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name: "stdio-server", Transport: "stdio", URL: "https://mcp.example.com/mcp",
	})
	adminErr, ok = AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, adminErr.Status)
	require.Equal(t, CodeInvalidTransport, adminErr.Code)

	_, err = h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name: "BadName", Transport: "streamable", URL: "https://mcp.example.com/mcp",
	})
	adminErr, _ = AsAdminError(err)
	require.Equal(t, CodeValidationFailed, adminErr.Code)

	_, err = h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name: "okname", Transport: "streamable", URL: "ftp://mcp.example.com/mcp",
	})
	adminErr, _ = AsAdminError(err)
	require.Equal(t, CodeValidationFailed, adminErr.Code)

	// 换行注入拒绝。
	_, err = h.service.CreateServer(context.Background(), h.actor(), CreateServerRequest{
		Name: "okname2", Transport: "streamable", URL: "https://mcp.example.com/mcp",
		Headers: map[string]string{"X-Evil": "value\r\nInjected: 1"},
	})
	adminErr, _ = AsAdminError(err)
	require.Equal(t, CodeValidationFailed, adminErr.Code)
}

func TestService_UpdateServer_OptimisticLock(t *testing.T) {
	h := newHarness(t, nil)
	created := h.createServer(t, "github")

	display := "GitHub MCP"
	updated, err := h.service.UpdateServer(context.Background(), h.actor(), created.ID, UpdateServerRequest{
		Version:     created.Version,
		DisplayName: &display,
	})
	require.NoError(t, err)
	require.Equal(t, "GitHub MCP", updated.DisplayName)
	require.Equal(t, created.Version+1, updated.Version)

	// 旧版本号必须冲突（409 + conflict）。
	_, err = h.service.UpdateServer(context.Background(), h.actor(), created.ID, UpdateServerRequest{
		Version: created.Version,
	})
	adminErr, ok := AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusConflict, adminErr.Status)
	require.Equal(t, CodeConflict, adminErr.Code)

	// 凭据空值 = 不修改（M0-06 语义）。
	var beforeCipher string
	require.NoError(t, h.db.QueryRow(`SELECT headers_encrypted FROM mcp_servers WHERE id = ?`, created.ID).Scan(&beforeCipher))
	_, err = h.service.UpdateServer(context.Background(), h.actor(), created.ID, UpdateServerRequest{
		Version: updated.Version,
		Headers: map[string]string{"Authorization": ""},
	})
	require.NoError(t, err)
	var afterCipher string
	require.NoError(t, h.db.QueryRow(`SELECT headers_encrypted FROM mcp_servers WHERE id = ?`, created.ID).Scan(&afterCipher))
	require.Equal(t, beforeCipher, afterCipher, "空值不得改写密文")

	// 审计含 before/after 且脱敏。
	entries := h.audit.Entries()
	var updateEntry *AuditEntry
	for index := range entries {
		if entries[index].Action == "update_server" {
			updateEntry = &entries[index]
			break
		}
	}
	require.NotNil(t, updateEntry)
	require.NotContains(t, fmt.Sprint(updateEntry.Before), "super-secret-token-123")
	require.NotContains(t, fmt.Sprint(updateEntry.After), "super-secret-token-123")
}

func TestService_EnableDisableAndDiscovery(t *testing.T) {
	h := newHarness(t, nil)
	created := h.createServer(t, "github")

	view, err := h.service.EnableServer(context.Background(), h.actor(), created.ID)
	require.NoError(t, err)
	require.True(t, view.Enabled)
	// D8：enable 立即返回，运行态为 connecting（异步建连）。
	snapshot, err := h.manager.Status(created.ID)
	require.NoError(t, err)
	require.Contains(t, []manager.ServerStatus{manager.StatusConnecting, manager.StatusHealthy}, snapshot.Status)

	waitForStatus(t, h, created.ID, manager.StatusHealthy)
	// 建连后自动发现工具；工具默认待治理。
	tools := waitForTools(t, h, created.ID, 2)
	require.Len(t, tools, 2)
	for _, tool := range tools {
		require.False(t, tool.ConfiguredEnabled, "新工具默认不启用（D7）")
		require.False(t, tool.Enabled)
	}

	// 停用。
	if _, err := h.service.DisableServer(context.Background(), h.actor(), created.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	waitForStatus(t, h, created.ID, manager.StatusDisabled)
	if _, ok := h.audit.Find("enable_server"); !ok {
		t.Fatal("缺少 enable_server 审计")
	}
	if _, ok := h.audit.Find("disable_server"); !ok {
		t.Fatal("缺少 disable_server 审计")
	}
}

func TestService_ToolsGovernanceWithoutReconnect(t *testing.T) {
	h := newHarness(t, nil)
	created := h.createServer(t, "github")
	_, err := h.service.EnableServer(context.Background(), h.actor(), created.ID)
	require.NoError(t, err)
	_, err = h.service.EnableServer(context.Background(), h.actor(), created.ID) // 幂等
	require.NoError(t, err)
	waitForStatus(t, h, created.ID, manager.StatusHealthy)

	tools := waitForTools(t, h, created.ID, 2)
	callable := tools[0].CallableName

	enabledTool, err := h.service.SetToolEnabled(context.Background(), h.actor(), created.ID, callable, true)
	require.NoError(t, err)
	require.True(t, enabledTool.ConfiguredEnabled)
	require.True(t, enabledTool.Enabled, "healthy + configured + 未隔离 = effective")

	// 治理位不得影响运行态（D6：不触发重连）。
	snapshot, _ := h.manager.Status(created.ID)
	require.Equal(t, manager.StatusHealthy, snapshot.Status)

	// classification：非法 risk 拒绝，合法更新 + 审计。
	readOnly := true
	_, err = h.service.SetToolClassification(context.Background(), h.actor(), created.ID, callable, ClassificationRequest{
		Risk: "super-danger",
	})
	adminErr, ok := AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, CodeValidationFailed, adminErr.Code)

	classified, err := h.service.SetToolClassification(context.Background(), h.actor(), created.ID, callable, ClassificationRequest{
		ReadOnly: &readOnly, Risk: "read", Category: "issue",
	})
	require.NoError(t, err)
	require.True(t, classified.ReadOnly)
	require.Equal(t, "read", classified.Risk)
	require.Equal(t, "issue", classified.Category)
	if _, ok := h.audit.Find("set_tool_classification"); !ok {
		t.Fatal("缺少 set_tool_classification 审计")
	}

	// 批量：空数组 = 全部。
	affected, err := h.service.BulkSetTools(context.Background(), h.actor(), created.ID, BulkToolRequest{Enabled: true})
	require.NoError(t, err)
	require.Equal(t, 2, affected)

	// 单工具 404。
	_, err = h.service.SetToolEnabled(context.Background(), h.actor(), created.ID, "mcp__github__nope", true)
	adminErr, _ = AsAdminError(err)
	require.Equal(t, http.StatusNotFound, adminErr.Status)
}

func TestService_RotateCredential(t *testing.T) {
	h := newHarness(t, nil)
	created := h.createServer(t, "github")

	var beforeCipher string
	require.NoError(t, h.db.QueryRow(`SELECT credential_encrypted FROM mcp_servers WHERE id = ?`, created.ID).Scan(&beforeCipher))

	view, err := h.service.RotateCredential(context.Background(), h.actor(), created.ID, RotateCredentialRequest{
		CredentialType: "static_header",
		Credential:     map[string]string{"X-Api-Key": "rotated-key-987654"},
	})
	require.NoError(t, err)
	require.Equal(t, "static_header", view.CredentialType)
	require.NotContains(t, view.CredentialMasked["X-Api-Key"], "rotated-key-987654")

	var afterCipher string
	require.NoError(t, h.db.QueryRow(`SELECT credential_encrypted FROM mcp_servers WHERE id = ?`, created.ID).Scan(&afterCipher))
	require.NotEqual(t, beforeCipher, afterCipher, "轮换必须改写密文")
	require.NotContains(t, afterCipher, "rotated-key-987654")

	entry, ok := h.audit.Find("rotate_credential")
	require.True(t, ok)
	require.NotContains(t, fmt.Sprint(entry.After), "rotated-key-987654")
}

func TestService_TestServer_RealMockConnectionAndSSRFAndFailure(t *testing.T) {
	// 真实 mock MCP 服务器（SDK；M0-13 将提供可复用版本）。
	server := mcp.NewServer(&mcp.Implementation{Name: "itsm-mcp-mock", Version: "v0.0.1"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "echo back",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	mock := httptest.NewServer(handler)
	defer mock.Close()
	port := mock.Listener.Addr().(*net.TCPAddr).Port

	// 允许 loopback + 端口，才能对 httptest 发起真实连接。
	guard := transport.NewSSRFGuard(transport.SSRFConfig{
		AllowHTTP:    true,
		AllowPrivate: true,
		AllowedPorts: []int{port},
	})
	h := newHarness(t, guard)
	created := h.createServer(t, "github")

	// 1) 测试连接成功：协议版本 + 服务器信息 + 工具预览；不落库。
	_, err := h.service.UpdateServer(context.Background(), h.actor(), created.ID, UpdateServerRequest{
		Version: created.Version,
		URL:     stringPtr(mock.URL),
	})
	require.NoError(t, err)

	result, err := h.service.TestServer(context.Background(), h.actor(), created.ID, TestServerRequest{})
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "2025-06-18", result.ProtocolVersion)
	require.Equal(t, "itsm-mcp-mock", result.ServerName)
	require.Len(t, result.Tools, 1)
	require.Equal(t, "mcp__github__echo", result.Tools[0].CallableName)
	require.GreaterOrEqual(t, result.DurationMS, int64(0))

	// 测试连接不落库：工具表仍为空、运行态未注册临时槽位。
	rows, err := h.client.MCPServerTool.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, rows, "test 不落库")
	_, err = h.manager.Status(testServerID)
	require.Error(t, err, "临时槽位已清理")

	// 2) SSRF 拒绝：默认守卫（禁私网）指向内网地址 → 422 + ssrf_blocked。
	strictService, err := NewService(Config{
		Client:      h.client,
		Credentials: mustCredentials(t),
		Manager:     h.manager,
		Guard:       transport.NewSSRFGuard(transport.SSRFConfig{AllowHTTP: true}),
		Audit:       h.audit,
		Events:      h.events,
	})
	require.NoError(t, err)
	_, err = strictService.TestServer(context.Background(), h.actor(), created.ID, TestServerRequest{
		URL: "http://10.0.0.5:8080/mcp",
	})
	adminErr, ok := AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusUnprocessableEntity, adminErr.Status)
	require.Equal(t, CodeSSRFBlocked, adminErr.Code)

	// 3) 连接失败注入：指向已关闭端口（守卫允许私网）→ 502 类。
	closedListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := closedListener.Addr().(*net.TCPAddr).Port
	closedListener.Close()

	failGuard := transport.NewSSRFGuard(transport.SSRFConfig{
		AllowHTTP:    true,
		AllowPrivate: true,
		AllowedPorts: []int{closedPort},
	})
	failService, err := NewService(Config{
		Client:      h.client,
		Credentials: mustCredentials(t),
		Manager:     h.manager,
		Guard:       failGuard,
		Audit:       h.audit,
		Events:      h.events,
	})
	require.NoError(t, err)
	_, err = failService.TestServer(context.Background(), h.actor(), created.ID, TestServerRequest{
		URL: fmt.Sprintf("http://127.0.0.1:%d/mcp", closedPort),
	})
	adminErr, ok = AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusBadGateway, adminErr.Status)
	require.Contains(t, []ErrorCode{CodeUnreachable, CodeConnectTimeout}, adminErr.Code)

}

func TestService_ListServersSummaryAndTenantIsolation(t *testing.T) {
	h := newHarness(t, nil)
	h.createServer(t, "github")
	second := h.createServer(t, "gitlab")
	_, err := h.service.EnableServer(context.Background(), h.actor(), second.ID)
	require.NoError(t, err)
	waitForStatus(t, h, second.ID, manager.StatusHealthy)
	waitForTools(t, h, second.ID, 2)

	list, err := h.service.ListServers(context.Background(), h.actor())
	require.NoError(t, err)
	require.Len(t, list.Items, 2)
	require.Equal(t, 2, list.Summary.Total)
	require.Equal(t, 1, list.Summary.Enabled)
	require.Equal(t, 1, list.Summary.Connected)
	require.Equal(t, 2, list.Summary.Tools)

	// 其它租户不可见（越权 → 404）。
	_, err = h.service.GetServer(context.Background(), Actor{TenantID: 2, UserID: 9}, second.ID)
	adminErr, ok := AsAdminError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusNotFound, adminErr.Status)
	require.Equal(t, CodeNotFound, adminErr.Code)

	// DeleteServer 幂等收尾（并级联工具缓存）。
	require.NoError(t, h.service.DeleteServer(context.Background(), h.actor(), second.ID))
	_, err = h.service.GetServer(context.Background(), h.actor(), second.ID)
	require.Error(t, err)
}

func TestService_StartupLoadsEnabledServers(t *testing.T) {
	h := newHarness(t, nil)
	created := h.createServer(t, "github")
	// 模拟上一次进程遗留的启用位（治理数据落在 DB，运行态需在启动期重建）。
	require.NoError(t, h.client.MCPServer.UpdateOneID(created.ID).
		SetEnabled(true).Exec(context.Background()))

	// 新进程：同一 DB + 全新 manager/service（运行态为空）。
	store := h.service.Store()
	freshManager := manager.New(manager.Options{
		Guard:          h.guard,
		Dial:           h.dial,
		StatusWriter:   store,
		ToolCache:      store,
		Events:         h.events,
		HealthInterval: time.Hour,
	})
	freshService, err := NewService(Config{
		Client:      h.client,
		Credentials: mustCredentials(t),
		Manager:     freshManager,
		Guard:       h.guard,
		Store:       store,
		Audit:       h.audit,
		Events:      h.events,
	})
	require.NoError(t, err)

	// Startup 拉起：装配运行态 + 异步建连 + 发现落库。
	require.NoError(t, freshService.Startup(context.Background()))
	require.Eventually(t, func() bool {
		snapshot, statusErr := freshManager.Status(created.ID)
		return statusErr == nil && snapshot.Status == manager.StatusHealthy
	}, 3*time.Second, 10*time.Millisecond, "启动期拉起连接")

	require.Eventually(t, func() bool {
		tools, listErr := freshService.ListTools(context.Background(), h.actor(), created.ID)
		return listErr == nil && len(tools) == 2
	}, 3*time.Second, 10*time.Millisecond, "启动期发现结果落库")

	// 幂等：重复 Startup 不产生错误、不改变治理位。
	require.NoError(t, freshService.Startup(context.Background()))
	entity, err := h.client.MCPServer.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.True(t, entity.Enabled)
}

func stringPtr(value string) *string { return &value }

func mustCredentials(t *testing.T) *CredentialService {
	t.Helper()
	credentials, err := NewCredentialService(serviceTestKey)
	require.NoError(t, err)
	return credentials
}
