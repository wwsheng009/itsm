package bootstrap

import (
	"context"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/bootstraptoken"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"

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

// IP-P1-5 + C1（2026-10-05 浏览器 E2E 修复）：首管身份策略——
// provider 租户落 msp_admin/provider_admin + home membership；客户租户落
// admin/customer_user；平台根租户（code=default）保持 super_admin；
// 供给未完成（目标角色缺失）时 fail-fast（错误信息指导先跑模板供给）。
func TestCreateFirstAdmin_MSPTenantScopedIdentity(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:bootstrap-msp-identity?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	sugar := zaptest.NewLogger(t).Sugar()

	prov, err := client.Tenant.Create().
		SetName("Prov").SetCode("prov-scope").SetDomain("prov-scope.local").SetStatus("active").
		SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	mspAdminRole, err := client.Role.Create().
		SetName("MSP管理员").SetCode("msp_admin").SetTenantID(prov.ID).Save(ctx)
	require.NoError(t, err)

	adminID, err := CreateFirstAdmin(ctx, client, sugar, prov.ID, "A-secure-admin-password-2026!")
	require.NoError(t, err)
	admin, err := client.User.Get(ctx, adminID)
	require.NoError(t, err)
	require.Equal(t, user.Role("admin"), admin.Role, "provider 首管不得拿平台 super_admin（跨租户只读通配）")
	require.Equal(t, user.MspRole("provider_admin"), admin.MspRole)

	mb, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(adminID), usertenantmembership.TenantIDEQ(prov.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, usertenantmembership.AccountKindProvider, mb.AccountKind)
	require.Equal(t, usertenantmembership.SourceHome, mb.Source)
	require.True(t, mb.IsDefault)
	require.NotNil(t, mb.RoleID)
	require.Equal(t, mspAdminRole.ID, *mb.RoleID)
	require.NotNil(t, mb.MspRole)
	require.Equal(t, "msp_admin", *mb.MspRole)

	roles, err := client.User.Query().Where(user.IDEQ(adminID)).QueryRoles().All(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "msp_admin", roles[0].Code)

	// 客户租户：admin + customer_user + customer 作用域。
	cust, err := client.Tenant.Create().
		SetName("Cust").SetCode("cust-scope").SetDomain("cust-scope.local").SetStatus("active").
		SetType("msp_customer").SetMspProviderID(prov.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.Role.Create().
		SetName("管理员").SetCode("admin").SetTenantID(cust.ID).Save(ctx)
	require.NoError(t, err)
	custAdminID, err := CreateFirstAdmin(ctx, client, sugar, cust.ID, "A-secure-admin-password-2026!")
	require.NoError(t, err)
	custAdmin, err := client.User.Get(ctx, custAdminID)
	require.NoError(t, err)
	require.Equal(t, user.Role("admin"), custAdmin.Role)
	require.Equal(t, user.MspRole("customer_user"), custAdmin.MspRole)
	custMB, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(custAdminID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, usertenantmembership.AccountKindCustomer, custMB.AccountKind)

	// 平台根租户（saas_msp 模式其类型为 msp_provider）保持 super_admin，不写 membership。
	rootTenant, err := client.Tenant.Create().
		SetName("Root").SetCode("default").SetDomain("localhost").SetStatus("active").
		SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	rootAdminID, err := CreateFirstAdmin(ctx, client, sugar, rootTenant.ID, "A-secure-admin-password-2026!")
	require.NoError(t, err)
	rootAdmin, err := client.User.Get(ctx, rootAdminID)
	require.NoError(t, err)
	require.Equal(t, user.Role("super_admin"), rootAdmin.Role)
	require.Zero(t, client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(rootAdminID)).CountX(ctx))

	// 供给未完成（目标角色缺失）→ fail-fast。
	orphan, err := client.Tenant.Create().
		SetName("Orphan").SetCode("orphan-prov").SetDomain("orphan.local").SetStatus("active").
		SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	_, err = CreateFirstAdmin(ctx, client, sugar, orphan.ID, "A-secure-admin-password-2026!")
	require.ErrorContains(t, err, "msp_admin")
}
