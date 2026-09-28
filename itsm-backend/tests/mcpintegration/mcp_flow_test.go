// Package mcpintegration 是 M0-14 的端到端集成验收：真实 ent + DB + mock MCP 服务器，
// 走通「新增服务器 → 测试连接 → 启用 → 工具发现 → 治理启用 → 只读调用 → 审计可查」，
// 叠加 SSRF / 凭据 / 输出超限 / 跨租户负向断言。
//
// 与生产装配的差异（仅两处，均为测试必需）：
//  1. 出站安全：mock 跑在 127.0.0.1，故放行 http + 私网 + 该端口（严格模式的拒绝能力由
//     TestM0Flow_SSRFBlockedWithStrictGuard 单独断言）；
//  2. 权限模式：切到 HardcodeOnly（无 DB 会话的 RBAC 判定路径），角色用 admin（持 mcp:read）。
package mcpintegration

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/mcp/admin"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/provider"
	"itsm-backend/mcp/testutil/mockserver"
	"itsm-backend/mcp/transport"
	"itsm-backend/middleware"
	"itsm-backend/service"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type harness struct {
	client     *ent.Client
	adminSvc   *admin.Service
	manager    *manager.Manager
	registry   *service.ToolRegistry
	aiSvc      *ai.Service
	mock       *mockserver.Server
	mockHTTP   *httptest.Server
	events     *admin.EventBuffer
	guard      transport.Guard
	tenantID   int
	userID     int
	otherActor admin.Actor

	mu      sync.Mutex
	tracked []int
}

// track 登记需要在其后关闭会话的服务器 ID（test cleanup 顺序依赖）。
func (h *harness) track(serverID int) {
	h.mu.Lock()
	h.tracked = append(h.tracked, serverID)
	h.mu.Unlock()
}

