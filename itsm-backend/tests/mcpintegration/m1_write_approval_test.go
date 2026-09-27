// M1-02 集成验收：写工具接入 Gate3 审批（对话 → 待审批 → 审批 → 执行 → 回填 → 审计）。
//
// 覆盖（对应实施方案 §4.2 M1-02 的「测试与证据」）：
//  1. 写路径：提交 → pending（含来源三元组 + 冻结参数 + 脱敏入参）→ approve → 队列执行 → 结果回填；
//  2. 参数冻结：执行以落库参数为唯一真源，审批接口不接受参数覆盖；再次提交生成新记录而非改写原记录；
//  3. 状态机：重复审批被拒（防重复执行），拒绝路径落 rejected 且不触发执行；
//  4. fail-closed：队列未装配时审批失败且记录保持 pending（不出现「已批准但永不执行」的悬空记录）；
//  5. 写调用不自动重试：成功路径恰好一次调用。
package mcpintegration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/mcp/admin"
	"itsm-backend/mcp/provider"
	"itsm-backend/service"
)

// writeApprovalService 构造「写工具面 + 审批队列」的完整装配（同一 ent/manager）。
func (h *harness) writeApprovalService() (*service.ToolRegistry, *ai.Service, *service.ToolQueue) {
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(provider.New(h.client, h.manager, provider.Options{
		Enabled:           true,
		IncludeWriteTools: true,
		MaxResultBytes:    4096,
	}))
	queue := service.NewToolQueue(h.client, registry, 8, zap.NewNop().Sugar())
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), nil, registry, queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	return registry, svc, queue
}

// writeFaceOnlyService 构造「写工具面但**未装配队列**」的装配（fail-closed 用）。
func (h *harness) writeFaceOnlyService() *ai.Service {
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(provider.New(h.client, h.manager, provider.Options{
		Enabled:           true,
		IncludeWriteTools: true,
		MaxResultBytes:    4096,
	}))
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), nil, registry, nil,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	return svc
}

func waitInvocationStatus(t *testing.T, h *harness, id int, want string) *ent.ToolInvocation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		inv, err := h.client.ToolInvocation.Get(context.Background(), id)
		require.NoError(t, err)
		if inv.Status == want {
			return inv
		}
		if time.Now().After(deadline) {
			t.Fatalf("invocation %d 未在 15s 内进入 %s：status=%s error=%v", id, want, inv.Status, inv.Error)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestM1WriteApproval_EndToEnd 写路径主链路：pending → approve → 执行 → 回填 → 审计。
func TestM1WriteApproval_EndToEnd(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)
	// 写工具：保持 read_only=false（默认按写），仅打开启用开关 → 进入写工具面。
	_, err := h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)

	registry, svc, _ := h.writeApprovalService()
	def := registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue")
	require.NotNil(t, def)
	assert.Equal(t, "write", def.Action)
	assert.Equal(t, "mcp", def.Provider)
	assert.Equal(t, "mock", def.ServerName)
	assert.Equal(t, "create_issue", def.RawToolName)
	assert.Equal(t, "high", def.Risk, "未标注风险默认 high（审批人可见）")

	// 1) 提交（含敏感参数）→ pending，来源三元组与脱敏快照一次落库。
	_, pendingID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "打印机故障", "token": "s3cr3t-value"})
	require.NoError(t, err)
	require.Greater(t, pendingID, 0)

	pending, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Status)
	assert.Equal(t, "pending", pending.ApprovalState)
	assert.True(t, pending.NeedsApproval)
	assert.Equal(t, "mcp", pending.Provider)
	assert.Equal(t, "mock", pending.McpServerName)
	assert.Equal(t, "create_issue", pending.McpRawToolName)
	assert.Equal(t, "mcp__mock__create_issue", pending.McpCallableName)
	assert.Equal(t, "sysadmin", pending.RoleSnapshot)
	require.Contains(t, pending.Arguments, "打印机故障", "落库参数是执行真源")
	assert.NotContains(t, pending.ArgsRedacted, "s3cr3t-value", "脱敏快照不得含明文敏感值")
	assert.Contains(t, pending.ArgsRedacted, `"token":"****"`, "敏感键必须掩码（pkg/redact 口径）")

	// 2) 参数冻结：再次提交生成**新记录**，既有 pending 的参数不被改写。
	_, secondID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "被篡改的标题"})
	require.NoError(t, err)
	require.NotEqual(t, pendingID, secondID)
	unchanged, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, pending.Arguments, unchanged.Arguments, "审批前后参数必须以落库快照为唯一真源")

	// 3) 审批通过 → 队列执行 → 结果回填。
	state, err := svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "同意执行")
	require.NoError(t, err)
	assert.Equal(t, "approved", state)

	done := waitInvocationStatus(t, h, pendingID, "done")
	assert.Equal(t, "approved", done.ApprovalState)
	assert.Equal(t, h.userID, done.ApprovedBy)
	require.NotNil(t, done.ApprovedAt)
	assert.GreaterOrEqual(t, done.DurationMs, 1, "执行耗时必须留痕（口径 ≥1ms）")
	require.NotEmpty(t, done.OutputSummary)
	assert.Equal(t, "", done.ErrorCode)

	calls := h.mock.Calls()
	require.Len(t, calls, 1, "写调用必须恰好一次（不自动重试）")
	assert.Equal(t, "create_issue", calls[0].Tool)
	assert.Equal(t, "打印机故障", calls[0].Args["title"], "执行参数必须来自落库快照（审批后篡改无效）")
	assert.NotEqual(t, "被篡改的标题", calls[0].Args["title"])

	// 4) 状态机：重复审批被拒，且不会二次执行。
	_, err = svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "再来一次")
	require.ErrorIs(t, err, ai.ErrInvocationNotPending)
	assert.Len(t, h.mock.Calls(), 1, "重复审批不得触发第二次执行")

	// 5) 拒绝路径：落 rejected + 原因 + 决策人，且不触发执行。
	_, rejectID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "拒绝示例"})
	require.NoError(t, err)
	state, err = svc.ApproveTool(ctx, rejectID, h.tenantID, h.userID, false, "风险过高")
	require.NoError(t, err)
	assert.Equal(t, "rejected", state)

	rejected, err := h.client.ToolInvocation.Get(ctx, rejectID)
	require.NoError(t, err)
	assert.Equal(t, "rejected", rejected.Status)
	assert.Equal(t, "rejected", rejected.ApprovalState)
	assert.Equal(t, "风险过高", rejected.ApprovalReason)
	assert.Equal(t, h.userID, rejected.ApprovedBy)
	assert.Len(t, h.mock.Calls(), 1, "拒绝不得触发执行")

	// 6) 审计可查：待审批 → 执行的两条记录都在（同一 invocation 生命周期）。
	count, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID)).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "两次提交 + 一次拒绝 = 3 条记录（全部留痕）")
}

// TestM1WriteApproval_FailClosedWithoutQueue 队列不可用时审批必须失败且保持 pending。
func TestM1WriteApproval_FailClosedWithoutQueue(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)
	_, err := h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)

	svc := h.writeFaceOnlyService() // 未装配 ToolQueue
	_, pendingID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "无队列场景"})
	require.NoError(t, err)
	require.Greater(t, pendingID, 0)

	_, err = svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "同意")
	require.ErrorIs(t, err, ai.ErrToolQueueUnavailable)

	inv, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", inv.ApprovalState, "队列不可用时必须保持 pending（可重试），不得留下已批准但永不执行的悬空记录")
	assert.Equal(t, "pending", inv.Status)
	assert.Empty(t, h.mock.Calls(), "未执行不得触达服务器")
}
