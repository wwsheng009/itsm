// Package botintegration 的 B1-10 流程验收：
//
//  1. 一条「对话发起 → 确认单 → 审批通过 → 队列执行 → 回读校验 → 运行档案收口」链路（含 run_id 贯通与脱敏）；
//  2. 一条「队列重启恢复」用例：已批准未执行单在带 run 上下文时恢复执行且只执行一次、档案仍可回溯；
//  3. run-summary：以查询形状固定「一次运行的可回溯摘要」字段（模板见证据文档）。
package botintegration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/botrun"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// runSummary 是 run-summary 的字段模板（B1-10 定稿；由已有查询直接装配，不新增存储）。
type runSummary struct {
	RunID           int    `json:"runId"`
	Status          string `json:"status"`
	Entrypoint      string `json:"entrypoint"`
	ConversationID  int    `json:"conversationId"`
	StepCount       int    `json:"stepCount"`
	EventCount      int    `json:"eventCount"`
	ToolInvocations int    `json:"toolInvocations"`
	LastEventType   string `json:"lastEventType"`
}

func buildRunSummary(t *testing.T, h *b1Harness, runID int) runSummary {
	t.Helper()
	ctx := context.Background()
	run, err := h.client.BotRun.Query().
		Where(botrun.ID(runID), botrun.TenantID(h.tenantID)).
		Only(ctx)
	require.NoError(t, err)
	steps, err := h.store.ListRunSteps(ctx, h.tenantID, runID)
	require.NoError(t, err)
	events, err := h.store.ListRunEvents(ctx, h.tenantID, runID)
	require.NoError(t, err)
	invocations, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID), toolinvocation.RunID(runID)).
		Count(ctx)
	require.NoError(t, err)

	last := ""
	if len(events) > 0 {
		last = events[len(events)-1].Type
	}
	return runSummary{
		RunID:           run.ID,
		Status:          run.Status,
		Entrypoint:      run.Entrypoint,
		ConversationID:  run.ConversationID,
		StepCount:       len(steps),
		EventCount:      len(events),
		ToolInvocations: invocations,
		LastEventType:   last,
	}
}

// TestB1FlowAcceptance_ConfirmExecuteVerifyWithRunArchive 覆盖 AB1-01/02/03/05/06/07/08 的主链路。
func TestB1FlowAcceptance_ConfirmExecuteVerifyWithRunArchive(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	// 1) 对话内发起写工具：运行上下文 + 会话归属 + 脱敏快照（AB1-01/AB1-05）。
	run, err := h.store.StartRun(ctx, bot.StartRunInput{TenantID: h.tenantID, ConversationID: h.convID, Entrypoint: "chat"})
	require.NoError(t, err)
	runCtx := bot.WithRunID(ctx, run.ID)

	_, pendingID, err := h.svc.ExecuteToolWithConversation(runCtx, h.userID, h.tenantID, "super_admin", "stub__create_note",
		map[string]interface{}{"title": "B1-10 验收：打印机故障", "token": "s3cr3t-value"}, h.convID)
	require.NoError(t, err)
	require.Positive(t, pendingID)

	pending, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Status)
	assert.Equal(t, "pending", pending.ApprovalState)
	assert.Equal(t, run.ID, pending.RunID, "AB1-01：运行内记录必须带 run_id")
	assert.Equal(t, h.convID, pending.ConversationID, "AB1-03：会话归属保持")
	assert.NotContains(t, pending.ArgsRedacted, "s3cr3t-value", "AB1-05：审计快照不得含明文密钥")

	// 2) 确认单档案：步骤 + 事件（对话内确认 required 的落档形状，供 run-summary 回溯）。
	_, err = h.store.AppendStep(ctx, h.tenantID, run.ID, 1, "tool", "invocation:"+strconv.Itoa(pendingID), 12)
	require.NoError(t, err)
	_, err = h.store.AppendEvent(ctx, h.tenantID, run.ID, "confirmation_required",
		map[string]any{"invocationId": pendingID, "tool": "stub__create_note"})
	require.NoError(t, err)

	// 3) 审批通过（B1-05 状态机）→ 队列执行 → 回读校验（AB1-06/07）。
	h.queue.SetVerifier(fakeVerifier{state: service.VerifyStateVerified, note: "回读通过：noteId 与预期一致"})
	decision, err := h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "")
	require.NoError(t, err)
	assert.Equal(t, "approved", decision)

	executed := waitStatus(t, h.b0Harness, pendingID, "done")
	assert.Equal(t, service.VerifyStateVerified, executed.VerifyState, "AB1-07：执行后回读状态落库")
	assert.Contains(t, executed.VerifyNote, "回读通过")
	assert.Equal(t, 1, h.provider.callCount(), "写调用恰好一次（无自动重试）")
	require.NotNil(t, executed.Result)
	assert.Contains(t, *executed.Result, "noteId", "AB1-06：执行结果回填")
	assert.Equal(t, run.ID, executed.RunID, "执行完成后 run 关联保持")

	// 4) 运行收口 + run-summary（AB1-08：一次运行的可回溯摘要）。
	_, err = h.store.AppendEvent(ctx, h.tenantID, run.ID, "tool_call_finished",
		map[string]any{"tool": "stub__create_note", "invocationId": pendingID})
	require.NoError(t, err)
	require.NoError(t, h.store.FinishRun(ctx, h.tenantID, run.ID, "done", ""))

	summary := buildRunSummary(t, h, run.ID)
	assert.Equal(t, run.ID, summary.RunID)
	assert.Equal(t, "done", summary.Status)
	assert.Equal(t, "chat", summary.Entrypoint)
	assert.Equal(t, h.convID, summary.ConversationID)
	assert.Equal(t, 1, summary.StepCount)
	assert.Equal(t, 2, summary.EventCount)
	assert.Equal(t, 1, summary.ToolInvocations)
	assert.Equal(t, "tool_call_finished", summary.LastEventType)

	// 5) 终态不可改判（与 B1-05 状态机一致）：再次审批必须是稳定错误而非二次执行。
	_, err = h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, false, "事后改判")
	require.Error(t, err, "终态记录不得被再次决策")
	assert.Equal(t, 1, h.provider.callCount(), "重复决策不得触发执行")
}

