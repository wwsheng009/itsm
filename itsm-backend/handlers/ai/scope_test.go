package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// B3-01 单元/集成测试：入口上下文解析、参数覆写、HTTP 语义映射、目标预检租户隔离。

func TestResolveRequestScope_NilResolverFailClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("关闭态默认 chat/无目标放行", func(t *testing.T) {
		scope, err := resolveRequestScope(ctx, nil, 1, 1, "agent", bot.ScopeInput{})
		require.NoError(t, err)
		assert.Equal(t, bot.EntrypointChat, scope.Entrypoint)
	})

	t.Run("关闭态携带非 chat 入口拒绝", func(t *testing.T) {
		_, err := resolveRequestScope(ctx, nil, 1, 1, "agent", bot.ScopeInput{Entrypoint: bot.EntrypointTicketDetail})
		assert.ErrorIs(t, err, bot.ErrScopeEntrypointUnknown)
	})

	t.Run("关闭态携带目标拒绝（不静默丢弃）", func(t *testing.T) {
		_, err := resolveRequestScope(ctx, nil, 1, 1, "agent", bot.ScopeInput{TargetType: bot.TargetTypeTicket, TargetID: 3})
		assert.ErrorIs(t, err, bot.ErrScopeCheckerUnavailable)
	})
}

func TestScopeHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{bot.ErrScopeEntrypointUnknown, 400, "AI_SCOPE_INVALID"},
		{bot.ErrScopeTargetPairIncomplete, 400, "AI_SCOPE_INVALID"},
		{bot.ErrScopeTargetTypeUnknown, 400, "AI_SCOPE_INVALID"},
		{bot.ErrScopeTargetDenied, 404, "AI_SCOPE_TARGET_UNAVAILABLE"},
		{bot.ErrScopeCheckerUnavailable, 503, "AI_SCOPE_UNAVAILABLE"},
		{assert.AnError, 500, "AI_SCOPE_ERROR"},
	}
	for _, tc := range cases {
		status, code := scopeHTTPStatus(tc.err)
		assert.Equal(t, tc.status, status, tc.err.Error())
		assert.Equal(t, tc.code, code, tc.err.Error())
	}
}

func TestSanitizeReservedArgs_StripsScopeProtocolKeys(t *testing.T) {
	cleaned, stripped := sanitizeReservedArgs(map[string]interface{}{
		"target_type": "ticket", "targetId": 9, "entrypoint": "ticket_detail",
		"title": "打印机故障", "ticket_id": 42, // 业务键必须保留
	})
	assert.Equal(t, []string{"entrypoint", "targetId", "target_type"}, stripped)
	assert.Equal(t, "打印机故障", cleaned["title"])
	assert.Equal(t, 42, cleaned["ticket_id"])
	assert.NotContains(t, cleaned, "target_type")
	assert.NotContains(t, cleaned, "entrypoint")
	assert.True(t, scopeProtocolKeysStripped(stripped))
}

func TestInjectScopeArgs_ServerResolvedOverwrite(t *testing.T) {
	scope := bot.Scope{Entrypoint: bot.EntrypointTicketDetail, TargetType: bot.TargetTypeTicket, TargetID: 7}

	t.Run("模型未使用协议键 → 入参形状不变", func(t *testing.T) {
		args := map[string]interface{}{"title": "x"}
		out := injectScopeArgs(args, scope, false)
		assert.Equal(t, map[string]interface{}{"title": "x"}, out)
	})

	t.Run("模型尝试覆写 → 以服务端解析值覆写", func(t *testing.T) {
		args := map[string]interface{}{"target_type": "ci", "target_id": 999, "title": "x"}
		out := injectScopeArgs(args, scope, true)
		assert.Equal(t, bot.TargetTypeTicket, out["target_type"])
		assert.Equal(t, 7, out["target_id"])
		assert.Equal(t, "x", out["title"])
	})

	t.Run("无服务端目标 → 保持剥离（模型不得凭空指定）", func(t *testing.T) {
		args := map[string]interface{}{"title": "x"}
		out := injectScopeArgs(args, bot.Scope{Entrypoint: bot.EntrypointChat}, true)
		assert.NotContains(t, out, "target_type")
		assert.NotContains(t, out, "target_id")
	})
}

func TestEntrypointFromContext_DefaultChat(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, bot.EntrypointChat, EntrypointFromContext(ctx))

	ctx = WithScope(ctx, bot.Scope{Entrypoint: bot.EntrypointIncidentDetail})
	assert.Equal(t, bot.EntrypointIncidentDetail, EntrypointFromContext(ctx))
}

// TestEntTargetChecker_TenantIsolationAndPermission：目标预检必须同时满足
// 「本租户存在」与「角色可读」——跨租户一律表现为不可用。
func TestEntTargetChecker_TenantIsolationAndPermission(t *testing.T) {
	ctx := context.Background()
	dsn := "file:ent-target-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("b3-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("b3-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("b3-a-user").SetEmail("b3-a@example.com").SetName("A User").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)
	ticketA := client.Ticket.Create().
		SetTenantID(tenantA.ID).SetTitle("A 的工单").SetTicketNumber("B3-A-1").
		SetRequesterID(userA.ID).
		SaveX(ctx)

	checker := newEntTargetChecker(client)
	require.NotNil(t, checker)

	// super_admin 直通 RBAC；存在性按租户收敛。
	assert.NoError(t, checker.CheckTarget(ctx, tenantA.ID, 1, "super_admin", bot.TargetTypeTicket, ticketA.ID))
	assert.Error(t, checker.CheckTarget(ctx, tenantB.ID, 1, "super_admin", bot.TargetTypeTicket, ticketA.ID))

	// 普通角色：资源读权限不足 → 拒绝（HardcodeOnly 模式下 agent 对 ticket:read 未授权）。
	assert.Error(t, checker.CheckTarget(ctx, tenantA.ID, 1, "some_unknown_role", bot.TargetTypeTicket, ticketA.ID))

	// 未知类型 → 拒绝。
	assert.Error(t, checker.CheckTarget(ctx, tenantA.ID, 1, "super_admin", "user", 1))
}
