package botintegration

import (
	"context"
	"testing"
	"time"

	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/service/bot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 B1-05 确认状态机的四条路径：
// 过期（惰性 + 扫描）、幂等回放（同人同向重试）、冲突（异人/改判）、无第二次执行。
// 复用 B0-07 的桩工具与真实 ent/SQLite 环境（b0Harness）。

// expireNow 把指定 pending 记录的有效期改到过去（模拟时间流逝，避免 sleep）。
func expireNow(t *testing.T, h *b0Harness, id int) {
	t.Helper()
	past := time.Now().Add(-time.Minute)
	_, err := h.client.ToolInvocation.UpdateOneID(id).
		SetExpiresAt(past).
		Save(context.Background())
	require.NoError(t, err)
}

// TestB1Confirmation_ExpiredCannotBeApproved 覆盖惰性过期：
// 超过 expires_at 的确认单在审批入口被拒（不改执行、不改业务状态），并落 expired 终态。
func TestB1Confirmation_ExpiredCannotBeApproved(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	h.svc.SetConfirmationTTL(time.Hour) // 先有 TTL，再手工改过期

	_, pendingID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "过期单-1"})
	require.NoError(t, err)
	require.Greater(t, pendingID, 0)

	// 创建时即落 expires_at（默认 TTL 生效）。
	created, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	require.NotNil(t, created.ExpiresAt, "pending 必须带有效期（B1-05）")

	expireNow(t, h, pendingID)
	callsBefore := h.provider.callCount()

	_, err = h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "过期后补批")
	require.ErrorIs(t, err, ai.ErrInvocationExpired)

	after, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, string(bot.ConfirmationExpired), after.ApprovalState)
	assert.Equal(t, string(bot.ConfirmationExpired), after.Status)
	assert.Equal(t, callsBefore, h.provider.callCount(), "过期单不得触发任何执行")
}

// TestB1Confirmation_SweeperMarksExpired 覆盖周期扫描：
// SweepExpiredConfirmations 只置位真正超期的 pending，且重复扫描幂等。
func TestB1Confirmation_SweeperMarksExpired(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()

	_, expiredID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "扫描过期"})
	require.NoError(t, err)
	_, freshID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "扫描保留"})
	require.NoError(t, err)

	expireNow(t, h, expiredID)

	result, err := h.svc.SweepExpiredConfirmations(ctx, time.Now(), 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Expired, 1)

	expiredRow, err := h.client.ToolInvocation.Get(ctx, expiredID)
	require.NoError(t, err)
	assert.Equal(t, string(bot.ConfirmationExpired), expiredRow.ApprovalState)

	freshRow, err := h.client.ToolInvocation.Get(ctx, freshID)
	require.NoError(t, err)
	assert.Equal(t, "pending", freshRow.ApprovalState, "未超期记录不受扫描影响")

	// 幂等：重复扫描不再计入（Expire 条件更新只对 pending 生效）。
	second, err := h.svc.SweepExpiredConfirmations(ctx, time.Now(), 0)
	require.NoError(t, err)
	assert.Zero(t, second.Expired)
}

// TestB1Confirmation_ReplayAndConflict 覆盖幂等回放与冲突语义：
// 同人同向重试 = 回放（无第二次执行）；异人/改判 = 冲突。
func TestB1Confirmation_ReplayAndConflict(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	otherUser, err := h.client.User.Create().
		SetUsername("b1-05-other").
		SetEmail("b1-05-other@example.com").
		SetName("B1-05 Other").
		SetPasswordHash("x").
		SetTenantID(h.tenantID).
		Save(ctx)
	require.NoError(t, err)

	_, pendingID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "回放路径"})
	require.NoError(t, err)

	state, err := h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "同意")
	require.NoError(t, err)
	require.Equal(t, "approved", state)
	require.Eventually(t, func() bool {
		row, getErr := h.client.ToolInvocation.Get(ctx, pendingID)
		return getErr == nil && row.Status == "done"
	}, 10*time.Second, 20*time.Millisecond, "确认后必须被执行一次")
	callsAfterFirst := h.provider.callCount()

	// 同人同向重试：回放既有决策，不报错、不重复执行、不改库。
	state, err = h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "重试")
	require.NoError(t, err, "同人同向重试必须幂等回放")
	assert.Equal(t, "approved", state)
	assert.Equal(t, callsAfterFirst, h.provider.callCount(), "回放不得触发第二次执行")

	// 异人决策：冲突可见（不静默回放）。
	_, err = h.svc.ApproveTool(ctx, pendingID, h.tenantID, otherUser.ID, true, "他人补批")
	require.ErrorIs(t, err, ai.ErrInvocationStateConflict)

	// 改判（同人反向）：冲突。
	_, err = h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, false, "改判拒绝")
	require.ErrorIs(t, err, ai.ErrInvocationStateConflict)
	assert.Equal(t, callsAfterFirst, h.provider.callCount(), "冲突路径不得触发执行")

	// 拒绝路径的重复拒绝同样回放。
	_, rejectID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "拒绝回放"})
	require.NoError(t, err)
	state, err = h.svc.ApproveTool(ctx, rejectID, h.tenantID, h.userID, false, "风险过高")
	require.NoError(t, err)
	assert.Equal(t, "rejected", state)
	state, err = h.svc.ApproveTool(ctx, rejectID, h.tenantID, h.userID, false, "再拒绝一次")
	require.NoError(t, err)
	assert.Equal(t, "rejected", state)
	assert.Equal(t, callsAfterFirst, h.provider.callCount(), "拒绝路径不得执行")
}

// TestB1Confirmation_ExpiresAtNotSetWhenTTLDisabled 固化「TTL<=0 = 不设期限」的离线口径，
// 防止测试/离线装配被误认为生产默认。
func TestB1Confirmation_ExpiresAtNotSetWhenTTLDisabled(t *testing.T) {
	h := newB0Harness(t)
	ctx := context.Background()
	h.svc.SetConfirmationTTL(0)

	_, pendingID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "sysadmin", "stub__create_note",
		map[string]interface{}{"note": "无期限"})
	require.NoError(t, err)

	row, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Nil(t, row.ExpiresAt, "TTL<=0 时不写有效期（仅离线/测试口径）")
	assert.Equal(t, "pending", row.ApprovalState)
	_ = toolinvocation.FieldExpiresAt // 编译期锚点：字段名与 ent 生成物一致
}
