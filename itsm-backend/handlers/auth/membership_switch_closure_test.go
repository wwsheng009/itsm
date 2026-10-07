package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	commonhandlers "itsm-backend/handlers/common"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// =============================================================================
// MSW-1 / MSW-A1–A2：建号（home membership）→ 分配（allocation membership）→
// 切换（目标租户权限非空且与 home 独立）→ 撤销（回收即失效 + 再次切换被拒）。
// 全链路走生产服务（UserProvisioningService / MSPAllocationService / auth.Service），
// 不手工建 membership。
// =============================================================================

func authClosureRole(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, code, resource, action string) *ent.Role {
	t.Helper()
	r, err := client.Role.Create().SetName(code).SetCode(code).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	p, err := client.Permission.Create().
		SetCode(resource + ":" + action).SetName(resource + ":" + action).
		SetResource(resource).SetAction(action).SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(r.ID).SetPermissionID(p.ID).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	return r
}

func TestMembershipSwitchClosure_ProvisionAllocateSwitchRevoke(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	dsn := fmt.Sprintf("file:auth_ms_closure_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	defer func() { _ = client.Close() }()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("closure-prov").SetStatus("active").SetType("msp_provider").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("closure-cust").SetStatus("active").SetType("msp_customer").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	providerRole := authClosureRole(t, ctx, client, provider.ID, "msp_manager", "ticket", "read")
	customerRole := authClosureRole(t, ctx, client, customer.ID, "msp_tech", "incident", "write")

	// 1) 建号（平台通道）→ home membership 由建号通道写入（此前 MSW-1 缺口）。
	users := service.NewUserService(client, logger)
	prov := service.NewUserProvisioningService(client, users, logger)
	prov.SetChannelsEnabled(true)
	created, err := prov.ProvisionUser(ctx,
		service.ProvisionActor{UserID: 4242, HomeTenantID: provider.ID, Role: "super_admin", Username: "platform-root"},
		provider.ID,
		&dto.CreateUserRequest{
			Username: "closure-tech", Email: "closure-tech@example.com", Name: "Closure Tech",
			Password: "Str0ng-P@ssw0rd!", Role: "admin", MSPRole: "provider_admin",
		})
	require.NoError(t, err)

	homeMB, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(created.ID),
			usertenantmembership.TenantID(provider.ID),
			usertenantmembership.DeletedAtIsNil(),
		).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, usertenantmembership.SourceHome, homeMB.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, homeMB.AccountKind)
	require.NotNil(t, homeMB.RoleID)
	assert.Equal(t, providerRole.ID, *homeMB.RoleID)

	// 2) 登录落 provider 家，权限来自 home membership（非空）。
	jwtSecret := "ms-closure-jwt-secret"
	commonSvc := commonhandlers.NewService(commonhandlers.NewEntRepository(client), jwtSecret, logger, client)
	loginRes, err := commonSvc.Login(ctx, "closure-tech", "Str0ng-P@ssw0rd!", 0, "")
	require.NoError(t, err)
	require.NotNil(t, loginRes.User)
	assert.Equal(t, provider.ID, loginRes.User.TenantID)
	assert.Equal(t, []string{"ticket:read"}, loginRes.User.Permissions)

	// 3) 分配（生产服务）→ 客户作用域 membership（source=allocation，角色按分配映射）。
	allocSvc := service.NewMSPAllocationService(client, logger)
	_, err = allocSvc.Create(ctx, created.ID, customer.ID, "backup")
	require.NoError(t, err)
	custMB, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(created.ID),
			usertenantmembership.TenantID(customer.ID),
			usertenantmembership.DeletedAtIsNil(),
		).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, usertenantmembership.SourceAllocation, custMB.Source)
	require.NotNil(t, custMB.RoleID)
	assert.Equal(t, customerRole.ID, *custMB.RoleID)

	// 4) 切换 → 目标租户权限非空且与 home 独立（MSW-A1/A2 核心断言）。
	authSvc := NewService(client, jwtSecret, logger, nil)
	switchRes, err := authSvc.SwitchTenant(ctx, created.ID, customer.ID)
	require.NoError(t, err)
	require.NotNil(t, switchRes.User)
	assert.Equal(t, customer.ID, switchRes.User.TenantID)
	assert.Equal(t, []string{"incident:write"}, switchRes.User.Permissions,
		"切换后权限必须来自目标租户 membership，不得沿用 home")
	assert.NotEqual(t, loginRes.User.Permissions, switchRes.User.Permissions)

	// 5) 撤销分配 → membership 回收（软删）+ 权限 fail-closed + 再次切换被拒。
	require.NoError(t, allocSvc.Deactivate(ctx, created.ID, customer.ID))
	revoked, err := client.UserTenantMembership.Get(ctx, custMB.ID)
	require.NoError(t, err)
	require.NotNil(t, revoked.DeletedAt, "撤销分配后 membership 必须软删")
	require.NotNil(t, revoked.DeassignedAt)

	middleware.InvalidateAllPermissionCaches()
	codes, source, err := middleware.ResolvePermissions(ctx, client, created.ID, customer.ID)
	require.NoError(t, err)
	assert.Empty(t, codes)
	assert.Equal(t, middleware.PermissionSourceNone, source)

	_, err = authSvc.SwitchTenant(ctx, created.ID, customer.ID)
	require.Error(t, err, "撤销分配后再次切换必须被拒")
}
