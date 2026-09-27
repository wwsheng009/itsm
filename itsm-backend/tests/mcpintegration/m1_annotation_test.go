// M1-01 集成验收：工具分类标注全链路（read_only / risk / category）。
//
// 覆盖（对应实施方案 §4.2 M1-01 的「测试与证据」）：
//  1. 默认值 D7：read_only=false、risk=high、category=""、enabled=false；
//  2. 工具面随标注**立即**变化（无会话冻结）：改只读 → 进只读面；改回写 → 离面且执行返回未知工具；
//  3. Gate2 映射：Resource/Action 由标注派生（mcp:read / mcp:write），权限不足即拒绝且不产生副作用；
//  4. 标注变更必审计：audit_logs(resource=mcp, action=set_tool_classification) 含 before/after 差异；
//  5. 写面（IncludeWriteTools=true）：写工具解析为 Action=write，经 Gate3 创建 pending 审批（执行链路由 M1-02 完成）。
package mcpintegration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/mcp/admin"
	"itsm-backend/mcp/provider"
	"itsm-backend/service"
)

// provisionMockServer 在 harness 中创建并启用一台 mock 服务器，返回 serverID 与工具视图。
// 与 M0-14 的用例共用夹具，但把「创建 → 启用 → 等待 healthy → 拉取工具」收敛为可复用步骤。
func (h *harness) provisionMockServer(t *testing.T) (int, []admin.ToolView) {
	t.Helper()
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	created, err := h.adminSvc.CreateServer(ctx, actor, admin.CreateServerRequest{
		Name:           "mock",
		DisplayName:    "Mock MCP",
		Transport:      "streamable",
		URL:            h.mockHTTP.URL,
		CredentialType: "none",
	})
	require.NoError(t, err)
	h.track(created.ID) // 收尾时先关会话再关 mock HTTP（避免 SSE 流阻塞 Close）

	_, err = h.adminSvc.EnableServer(ctx, actor, created.ID)
	require.NoError(t, err)

	deadline := time.Now().Add(20 * time.Second)
	var tools []admin.ToolView
	for {
		view, getErr := h.adminSvc.GetServer(ctx, actor, created.ID)
		require.NoError(t, getErr)
		// 注意：`healthy` 表示连接可用，工具发现落库可能在其后毫秒级完成
		// （M0-07 的状态机先置 healthy 再写 ToolCache），故这里以「工具面非空」为完成条件。
		if view.RunningStatus == "healthy" {
			if listed, listErr := h.adminSvc.ListTools(ctx, actor, created.ID); listErr == nil && len(listed) > 0 {
				tools = listed
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("服务器未在 20s 内进入 healthy：status=%s running=%s last_error=%s events=%+v",
				view.Status, view.RunningStatus, view.LastError, h.events.List(created.ID))
		}
		time.Sleep(200 * time.Millisecond)
	}

	require.NotEmpty(t, tools, "健康服务器必须完成工具发现")
	return created.ID, tools
}

func toolIndex(tools []admin.ToolView) map[string]admin.ToolView {
	out := make(map[string]admin.ToolView, len(tools))
	for _, tool := range tools {
		out[tool.CallableName] = tool
	}
	return out
}

// writeFaceService 构造「写工具面开启」的独立装配（同一 ent/manager，仅 provider 选项不同）。
// 返回注册表便于直接断言定义投影；返回 service 便于走 Gate2/Gate3。
func (h *harness) writeFaceService() (*service.ToolRegistry, *ai.Service) {
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(provider.New(h.client, h.manager, provider.Options{
		Enabled:           true,
		IncludeWriteTools: true,
		MaxResultBytes:    4096,
	}))
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	return registry, svc
}

// TestM1Annotation_DefaultsAndFaceSwitching 锁定 D7 默认值 + 工具面随标注即时切换。
func TestM1Annotation_DefaultsAndFaceSwitching(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, tools := h.provisionMockServer(t)
	index := toolIndex(tools)

	writeTool, ok := index["mcp__mock__create_issue"]
	require.True(t, ok, "create_issue 必须被发现")
	// 1) D7 默认值：默认按写 + 风险最高 + 无分类 + 不启用。
	assert.False(t, writeTool.ReadOnly, "新工具默认按写（read_only=false）")
	assert.Equal(t, "high", writeTool.Risk, "默认风险级别为 high")
	assert.Equal(t, "", writeTool.Category, "默认无分类")
	assert.False(t, writeTool.Enabled, "新工具默认不启用")
	assert.False(t, writeTool.Quarantined)

	// 2) 只读面（IncludeWriteTools=false）不含写工具：默认拒绝的可执行边界。
	require.Nil(t, h.registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue"),
		"未标注只读的工具不得进入只读工具面")

	// 3) 标注只读（治理步骤一）：未启用时仍不进面（D7 两道闸：默认按写 + 默认不启用）。
	readOnly := true
	_, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{ReadOnly: &readOnly, Risk: "act_medium", Category: "issue"})
	require.NoError(t, err)
	require.Nil(t, h.registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue"),
		"仅标注只读、未启用时不得进入工具面")

	// 4) 启用（治理步骤二）→ 立即进面（无会话冻结：下一轮解析即可见）。
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)

	def := h.registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue")
	require.NotNil(t, def, "标注只读 + 启用后必须立即进入只读工具面")
	assert.Equal(t, "mcp", def.Resource)
	assert.Equal(t, "read", def.Action, "Action 必须由 read_only 派生")
	assert.True(t, def.ReadOnly)

	// 5) 改回写标注 → 立即离面（解析不可见，执行按未知工具处理）。
	readOnly = false
	updated, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{ReadOnly: &readOnly})
	require.NoError(t, err)
	assert.False(t, updated.ReadOnly)
	assert.Equal(t, "act_medium", updated.Risk, "未提交的字段保持不变")
	require.Nil(t, h.registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue"),
		"改回写标注后必须立即离开只读工具面")

	_, _, execErr := h.aiSvc.ExecuteTool(ctx, h.userID, h.tenantID, "admin", "mcp__mock__create_issue", nil)
	require.Error(t, execErr)
	assert.ErrorIs(t, execErr, ai.ErrUnknownTool)
}

