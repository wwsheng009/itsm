package botintegration

import (
	"context"
	"testing"
	"time"

	"itsm-backend/ent/toolinvocation"
	"itsm-backend/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 B1-06 队列持久化与恢复：
// 启动恢复（已批准未执行 → 重新入队 → 恰好执行一次）、抢占幂等（重复恢复/并发消费不双执行）、
// 尝试次数与最近错误码留痕、执行后回读校验（verify_state）。

// seedApprovedPending 模拟「审批已通过但进程在入队前崩溃」的持久状态：
// 直接落库 approval_state=approved ∧ status=pending，不经过内存队列。
func seedApprovedPending(t *testing.T, h *b0Harness, note string) int {
	t.Helper()
	row, err := h.client.ToolInvocation.Create().
		SetTenantID(h.tenantID).
		SetToolName("stub__create_note").
		SetArguments(`{"note":"` + note + `"}`).
		SetStatus("pending").
		SetApprovalState("approved").
		SetNeedsApproval(true).
		SetUserID(h.userID).
		SetProvider("stub").
		SetPermissionCheck("passed").
		Save(context.Background())
	require.NoError(t, err)
	return row.ID
}

// TestB1QueueRecovery_ApprovedNotExecutedIsResumed 覆盖核心恢复路径：
// 启动扫描把 approved+pending 重新入队并执行完成，attempt_count 留痕。
func TestB1QueueRecovery_ApprovedNotExecutedIsResumed(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	id := seedApprovedPending(t, h, "恢复执行")

	callsBefore := h.provider.callCount()
	result, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Found, 1)
	assert.GreaterOrEqual(t, result.Enqueued, 1)
	assert.Zero(t, result.Skipped)

	require.Eventually(t, func() bool {
		row, getErr := h.client.ToolInvocation.Get(ctx, id)
		return getErr == nil && row.Status == "done"
	}, 10*time.Second, 20*time.Millisecond, "恢复的确认单必须被执行")

	row, err := h.client.ToolInvocation.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, row.AttemptCount, "消费尝试次数必须留痕（B1-06）")
	assert.GreaterOrEqual(t, h.provider.callCount(), callsBefore+1)
}

// TestB1QueueRecovery_RepeatedRecoveryDoesNotDoubleExecute 覆盖抢占幂等：
// 同一确认单被重复恢复（或并发消费者）时，条件状态迁移保证只执行一次。
func TestB1QueueRecovery_RepeatedRecoveryDoesNotDoubleExecute(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	id := seedApprovedPending(t, h, "重复恢复")

	// 两次扫描（第二次时记录可能已 running/done，必须被 claim 挡住）。
	_, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	_, err = h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		row, getErr := h.client.ToolInvocation.Get(ctx, id)
		return getErr == nil && row.Status == "done"
	}, 10*time.Second, 20*time.Millisecond)

	// 关键断言：执行后再次恢复扫描不得重新入队（status != pending）。
	result, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	row, err := h.client.ToolInvocation.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "done", row.Status)
	assert.Equal(t, 1, row.AttemptCount, "重复恢复不得累加尝试次数（未再次抢占）")
	_ = result
}

// TestB1QueueRecovery_FailedRecordIsTerminal 覆盖失败终态：
// 写工具失败即终态（不自动重试），last_error_code 与 attempt_count 留痕。
func TestB1QueueRecovery_FailedRecordIsTerminal(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	h.provider.setExecErr(assert.AnError)
	t.Cleanup(func() { h.provider.setExecErr(nil) })

	id := seedApprovedPending(t, h, "失败终态")
	_, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		row, getErr := h.client.ToolInvocation.Get(ctx, id)
		return getErr == nil && row.Status == "failed"
	}, 10*time.Second, 20*time.Millisecond, "写工具失败即终态")

	row, err := h.client.ToolInvocation.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, row.AttemptCount)
	assert.NotEmpty(t, row.LastErrorCode, "最近一次消费错误码必须留痕（B1-06）")

	// 失败记录不会被恢复扫描重新捡起（要求 status=pending）。
	result, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	assert.Zero(t, result.Found, "终态记录不得被重复恢复")
}

// TestB1QueueVerifier_SkippedByDefaultAndWired 覆盖回读校验的两条口径：
// 未注入校验器 → skipped；注入后 update_ticket 的失败态可被识别为 failed。
func TestB1QueueVerifier_SkippedByDefaultAndWired(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()

	// 默认（无校验器）：成功终态落 skipped。
	id := seedApprovedPending(t, h, "默认跳过校验")
	_, err := h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		row, getErr := h.client.ToolInvocation.Get(ctx, id)
		return getErr == nil && row.Status == "done"
	}, 10*time.Second, 20*time.Millisecond)
	row, err := h.client.ToolInvocation.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, service.VerifyStateSkipped, row.VerifyState)

	// 注入「回读失败」的校验器：verify_state=failed 且 note 可见。
	h.queue.SetVerifier(fakeVerifier{state: service.VerifyStateFailed, note: "状态未生效：期望 resolved，实际 open"})
	id2 := seedApprovedPending(t, h, "校验失败")
	_, err = h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		row2, getErr := h.client.ToolInvocation.Get(ctx, id2)
		return getErr == nil && row2.Status == "done"
	}, 10*time.Second, 20*time.Millisecond)
	row2, err := h.client.ToolInvocation.Get(ctx, id2)
	require.NoError(t, err)
	assert.Equal(t, service.VerifyStateFailed, row2.VerifyState)
	assert.Contains(t, row2.VerifyNote, "状态未生效")
}

type fakeVerifier struct {
	state string
	note  string
}

func (f fakeVerifier) Verify(context.Context, int, string, map[string]interface{}, interface{}) (string, string) {
	return f.state, f.note
}

// TestB1QueueRecovery_DoesNotTouchUnapprovedPending 覆盖边界：
// 仅 approved+pending 会被恢复；普通 pending（待审批）与 rejected 不受影响。
func TestB1QueueRecovery_DoesNotTouchUnapprovedPending(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()

	_, pendingID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "仍待审批"})
	require.NoError(t, err)

	_, err = h.queue.RecoverPending(ctx, 0)
	require.NoError(t, err)

	row, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", row.ApprovalState)
	assert.Equal(t, "pending", row.Status, "待审批记录不得被恢复执行")
	_ = toolinvocation.FieldAttemptCount
}
