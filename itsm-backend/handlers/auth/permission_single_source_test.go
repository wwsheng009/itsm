package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	commonhandlers "itsm-backend/handlers/common"
	"itsm-backend/middleware"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

// =============================================================================
// IP-P1-2 端点级一致性：Login / GetMe / SwitchTenant / RefreshToken 同源解析器，
// 且切换后按目标租户计算（A6）。测试不依赖环境变量（AUTHZ_STATIC_FALLBACK 显式置 false），
// 通过同一解析器结果对比证明 Login/Me/Switch/Refresh 四处同源。
// =============================================================================

func endpointPermSeedRole(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, code string, resource, action string) *ent.Role {
	t.Helper()
	roleEntity, err := client.Role.Create().
		SetName(code).
		SetCode(code).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	permEntity, err := client.Permission.Create().
		SetCode(resource + ":" + action).
		SetName(resource + ":" + action).
		SetResource(resource).
		SetAction(action).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().
		SetRoleID(roleEntity.ID).
		SetPermissionID(permEntity.ID).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	return roleEntity
}

func TestPermissionEndpoints_ShareMembershipSingleSource(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	dsn := fmt.Sprintf("file:auth_perm_endpoint_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	defer func() { _ = client.Close() }()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })

	// provider 家租户 P（登录落点）+ 客户租户 C（切换目标）。
	providerTenant, err := client.Tenant.Create().
		SetName("Provider").SetCode("prov").SetStatus("active").SetType("msp_provider").
		Save(ctx)
	require.NoError(t, err)
	customerTenant, err := client.Tenant.Create().
		SetName("Customer").SetCode("cust").SetStatus("active").SetType("msp_customer").
		Save(ctx)
	require.NoError(t, err)

	hashed, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	userEntity, err := client.User.Create().
		SetUsername("perm-single-source-user").
		SetEmail("perm-single-source@example.com").
		SetName("Perm Single Source").
		SetPasswordHash(string(hashed)).
		SetRole(user.RoleEndUser).
		SetMspRole(user.MspRoleProviderAdmin).
		SetActive(true).
		SetTenantID(providerTenant.ID).
		Save(ctx)
	require.NoError(t, err)

	// P 租户角色：ticket:read；C 租户角色：incident:write —— A/B 权限刻意不同。
	providerRole := endpointPermSeedRole(t, ctx, client, providerTenant.ID, "endpoint_role_p", "ticket", "read")
	customerRole := endpointPermSeedRole(t, ctx, client, customerTenant.ID, "endpoint_role_c", "incident", "write")

	_, err = client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(providerTenant.ID).
		SetRoleID(providerRole.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(customerTenant.ID).
		SetRoleID(customerRole.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceAllocation).
		Save(ctx)
	require.NoError(t, err)

	// 切换前提：provider → customer 的有效 allocation。
	_, err = client.MSPAllocation.Create().
		SetMspUserID(userEntity.ID).
		SetCustomerTenantID(customerTenant.ID).
		SetProviderTenantID(providerTenant.ID).
		Save(ctx)
	require.NoError(t, err)

	const jwtSecret = "perm-single-source-jwt-secret"
	commonSvc := commonhandlers.NewService(commonhandlers.NewEntRepository(client), jwtSecret, logger, client)
	authSvc := NewService(client, jwtSecret, logger, nil)

	// 1) Login：落 home 租户 P，权限来自 P 的 membership → role_permissions。
	loginRes, err := commonSvc.Login(ctx, "perm-single-source-user", "password123", 0, "")
	require.NoError(t, err)
	require.NotNil(t, loginRes.User)
	assert.Equal(t, providerTenant.ID, loginRes.User.TenantID, "登录必须落 home 租户")
	assert.Equal(t, []string{"ticket:read"}, loginRes.User.Permissions)

	// 2) /auth/me：token 作用域为 C（模拟切换后的 access token），权限必须按 C 计算。
	meRes, err := commonSvc.GetUserScoped(ctx, userEntity.ID, customerTenant.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"incident:write"}, meRes.Permissions, "/auth/me 必须按 token 目标租户计算")

	// 3) SwitchTenant：响应权限按目标租户 C 计算（A6 核心断言）。
	switchRes, err := authSvc.SwitchTenant(ctx, userEntity.ID, customerTenant.ID)
	require.NoError(t, err)
	require.NotNil(t, switchRes.User)
	assert.Equal(t, customerTenant.ID, switchRes.User.TenantID)
	assert.Equal(t, []string{"incident:write"}, switchRes.User.Permissions,
		"切换后 permissions 必须来自目标租户，不得沿用 home 权限")

	// 4) RefreshToken：按 claims 作用域 C 续签，权限同样按 C 计算。
	refreshToken, err := middleware.GenerateRefreshTokenWithSource(
		userEntity.ID, userEntity.Username, string(userEntity.Role), customerTenant.ID, "switch", jwtSecret, time.Hour)
	require.NoError(t, err)
	refreshRes, err := commonSvc.RefreshToken(ctx, refreshToken)
	require.NoError(t, err)
	require.NotNil(t, refreshRes.User)
	assert.Equal(t, []string{"incident:write"}, refreshRes.User.Permissions)

	// 4 个端点结果与解析器直接调用完全一致 = 同源。
	directA, sourceA, err := middleware.ResolvePermissions(ctx, client, userEntity.ID, providerTenant.ID)
	require.NoError(t, err)
	assert.Equal(t, middleware.PermissionSourceMembership, sourceA)
	directC, sourceC, err := middleware.ResolvePermissions(ctx, client, userEntity.ID, customerTenant.ID)
	require.NoError(t, err)
	assert.Equal(t, middleware.PermissionSourceMembership, sourceC)

	assert.Equal(t, directA, loginRes.User.Permissions)
	assert.Equal(t, directC, meRes.Permissions)
	assert.Equal(t, directC, switchRes.User.Permissions)
	assert.Equal(t, directC, refreshRes.User.Permissions)
	assert.NotEqual(t, loginRes.User.Permissions, switchRes.User.Permissions, "A/B 租户权限必须互不影响")
}
