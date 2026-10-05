package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
)

// tenant_user_admin_test.go：TUM-1/TUM-2 后端读通道 + 账号治理核心行为。

type tenantUserAdminFixture struct {
	svc     *TenantUserAdminService
	client  *ent.Client
	ctx     context.Context
	tenantA int
	tenantB int
	adminA1 int
	adminA2 int
	normalA int
	adminB  int
	actor   TenantAdminActor
}

func newTenantUserAdminFixture(t *testing.T) *tenantUserAdminFixture {
	t.Helper()
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	tenantA, err := client.Tenant.Create().SetName("客户A").SetCode("tua-a").SetStatus("active").SetType("saas_customer").Save(ctx)
	require.NoError(t, err)
	tenantB, err := client.Tenant.Create().SetName("客户B").SetCode("tua-b").SetStatus("active").SetType("saas_customer").Save(ctx)
	require.NoError(t, err)

	adminA1 := createTUAUser(t, client, tenantA.ID, "tua-a-admin1", user.RoleAdmin)
	adminA2 := createTUAUser(t, client, tenantA.ID, "tua-a-admin2", user.RoleAdmin)
	normalA := createTUAUser(t, client, tenantA.ID, "tua-a-user1", user.RoleEndUser)
	adminB := createTUAUser(t, client, tenantB.ID, "tua-b-admin1", user.RoleAdmin)

	svc := NewTenantUserAdminService(client, NewUserService(client, logger), logger)
	svc.SetEnabled(true)
	return &tenantUserAdminFixture{
		svc: svc, client: client, ctx: ctx,
		tenantA: tenantA.ID, tenantB: tenantB.ID,
		adminA1: adminA1.ID, adminA2: adminA2.ID, normalA: normalA.ID, adminB: adminB.ID,
		actor: TenantAdminActor{UserID: 9001, HomeTenantID: tenantB.ID, Role: "super_admin", Username: "platform-admin"},
	}
}

func createTUAUser(t *testing.T, client *ent.Client, tenantID int, username string, role user.Role) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@example.com").
		SetName(username).
		SetPasswordHash("x").
		SetTenantID(tenantID).
		SetRole(role).
		SetActive(true).
		Save(context.Background())
	require.NoError(t, err)
	return u
}

func TestTenantUserAdmin_ListUsers(t *testing.T) {
	f := newTenantUserAdminFixture(t)

	resp, err := f.svc.ListUsers(f.ctx, f.actor, f.tenantA, &dto.TenantUserListQuery{})
	require.NoError(t, err)
	assert.EqualValues(t, 3, resp.Total, "仅返回目标租户用户（跨租户隔离）")

	resp, err = f.svc.ListUsers(f.ctx, f.actor, f.tenantA, &dto.TenantUserListQuery{Keyword: "admin1"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, resp.Total, "关键字命中用户名")

	resp, err = f.svc.ListUsers(f.ctx, f.actor, f.tenantA, &dto.TenantUserListQuery{Status: "inactive"})
	require.NoError(t, err)
	assert.EqualValues(t, 0, resp.Total)

	resp, err = f.svc.ListUsers(f.ctx, f.actor, f.tenantA, &dto.TenantUserListQuery{Role: "end_user"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, resp.Total)

	// 非平台角色 fail-closed（TUM-D1）。
	_, err = f.svc.ListUsers(f.ctx, TenantAdminActor{UserID: 1, Role: "admin"}, f.tenantA, nil)
	ae, ok := AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodePlatformScope, ae.Code)
	assert.Equal(t, 403, ae.Status)

	// 跨租户用户按双键隔离：B 租户用户 id 在 A 租户命名空间下不可见。
	_, err = f.svc.GetUser(f.ctx, f.actor, f.tenantA, f.adminB)
	ae, ok = AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodeUserNotFound, ae.Code)
}

