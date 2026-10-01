package bootstrap

import (
	"context"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/bootstraptoken"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestBootstrapTokenLifecycleIsSingleUseAndAudited(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:bootstrap-token-lifecycle?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	rootTenant, err := client.Tenant.Create().
		SetName("Default").SetCode("default").SetDomain("default.local").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	manager := NewBootstrapTokenManager(client, zaptest.NewLogger(t).Sugar())

	oldToken, err := manager.GenerateToken(ctx, rootTenant.ID)
	require.NoError(t, err)
	newToken, err := manager.GenerateToken(ctx, rootTenant.ID)
	require.NoError(t, err)
	require.NotEqual(t, oldToken, newToken)
	require.Equal(t, 1, mustCountBootstrapTokens(t, client, true))
	require.Equal(t, 1, mustCountBootstrapTokens(t, client, false))

	_, err = manager.ConsumeToken(ctx, oldToken, rootTenant.ID, "A-secure-admin-password-2026!")
	require.Error(t, err)
	adminID, err := manager.ConsumeToken(ctx, newToken, rootTenant.ID, "A-secure-admin-password-2026!")
	require.NoError(t, err)
	require.Positive(t, adminID)
	require.Equal(t, 1, mustCountUsers(t, client))
	require.Equal(t, 1, mustCountBootstrapAudits(t, client))

	required, available, expiresAt, err := manager.Status(ctx, rootTenant.ID)
	require.NoError(t, err)
	require.False(t, required)
	require.False(t, available)
	require.Nil(t, expiresAt)
	_, err = manager.ConsumeToken(ctx, newToken, rootTenant.ID, "A-secure-admin-password-2026!")
	require.Error(t, err)
}

// IP-P1-5 / 07:G2：账号策略 admin-<tenantCode>，连续 bootstrapping 两个租户互不冲突。
func TestBootstrapToken_MultiTenantAdminsDoNotCollide(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:bootstrap-token-multitenant?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	manager := NewBootstrapTokenManager(client, zaptest.NewLogger(t).Sugar())

	for _, code := range []string{"msp-acme", "msp-beta"} {
		tnt, err := client.Tenant.Create().
			SetName(code).SetCode(code).SetDomain(code + ".local").SetStatus("active").
			Save(ctx)
		require.NoError(t, err)
		token, err := manager.GenerateToken(ctx, tnt.ID)
		require.NoError(t, err)
		adminID, err := manager.ConsumeToken(ctx, token, tnt.ID, "A-secure-admin-password-2026!")
		require.NoError(t, err, "第二个租户 bootstrap 不得因全局唯一冲突失败")
		admin, err := client.User.Get(ctx, adminID)
		require.NoError(t, err)
		require.Equal(t, "admin-"+code, admin.Username)
		require.Equal(t, "admin-"+code+"@bootstrap.local", admin.Email)
		require.True(t, admin.MustChangePassword, "bootstrap 管理员默认首登强制改密")
	}
	require.Equal(t, 2, mustCountUsers(t, client))
}

// IP-P1-5：provision_tenant 无 token 通道——CreateFirstAdmin 幂等 + 身份策略一致。
func TestCreateFirstAdmin_IdempotentAndTenantScoped(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:bootstrap-first-admin?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	sugar := zaptest.NewLogger(t).Sugar()

	tnt, err := client.Tenant.Create().
		SetName("Prov").SetCode("prov-1").SetDomain("prov-1.local").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	adminID, err := CreateFirstAdmin(ctx, client, sugar, tnt.ID, "A-secure-admin-password-2026!")
	require.NoError(t, err)
	admin, err := client.User.Get(ctx, adminID)
	require.NoError(t, err)
	require.Equal(t, "admin-prov-1", admin.Username)
	require.True(t, admin.MustChangePassword)

	_, err = CreateFirstAdmin(ctx, client, sugar, tnt.ID, "A-secure-admin-password-2026!")
	require.ErrorIs(t, err, ErrAdminExists)

	// 显式覆盖身份仍遵守幂等保护（不产生第二管理员）。
	_, err = CreateFirstAdmin(ctx, client, sugar, tnt.ID, "A-secure-admin-password-2026!",
		WithAdminIdentity("ops-admin", "ops@example.com"))
	require.ErrorIs(t, err, ErrAdminExists)
	require.Equal(t, 1, mustCountUsers(t, client))
}

func mustCountBootstrapTokens(t *testing.T, client *ent.Client, used bool) int {
	t.Helper()
	count, err := client.BootstrapToken.Query().Where(bootstraptoken.UsedEQ(used)).Count(context.Background())
	require.NoError(t, err)
	return count
}

func mustCountUsers(t *testing.T, client *ent.Client) int {
	t.Helper()
	count, err := client.User.Query().Where(user.IsBootstrapAdminEQ(true)).Count(context.Background())
	require.NoError(t, err)
	return count
}

func mustCountBootstrapAudits(t *testing.T, client *ent.Client) int {
	t.Helper()
	count, err := client.AuditLog.Query().Where(auditlog.ActionEQ("BOOTSTRAP_ADMIN_CREATED")).Count(context.Background())
	require.NoError(t, err)
	return count
}
