package ai_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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

// B2-02 执行面测试：Gate 2.5 二次校验（授权 ∩ RBAC ∩ 风险上限 ∩ 入口）+ 审计字段 +
// 兼容默认（未配置授权 = 既有行为）。

type botPolicyEnv struct {
	svc   *ai.Service
	repo  *rbacMockRepo
	admin *bot.TemplateAdmin
	prev  middleware.PermissionConfigMode
}

func newBotPolicyEnv(t *testing.T) *botPolicyEnv {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bot-policy-exec.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	repo := &rbacMockRepo{}
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	svc := ai.NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	svc.SetBotPolicy(bot.NewPolicy(client))

	env := &botPolicyEnv{svc: svc, repo: repo, admin: bot.NewTemplateAdmin(client), prev: middleware.PermissionConfig.Mode}
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	ai.ResetRBACFlagForTest()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = env.prev
		middleware.InvalidateAllPermissionCaches()
		ai.ResetRBACFlagForTest()
	})
	return env
}

func (e *botPolicyEnv) gaTemplateWithGrant(t *testing.T, tenantID int, entrypoints []string, grantTool string, grantRisk string) *ent.BotTemplate {
	t.Helper()
	tpl, err := e.admin.CreateTemplate(context.Background(), tenantID, bot.TemplateInput{
		Slug: "ops-helper", Name: "运维助手", RiskLimit: bot.RiskActMedium,
		Status: bot.StatusGA, Entrypoints: entrypoints,
	})
	require.NoError(t, err)
	_, err = e.admin.UpsertGrant(context.Background(), tenantID, tpl.ID, bot.GrantInput{
		ToolName: grantTool, RiskLimit: grantRisk,
	})
	require.NoError(t, err)
	// B2-04 尚未落地会话→Bot 绑定，这里用解析器模拟「当前会话归属该模板」。
	e.bindBot(t, tpl.ID)
	return tpl
}

// bindBot 让服务把「本次调用」解析到指定模板（等价 B2-04 落库后的会话归属查询）。
func (e *botPolicyEnv) bindBot(t *testing.T, botID int) {
	t.Helper()
	e.svc.SetBotIDResolver(func(context.Context, int, int) int { return botID })
}

// TestExecuteTool_BotPolicyDeniesUngrantedTool 严格模式下未授权工具在执行面被拒，
// 且审计落 permission_check=denied / permission_reason。
func TestExecuteTool_BotPolicyDeniesUngrantedTool(t *testing.T) {
	env := newBotPolicyEnv(t)
	// 只授权 list_tickets；create_ticket 未授权 → 执行拒绝。
	env.gaTemplateWithGrant(t, 1, []string{bot.EntrypointChat}, "list_tickets", bot.RiskRead)

	_, _, err := env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ai.ErrToolPermissionDenied)
	assert.Contains(t, err.Error(), bot.ReasonToolNotGranted)

	inv := env.repo.lastInvocation()
	require.NotNil(t, inv, "拒绝也必须落审计")
	assert.Equal(t, "denied", inv.PermissionCheck)
	assert.Contains(t, inv.PermissionReason, bot.ReasonToolNotGranted)
}

// TestExecuteTool_BotPolicyAllowsGrantedAndRiskBounded 已授权且在风险上限内的写工具
// 照常进入审批流（Gate3）；风险超限则在执行面被拒。
func TestExecuteTool_BotPolicyAllowsGrantedAndRiskBounded(t *testing.T) {
	env := newBotPolicyEnv(t)
	// 模板风险上限 act_medium；授权 create_ticket=act_low。
	env.gaTemplateWithGrant(t, 1, []string{bot.EntrypointChat}, "create_ticket", bot.RiskActLow)

	_, pendingID, err := env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "策略放行用例"})
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0, "写工具经审批流返回 pending id")

	// 风险超限：update_ticket 的元数据风险高于授权上限（此处用授权上限 read 反证）。
	env2 := newBotPolicyEnv(t)
	env2.gaTemplateWithGrant(t, 1, []string{bot.EntrypointChat}, "create_ticket", bot.RiskRead)
	_, _, err = env2.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ai.ErrToolPermissionDenied)
	assert.Contains(t, err.Error(), bot.ReasonRiskExceeded)
}

// TestExecuteTool_BotPolicyEntrypointAndDraftDeny 入口与状态的拒绝路径。
func TestExecuteTool_BotPolicyEntrypointAndDraftDeny(t *testing.T) {
	// ① 入口不匹配（模板只允许 ticket 入口，聊天调用被拒）。
	env := newBotPolicyEnv(t)
	env.gaTemplateWithGrant(t, 1, []string{"ticket"}, "create_ticket", bot.RiskActLow)
	_, _, err := env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bot.ReasonEntrypointDenied)

	// ② draft 模板不下发。
	env2 := newBotPolicyEnv(t)
	tpl, err := env2.admin.CreateTemplate(context.Background(), 1, bot.TemplateInput{
		Slug: "draft-bot", Name: "草稿", RiskLimit: bot.RiskActMedium,
		Status: bot.StatusDraft, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)
	_, err = env2.admin.UpsertGrant(context.Background(), 1, tpl.ID, bot.GrantInput{
		ToolName: "create_ticket", RiskLimit: bot.RiskActLow,
	})
	require.NoError(t, err)
	env2.bindBot(t, tpl.ID)
	_, _, err = env2.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bot.ReasonStatusDraft)
}

// TestExecuteTool_BotPolicyLegacyCompat 未配置任何授权（含未命中模板）时行为与既有版本一致：
// 写白名单工具照常进入审批流，非白名单写工具仍被拒（原因码为兼容默认/tool_not_granted）。
func TestExecuteTool_BotPolicyLegacyCompat(t *testing.T) {
	env := newBotPolicyEnv(t)
	// 不建任何模板/授权 → 兼容默认。
	_, pendingID, err := env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "兼容默认可写"})
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0)

	// 模板存在但零授权（管理员刚建模板尚未授权）→ 仍是兼容默认（显式绑定到该模板验证）。
	tpl, err := env.admin.CreateTemplate(context.Background(), 1, bot.TemplateInput{
		Slug: "unconfigured", Name: "未配置", RiskLimit: bot.RiskActMedium,
		Status: bot.StatusGA, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)
	env.bindBot(t, tpl.ID)
	_, pendingID, err = env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "零授权仍兼容"})
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0, "零授权的模板 = 未上线策略，行为不变")
}

// TestExecuteTool_BotPolicyNilKeepsLegacy 未注入 Policy（bot.enabled=false）时 Gate 2.5 完全跳过。
func TestExecuteTool_BotPolicyNilKeepsLegacy(t *testing.T) {
	env := newBotPolicyEnv(t)
	env.svc.SetBotPolicy(nil) // 关闭态

	_, pendingID, err := env.svc.ExecuteTool(context.Background(), 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "关闭态"})
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0)

	inv := env.repo.lastInvocation()
	require.NotNil(t, inv)
	assert.NotEqual(t, "denied", inv.PermissionCheck, "关闭态不得产生策略拒绝")
	assert.NotContains(t, strings.ToLower(inv.PermissionReason), "bot policy")
}