// TestM1Annotation_Gate2ReadVsWrite 锁定 Gate2 的 Resource/Action 映射与默认零授予角色矩阵。
func TestM1Annotation_Gate2ReadVsWrite(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)

	// 只读工具：admin 持 mcp:read、agent 默认零授予。
	readOnly := true
	_, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__list_issues",
		admin.ClassificationRequest{ReadOnly: &readOnly, Risk: "read", Category: "issue"})
	require.NoError(t, err)
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__list_issues", true)
	require.NoError(t, err)

	_, _, err = h.aiSvc.ExecuteTool(ctx, h.userID, h.tenantID, "agent", "mcp__mock__list_issues", nil)
	require.Error(t, err, "agent 默认不持 mcp:read，必须拒绝")
	assert.Contains(t, err.Error(), "lacks mcp:read")
	assert.NotContains(t, err.Error(), "mcp__mock__list_issues", "拒绝信息不回显工具名之外的内网细节")

	result, pendingID, err := h.aiSvc.ExecuteTool(ctx, h.userID, h.tenantID, "admin", "mcp__mock__list_issues", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, pendingID, "只读工具免审批直通")
	require.NotNil(t, result)

	// 写工具面：sysadmin 持 mcp:write；admin 不持（使用与治理分离）。
	// 写工具同样需启用（D7）：read_only 保持默认 false，仅打开启用开关。
	_, err = h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)
	writeRegistry, writeSvc := h.writeFaceService()
	writeDef := writeRegistry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue")
	require.NotNil(t, writeDef)
	assert.Equal(t, "write", writeDef.Action, "未标注只读的工具在写面投影为 write")
	assert.False(t, writeDef.ReadOnly)

	beforeDenied := invocationCount(t, h, "mcp__mock__create_issue")
	_, _, err = writeSvc.ExecuteTool(ctx, h.userID, h.tenantID, "admin", "mcp__mock__create_issue",
		map[string]any{"title": "x"})
	require.Error(t, err, "admin 默认不持 mcp:write")
	assert.Contains(t, err.Error(), "lacks mcp:write")
	assert.Equal(t, beforeDenied+1, invocationCount(t, h, "mcp__mock__create_issue"),
		"拒绝也要留痕（denied 审计行）")
	assert.Equal(t, 0, pendingCount(t, h, "mcp__mock__create_issue"), "权限拒绝不得创建 pending 审批")

	_, pendingID, err = writeSvc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]any{"title": "x"})
	require.NoError(t, err)
	require.Greater(t, pendingID, 0, "写工具必须进入待审批")

	invocation, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", invocation.Status)
	assert.True(t, invocation.NeedsApproval)
	assert.Equal(t, "pending", invocation.ApprovalState)
	assert.Equal(t, "sysadmin", invocation.RoleSnapshot)
}

