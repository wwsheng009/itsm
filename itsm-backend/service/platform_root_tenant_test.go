package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
)

// platform_root_tenant_test.go：D4=B 平台根判定回归（IP-P2-7 补丁 5）。
//
// saas_msp 模式根租户（code=default，type=msp_provider）须保留平台治理面：
//   - 平台角色可见/可读/可授予（F2 转正）；
//   - 通配权限与 system:* 可创建/可分配（F1 转正）；
//   - 用户主角色/roleIds 可授予平台角色；
//   - provider 语义不放大：普通服务商租户（code≠default）仍全部拒绝/隐藏。

func TestPlatformRootTenantPredicate(t *testing.T) {
	assert.True(t, isPlatformRootTenantKind("internal"))
	assert.True(t, isPlatformRootTenantKind("standard"))
	assert.False(t, isPlatformRootTenantKind("msp_provider"))

	assert.True(t, isPlatformRootTenant("internal", "any"))
	assert.True(t, isPlatformRootTenant("msp_provider", "default"), "saas_msp 根租户（code=default）须判为平台根")
	assert.True(t, isPlatformRootTenant("msp_provider", " Default "), "大小写/空白容忍")
	assert.False(t, isPlatformRootTenant("msp_provider", "sp-1"), "普通服务商租户不得放大")
	assert.False(t, isPlatformRootTenant("msp_customer", "cust-1"))
	assert.False(t, isPlatformRootTenant("", ""))

	assert.Nil(t, hiddenRoleCodesForTenant("msp_provider", "default"), "平台根不隐藏任何角色")
	assert.NotEmpty(t, hiddenRoleCodesForTenant("msp_provider", "sp-1"), "普通服务商隐藏平台角色")
	assert.NotEmpty(t, hiddenRoleCodesForTenant("msp_customer", "cust-1"), "客户隐藏平台+MSP 角色")
}

func TestSaasMSPRootTenant_PlatformGovernanceRestored(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	roleSvc := NewRoleService(client, logger)
	permSvc := NewPermissionService(client, logger)
	userSvc := NewUserService(client, logger)

	// saas_msp 形态：根租户 code=default 且类型为 msp_provider（seeder 固定，§7.3 根因）。
	root := mkProvTenant(t, client, ctx, "MSP Provider Tenant", "default", "msp_provider", "active")
	provider := mkProvTenant(t, client, ctx, "SP-1", "sp-root-probe", "msp_provider", "active")

	rootSuper := mkProvRole(t, client, ctx, root.ID, "super_admin", true)
	rootSys := mkProvRole(t, client, ctx, root.ID, "sysadmin", true)
	provSuper := mkProvRole(t, client, ctx, provider.ID, "super_admin", true) // 存在但必须对普通服务商隐藏
	provAdmin := mkProvRole(t, client, ctx, provider.ID, "admin", true)

	// ① 角色可见性（F2 转正）：根租户列表含平台角色；普通服务商仍隐藏。
	rootList, rootTotal, err := roleSvc.ListRoles(ctx, root.ID, &dto.GetRolesParams{Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, rootTotal, "平台根 total 不应被隐藏过滤")
	rootCodes := make([]string, 0, len(rootList))
	for _, r := range rootList {
		rootCodes = append(rootCodes, r.Code)
	}
	assert.ElementsMatch(t, []string{"super_admin", "sysadmin"}, rootCodes)

	provList, provTotal, err := roleSvc.ListRoles(ctx, provider.ID, &dto.GetRolesParams{Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, 1, provTotal, "普通服务商租户仍隐藏平台角色")
	require.Len(t, provList, 1)
	assert.Equal(t, provAdmin.Code, provList[0].Code)

	// 详情面：根租户可读平台角色；普通服务商按"不存在"处理。
	detail, err := roleSvc.GetRole(ctx, rootSuper.ID, root.ID)
	require.NoError(t, err)
	assert.Equal(t, "super_admin", detail.Code)
	_, err = roleSvc.GetRole(ctx, rootSuper.ID, provider.ID)
	assert.Error(t, err, "普通服务商租户不得读取平台角色详情")

	// ② 分配护栏（F1 转正）：根租户可分配 system:*；普通服务商仍拒绝。
	rootSysWrite, err := client.Permission.Create().
		SetCode("system:write").SetName("系统写").SetResource("system").SetAction("write").
		SetTenantID(root.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, roleSvc.AssignPermissions(ctx, rootSys.ID, []int{rootSysWrite.ID}, root.ID),
		"平台根应可分配 system:*")

	provSysWrite, err := client.Permission.Create().
		SetCode("system:write").SetName("系统写").SetResource("system").SetAction("write").
		SetTenantID(provider.ID).Save(ctx)
	require.NoError(t, err)
	err = roleSvc.AssignPermissions(ctx, provAdmin.ID, []int{provSysWrite.ID}, provider.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台治理")

	// ③ 通配权限：根租户可创建；普通服务商拒绝。
	_, err = permSvc.CreatePermission(ctx, &dto.CreatePermissionRequest{
		Code: "*:*", Name: "根全权", Resource: "*", Action: "*",
	}, root.ID)
	require.NoError(t, err, "平台根应可创建通配权限")
	_, err = permSvc.CreatePermission(ctx, &dto.CreatePermissionRequest{
		Code: "*:*", Name: "全权", Resource: "*", Action: "*",
	}, provider.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "通配权限")

	// ④ 用户主角色：根租户可设 super_admin；普通服务商拒绝。
	_, err = userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "root-admin", Email: "root-admin@example.com", Name: "R",
		Password: "Str0ng-P@ssw0rd!", Role: "super_admin",
	}, root.ID)
	require.NoError(t, err, "平台根应可设置平台角色主角色")

	_, err = userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "prov-admin", Email: "prov-admin@example.com", Name: "P",
		Password: "Str0ng-P@ssw0rd!", Role: "super_admin",
	}, provider.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台角色")

	// ⑤ roleIds 授予（CanGrantRoles）：根租户可授予平台角色；普通服务商拒绝。
	require.NoError(t, userSvc.CanGrantRoles(ctx, root.ID, []int{rootSuper.ID}, "super_admin"),
		"平台根应可授予平台角色")
	err = userSvc.CanGrantRoles(ctx, provider.ID, []int{provSuper.ID}, "super_admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台角色")

	// ⑥ UpdateUser：根租户可改主角色为平台角色；普通服务商拒绝。
	rootUser, err := userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "root-plain", Email: "root-plain@example.com", Name: "R2",
		Password: "Str0ng-P@ssw0rd!",
	}, root.ID)
	require.NoError(t, err)
	_, err = userSvc.UpdateUser(ctx, rootUser.ID, &dto.UpdateUserRequest{Role: "sysadmin"}, root.ID)
	require.NoError(t, err, "平台根应可经 UpdateUser 设置平台角色")

	provUser, err := userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "prov-plain", Email: "prov-plain@example.com", Name: "P2",
		Password: "Str0ng-P@ssw0rd!",
	}, provider.ID)
	require.NoError(t, err)
	_, err = userSvc.UpdateUser(ctx, provUser.ID, &dto.UpdateUserRequest{Role: "sysadmin"}, provider.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台角色")
}