func TestM0Flow_EndToEnd(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, ctx)

	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID, IP: "127.0.0.1"}

	// 1) 新增服务器（凭据只写不读回）。
	created, err := h.adminSvc.CreateServer(ctx, actor, admin.CreateServerRequest{
		Name:           "mock",
		DisplayName:    "Mock MCP",
		Transport:      "streamable",
		URL:            h.mockHTTP.URL,
		CredentialType: "static_header",
		Credential:     map[string]string{"Authorization": "Bearer super-secret-token"},
		Headers:        map[string]string{"X-Tenant": "itsm"},
	})
	require.NoError(t, err)
	assert.False(t, created.Enabled, "D7 默认拒绝：新建服务器默认不启用")
	assert.Equal(t, "configured", created.Status)
	require.Contains(t, created.CredentialMasked, "Authorization")
	assert.NotContains(t, created.CredentialMasked["Authorization"], "super-secret-token", "凭据明文不得回显")

	// 2) 测试连接（同步，不落库）：协议版本 + 工具预览。
	testResult, err := h.adminSvc.TestServer(ctx, actor, created.ID, admin.TestServerRequest{})
	require.NoError(t, err)
	require.True(t, testResult.OK, "测试连接必须成功：%+v", testResult)
	assert.NotEmpty(t, testResult.ProtocolVersion)
	assert.Equal(t, "itsm-mcp-mock", testResult.ServerName)
	assert.Len(t, testResult.Tools, 6, "default 夹具应有 6 个工具预览")

	h.track(created.ID) // 收尾时先关会话再关 mock（避免 SSE 流阻塞 Close）

	// 3) 启用（202 语义）→ 轮询至 healthy（连接 + 工具发现完成；失败时输出 manager 事件便于定位）。
	_, err = h.adminSvc.EnableServer(ctx, actor, created.ID)
	require.NoError(t, err)
	var lastView admin.ServerView
	var lastErr error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		lastView, lastErr = h.adminSvc.GetServer(ctx, actor, created.ID)
		if lastErr == nil && lastView.RunningStatus == "healthy" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil || lastView.RunningStatus != "healthy" {
		t.Fatalf("启用后未进入 healthy：status=%s running=%s last_error=%s err=%v events=%+v",
			lastView.Status, lastView.RunningStatus, lastView.LastError, lastErr, h.events.List(created.ID))
	}

	// 4) 工具发现：落库、非法字符名被隔离、计数正确。
	// 注意：healthy 只保证「连接 + 首轮发现已启动」，工具落库是异步的；全量并发跑测试时
	// （go test ./... 会并行多个包）落库可能晚于 healthy，因此按目标数量做有界等待。
	var tools []admin.ToolView
	deadline = time.Now().Add(20 * time.Second)
	for {
		tools, err = h.adminSvc.ListTools(ctx, actor, created.ID)
		require.NoError(t, err)
		if len(tools) >= 6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("20s 内工具发现未达 6 个（实际 %d）：%+v", len(tools), tools)
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.Len(t, tools, 6)

	byCallable := map[string]admin.ToolView{}
	for _, tool := range tools {
		byCallable[tool.CallableName] = tool
	}
	listIssues, ok := byCallable["mcp__mock__list_issues"]
	require.True(t, ok, "只读工具必须投影为 mcp__mock__list_issues；实际：%v", keys(byCallable))
	require.False(t, listIssues.Quarantined, "合法工具不得被隔离")
	assert.False(t, listIssues.Enabled, "D7：新工具默认不启用")

	var illegalName admin.ToolView
	for _, tool := range tools {
		t.Logf("discovered tool: callable=%q raw=%q quarantined=%v reason=%q enabled=%v read_only=%v risk=%s",
			tool.CallableName, tool.RawName, tool.Quarantined, tool.QuarantineReason, tool.Enabled, tool.ReadOnly, tool.Risk)
		if tool.RawName == "bad name!" {
			illegalName = tool
		}
	}
	// 非法字符名：原始名保留用于诊断，投影名必须被规范化成可调用且安全的形态。
	require.NotEmpty(t, illegalName.CallableName, "非法字符名工具仍须被发现（原始名保留）")
	assert.NotContains(t, illegalName.CallableName, " ")
	assert.True(t, strings.HasPrefix(illegalName.CallableName, "mcp__mock__bad_name"),
		"非法字符应被规范化：%s", illegalName.CallableName)

	// 5) 治理：标注只读（D7 默认按写，未标注不进只读工具面）+ 启用 → 工具进入工具面。
	readOnly := true
	_, err = h.adminSvc.SetToolClassification(ctx, actor, created.ID, "mcp__mock__list_issues",
		admin.ClassificationRequest{ReadOnly: &readOnly})
	require.NoError(t, err)
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, created.ID, "mcp__mock__list_issues", true)
	require.NoError(t, err)

	// 6) 只读调用：经 ai.Service（Gate1/Gate2 同源编排）落到真实 mock。
	result, pendingID, err := h.aiSvc.ExecuteTool(ctx, h.tenantID, h.userID, "admin",
		"mcp__mock__list_issues", map[string]any{"state": "open", "token": "s3cr3t-value"})
	require.NoError(t, err)
	require.Equal(t, 0, pendingID, "只读工具不应进入审批")
	require.NotNil(t, result)

	calls := h.mock.Calls()
	require.NotEmpty(t, calls, "调用必须真实到达 mock 服务器")
	assert.Equal(t, "list_issues", calls[len(calls)-1].Tool)
	assert.Equal(t, "open", calls[len(calls)-1].Args["state"])

	// 7) 审计可查：tool_invocations 三元组 + 脱敏 + 耗时。
	invocations, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.ProviderEQ("mcp"), toolinvocation.TenantID(h.tenantID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, invocations, 1)
	invocation := invocations[0]
	assert.Equal(t, "mock", invocation.McpServerName)
	assert.Equal(t, "list_issues", invocation.McpRawToolName)
	assert.Equal(t, "mcp__mock__list_issues", invocation.McpCallableName)
	assert.Equal(t, "executed", invocation.Status)
	assert.Greater(t, invocation.DurationMs, 0, "必须记录耗时")
	assert.Contains(t, invocation.ArgsRedacted, "****", "敏感入参必须掩码")
	assert.NotContains(t, invocation.ArgsRedacted, "s3cr3t-value", "明文不得落审计")

	// 8) 管理操作审计落库（resource=mcp）。
	auditCount, err := h.client.AuditLog.Query().
		Where(auditlog.ResourceEQ("mcp"), auditlog.TenantID(h.tenantID)).
		Count(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, auditCount, 3, "创建/启用/工具治理都应写入审计")

	// 9) 跨租户隔离：另一个租户看不到该服务器与工具。
	otherList, err := h.adminSvc.ListServers(ctx, h.otherActor)
	require.NoError(t, err)
	assert.Empty(t, otherList.Items)
	_, err = h.adminSvc.GetServer(ctx, h.otherActor, created.ID)
	require.Error(t, err)
	adminErr, ok := admin.AsAdminError(err)
	require.True(t, ok)
	assert.Equal(t, 404, adminErr.Status)

	// 10) 输出超限：huge_output 被 provider 截断（同样先标注只读 + 启用）。
	_, err = h.adminSvc.SetToolClassification(ctx, actor, created.ID, "mcp__mock__huge_output",
		admin.ClassificationRequest{ReadOnly: &readOnly})
	require.NoError(t, err)
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, created.ID, "mcp__mock__huge_output", true)
	require.NoError(t, err)
	oversize, _, err := h.aiSvc.ExecuteTool(ctx, h.tenantID, h.userID, "admin", "mcp__mock__huge_output", nil)
	require.NoError(t, err)
	oversizeOutput, ok := oversize.(provider.Output)
	require.True(t, ok, "MCP 结果应规范化为 provider.Output，实际 %T", oversize)
	assert.True(t, oversizeOutput.Truncated, "超过 MaxResultBytes 必须标记截断")
	assert.LessOrEqual(t, oversizeOutput.Bytes, 4096)
}

