// B2-05 授权负向安全集（handler 内部包，直接覆盖不可导出判定）：
//
//	① 未授权工具不可见（下发面 chatToolDecision）+ 不可调用（执行面 Decide/Gate2.5）；
//	② 跨租户 fail-closed（ValidateBotSelection，显式 botId 必须命中本租户模板）；
//	③ 黑名单永不下发（admin/permission/删除/任意 IO）；
//	④ 模型伪造身份/租户/权限参数被剥离且审计留痕（args_stripped）。
package ai

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// blacklistProbeProvider 暴露一个可配置的工具面（只读，指向上表），用于下发面判定。
type blacklistProbeProvider struct {
	defs    []service.ToolDefinition
	lastArg map[string]interface{}
}

func (p *blacklistProbeProvider) ProviderName() string { return "probe" }

func (p *blacklistProbeProvider) ListTools(_ context.Context, _ int) []service.ToolDefinition {
	return p.defs
}

func (p *blacklistProbeProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	for i := range p.defs {
		if p.defs[i].Name == name {
			def := p.defs[i]
			return &def, true
		}
	}
	return nil, false
}

func (p *blacklistProbeProvider) Execute(_ context.Context, _ int, _ string, args map[string]interface{}) (*service.ToolExecution, error) {
	p.lastArg = args
	return &service.ToolExecution{Provider: "builtin", Value: map[string]interface{}{"ok": true}}, nil
}

type b2NegativeEnv struct {
	client   *ent.Client
	svc      *Service
	registry *service.ToolRegistry
	probe    *blacklistProbeProvider
	admin    *bot.TemplateAdmin
}

func newB2NegativeEnv(t *testing.T) *b2NegativeEnv {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bot-negative.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	probe := &blacklistProbeProvider{defs: []service.ToolDefinition{
		{Name: "admin_export_users", ReadOnly: true, Resource: "admin", Action: "read", Provider: "probe", Risk: "read"},
		{Name: "http_request", ReadOnly: true, Resource: "net", Action: "read", Provider: "probe", Risk: "read"},
		{Name: "delete_ticket", ReadOnly: false, Resource: "ticket", Action: "write", Provider: "probe", Risk: "act_low"},
		{Name: "demo_read_probe", ReadOnly: true, Resource: "incident", Action: "read", Provider: "probe", Risk: "read"},
	}}
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(probe)

	svc := NewService(NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)

	return &b2NegativeEnv{client: client, svc: svc, registry: registry, probe: probe, admin: bot.NewTemplateAdmin(client)}
}

func (e *b2NegativeEnv) createTenant(t *testing.T, code string) *ent.Tenant {
	t.Helper()
	return e.client.Tenant.Create().SetName(code).SetCode(code).SetDomain(code + ".test").SaveX(context.Background())
}

