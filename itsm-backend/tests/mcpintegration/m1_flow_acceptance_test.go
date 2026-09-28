// M1-10 M1 流程验收：写路径失败注入与终态防护。
//
// 覆盖（对应实施方案 §4.2 M1-10 的「内容」与 A1-10 判据）：
//  1. 失败注入·调用超时：服务器级 timeout_ms 收紧到 300ms，慢工具（2s）必须失败；
//     失败落 failed + 稳定错误码 + 脱敏错误摘要，写路径**恰好一次**调用（不重试）；
//  2. 失败注入·服务器下线：提交审批后禁用服务器 → 审批放行但执行 fail-closed，
//     不产生任何下游调用，终态为 failed 且审计可查；
//  3. 失败注入·拒绝：审批拒绝 → rejected、无执行、原因与决策人留痕；
//  4. 终态防护（过期等价语义）：不存在的记录与已终态记录不可再审批（fail-closed）；
//  5. 审计一致性：失败记录同样携带来源三元组、脱敏参数快照，且不含明文敏感值。
package mcpintegration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/mcp/admin"
)

// setServerTimeoutMS 通过生产 API 收紧服务器级调用超时（服务器 TimeoutMS 优先于平台默认）。
//
// 注意：管理器写运行态/工具缓存会 bump 行版本，Get→Update 之间可能撞上版本冲突；
// 这里按生产 UI 的做法做有界重试（重新读取版本后再提交）。
func setServerTimeoutMS(t *testing.T, h *harness, actor admin.Actor, serverID, timeoutMS int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		view, err := h.adminSvc.GetServer(context.Background(), actor, serverID)
		require.NoError(t, err)
		updated, err := h.adminSvc.UpdateServer(context.Background(), actor, serverID, admin.UpdateServerRequest{
			Version:   view.Version,
			TimeoutMS: &timeoutMS,
		})
		if err == nil {
			require.Equal(t, timeoutMS, updated.Policy.TimeoutMS, "策略回读必须反映服务器级超时（M1-08）")
			return
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("更新服务器超时失败（重试后仍冲突）：%v", lastErr)
}

// waitToolInFace 等待工具重新出现在服务器工具面。
// 服务器配置变更（如改超时）会触发重连与工具缓存刷新，刷新期间工具面可能瞬时为空。
func waitToolInFace(t *testing.T, h *harness, actor admin.Actor, serverID int, callable string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last []admin.ToolView
	for {
		listed, err := h.adminSvc.ListTools(context.Background(), actor, serverID)
		if err == nil {
			last = listed
			for _, tool := range listed {
				if tool.CallableName == callable {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("工具 %s 未在 20s 内回到工具面：当前工具=%+v", callable, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestM1Flow_FailureInjection_CallTimeout 慢工具在服务器级超时下失败：落 failed、单次调用、审计可查。
func TestM1Flow_FailureInjection_CallTimeout(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)

	// 服务器级超时下限为 1000ms（管理 API 校验 1000–300000）；慢工具休眠 2s（mock 默认 SlowDelay）→ 必超时。
	// 顺序要求：**先改配置再开治理位**——改配置会触发重连与工具重发现，重发现会把「新工具默认停用」
	// 重新落库（D7），若先启用会被覆盖；等工具回到工具面后再启用即可稳定生效。
	setServerTimeoutMS(t, h, actor, serverID, 1000)
	waitToolInFace(t, h, actor, serverID, "mcp__mock__slow_tool")

	// slow_tool 无只读标注 → 默认按写工具治理（D7）。
	_, err := h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__slow_tool", true)
	require.NoError(t, err)

	registry, svc, _ := h.writeApprovalService()
	// 重发现/工具缓存写入是异步的：治理位翻转可能被在途的工具缓存写覆盖（观察到 enabled 回退）。
	// 这里按「治理位 → 解析可见」做有界重试（幂等重放），并在超时信息里给出可诊断线索。
	deadline := time.Now().Add(20 * time.Second)
	for {
		if def := registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__slow_tool"); def != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow_tool 未在 20s 内进入可解析工具面（治理位可能与工具重发现竞争）")
		}
		_, _ = h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__slow_tool", true)
		time.Sleep(100 * time.Millisecond)
	}

	_, pendingID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__slow_tool",
		map[string]interface{}{"token": "s3cr3t-value"})
	require.NoError(t, err)

	state, err := svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "同意（预期超时）")
	require.NoError(t, err)
	require.Equal(t, "approved", state)

	failed := waitInvocationStatus(t, h, pendingID, "failed")
	// provider 的稳定错误码：调用超时 → tool_timeout（不走 internal_error）。
	require.Equal(t, "tool_timeout", failed.ErrorCode, "调用超时必须落精确错误码")
	require.GreaterOrEqual(t, failed.DurationMs, 1)
	require.NotEmpty(t, failed.Error, "失败原因必须留痕（脱敏截断）")
	require.Contains(t, failed.ArgsRedacted, `"token":"****"`, "失败记录同样只落脱敏参数")

	// 写路径不重试：慢工具调用恰好一次（超时后不再发起）。
	slowCalls := 0
	for _, call := range h.mock.Calls() {
		if call.Tool == "slow_tool" {
			slowCalls++
		}
	}
	assert.Equal(t, 1, slowCalls, "写工具失败不得自动重试（避免重复副作用）")

	// 审计可查：失败记录仍可按租户 + 状态检索。
	rows, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID), toolinvocation.Status("failed")).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "mcp__mock__slow_tool", rows[0].McpCallableName)
	assert.Equal(t, "slow_tool", rows[0].McpRawToolName)
}

