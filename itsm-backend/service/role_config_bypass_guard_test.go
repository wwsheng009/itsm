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

// role_config_bypass_guard_test.go：/admin/roles 角色配置面的越权绕过回归（IP-P2-7 收口）。
//
// 覆盖三条真实绕过路径：
//  1. 角色码可变：把自有角色改名成 super_admin → RBAC 直通
//     （AuthorizeResource 对 super_admin 无条件放行；roles 表无 (tenant,code) 唯一约束）。
//  2. 通配权限：非平台租户自造/分配 `*:*`（resource/action=*）→ 绕过全部权限判定。
//  3. 用户主角色直填：users.role 写 super_admin/sysadmin → 直通；msp_role 写入非服务商租户。

func TestRoleConfig_ReservedRoleCodeGuard(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	roleSvc := NewRoleService(client, logger)

	customer := mkProvTenant(t, client, ctx, "Cust", "cust-bypass-1", "msp_customer", "active")

	// ① 创建保留角色码：拒绝（平台角色 + MSP 专属角色）。
	for _, code := range []string{"super_admin", "sysadmin", "msp_tech", "provider_tenant_admin"} {
		_, err := roleSvc.CreateRole(ctx, &dto.CreateRoleRequest{Name: "x-" + code, Code: code}, customer.ID)
		require.Error(t, err, "保留角色码应拒绝创建: %s", code)
		assert.Contains(t, err.Error(), "系统保留")
	}

	// ② 普通自定义角色：允许。
	probe, err := roleSvc.CreateRole(ctx, &dto.CreateRoleRequest{Name: "Probe", Code: "probe_role"}, customer.ID)
	require.NoError(t, err)

	// ③ 重命名成保留码：拒绝，且原 code 保持不变（防止改名获取 RBAC 直通）。
	reserved := "super_admin"
	_, err = roleSvc.UpdateRole(ctx, probe.ID, &dto.UpdateRoleRequest{Code: &reserved}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "系统保留")
	got, err := roleSvc.GetRole(ctx, probe.ID, customer.ID)
	require.NoError(t, err)
	assert.Equal(t, "probe_role", got.Code, "拒绝后原角色码不得变化")

	// ④ 普通重命名：允许（护栏只拦保留码）。
	newCode := "probe_role_v2"
	renamed, err := roleSvc.UpdateRole(ctx, probe.ID, &dto.UpdateRoleRequest{Code: &newCode}, customer.ID)
	require.NoError(t, err)
	assert.Equal(t, "probe_role_v2", renamed.Code)
}

func TestRoleConfig_WildcardPermissionOnlyPlatformRoot(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	roleSvc := NewRoleService(client, logger)
	permSvc := NewPermissionService(client, logger)

	customer := mkProvTenant(t, client, ctx, "Cust", "cust-bypass-2", "msp_customer", "active")
	internal := mkProvTenant(t, client, ctx, "Root", "root-bypass-2", "internal", "active")

	// ① 客户租户自造通配权限：拒绝。
	_, err := permSvc.CreatePermission(ctx, &dto.CreatePermissionRequest{
		Code: "*:*", Name: "全权", Resource: "*", Action: "*",
	}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "通配权限")

	// ② 客户租户挂通配权限（模拟历史克隆/脏数据）：AssignPermissions 拒绝。
	dirty, err := client.Permission.Create().
		SetCode("wild_all").SetName("wild").SetResource("*").SetAction("*").
		SetTenantID(customer.ID).Save(ctx)
	require.NoError(t, err)
	role, err := roleSvc.CreateRole(ctx, &dto.CreateRoleRequest{Name: "Ops", Code: "ops_wild"}, customer.ID)
	require.NoError(t, err)
	err = roleSvc.AssignPermissions(ctx, role.ID, []int{dirty.ID}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "通配权限")

	// ③ 客户租户经 UpdateRole 的 code 路径挂通配权限：同样拒绝。
	_, err = roleSvc.UpdateRole(ctx, role.ID, &dto.UpdateRoleRequest{Permissions: []string{"wild_all"}}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "通配权限")

	// ④ 平台根租户：通配权限可创建、可分配（sysadmin/平台治理语义保留）。
	rootPerm, err := permSvc.CreatePermission(ctx, &dto.CreatePermissionRequest{
		Code: "root_all", Name: "根全权", Resource: "*", Action: "*",
	}, internal.ID)
	require.NoError(t, err)
	rootRole, err := roleSvc.CreateRole(ctx, &dto.CreateRoleRequest{Name: "RootOps", Code: "root_ops"}, internal.ID)
	require.NoError(t, err)
	require.NoError(t, roleSvc.AssignPermissions(ctx, rootRole.ID, []int{rootPerm.ID}, internal.ID))
}

func TestUserCreateUpdate_PlatformRoleGuard(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	userSvc := NewUserService(client, logger)

	customer := mkProvTenant(t, client, ctx, "Cust", "cust-bypass-3", "msp_customer", "active")
	provider := mkProvTenant(t, client, ctx, "Prov", "prov-bypass-3", "msp_provider", "active")
	internal := mkProvTenant(t, client, ctx, "Root", "root-bypass-3", "internal", "active")

	// ① 客户租户直填主角色 super_admin：拒绝（否则 RBAC 直通）。
	_, err := userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "cust-root", Email: "cust-root@example.com", Name: "C",
		Password: "Str0ng-P@ssw0rd!", Role: "super_admin",
	}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台角色")

	// ② 客户租户写 msp_role：拒绝（客户租户不得出现 MSP 概念）。
	_, err = userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "cust-msp", Email: "cust-msp@example.com", Name: "C",
		Password: "Str0ng-P@ssw0rd!", MSPRole: "provider_admin",
	}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "服务商租户")

	// ③ 服务商租户写 msp_role：允许（服务商员工身份）。
	_, err = userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "prov-msp", Email: "prov-msp@example.com", Name: "P",
		Password: "Str0ng-P@ssw0rd!", MSPRole: "provider_admin",
	}, provider.ID)
	require.NoError(t, err)

	// ④ 平台根租户直填 super_admin：允许（平台维护通道）。
	_, err = userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "root-ok", Email: "root-ok@example.com", Name: "R",
		Password: "Str0ng-P@ssw0rd!", Role: "super_admin",
	}, internal.ID)
	require.NoError(t, err)

	// ⑤ UpdateUser：客户租户把既有用户改成 sysadmin：拒绝。
	u, err := userSvc.CreateUser(ctx, &dto.CreateUserRequest{
		Username: "cust-user", Email: "cust-user@example.com", Name: "U",
		Password: "Str0ng-P@ssw0rd!",
	}, customer.ID)
	require.NoError(t, err)
	_, err = userSvc.UpdateUser(ctx, u.ID, &dto.UpdateUserRequest{Role: "sysadmin"}, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "平台角色")
}