func TestM0Flow_SSRFBlockedWithStrictGuard(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, ctx)

	mock := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureMinimal})
	httpServer := mock.Start()
	defer httpServer.Close()

	// 严格 guard（生产默认：仅 https + 公网）→ 环回地址必须被拒绝。
	strict := transport.NewSSRFGuard(transport.SSRFConfig{})
	store, err := admin.NewEntStore(h.client)
	require.NoError(t, err)
	credentials, err := admin.NewCredentialService("m0-14-strict-guard-key")
	require.NoError(t, err)
	strictService, err := admin.NewService(admin.Config{
		Client:      h.client,
		Credentials: credentials,
		Manager:     manager.New(manager.Options{Guard: strict}),
		Guard:       strict,
		Store:       store,
		Audit:       admin.NewEntAuditSink(h.client),
	})
	require.NoError(t, err)

	// 配置阶段只做形态校验；出站安全在**建连/请求**时强制执行（transport.New → Guard.Validate），
	// 因此负向断言落在 TestServer（真实发起连接）。
	blocked, err := strictService.CreateServer(ctx, admin.Actor{TenantID: h.tenantID, UserID: h.userID}, admin.CreateServerRequest{
		Name:      "blocked",
		Transport: "streamable",
		URL:       httpServer.URL,
	})
	require.NoError(t, err, "配置阶段不做 DNS 解析（拒绝发生在建连时）")

	result, err := strictService.TestServer(ctx, admin.Actor{TenantID: h.tenantID, UserID: h.userID}, blocked.ID, admin.TestServerRequest{})
	if err != nil {
		adminErr, ok := admin.AsAdminError(err)
		require.True(t, ok, "应为契约错误：%v", err)
		assert.Equal(t, admin.CodeSSRFBlocked, adminErr.Code)
		assert.Equal(t, 422, adminErr.Status)
		assert.NotContains(t, adminErr.Message, "127.0.0.1", "拒绝信息不得暴露内网细节")
		return
	}
	assert.False(t, result.OK, "环回地址必须被出站安全策略拒绝")
	assert.Equal(t, string(admin.CodeSSRFBlocked), result.ErrorCode)
	assert.NotContains(t, result.Message, "127.0.0.1")
}

func TestM0Flow_AuthRequiredOnTestConnection(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, ctx)

	authMock := mockserver.New(mockserver.Config{Fault: mockserver.FaultAuthRequired})
	authHTTP := authMock.Start()
	defer authHTTP.Close()

	parsed, err := url.Parse(authHTTP.URL)
	require.NoError(t, err)
	authPort, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	authGuard := transport.NewSSRFGuard(transport.SSRFConfig{
		AllowHTTP:    true,
		AllowPrivate: true,
		AllowedPorts: []int{authPort},
	})

	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}
	// 允许该端口的测试 guard（复用 harness 的放行策略）。
	service, err := admin.NewService(admin.Config{
		Client:      h.client,
		Credentials: mustCredentials(t),
		Manager:     manager.New(manager.Options{Guard: authGuard, ConnectTimeout: 2 * time.Second}),
		Guard:       authGuard,
		Store:       mustStore(t, h.client),
		Audit:       admin.NewEntAuditSink(h.client),
	})
	require.NoError(t, err)

	created, err := service.CreateServer(ctx, actor, admin.CreateServerRequest{
		Name:      "auth-required",
		Transport: "streamable",
		URL:       authHTTP.URL,
	})
	require.NoError(t, err)

	result, err := service.TestServer(ctx, actor, created.ID, admin.TestServerRequest{})
	if err != nil {
		adminErr, ok := admin.AsAdminError(err)
		require.True(t, ok, "应为契约错误：%v", err)
		assert.Equal(t, admin.CodeAuthRequired, adminErr.Code)
		assert.Contains(t, adminErr.Message, "认证")
		return
	}
	assert.False(t, result.OK)
	assert.Equal(t, "auth_required", result.ErrorCode)
}