// TestB1FlowAcceptance_QueueRestartKeepsRunLink 覆盖 AB1-09 的恢复口径：
// 「审批通过但进程在入队前退出」的带 run 记录，重启扫描后恰好执行一次，档案仍可回溯。
func TestB1FlowAcceptance_QueueRestartKeepsRunLink(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	run, err := h.store.StartRun(ctx, bot.StartRunInput{TenantID: h.tenantID, ConversationID: h.convID, Entrypoint: "chat"})
	require.NoError(t, err)

	// 模拟崩溃现场：approved + pending + 带 run_id（不经内存队列）。
	row, err := h.client.ToolInvocation.Create().
		SetTenantID(h.tenantID).
		SetToolName("stub__create_note").
		SetArguments(`{"note":"重启恢复"}`).
		SetStatus("pending").
		SetApprovalState("approved").
		SetNeedsApproval(true).
		SetUserID(h.userID).
		SetProvider("stub").
		SetPermissionCheck("passed").
		SetRunID(run.ID).
		SetConversationID(h.convID).
		Save(ctx)
	require.NoError(t, err)

	// 新队列实例（模拟进程重启后的启动恢复）：恢复 → 执行一次 → 终态。
	restarted := service.NewToolQueue(h.client, h.registry, 8, zap.NewNop().Sugar())
	restarted.SetVerifier(fakeVerifier{state: service.VerifyStateVerified, note: "重启后回读通过"})
	res, err := restarted.RecoverPending(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Enqueued)

	executed := waitStatus(t, h.b0Harness, row.ID, "done")
	assert.Equal(t, run.ID, executed.RunID, "恢复执行后 run 关联保持")
	assert.Equal(t, 1, executed.AttemptCount, "恢复消费只尝试一次")
	assert.Equal(t, service.VerifyStateVerified, executed.VerifyState)

	// 重复恢复不得双执行（幂等），且不会重复计数。
	_, err = restarted.RecoverPending(ctx, 0)
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, h.provider.callCount(), "重复恢复不得双执行")

	summary := buildRunSummary(t, h, run.ID)
	assert.Equal(t, 1, summary.ToolInvocations, "恢复执行后工具记录仍归属该运行")
}
