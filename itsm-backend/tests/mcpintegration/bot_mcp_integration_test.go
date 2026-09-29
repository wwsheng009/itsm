// Bot × MCP 集成验收：Bot 策略门禁（service/bot）与 MCP 外部工具面
// （mcp/provider + service.ToolRegistry）的交叉语义。
//
// 与生产装配的差异沿用 mcp_flow_test.go 的 harness（真实 ent + sqlite + mock MCP 服务器；
// 权限模式 HardcodeOnly；SSRF 放行本机回环端口）。覆盖：
//  1. 严格交集：已授权 MCP 读工具放行；未授权 MCP 工具 ReasonToolNotGranted；
//  2. 风险上限：工具风险 > 授权上限 → ReasonRiskExceeded；RBAC 不通过 → ReasonRBACDenied；
//  3. 写面：MCP 写工具（未标注只读）经写面注册表投影为 write，已授权放行、未授权拒绝；
//  4. 停用服务器 → 工具面收缩（Bot 可见面同步收缩）；
//  5. 真实执行：经 registry 调用 MCP 读工具，mock 侧记录到该次调用。
package mcpintegration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/admin"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// default 夹具中的三个工具：两个只读候选 + 一个写候选（无只读标注 → 写面投影为 write）。
const (
	mcpReadTool      = "mcp__mock__list_issues"
	mcpOtherReadTool = "mcp__mock__huge_output"
	mcpWriteTool     = "mcp__mock__slow_tool"
)

// botToolMeta 把工具面定义映射为 Bot 策略判定入参（生产由 handlers/ai.Service 执行同一映射）。
func botToolMeta(def service.ToolDefinition) bot.ToolMeta {
	return bot.ToolMeta{
		Name:     def.Name,
		Provider: def.Provider,
		ReadOnly: def.ReadOnly,
		Resource: def.Resource,
		Action:   def.Action,
		Risk:     def.Risk,
	}
}

// enableTool 完成 D7 的两道治理闸：标注（read_only/risk/category）+ 启用；缺一不进工具面。
func enableTool(t *testing.T, h *harness, serverID int, callable string, readOnly bool, risk string) {
	t.Helper()
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	ro := readOnly
	_, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, callable,
		admin.ClassificationRequest{ReadOnly: &ro, Risk: risk, Category: "issue"})
	require.NoError(t, err)
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, serverID, callable, true)
	require.NoError(t, err)
}

