// B2-06 B2 集成验收（AB2-06）：2 角色 × 2 入口下发矩阵 + 审计证据 + 跨租户 404 契约。
//
// 组织方式：
//  1. 下发/执行共用判定面：bot.Decide（`Service.chatToolDecision` 与 Gate 2.5 同源委托），
//     角色差异由 RBAC 回调表达（生产回调来源 `rbacAllowedFunc`，单一来源）；
//  2. 审计证据：执行面真实调用 `Service.ExecuteTool`，核对 `permission_check`/`permission_reason`；
//  3. HTTP 契约：跨租户 `botId` → 404 `BOT_NOT_FOUND`（B2-05 遗留项在本任务闭环）。
package botintegration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	ai "itsm-backend/handlers/ai"
	"itsm-backend/middleware"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// ---------------------------------------------------------------------------
// 验收环境
// ---------------------------------------------------------------------------

type matrixTool struct {
	name     string
	readOnly bool
	risk     string
	resource string
	action   string
}

// 工具面（名称避开内置注册表，便于执行面路由到探针 provider 而不触达真实业务服务）。
var matrixTools = []matrixTool{
	{"probe_list_tickets", true, bot.RiskRead, "ticket", "read"},
	{"probe_create_note", false, bot.RiskActLow, "ticket", "write"},
	{"probe_bulk_archive", false, bot.RiskActHigh, "ticket", "write"},
	{"probe_update_note", false, bot.RiskActLow, "ticket", "write"},
	{"delete_probe_item", false, bot.RiskActLow, "ticket", "write"}, // 命中 destructive 黑名单
	{"admin_probe_users", true, bot.RiskRead, "admin", "read"},      // 命中 admin_surface 黑名单
	{"http_probe_fetch", true, bot.RiskRead, "net", "read"},         // 命中 arbitrary_io 黑名单
}

type b2Harness struct {
	client   *ent.Client
	svc      *ai.Service
	admin    *bot.TemplateAdmin
	probe    *b2ProbeProvider
	tenantID int
	userID   int
}

type b2ProbeProvider struct {
	defs []service.ToolDefinition
}

func (p *b2ProbeProvider) ProviderName() string { return "probe" }

func (p *b2ProbeProvider) ListTools(_ context.Context, _ int) []service.ToolDefinition { return p.defs }

func (p *b2ProbeProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	for i := range p.defs {
		if p.defs[i].Name == name {
			def := p.defs[i]
			return &def, true
		}
	}
	return nil, false
}

func (p *b2ProbeProvider) Execute(_ context.Context, _ int, _ string, _ map[string]interface{}) (*service.ToolExecution, error) {
	return &service.ToolExecution{Provider: "probe", Value: map[string]interface{}{"ok": true}}, nil
}

func newB2Harness(t *testing.T) *b2Harness {
	t.Helper()
	ctx := context.Background()

	dsn := filepath.Join(t.TempDir(), "bot-b2-acceptance.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("b2-accept").SetName("B2 验收").SaveX(ctx)
	user := client.User.Create().
		SetUsername("b2-accept-user").SetEmail("b2-accept@example.com").SetName("B2 User").
		SetPasswordHash("x").SetTenantID(tenant.ID).SaveX(ctx)

	probe := &b2ProbeProvider{}
	for _, tool := range matrixTools {
		probe.defs = append(probe.defs, service.ToolDefinition{
			Name: tool.name, ReadOnly: tool.readOnly, Resource: tool.resource, Action: tool.action,
			Provider: "probe", Risk: tool.risk, Category: "ticket",
		})
	}
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(probe)

	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
	})

	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	svc.SetBotPolicy(bot.NewPolicy(client))

	return &b2Harness{client: client, svc: svc, admin: bot.NewTemplateAdmin(client), probe: probe, tenantID: tenant.ID, userID: user.ID}
}

