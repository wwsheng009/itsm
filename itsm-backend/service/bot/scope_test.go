package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B3-01 单元测试：入口归一化、目标参数成对校验、目标预检 fail-closed。

type stubChecker struct {
	calls  int
	denied bool
	seen   struct {
		tenantID, userID int
		role             string
		targetType       string
		targetID         int
	}
}

func (c *stubChecker) CheckTarget(_ context.Context, tenantID, userID int, role, targetType string, targetID int) error {
	c.calls++
	c.seen.tenantID, c.seen.userID, c.seen.role = tenantID, userID, role
	c.seen.targetType, c.seen.targetID = targetType, targetID
	if c.denied {
		return ErrScopeTargetDenied
	}
	return nil
}

func TestScopeResolver_EntrypointNormalization(t *testing.T) {
	resolver := NewScopeResolver(nil)
	ctx := context.Background()

	t.Run("空值与空白归一为 chat", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "\t"} {
			scope, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{Entrypoint: raw})
			require.NoError(t, err)
			assert.Equal(t, EntrypointChat, scope.Entrypoint)
			assert.False(t, scope.HasTarget())
		}
	})

	t.Run("已知入口接受（大小写归一）", func(t *testing.T) {
		for _, entrypoint := range KnownEntrypoints() {
			scope, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{Entrypoint: strings.ToUpper(entrypoint)})
			require.NoError(t, err)
			assert.Equal(t, entrypoint, scope.Entrypoint)
		}
	})

	t.Run("未知入口拒绝（不静默降级）", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{Entrypoint: "ticketdetail"})
		assert.ErrorIs(t, err, ErrScopeEntrypointUnknown)
	})

	t.Run("summary 按 rune 截断", func(t *testing.T) {
		long := strings.Repeat("中", ScopeSummaryMaxRunes+50)
		scope, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{Entrypoint: EntrypointChat, Summary: "  " + long + "  "})
		require.NoError(t, err)
		assert.Equal(t, ScopeSummaryMaxRunes, len([]rune(scope.Summary)))
	})
}

func TestScopeResolver_TargetPairing(t *testing.T) {
	resolver := NewScopeResolver(&stubChecker{})
	ctx := context.Background()

	t.Run("只给类型不给 ID 拒绝", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{TargetType: TargetTypeTicket})
		assert.ErrorIs(t, err, ErrScopeTargetPairIncomplete)
	})

	t.Run("只给 ID 不给类型拒绝", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{TargetID: 7})
		assert.ErrorIs(t, err, ErrScopeTargetPairIncomplete)
	})

	t.Run("未知目标类型拒绝", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, 1, 1, "agent", ScopeInput{TargetType: "user", TargetID: 7})
		assert.ErrorIs(t, err, ErrScopeTargetTypeUnknown)
	})

	t.Run("校验器缺失 fail-closed", func(t *testing.T) {
		_, err := NewScopeResolver(nil).Resolve(ctx, 1, 1, "agent", ScopeInput{TargetType: TargetTypeTicket, TargetID: 7})
		assert.ErrorIs(t, err, ErrScopeCheckerUnavailable)
	})

	t.Run("校验通过携带目标与调用参数透传", func(t *testing.T) {
		checker := &stubChecker{}
		scope, err := NewScopeResolver(checker).Resolve(ctx, 11, 22, "agent", ScopeInput{
			Entrypoint: EntrypointTicketDetail, TargetType: "Ticket", TargetID: 42, Summary: "现场",
		})
		require.NoError(t, err)
		assert.True(t, scope.HasTarget())
		assert.Equal(t, TargetTypeTicket, scope.TargetType)
		assert.Equal(t, 42, scope.TargetID)
		assert.Equal(t, "现场", scope.Summary)
		assert.Equal(t, 1, checker.calls)
		assert.Equal(t, 11, checker.seen.tenantID)
		assert.Equal(t, 22, checker.seen.userID)
		assert.Equal(t, "agent", checker.seen.role)
	})

	t.Run("校验失败 → 目标不可用（fail-closed）", func(t *testing.T) {
		checker := &stubChecker{denied: true}
		_, err := NewScopeResolver(checker).Resolve(ctx, 1, 1, "agent", ScopeInput{TargetType: TargetTypeCI, TargetID: 7})
		assert.ErrorIs(t, err, ErrScopeTargetDenied)
		assert.Equal(t, 1, checker.calls)
	})
}

func TestScopeResolver_ErrorSentinelWrapping(t *testing.T) {
	checker := &stubChecker{denied: true}
	_, err := NewScopeResolver(checker).Resolve(context.Background(), 1, 1, "agent", ScopeInput{TargetType: TargetTypeIncident, TargetID: 5})
	require.Error(t, err)
	// 包装层必须保留哨兵语义（HTTP 层按 errors.Is 映射状态码）。
	assert.True(t, errors.Is(err, ErrScopeTargetDenied))
	assert.Contains(t, err.Error(), "incident")
}