func TestTenantUserAdmin_ResetPassword(t *testing.T) {
	f := newTenantUserAdminFixture(t)

	// generated：满足默认策略（大写/小写/数字），强制改密，refresh 双吊销。
	resp, err := f.svc.ResetPassword(f.ctx, f.actor, f.tenantA, f.normalA, &dto.ResetTenantUserPasswordRequest{Mode: dto.TenantUserResetModeGenerated})
	require.NoError(t, err)
	assert.Equal(t, dto.TenantUserResetModeGenerated, resp.Mode)
	assert.True(t, resp.MustChangePassword)
	assert.GreaterOrEqual(t, len(resp.GeneratedPassword), 8)
	assert.True(t, hasUpper(resp.GeneratedPassword) && hasLower(resp.GeneratedPassword) && hasNumber(resp.GeneratedPassword))

	updated, err := f.client.User.Get(f.ctx, f.normalA)
	require.NoError(t, err)
	assert.True(t, updated.MustChangePassword)

	revoked, err := middleware.IsUserRefreshRevoked(f.ctx, f.normalA, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked, "重置后历史 refresh 必须被吊销")
	revoked, err = middleware.IsUserRefreshRevoked(f.ctx, f.normalA, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, revoked, "吊销后新签发 refresh 可用")

	// specified：弱口令拒绝 + 强口令放行且不强制改密。
	_, err = f.svc.ResetPassword(f.ctx, f.actor, f.tenantA, f.normalA, &dto.ResetTenantUserPasswordRequest{
		Mode: dto.TenantUserResetModeSpecified, NewPassword: "short",
	})
	ae, ok := AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodePasswordPolicy, ae.Code)

	resp, err = f.svc.ResetPassword(f.ctx, f.actor, f.tenantA, f.normalA, &dto.ResetTenantUserPasswordRequest{
		Mode: dto.TenantUserResetModeSpecified, NewPassword: "StrongPass123",
	})
	require.NoError(t, err)
	assert.False(t, resp.MustChangePassword)
	assert.Empty(t, resp.GeneratedPassword)
}

func TestTenantUserAdmin_SetActiveGuards(t *testing.T) {
	f := newTenantUserAdminFixture(t)
	enable := true
	disable := false

	// 停用第二个管理员：允许（仍有 adminA1 可用）。
	require.NoError(t, f.svc.SetActive(f.ctx, f.actor, f.tenantA, f.adminA2, &dto.SetTenantUserStatusRequest{Active: &disable}))
	u, err := f.client.User.Get(f.ctx, f.adminA2)
	require.NoError(t, err)
	assert.False(t, u.Active)
	revoked, err := middleware.IsUserRefreshRevoked(f.ctx, f.adminA2, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked, "停用后历史 refresh 必须被吊销")

	// 最后一个管理员：拒绝（409 LAST_ADMIN_PROTECTED）。
	err = f.svc.SetActive(f.ctx, f.actor, f.tenantA, f.adminA1, &dto.SetTenantUserStatusRequest{Active: &disable})
	ae, ok := AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodeLastAdmin, ae.Code)
	assert.Equal(t, 409, ae.Status)

	// 启用恢复。
	require.NoError(t, f.svc.SetActive(f.ctx, f.actor, f.tenantA, f.adminA2, &dto.SetTenantUserStatusRequest{Active: &enable}))

	// 禁止停用调用者自身。
	selfActor := f.actor
	selfActor.UserID = f.normalA
	err = f.svc.SetActive(f.ctx, selfActor, f.tenantA, f.normalA, &dto.SetTenantUserStatusRequest{Active: &disable})
	ae, ok = AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodeSelfOperation, ae.Code)
}

func TestTenantUserAdmin_DefaultTenantConfirm(t *testing.T) {
	f := newTenantUserAdminFixture(t)
	defTenant, err := f.client.Tenant.Create().SetName("平台默认").SetCode("default").SetStatus("active").Save(f.ctx)
	require.NoError(t, err)
	defUser := createTUAUser(t, f.client, defTenant.ID, "tua-default-user", user.RoleEndUser)

	disable := false
	err = f.svc.SetActive(f.ctx, f.actor, defTenant.ID, defUser.ID, &dto.SetTenantUserStatusRequest{Active: &disable})
	ae, ok := AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodeConfirmRequired, ae.Code, "default 租户停用需二次确认")

	require.NoError(t, f.svc.SetActive(f.ctx, f.actor, defTenant.ID, defUser.ID, &dto.SetTenantUserStatusRequest{Active: &disable, ConfirmCode: "default"}))
}