// createTemplate 建模板并授权（grants = toolName → riskLimit）。
func (h *b2Harness) createTemplate(t *testing.T, slug, status, riskLimit string, entrypoints []string, grants map[string]string) *ent.BotTemplate {
	t.Helper()
	ctx := context.Background()
	tpl, err := h.admin.CreateTemplate(ctx, h.tenantID, bot.TemplateInput{
		Slug: slug, Name: slug, RiskLimit: riskLimit, Status: status, Entrypoints: entrypoints,
	})
	require.NoError(t, err)
	for name, risk := range grants {
		_, err = h.admin.UpsertGrant(ctx, h.tenantID, tpl.ID, bot.GrantInput{ToolName: name, RiskLimit: risk})
		require.NoErrorf(t, err, "授权失败：%s", name)
	}
	return tpl
}

func (h *b2Harness) snapshot(t *testing.T, botID int) *bot.Snapshot {
	t.Helper()
	snapshot, err := bot.NewPolicy(h.client).SnapshotForBot(context.Background(), h.tenantID, botID)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	return snapshot
}

// 角色 → RBAC 回调（生产侧同源：handlers/ai.rbacAllowedFunc → middleware.HasResourcePermission）。
func rbacAllowing(actions map[string]bool) func(string, string) bool {
	return func(resource, action string) bool {
		return actions[resource+":"+action]
	}
}

var (
	roleOperatorFull = rbacAllowing(map[string]bool{"ticket:read": true, "ticket:write": true})
	roleViewerRead   = rbacAllowing(map[string]bool{"ticket:read": true})
)

func meta(name string) bot.ToolMeta {
	for _, tool := range matrixTools {
		if tool.name == name {
			return bot.ToolMeta{
				Name: tool.name, Provider: "probe", ReadOnly: tool.readOnly,
				Resource: tool.resource, Action: tool.action, Risk: tool.risk,
			}
		}
	}
	panic("未知工具：" + name)
}

// ---------------------------------------------------------------------------
// ① 下发矩阵：2 角色 × 2 入口（含黑名单优先级与风险上限）
// ---------------------------------------------------------------------------

// 授权：list(read)、create_note(act_low)、bulk_archive(act_low 上限→工具 act_high 超限)。
var acceptanceGrants = map[string]string{
	"probe_list_tickets": bot.RiskRead,
	"probe_create_note":  bot.RiskActLow,
	"probe_bulk_archive": bot.RiskActLow,
}