// TestBotMCP_ToolFaceGating 锁定「MCP 工具面 ∩ Bot 授权」的四重交集（模板/入口/风险/RBAC）。
func TestBotMCP_ToolFaceGating(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, ctx)
	serverID, _ := h.provisionMockServer(t)

	enableTool(t, h, serverID, mcpReadTool, true, bot.RiskRead)
	enableTool(t, h, serverID, mcpOtherReadTool, true, bot.RiskRead)
	enableTool(t, h, serverID, mcpWriteTool, false, bot.RiskActMedium)

	adminSvc := bot.NewTemplateAdmin(h.client)
	strictTpl, err := adminSvc.CreateTemplate(ctx, h.tenantID, bot.TemplateInput{
		Slug: "mcp-analyst", Name: "MCP 分析助手", Audience: "internal",
		RiskLimit: bot.RiskActMedium, Entrypoints: []string{bot.EntrypointChat}, Status: bot.StatusPilot,
	})
	require.NoError(t, err)
	_, err = adminSvc.UpsertGrant(ctx, h.tenantID, strictTpl.ID,
		bot.GrantInput{ToolName: mcpReadTool, RiskLimit: bot.RiskRead})
	require.NoError(t, err)
	_, err = adminSvc.UpsertGrant(ctx, h.tenantID, strictTpl.ID,
		bot.GrantInput{ToolName: mcpWriteTool, RiskLimit: bot.RiskActMedium})
	require.NoError(t, err)

	policy := bot.NewPolicy(h.client)
	allowAll := func(string, string) bool { return true }

	// ① 已授权的 MCP 读工具：放行。
	readDef := h.registry.GetToolForTenant(ctx, h.tenantID, mcpReadTool)
	require.NotNil(t, readDef, "标注只读 + 启用后必须进入只读工具面")
	assert.Equal(t, "mcp", readDef.Provider)
	assert.Equal(t, "mock", readDef.ServerName)
	assert.Equal(t, "read", readDef.Action)
	decision := policy.CheckTool(ctx, h.tenantID, strictTpl.ID, bot.EntrypointChat, botToolMeta(*readDef), allowAll)
	assert.True(t, decision.Allowed, "已授权的 MCP 读工具应放行：reason=%s", decision.Reason)

	// ② 同服务器未授权的 MCP 工具：严格交集拒绝。
	otherDef := h.registry.GetToolForTenant(ctx, h.tenantID, mcpOtherReadTool)
	require.NotNil(t, otherDef)
	decision = policy.CheckTool(ctx, h.tenantID, strictTpl.ID, bot.EntrypointChat, botToolMeta(*otherDef), allowAll)
	assert.False(t, decision.Allowed)
	assert.Equal(t, bot.ReasonToolNotGranted, decision.Reason, "未授权的 MCP 工具必须被拒绝")

	// ③ 写面：MCP 写工具（未标注只读）由写面注册表投影为 write；已授权 → 放行。
	writeRegistry, _ := h.writeFaceService()
	writeDef := writeRegistry.GetToolForTenant(ctx, h.tenantID, mcpWriteTool)
	require.NotNil(t, writeDef, "启用后必须进入写工具面")
	assert.Equal(t, "write", writeDef.Action)
	assert.False(t, writeDef.ReadOnly)
	decision = policy.CheckTool(ctx, h.tenantID, strictTpl.ID, bot.EntrypointChat, botToolMeta(*writeDef), allowAll)
	assert.True(t, decision.Allowed, "已授权的 MCP 写工具（风险 ≤ 上限）应放行：reason=%s", decision.Reason)

	// ④ 另一个 Bot 只授权读工具 → MCP 写工具被拒（授权按 Bot 隔离）。
	readOnlyTpl, err := adminSvc.CreateTemplate(ctx, h.tenantID, bot.TemplateInput{
		Slug: "mcp-readonly", Name: "MCP 只读助手", Audience: "internal",
		RiskLimit: bot.RiskActMedium, Entrypoints: []string{bot.EntrypointChat}, Status: bot.StatusPilot,
	})
	require.NoError(t, err)
	_, err = adminSvc.UpsertGrant(ctx, h.tenantID, readOnlyTpl.ID,
		bot.GrantInput{ToolName: mcpReadTool, RiskLimit: bot.RiskRead})
	require.NoError(t, err)
	decision = policy.CheckTool(ctx, h.tenantID, readOnlyTpl.ID, bot.EntrypointChat, botToolMeta(*writeDef), allowAll)
	assert.False(t, decision.Allowed)
	assert.Equal(t, bot.ReasonToolNotGranted, decision.Reason)

	// ⑤ 风险上限：授权上限压到 read（≤ 模板上限，管理面可写），工具风险 act_medium → 越界拒绝。
	_, err = adminSvc.UpsertGrant(ctx, h.tenantID, strictTpl.ID,
		bot.GrantInput{ToolName: mcpWriteTool, RiskLimit: bot.RiskRead})
	require.NoError(t, err)
	decision = policy.CheckTool(ctx, h.tenantID, strictTpl.ID, bot.EntrypointChat, botToolMeta(*writeDef), allowAll)
	assert.False(t, decision.Allowed)
	assert.Equal(t, bot.ReasonRiskExceeded, decision.Reason)

	// ⑥ RBAC 交集：工具已授权，但角色无对应 permission（如 agent 不持 mcp:read）→ 拒绝。
	decision = policy.CheckTool(ctx, h.tenantID, strictTpl.ID, bot.EntrypointChat, botToolMeta(*readDef),
		func(string, string) bool { return false })
	assert.False(t, decision.Allowed)
	assert.Equal(t, bot.ReasonRBACDenied, decision.Reason)
}

// TestBotMCP_ExecuteReadToolAndServerDisableShrinksFace 锁定执行链路与工具面收缩。
func TestBotMCP_ExecuteReadToolAndServerDisableShrinksFace(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, ctx)
	serverID, _ := h.provisionMockServer(t)

	enableTool(t, h, serverID, mcpReadTool, true, bot.RiskRead)

	def := h.registry.GetToolForTenant(ctx, h.tenantID, mcpReadTool)
	require.NotNil(t, def)
	assert.Equal(t, "mcp", def.Provider)
	assert.Equal(t, "mock", def.ServerName)
	assert.Equal(t, "list_issues", def.RawToolName, "审计三元组同源字段（provider/server/raw）")

	// 真实执行：mock 服务器必须记录到该次调用。
	before := len(h.mock.Calls())
	result, err := h.registry.Execute(ctx, h.tenantID, mcpReadTool, map[string]interface{}{"state": "open"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Greater(t, len(h.mock.Calls()), before, "mock 侧必须记录到 MCP 工具调用")

	// 停用服务器 → 工具面收缩（Bot 可见面同步收缩；不依赖缓存残留）。
	_, err = h.adminSvc.DisableServer(ctx, admin.Actor{TenantID: h.tenantID, UserID: h.userID}, serverID)
	require.NoError(t, err)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && h.registry.GetToolForTenant(ctx, h.tenantID, mcpReadTool) != nil {
		time.Sleep(200 * time.Millisecond)
	}
	assert.Nil(t, h.registry.GetToolForTenant(ctx, h.tenantID, mcpReadTool),
		"停用服务器后 MCP 工具必须从工具面消失")
}