// 下发面：黑名单命中即不可见——无论关闭态（兼容默认）还是开启态（即使显式授权）。
func TestChatToolDecisionBlacklistNeverDispatched(t *testing.T) {
	env := newB2NegativeEnv(t)
	ctx := context.Background()
	tenant := env.createTenant(t, "neg-face")
	td := func(name string) service.ToolDefinition {
		def := env.registry.GetToolForTenant(ctx, tenant.ID, name)
		require.NotNilf(t, def, "工具应可解析：%s", name)
		return *def
	}

	// ① 关闭态（未注入 Policy，bot.enabled=false 路径）：黑名单仍然拦截。
	for _, name := range []string{"admin_export_users", "http_request", "delete_ticket"} {
		allowed, reason, _ := env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td(name), true, nil)
		assert.Falsef(t, allowed, "关闭态下黑名单工具不得下发：%s", name)
		assert.Containsf(t, reason, bot.ReasonToolBlacklisted, "拒绝原因应含黑名单码：%s", name)
	}
	// 业务只读工具在关闭态照常下发（证明黑名单没有扩大打击面）。
	allowed, reason, legacy := env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td("get_incident_stats"), true, nil)
	require.True(t, allowed, "业务只读工具应放行，reason=%s", reason)
	assert.True(t, legacy, "关闭态必须走兼容默认标记")

	// ② 开启态 + 显式授权：黑名单仍优先于授权。
	env.svc.SetBotPolicy(bot.NewPolicy(env.client))
	tpl, err := env.admin.CreateTemplate(ctx, tenant.ID, bot.TemplateInput{
		Slug: "ops-helper", Name: "运维助手", RiskLimit: bot.RiskActHigh,
		Status: bot.StatusGA, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)
	for _, name := range []string{"admin_export_users", "http_request", "delete_ticket"} {
		_, err = env.admin.UpsertGrant(ctx, tenant.ID, tpl.ID, bot.GrantInput{ToolName: name, RiskLimit: bot.RiskActHigh})
		require.NoErrorf(t, err, "显式授权应可写入（黑名单在执行/下发面才拦）：%s", name)
	}
	snapshot, err := bot.NewPolicy(env.client).SnapshotForBot(ctx, tenant.ID, tpl.ID)
	require.NoError(t, err)

	for _, name := range []string{"admin_export_users", "http_request", "delete_ticket"} {
		allowed, reason, _ := env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td(name), true, snapshot)
		assert.Falsef(t, allowed, "开启态下即使显式授权也不得下发：%s", name)
		assert.Containsf(t, reason, bot.ReasonToolBlacklisted, "拒绝原因应含黑名单码：%s", name)
	}
	// 未授权的业务工具 → tool_not_granted（与黑名单原因可区分）。
	allowed, reason, _ = env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td("create_ticket"), true, snapshot)
	assert.False(t, allowed)
	assert.Contains(t, reason, bot.ReasonToolNotGranted)
	// 已授权的业务工具 → 放行。
	_, err = env.admin.UpsertGrant(ctx, tenant.ID, tpl.ID, bot.GrantInput{ToolName: "create_ticket", RiskLimit: bot.RiskActLow})
	require.NoError(t, err)
	snapshot, err = bot.NewPolicy(env.client).SnapshotForBot(ctx, tenant.ID, tpl.ID)
	require.NoError(t, err)
	allowed, reason, _ = env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td("create_ticket"), true, snapshot)
	assert.True(t, allowed, "已授权业务工具应放行，reason=%s", reason)

	// ③ 快照不可用（policyReady=false）→ fail-closed 全拒。
	allowed, reason, _ = env.svc.chatToolDecision(ctx, tenant.ID, "super_admin", td("get_incident_stats"), false, nil)
	assert.False(t, allowed, "策略快照不可用时必须 fail-closed")
	assert.Equal(t, bot.ReasonSnapshotError, reason)
}