func TestB2Matrix_RoleByEntrypointDispatch(t *testing.T) {
	h := newB2Harness(t)
	tpl := h.createTemplate(t, "matrix-chat", bot.StatusGA, bot.RiskActHigh, []string{bot.EntrypointChat}, acceptanceGrants)
	snapshot := h.snapshot(t, tpl.ID)

	type expectation struct {
		allowed bool
		reason  string
	}
	type cell struct {
		role       string
		rbac       func(string, string) bool
		entrypoint string
		want       map[string]expectation
	}

	// 期望表（可读性优先：按工具逐行列出，便于逐格核对）。
	chatOperator := map[string]expectation{
		"probe_list_tickets": {true, ""},
		"probe_create_note":  {true, ""},
		"probe_bulk_archive": {false, bot.ReasonRiskExceeded}, // 工具 act_high > 授权上限 act_low
		"probe_update_note":  {false, bot.ReasonToolNotGranted},
		"delete_probe_item":  {false, bot.ReasonToolBlacklisted + ":destructive"},
		"admin_probe_users":  {false, bot.ReasonToolBlacklisted + ":admin_surface"},
		"http_probe_fetch":   {false, bot.ReasonToolBlacklisted + ":arbitrary_io"},
	}
	chatViewer := map[string]expectation{
		"probe_list_tickets": {true, ""},
		"probe_create_note":  {false, bot.ReasonRBACDenied}, // 只读角色：写工具 RBAC 拒
		"probe_bulk_archive": {false, bot.ReasonRiskExceeded},
		"probe_update_note":  {false, bot.ReasonToolNotGranted},
		"delete_probe_item":  {false, bot.ReasonToolBlacklisted + ":destructive"},
		"admin_probe_users":  {false, bot.ReasonToolBlacklisted + ":admin_surface"},
		"http_probe_fetch":   {false, bot.ReasonToolBlacklisted + ":arbitrary_io"},
	}
	// 入口不在模板 entrypoints 内：全部入口拒绝；黑名单仍先行（原因码可区分）。
	ticketDetail := map[string]expectation{
		"probe_list_tickets": {false, bot.ReasonEntrypointDenied},
		"probe_create_note":  {false, bot.ReasonEntrypointDenied},
		"probe_bulk_archive": {false, bot.ReasonEntrypointDenied},
		"probe_update_note":  {false, bot.ReasonEntrypointDenied},
		"delete_probe_item":  {false, bot.ReasonToolBlacklisted + ":destructive"},
		"admin_probe_users":  {false, bot.ReasonToolBlacklisted + ":admin_surface"},
		"http_probe_fetch":   {false, bot.ReasonToolBlacklisted + ":arbitrary_io"},
	}

	cells := []cell{
		{"operator_full(act)", roleOperatorFull, bot.EntrypointChat, chatOperator},
		{"viewer_readonly(act)", roleViewerRead, bot.EntrypointChat, chatViewer},
		{"operator_full(act)", roleOperatorFull, "ticket_detail", ticketDetail},
		{"viewer_readonly(act)", roleViewerRead, "ticket_detail", ticketDetail},
	}

	for _, tc := range cells {
		for _, tool := range matrixTools {
			t.Run(tc.role+"/"+tc.entrypoint+"/"+tool.name, func(t *testing.T) {
				decision := bot.Decide(bot.CheckInput{
					Snapshot: snapshot, Tool: meta(tool.name),
					Entrypoint: tc.entrypoint, RBACAllowed: tc.rbac,
				})
				want := tc.want[tool.name]
				assert.Equalf(t, want.allowed, decision.Allowed, "可见性不符：reason=%s", decision.Reason)
				if !want.allowed {
					assert.Equal(t, want.reason, decision.Reason)
				}
			})
		}
	}
}

// ② draft 模板 + 兼容默认（关闭态/未配置授权）回归。
func TestB2Matrix_StatusDraftAndLegacyDefault(t *testing.T) {
	h := newB2Harness(t)

	// draft：即使已配置授权，一律不下发（黑名单仍先行）。
	draft := h.createTemplate(t, "matrix-draft", bot.StatusDraft, bot.RiskActHigh, []string{bot.EntrypointChat}, acceptanceGrants)
	draftSnapshot := h.snapshot(t, draft.ID)
	for _, tool := range matrixTools {
		decision := bot.Decide(bot.CheckInput{Snapshot: draftSnapshot, Tool: meta(tool.name), Entrypoint: bot.EntrypointChat, RBACAllowed: roleOperatorFull})
		assert.Falsef(t, decision.Allowed, "draft 模板不得下发：%s", tool.name)
		if strings.HasPrefix(tool.name, "delete_") || strings.HasPrefix(tool.name, "admin_") || strings.HasPrefix(tool.name, "http_") {
			assert.Containsf(t, decision.Reason, bot.ReasonToolBlacklisted, "黑名单应先于草稿判定：%s", tool.name)
			continue
		}
		assert.Equalf(t, bot.ReasonStatusDraft, decision.Reason, "非黑名单工具应草稿拒：%s", tool.name)
	}

	// 兼容默认（未配置任何授权）：只读 ∪ 遗留写白名单，黑名单不得放行。
	legacy := h.createTemplate(t, "matrix-legacy", bot.StatusGA, bot.RiskActHigh, []string{bot.EntrypointChat}, nil)
	legacySnapshot := h.snapshot(t, legacy.ID)
	require.NotNil(t, legacySnapshot)
	assert.Empty(t, legacySnapshot.Grants, "未配置授权的快照应无授权项")

	cases := []struct {
		tool       string
		allowed    bool
		reason     string
		wantLegacy bool
	}{
		{"probe_list_tickets", true, bot.ReasonLegacyDefault, true},  // 只读放行
		{"probe_update_note", false, bot.ReasonToolNotGranted, true}, // 写工具不在白名单
		// 黑名单是硬门禁（不属兼容默认路径，Legacy=false），且先于兼容默认判定。
		{"admin_probe_users", false, bot.ReasonToolBlacklisted + ":admin_surface", false},
		{"http_probe_fetch", false, bot.ReasonToolBlacklisted + ":arbitrary_io", false},
	}
	for _, tc := range cases {
		decision := bot.Decide(bot.CheckInput{Snapshot: legacySnapshot, Tool: meta(tc.tool), Entrypoint: bot.EntrypointChat, RBACAllowed: roleOperatorFull})
		assert.Equalf(t, tc.allowed, decision.Allowed, "兼容默认判定不符：%s", tc.tool)
		assert.Equalf(t, tc.reason, decision.Reason, "兼容默认原因不符：%s", tc.tool)
		assert.Equalf(t, tc.wantLegacy, decision.Legacy, "Legacy 标记不符：%s", tc.tool)
	}
}