// TestM1Annotation_ClassificationAuditBeforeAfter 锁定标注变更的审计可追溯（before/after 差异）。
func TestM1Annotation_ClassificationAuditBeforeAfter(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)

	readOnly := true
	_, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{ReadOnly: &readOnly, Risk: "act_high", Category: "change"})
	require.NoError(t, err)

	readOnly = false
	_, err = h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{ReadOnly: &readOnly})
	require.NoError(t, err)

	logs, err := h.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(h.tenantID),
			auditlog.ResourceEQ("mcp"),
			auditlog.ActionEQ("set_tool_classification"),
		).
		Order(auditlog.ByID()).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 2, "每次标注变更都必须留痕")

	first := auditBody(t, logs[0].RequestBody)
	assert.Equal(t, "mcp_tool", first["object_type"])
	assert.Equal(t, "mcp__mock__create_issue", first["object_id"])
	assert.Equal(t, "success", first["result"])
	firstBefore, firstAfter := first["before"].(map[string]any), first["after"].(map[string]any)
	assert.Equal(t, false, firstBefore["read_only"])
	assert.Equal(t, "high", firstBefore["risk"])
	assert.Equal(t, true, firstAfter["read_only"], "只读标注必须体现在 after")
	assert.Equal(t, "act_high", firstAfter["risk"])
	assert.Equal(t, "change", firstAfter["category"])

	second := auditBody(t, logs[1].RequestBody)
	secondBefore, secondAfter := second["before"].(map[string]any), second["after"].(map[string]any)
	assert.Equal(t, true, secondBefore["read_only"])
	assert.Equal(t, false, secondAfter["read_only"], "改回写标注同样必须留痕（回退可追溯）")
	assert.NotEqual(t, secondBefore, secondAfter, "无差异的标注不应写入审计（避免噪声）")
}

// TestM1Annotation_InvalidRiskRejected 锁定标注参数校验（非法值拒绝且不落库/不写审计）。
func TestM1Annotation_InvalidRiskRejected(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, tools := h.provisionMockServer(t)
	_ = tools

	_, err := h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{Risk: "super-danger"})
	require.Error(t, err)
	adminErr, ok := admin.AsAdminError(err)
	require.True(t, ok)
	assert.Equal(t, admin.CodeValidationFailed, adminErr.Code)

	_, err = h.adminSvc.SetToolClassification(ctx, actor, serverID, "mcp__mock__create_issue",
		admin.ClassificationRequest{Category: strings.Repeat("x", 33)})
	require.Error(t, err)
	adminErr, ok = admin.AsAdminError(err)
	require.True(t, ok)
	assert.Equal(t, admin.CodeValidationFailed, adminErr.Code)

	count, err := h.client.AuditLog.Query().
		Where(auditlog.TenantIDEQ(h.tenantID), auditlog.ActionEQ("set_tool_classification")).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "校验失败不得留痕（未发生变更）")

	row, err := h.adminSvc.ListTools(ctx, actor, serverID)
	require.NoError(t, err)
	for _, tool := range row {
		if tool.CallableName == "mcp__mock__create_issue" {
			assert.Equal(t, "high", tool.Risk, "校验失败不得部分落库")
			assert.Equal(t, "", tool.Category)
		}
	}
}

func invocationCount(t *testing.T, h *harness, toolName string) int {
	t.Helper()
	count, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID), toolinvocation.ToolNameEQ(toolName)).
		Count(context.Background())
	require.NoError(t, err)
	return count
}

func pendingCount(t *testing.T, h *harness, toolName string) int {
	t.Helper()
	count, err := h.client.ToolInvocation.Query().
		Where(
			toolinvocation.TenantID(h.tenantID),
			toolinvocation.ToolNameEQ(toolName),
			toolinvocation.StatusEQ("pending"),
		).
		Count(context.Background())
	require.NoError(t, err)
	return count
}

func auditBody(t *testing.T, raw *string) map[string]any {
	t.Helper()
	require.NotNil(t, raw, "审计 request_body 不得为空")
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(*raw), &body), "审计 request_body 必须是 JSON：%v", raw)
	return body
}
