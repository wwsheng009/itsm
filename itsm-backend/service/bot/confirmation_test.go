package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeConfirmationState_兼容与派生 覆盖状态归一化矩阵：
// 既有 approval_state 取值（含 approved/none/auto）与 status 派生都必须稳定。
func TestNormalizeConfirmationState_兼容与派生(t *testing.T) {
	cases := []struct {
		name     string
		approval string
		status   string
		want     ConfirmationState
	}{
		{"pending", "pending", "pending", ConfirmationPending},
		{"approved 映射为 confirmed", "approved", "success", ConfirmationConfirmed},
		{"rejected", "rejected", "rejected", ConfirmationRejected},
		{"expired", "expired", "expired", ConfirmationExpired},
		{"cancelled", "cancelled", "cancelled", ConfirmationCancelled},
		{"auto+success → unknown（只读）", "auto", "success", ConfirmationUnknown},
		{"none+空 → unknown", "none", "", ConfirmationUnknown},
		{"空值+failed → unknown", "", "failed", ConfirmationUnknown},
		{"approval 未知但 status=pending → pending", "weird", "pending", ConfirmationPending},
		{"approval 未知且 status 未知 → unknown", "weird", "weird", ConfirmationUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeConfirmationState(tc.approval, tc.status))
		})
	}
}

// TestConfirmationState_判定 固化「仅 pending 可决策 / 终态集合」两个不变量。
func TestConfirmationState_判定(t *testing.T) {
	assert.True(t, ConfirmationPending.IsDecidable())
	for _, state := range []ConfirmationState{
		ConfirmationConfirmed, ConfirmationRejected, ConfirmationExpired,
		ConfirmationCancelled, ConfirmationUnknown,
	} {
		assert.False(t, state.IsDecidable(), "%s 不可决策", state)
	}
	assert.True(t, ConfirmationConfirmed.IsTerminal())
	assert.True(t, ConfirmationRejected.IsTerminal())
	assert.True(t, ConfirmationExpired.IsTerminal())
	assert.True(t, ConfirmationCancelled.IsTerminal())
	assert.False(t, ConfirmationPending.IsTerminal())
	assert.False(t, ConfirmationUnknown.IsTerminal(), "unknown 属展示口径，不参与迁移")
}

// TestEvaluateDecision_幂等回放与冲突 覆盖四类语义：首次 / 同人同向重试 / 异人 / 改判。
func TestEvaluateDecision_幂等回放与冲突(t *testing.T) {
	const actor, other = 7, 8
	cases := []struct {
		name       string
		current    ConfirmationState
		approvedBy int
		actor      int
		approveNow bool
		prior      bool
		want       DecisionOutcome
	}{
		{"首次确认", ConfirmationPending, 0, actor, true, false, DecisionApply},
		{"首次拒绝", ConfirmationPending, 0, actor, false, false, DecisionApply},
		{"同人重复确认 → 回放", ConfirmationConfirmed, actor, actor, true, true, DecisionReplay},
		{"同人重复拒绝 → 回放", ConfirmationRejected, actor, actor, false, false, DecisionReplay},
		{"异人重复 → 冲突", ConfirmationConfirmed, actor, other, true, true, DecisionConflict},
		{"同人改判 → 冲突", ConfirmationRejected, actor, actor, true, false, DecisionConflict},
		{"过期后重试 → 冲突（需重新发起）", ConfirmationExpired, actor, actor, true, true, DecisionConflict},
		{"未知状态 → 冲突", ConfirmationUnknown, actor, actor, true, true, DecisionConflict},
		{"决策人缺失 → 冲突", ConfirmationConfirmed, 0, actor, true, true, DecisionConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EvaluateDecision(tc.current, tc.approvedBy, tc.actor, tc.approveNow, tc.prior))
		})
	}
}

// TestIsExpiredAt 固化过期判定：零值/空指针 = 不设期限。
func TestIsExpiredAt(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	assert.False(t, IsExpiredAt(nil, now))
	zero := time.Time{}
	assert.False(t, IsExpiredAt(&zero, now))
	assert.True(t, IsExpiredAt(&past, now))
	assert.False(t, IsExpiredAt(&future, now))
}

type fakeStore struct {
	items   []ExpirableConfirmation
	expired []int
	failExp bool
}

func (f *fakeStore) ListExpirable(context.Context, time.Time, int) ([]ExpirableConfirmation, error) {
	return f.items, nil
}

func (f *fakeStore) Expire(_ context.Context, tenantID, id int, _ time.Time) (bool, error) {
	if f.failExp {
		return false, assert.AnError
	}
	f.expired = append(f.expired, tenantID*1000+id)
	return true, nil
}

// TestSweepOnce 覆盖：只过期真正超期的记录；重复扫描不重复计数；单条错误向上传递。
func TestSweepOnce(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	store := &fakeStore{items: []ExpirableConfirmation{
		{ID: 1, TenantID: 1, ExpiresAt: &past},
		{ID: 2, TenantID: 1, ExpiresAt: &future}, // 竞态：已被续期
		{ID: 3, TenantID: 2, ExpiresAt: nil},     // 不设期限
	}}
	result, err := SweepOnce(context.Background(), store, now, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Scanned)
	assert.Equal(t, 1, result.Expired)
	assert.Equal(t, []int{1001}, store.expired)

	// 存储层报错必须上抛（由调用方决定告警）。
	store.failExp = true
	_, err = SweepOnce(context.Background(), store, now, 0)
	require.Error(t, err)
}

// TestSweeper_NilStoreIsNoop 防御：未装配存储时 Run 立即返回（不 panic）。
func TestSweeper_NilStoreIsNoop(t *testing.T) {
	done := make(chan struct{})
	go func() {
		(&Sweeper{}).Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("空 Sweeper 必须立即返回")
	}
}