// 跨租户：显式 botId 未命中本租户 → ErrBotSelectionNotFound（不静默降级到兼容默认）。
func TestValidateBotSelectionCrossTenantFailClosed(t *testing.T) {
	env := newB2NegativeEnv(t)
	ctx := context.Background()
	tenantA := env.createTenant(t, "neg-a")
	tenantB := env.createTenant(t, "neg-b")

	tpl, err := env.admin.CreateTemplate(ctx, tenantA.ID, bot.TemplateInput{
		Slug: "a-bot", Name: "A Bot", RiskLimit: bot.RiskActLow,
		Status: bot.StatusGA, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)

	// 关闭态（未注入 Policy）：不校验，保持既有行为逐字节不变。
	bogusCtx := WithBotID(ctx, 99999)
	assert.NoError(t, env.svc.ValidateBotSelection(bogusCtx, tenantA.ID))

	env.svc.SetBotPolicy(bot.NewPolicy(env.client))

	// 同租户命中 → 通过；缺省（0）→ 通过。
	assert.NoError(t, env.svc.ValidateBotSelection(WithBotID(ctx, tpl.ID), tenantA.ID))
	assert.NoError(t, env.svc.ValidateBotSelection(ctx, tenantA.ID))

	// 跨租户引用 → 404 语义（fail-closed）。
	err = env.svc.ValidateBotSelection(WithBotID(ctx, tpl.ID), tenantB.ID)
	assert.ErrorIs(t, err, ErrBotSelectionNotFound)

	// 不存在的 ID → 同样是 NotFound（不降级）。
	err = env.svc.ValidateBotSelection(WithBotID(ctx, tpl.ID+1000), tenantA.ID)
	assert.ErrorIs(t, err, ErrBotSelectionNotFound)
}

// 参数守卫：身份/租户/权限与入口协议键被剥离（大小写与变体归一），业务键保留，留痕键名排序稳定。
//
// 注：`target_id`/`target_type`/`entrypoint` 自 B3-01 起属**入口协议键**（由 ScopeResolver
// 以服务端解析值覆写，见 scope_test.go），不再随业务参数透传——本用例已相应更新。
func TestSanitizeReservedArgs(t *testing.T) {
	in := map[string]interface{}{
		"tenant_id": 999, "TenantID": 999, "user_id": 1, "userId": 1,
		"Role": "super_admin", "is_admin": true,
		"title": "打印机故障", "assignee_id": 7, "priority": "high",
	}
	cleaned, stripped := sanitizeReservedArgs(in)
	require.Len(t, cleaned, 3, "只保留业务键：%v", cleaned)
	assert.Equal(t, "打印机故障", cleaned["title"])
	assert.Equal(t, 7, cleaned["assignee_id"])
	assert.Equal(t, "high", cleaned["priority"])
	assert.Equal(t, []string{"Role", "TenantID", "is_admin", "tenant_id", "userId", "user_id"}, stripped)
	assert.Equal(t, "args_stripped:Role,TenantID,is_admin,tenant_id,userId,user_id", argsStrippedMarker(stripped))

	// 无保留键：原样返回（零分配、语义不变）。
	clean := map[string]interface{}{"title": "x"}
	got, stripped := sanitizeReservedArgs(clean)
	assert.Empty(t, stripped)
	assert.Equal(t, clean["title"], got["title"])
	assert.Equal(t, "", argsStrippedMarker(nil))

	// 空 map / nil。
	got, stripped = sanitizeReservedArgs(nil)
	assert.Nil(t, got)
	assert.Nil(t, stripped)
}

// 执行面：模型伪造身份键被剥离（执行器拿不到），审计落 args_stripped 留痕。
func TestExecuteToolStripsIdentityArgsAndAudits(t *testing.T) {
	env := newB2NegativeEnv(t)
	ctx := context.Background()
	tenant := env.createTenant(t, "neg-args")
	// 真实用户：审计表 user_id 有外键约束（sqlite `_fk=1`）。
	user := env.client.User.Create().
		SetUsername("neg-user").SetEmail("neg-user@example.com").SetName("Neg User").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)
	userID := user.ID

	_, _, err := env.svc.ExecuteTool(ctx, userID, tenant.ID, "super_admin", "demo_read_probe", map[string]interface{}{
		"tenant_id": 999, "user_id": 1234, "role": "super_admin",
		"limit": 10,
	})
	require.NoError(t, err)

	// 执行器只拿到业务参数（身份/租户一律由服务端上下文提供）。
	require.NotNil(t, env.probe.lastArg)
	assert.NotContains(t, env.probe.lastArg, "tenant_id")
	assert.NotContains(t, env.probe.lastArg, "user_id")
	assert.NotContains(t, env.probe.lastArg, "role")
	assert.Equal(t, 10, env.probe.lastArg["limit"])

	rows, err := env.client.ToolInvocation.Query().All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	row := rows[len(rows)-1]
	assert.Contains(t, row.PermissionReason, "args_stripped:")
	assert.Contains(t, row.PermissionReason, "role")
	assert.Contains(t, row.PermissionReason, "tenant_id")
	assert.Contains(t, row.PermissionReason, "user_id")
	assert.NotContains(t, row.ArgsRedacted, "999", "被剥离的身份键不得出现在快照里")
	assert.Contains(t, row.ArgsRedacted, "limit")
}