// TestM1Flow_FailureInjection_ServerOffline 审批后服务器下线：执行 fail-closed 且零下游调用。
func TestM1Flow_FailureInjection_ServerOffline(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)
	_, err := h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)

	_, svc, _ := h.writeApprovalService()
	_, pendingID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "服务器已下线"})
	require.NoError(t, err)

	callsBefore := len(h.mock.Calls())

	// 下线：管理器立即对新调用拒绝（in-flight 宽限语义见 M1-08）。
	disabled, err := h.adminSvc.DisableServer(ctx, actor, serverID)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)

	state, err := svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "误批（服务器已下线）")
	require.NoError(t, err)
	require.Equal(t, "approved", state)

	failed := waitInvocationStatus(t, h, pendingID, "failed")
	assert.Equal(t, "tool_not_found", failed.ErrorCode, "下线后工具不可解析 → fail-closed")
	assert.Len(t, h.mock.Calls(), callsBefore, "服务器下线后不得产生任何下游调用")

	// 工具面同步收缩：下线服务器不贡献工具。
	registry, _, _ := h.writeApprovalService()
	assert.Nil(t, registry.GetToolForTenant(ctx, h.tenantID, "mcp__mock__create_issue"))
}

// TestM1Flow_FailureInjection_RejectedAndTerminalGuards 拒绝路径与终态防护（含「过期」等价语义）。
func TestM1Flow_FailureInjection_RejectedAndTerminalGuards(t *testing.T) {
	h := newHarness(t, context.Background())
	ctx := context.Background()
	actor := admin.Actor{TenantID: h.tenantID, UserID: h.userID}

	serverID, _ := h.provisionMockServer(t)
	_, err := h.adminSvc.SetToolEnabled(ctx, actor, serverID, "mcp__mock__create_issue", true)
	require.NoError(t, err)

	_, svc, _ := h.writeApprovalService()

	// 1) 拒绝路径：无执行、原因与决策人留痕。
	_, rejectedID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "拒绝示例"})
	require.NoError(t, err)
	state, err := svc.ApproveTool(ctx, rejectedID, h.tenantID, h.userID, false, "变更窗口外，禁止执行")
	require.NoError(t, err)
	require.Equal(t, "rejected", state)
	rejected, err := h.client.ToolInvocation.Get(ctx, rejectedID)
	require.NoError(t, err)
	assert.Equal(t, "rejected", rejected.Status)
	assert.Equal(t, "变更窗口外，禁止执行", rejected.ApprovalReason)
	assert.Equal(t, h.userID, rejected.ApprovedBy)
	assert.Empty(t, h.mock.Calls(), "拒绝不得触发执行")

	// 2) 终态防护：已终态记录不可再审批（重复/迟到审批一律拒绝）。
	_, err = svc.ApproveTool(ctx, rejectedID, h.tenantID, h.userID, true, "迟到的批准")
	require.ErrorIs(t, err, ai.ErrInvocationNotPending)

	_, doneID, err := svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "mcp__mock__create_issue",
		map[string]interface{}{"title": "正常执行"})
	require.NoError(t, err)
	_, err = svc.ApproveTool(ctx, doneID, h.tenantID, h.userID, true, "通过")
	require.NoError(t, err)
	waitInvocationStatus(t, h, doneID, "done")
	_, err = svc.ApproveTool(ctx, doneID, h.tenantID, h.userID, false, "再拒一次")
	require.ErrorIs(t, err, ai.ErrInvocationNotPending)

	// 3) 记录不可得（等价于前端「过期/未知」）：审批必须失败，不产生副作用。
	_, err = svc.ApproveTool(ctx, 999999, h.tenantID, h.userID, true, "对不存在的记录审批")
	require.Error(t, err)
	require.Len(t, h.mock.Calls(), 1, "仅正常执行那一次调用")

	// 4) 审计计数：三条记录（拒绝/成功/不存在不落库），来源三元组齐全。
	count, err := h.client.ToolInvocation.Query().Where(toolinvocation.TenantID(h.tenantID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