func TestTenantUserAdmin_DisabledFlagAndForceLogout(t *testing.T) {
	f := newTenantUserAdminFixture(t)

	// 灰度关闭：写通道 403 TENANT_USER_ADMIN_DISABLED（读通道不受限）。
	f.svc.SetEnabled(false)
	_, err := f.svc.ForceLogout(f.ctx, f.actor, f.tenantA, f.normalA)
	ae, ok := AsTenantUserAdminError(err)
	require.True(t, ok)
	assert.Equal(t, TenantUserAdminCodeDisabled, ae.Code)
	assert.Equal(t, 403, ae.Status)
	_, err = f.svc.ListUsers(f.ctx, f.actor, f.tenantA, nil)
	assert.NoError(t, err, "读通道不随写开关关闭")

	// 开启后强制下线：双吊销生效。
	f.svc.SetEnabled(true)
	resp, err := f.svc.ForceLogout(f.ctx, f.actor, f.tenantA, f.normalA)
	require.NoError(t, err)
	assert.Equal(t, f.normalA, resp.UserID)
	revoked, err := middleware.IsUserRefreshRevoked(f.ctx, f.normalA, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked)
}

// TestTenantUserAdmin_AuditRows TUM-3：三类治理动作显式审计行必须携带 target_user_id，
// 行归属 actor 家租户 + target_tenant_id=目标租户 + source=platform_selected；
// 一次性口令绝不落审计（RequestBody 不含明文）。
func TestTenantUserAdmin_AuditRows(t *testing.T) {
	f := newTenantUserAdminFixture(t)

	resetResp, err := f.svc.ResetPassword(f.ctx, f.actor, f.tenantA, f.normalA, &dto.ResetTenantUserPasswordRequest{Mode: dto.TenantUserResetModeGenerated})
	require.NoError(t, err)

	disable := false
	require.NoError(t, f.svc.SetActive(f.ctx, f.actor, f.tenantA, f.adminA2, &dto.SetTenantUserStatusRequest{Active: &disable}))

	_, err = f.svc.ForceLogout(f.ctx, f.actor, f.tenantA, f.normalA)
	require.NoError(t, err)

	logs, err := f.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(f.tenantB),
			auditlog.ActionIn("user.admin_password_reset", "user.admin_status", "user.admin_force_logout"),
		).
		All(f.ctx)
	require.NoError(t, err)
	require.Len(t, logs, 3, "三类治理动作各 1 行显式审计")

	byAction := map[string]*ent.AuditLog{}
	for _, l := range logs {
		byAction[l.Action] = l
		assert.Equal(t, f.tenantA, l.TargetTenantID, "target_tenant_id=目标租户")
		assert.Equal(t, f.tenantB, l.TenantID, "行归属 actor 家租户")
		assert.Equal(t, f.actor.UserID, l.UserID)
		assert.Equal(t, "platform-admin", l.ActorAccount)
		assert.Equal(t, middleware.AuditSourcePlatformSelected, l.Source)
	}

	resetRow := byAction["user.admin_password_reset"]
	require.NotNil(t, resetRow)
	assert.Equal(t, f.normalA, resetRow.TargetUserID)
	if resetRow.RequestBody != nil {
		assert.NotContains(t, *resetRow.RequestBody, resetResp.GeneratedPassword, "一次性口令不落审计")
	}

	statusRow := byAction["user.admin_status"]
	require.NotNil(t, statusRow)
	assert.Equal(t, f.adminA2, statusRow.TargetUserID)

	logoutRow := byAction["user.admin_force_logout"]
	require.NotNil(t, logoutRow)
	assert.Equal(t, f.normalA, logoutRow.TargetUserID)
}