// —— 脚手架 ——

func newHarness(t *testing.T, ctx context.Context) *harness {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "mcp-m0-14.db") + "?_fk=1&_busy_timeout=15000&_journal_mode=WAL"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	// 租户/用户：tool_invocations.user_id 是外键，需真实行。
	tenant, err := client.Tenant.Create().SetCode("m0-14").SetName("M0-14").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("m0-14-user").
		SetEmail("m0-14@example.com").
		SetName("M0-14 User").
		SetPasswordHash("x").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	otherTenant, err := client.Tenant.Create().SetCode("m0-14-other").SetName("M0-14 Other").Save(ctx)
	require.NoError(t, err)

	// mock MCP 服务器（default 夹具；超长输出 512KiB 供截断断言使用）。
	mock := mockserver.New(mockserver.Config{Name: "itsm-mcp-mock", Fixture: mockserver.FixtureDefault})
	mockHTTP := mock.Start()
	// 收尾顺序（LIFO：后注册先执行）：务必先关闭 MCP 会话再关 mock HTTP，
	// 否则长驻 SSE 流会让 httptest.Server.Close 永久阻塞。
	t.Cleanup(mockHTTP.Close)

	// 出站安全：放行环回 + 本端口（仅测试；严格模式见 TestM0Flow_SSRFBlockedWithStrictGuard）。
	parsed, err := url.Parse(mockHTTP.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	guard := transport.NewSSRFGuard(transport.SSRFConfig{
		AllowHTTP:    true,
		AllowPrivate: true,
		AllowedPorts: []int{port},
	})

	credentials, err := admin.NewCredentialService("m0-14-integration-key")
	require.NoError(t, err)
	store, err := admin.NewEntStore(client)
	require.NoError(t, err)
	events := admin.NewEventBuffer(0)

	mgr := manager.New(manager.Options{
		Guard:          guard,
		StatusWriter:   store,
		ToolCache:      store,
		Events:         events,
		ConnectTimeout: 3 * time.Second,
		CallTimeout:    3 * time.Second,
		HealthInterval: time.Hour, // 稳态用例不依赖周期健康检查
	})
	mgr.Start(ctx)

	adminSvc, err := admin.NewService(admin.Config{
		Client:      client,
		Credentials: credentials,
		Manager:     mgr,
		Guard:       guard,
		Store:       store,
		Audit:       admin.NewEntAuditSink(client),
		Events:      events,
	})
	require.NoError(t, err)

	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(provider.New(client, mgr, provider.Options{Enabled: true, MaxResultBytes: 4096}))

	repo := ai.NewEntRepository(client)
	aiSvc := ai.NewService(repo, zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	aiSvc.SetEntClient(client)

	// 权限模式：HardcodeOnly（无 DB 会话），角色矩阵取 M0-10 的硬编码表。
	previousMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	ai.ResetRBACFlagForTest()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = previousMode
		middleware.InvalidateAllPermissionCaches()
		ai.ResetRBACFlagForTest()
	})

	h := &harness{
		client:     client,
		adminSvc:   adminSvc,
		manager:    mgr,
		registry:   registry,
		aiSvc:      aiSvc,
		mock:       mock,
		mockHTTP:   mockHTTP,
		events:     events,
		guard:      guard,
		tenantID:   tenant.ID,
		userID:     user.ID,
		otherActor: admin.Actor{TenantID: otherTenant.ID, UserID: user.ID},
	}
	// 收尾顺序（LIFO：后注册先执行）：先关 MCP 会话再关 mock HTTP，避免 SSE 流阻塞 Close。
	t.Cleanup(func() {
		h.mu.Lock()
		ids := append([]int(nil), h.tracked...)
		h.mu.Unlock()
		for _, id := range ids {
			mgr.Remove(id)
		}
		mgr.Stop()
	})
	return h
}

func mustCredentials(t *testing.T) *admin.CredentialService {
	t.Helper()
	credentials, err := admin.NewCredentialService("m0-14-auth-required-key")
	require.NoError(t, err)
	return credentials
}

func mustStore(t *testing.T, client *ent.Client) *admin.EntStore {
	t.Helper()
	store, err := admin.NewEntStore(client)
	require.NoError(t, err)
	return store
}

func keys(m map[string]admin.ToolView) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