// ---------------------------------------------------------------------------
// ③ 审计证据：执行面 allow/deny 的 permission_check / permission_reason 快照
// ---------------------------------------------------------------------------

func TestB2AuditEvidence_ExecutionFace(t *testing.T) {
	h := newB2Harness(t)
	ctx := context.Background()
	tpl := h.createTemplate(t, "audit-face", bot.StatusGA, bot.RiskActHigh, []string{bot.EntrypointChat}, acceptanceGrants)
	h.svc.SetBotIDResolver(func(context.Context, int, int) int { return tpl.ID })

	// 允许：授权只读工具 → executed + passed。
	_, _, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "probe_list_tickets", nil)
	require.NoError(t, err)

	// 拒绝 1：未授权写工具。
	_, _, err = h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "probe_update_note", nil)
	require.Error(t, err)

	// 拒绝 2：黑名单工具（即使模板已显式授权）。
	_, _, err = h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "admin_probe_users", nil)
	require.Error(t, err)

	rows, err := h.client.ToolInvocation.Query().All(ctx)
	require.NoError(t, err)
	byTool := make(map[string]*ent.ToolInvocation, len(rows))
	for _, row := range rows {
		byTool[row.ToolName] = row
	}

	require.Contains(t, byTool, "probe_list_tickets")
	assert.Equal(t, "executed", byTool["probe_list_tickets"].Status)
	assert.Equal(t, "passed", byTool["probe_list_tickets"].PermissionCheck)

	require.Contains(t, byTool, "probe_update_note")
	assert.Equal(t, "denied", byTool["probe_update_note"].PermissionCheck)
	assert.Contains(t, byTool["probe_update_note"].PermissionReason, bot.ReasonToolNotGranted)

	require.Contains(t, byTool, "admin_probe_users")
	assert.Equal(t, "denied", byTool["admin_probe_users"].PermissionCheck)
	assert.Contains(t, byTool["admin_probe_users"].PermissionReason, bot.ReasonToolBlacklisted)
	assert.Contains(t, byTool["admin_probe_users"].PermissionReason, "admin_surface")
}

// ---------------------------------------------------------------------------
// ④ HTTP 契约：跨租户 botId → 404 BOT_NOT_FOUND（B2-05 遗留项闭环）
// ---------------------------------------------------------------------------

func TestB2HTTP_CrossTenantBotSelection404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newB2Harness(t)
	ctx := context.Background()

	otherTenant := h.client.Tenant.Create().SetCode("b2-other").SetName("B2 Other").SaveX(ctx)
	otherTpl, err := bot.NewTemplateAdmin(h.client).CreateTemplate(ctx, otherTenant.ID, bot.TemplateInput{
		Slug: "other-bot", Name: "Other Bot", RiskLimit: bot.RiskActLow,
		Status: bot.StatusGA, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", h.tenantID)
		c.Set("user_id", h.userID)
		c.Set("role", "super_admin")
		c.Next()
	})
	router.POST("/api/v1/ai/chat", ai.NewHandler(h.svc).Chat)

	body := `{"query":"你好","botId":` + strconv.Itoa(otherTpl.ID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "跨租户 botId 必须 404（不得静默降级）：%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "BOT_NOT_FOUND")
}
