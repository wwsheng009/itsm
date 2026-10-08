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

// msp_role_visibility_test.go：非平台租户角色边界回归（IP-P2-7 收口）。
//
// 规则：
//   - 客户类型租户（msp_customer/saas_customer/legacy customer）：不得出现/持有/管理 MSP 专属角色
//     （msp_viewer/tech/specialist/manager/admin + provider_tenant_admin），也不得出现平台角色；
//   - 服务商租户（msp_provider）：保留 MSP 专属角色（服务商员工身份载体），但不得出现平台角色
//     （super_admin/sysadmin——非平台租户不该有超级管理员/平台管理员）；
//   - 平台根租户（internal/standard）：平台角色与词表照常可见/可授予。

func TestMSPRoleVisibility_CustomerTenantHidesRolesAndGrants(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	roleSvc := NewRoleService(client, logger)
	userSvc := NewUserService(client, logger)
	prov := NewUserProvisioningService(client, userSvc, logger)
	prov.SetChannelsEnabled(true)

	customer := mkProvTenant(t, client, ctx, "Cust", "cust-vis-1", "msp_customer", "active")
	provider := mkProvTenant(t, client, ctx, "Prov", "prov-vis-1", "msp_provider", "active")
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-vis-1", "internal", "active")

	adminRole := mkProvRole(t, client, ctx, customer.ID, "admin", true)
	mspTech := mkProvRole(t, client, ctx, customer.ID, "msp_tech", true)
	mspManager := mkProvRole(t, client, ctx, customer.ID, "msp_manager", true)
	providerAdmin := mkProvRole(t, client, ctx, customer.ID, "provider_tenant_admin", true)
	provMspTech := mkProvRole(t, client, ctx, provider.ID, "msp_tech", true)
	custSysadmin := mkProvRole(t, client, ctx, customer.ID, "sysadmin", true)
	provSysadmin := mkProvRole(t, client, ctx, provider.ID, "sysadmin", true)
	mkProvRole(t, client, ctx, provider.ID, "super_admin", true)
	platformSysadmin := mkProvRole(t, client, ctx, platform.ID, "sysadmin", true)

	// ① 列表：客户租户隐藏全部 MSP 专属角色，total 同步收敛（服务端过滤，不依赖前端）。
	list, total, err := roleSvc.ListRoles(ctx, customer.ID, &dto.GetRolesParams{Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, 1, total, "total 必须与过滤后的集合一致")
	require.Len(t, list, 1)
	assert.Equal(t, adminRole.Code, list[0].Code)

	// ② 详情/删除/授权：按"不存在"处理（隐藏存在性，不提示"无权访问"）。
	_, err = roleSvc.GetRole(ctx, mspTech.ID, customer.ID)
	assert.Error(t, err, "客户租户不得读取 MSP 角色详情")
	err = roleSvc.DeleteRole(ctx, mspManager.ID, customer.ID)
	assert.Error(t, err, "客户租户不得删除 MSP 角色")
	err = roleSvc.AssignPermissions(ctx, providerAdmin.ID, nil, customer.ID)
	assert.Error(t, err, "客户租户不得改动 MSP 角色权限")
	_, err = roleSvc.GetRole(ctx, custSysadmin.ID, customer.ID)
	assert.Error(t, err, "客户租户不得读取平台角色详情")

	// ③ 用户角色授予：客户租户拒绝 MSP 角色；provider 租户照常放行。
	err = userSvc.CanGrantRoles(ctx, customer.ID, []int{mspTech.ID}, "admin")
	require.Error(t, err, "客户租户不得把 MSP 角色授予用户")
	err = userSvc.CanGrantRoles(ctx, provider.ID, []int{provMspTech.ID}, "admin")
	require.NoError(t, err, "provider 租户内 MSP 角色可授予（服务商员工身份）")
	err = userSvc.CanGrantRoles(ctx, customer.ID, []int{custSysadmin.ID}, "super_admin")
	require.Error(t, err, "客户租户不得授予平台角色")
	err = userSvc.CanGrantRoles(ctx, provider.ID, []int{provSysadmin.ID}, "super_admin")
	require.Error(t, err, "服务商租户不得授予平台角色")
	_, err = roleSvc.GetRole(ctx, provSysadmin.ID, provider.ID)
	assert.Error(t, err, "服务商租户不得读取平台角色详情")
	// 平台根租户：平台角色照常可见/可授予（rank 5 调用者）。
	detail, err := roleSvc.GetRole(ctx, platformSysadmin.ID, platform.ID)
	require.NoError(t, err)
	assert.Equal(t, "sysadmin", detail.Code)
	err = userSvc.CanGrantRoles(ctx, platform.ID, []int{platformSysadmin.ID}, "super_admin")
	require.NoError(t, err, "平台根租户内平台角色可授予")

	// ④ 建号写入：平台通道向客户租户建号携带 msp_tech 同样被拒（422 ROLE_NOT_GRANTABLE）。
	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}
	_, err = prov.ProvisionUser(ctx, actor, customer.ID, &dto.CreateUserRequest{
		Username: "cust-msp-role", Email: "cust-msp-role@corp.example.com",
		Name: "cust", Password: "Str0ng-P@ssw0rd!", RoleIDs: []int{mspTech.ID},
	})
	pe, ok := AsProvisionError(err)
	require.True(t, ok, "应返回稳定建号错误码: %v", err)
	assert.Equal(t, ProvisionCodeRoleNotGrantable, pe.Code)
}

func TestMSPRoleVisibility_ProviderTenantUnaffected(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	roleSvc := NewRoleService(client, logger)

	provider := mkProvTenant(t, client, ctx, "Prov", "prov-vis-2", "msp_provider", "active")
	mkProvRole(t, client, ctx, provider.ID, "admin", true)
	mkProvRole(t, client, ctx, provider.ID, "msp_tech", true)
	mkProvRole(t, client, ctx, provider.ID, "msp_manager", true)
	mkProvRole(t, client, ctx, provider.ID, "provider_tenant_admin", true)
	// 平台角色存在但隐藏（非平台租户不该有超级管理员/平台管理员）。
	mkProvRole(t, client, ctx, provider.ID, "sysadmin", true)
	mkProvRole(t, client, ctx, provider.ID, "super_admin", true)

	list, total, err := roleSvc.ListRoles(ctx, provider.ID, &dto.GetRolesParams{Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, 4, total)
	codes := make([]string, 0, len(list))
	for _, r := range list {
		codes = append(codes, r.Code)
	}
	assert.ElementsMatch(t, []string{"admin", "msp_tech", "msp_manager", "provider_tenant_admin"}, codes)

	// provider 租户内详情照常可见（MSP 角色是服务商侧正常词表）。
	allRoles, err := client.Role.Query().All(ctx)
	require.NoError(t, err)
	found := false
	for _, r := range allRoles {
		if r.Code == "msp_tech" {
			detail, gerr := roleSvc.GetRole(ctx, r.ID, provider.ID)
			require.NoError(t, gerr)
			assert.Equal(t, "msp_tech", detail.Code)
			found = true
		}
	}
	assert.True(t, found, "provider 租户应能读取 msp_tech 详情")
}
