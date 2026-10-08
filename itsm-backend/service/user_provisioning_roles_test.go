package service

import (
	"context"
	"net/http"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/usertenantmembership"
)

// user_provisioning_roles_test.go：IP-P2-7 建号角色多选的权威回归——
// ① GrantableRoles 过滤（平台角色/rank/客户作用域/inactive/租户隔离 + 排序）；
// ② roleIds 主角色派生与 membership 一致性；③ 平台角色经 roleIds 的越权防护。

func newRoleProvFixture(t *testing.T) (*ent.Client, *UserProvisioningService, context.Context) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	prov := NewUserProvisioningService(client, NewUserService(client, logger), logger)
	prov.SetChannelsEnabled(true)
	return client, prov, context.Background()
}

func mkProvTenant(t *testing.T, client *ent.Client, ctx context.Context, name, code, typ, status string) *ent.Tenant {
	t.Helper()
	tn, err := client.Tenant.Create().
		SetName(name).SetCode(code).SetType(tenant.Type(typ)).SetStatus(status).
		Save(ctx)
	require.NoError(t, err)
	return tn
}

func mkProvRole(t *testing.T, client *ent.Client, ctx context.Context, tenantID int, code string, active bool) *ent.Role {
	t.Helper()
	r, err := client.Role.Create().
		SetName(code).SetCode(code).SetTenantID(tenantID).SetIsActive(active).
		Save(ctx)
	require.NoError(t, err)
	return r
}

func roleOptionCodes(opts []dto.RoleOption) []string {
	codes := make([]string, 0, len(opts))
	for _, o := range opts {
		codes = append(codes, o.Code)
	}
	return codes
}

func roleOptionIndex(opts []dto.RoleOption, code string) int {
	for i, o := range opts {
		if o.Code == code {
			return i
		}
	}
	return -1
}

func TestGrantableRoles_CustomerScopeFiltering(t *testing.T) {
	client, prov, ctx := newRoleProvFixture(t)
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-1", "internal", "active")
	customer := mkProvTenant(t, client, ctx, "Customer", "customer-1", "msp_customer", "active")
	other := mkProvTenant(t, client, ctx, "Other", "other-1", "saas_customer", "active")

	mkProvRole(t, client, ctx, customer.ID, "admin", true)
	mkProvRole(t, client, ctx, customer.ID, "agent", true)
	mkProvRole(t, client, ctx, customer.ID, "super_admin", true)
	mkProvRole(t, client, ctx, customer.ID, "sysadmin", true)
	mkProvRole(t, client, ctx, customer.ID, "msp_tech", true)
	mkProvRole(t, client, ctx, customer.ID, "provider_tenant_admin", true)
	mkProvRole(t, client, ctx, customer.ID, "ops_readonly", true)
	mkProvRole(t, client, ctx, customer.ID, "retired_role", false) // inactive → 不返回
	mkProvRole(t, client, ctx, other.ID, "other_only", true)       // 租户隔离：不得串场

	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}
	got, err := prov.GrantableRoles(ctx, actor, customer.ID)
	require.NoError(t, err)
	codes := roleOptionCodes(got)

	assert.Contains(t, codes, "admin")
	assert.Contains(t, codes, "agent")
	assert.Contains(t, codes, "ops_readonly", "租户自定义角色必须数据驱动地出现在候选中")
	assert.NotContains(t, codes, "super_admin", "平台角色不得作为候选")
	assert.NotContains(t, codes, "sysadmin", "平台角色不得作为候选")
	assert.NotContains(t, codes, "msp_tech", "客户类型租户不应返回 msp_* C2 角色")
	assert.NotContains(t, codes, "provider_tenant_admin", "客户类型租户不应返回 provider 组合角色")
	assert.NotContains(t, codes, "retired_role", "inactive 角色不得作为候选")
	assert.NotContains(t, codes, "other_only", "候选必须限定在目标租户内")

	// rank 升序：ops_readonly(0) → agent(2) → admin(4)
	iCustom := roleOptionIndex(got, "ops_readonly")
	iAgent := roleOptionIndex(got, "agent")
	iAdmin := roleOptionIndex(got, "admin")
	require.NotEqual(t, -1, iCustom)
	require.NotEqual(t, -1, iAgent)
	require.NotEqual(t, -1, iAdmin)
	assert.True(t, iCustom < iAgent && iAgent < iAdmin, "候选应按 rank 升序排序: %v", codes)
}

func TestGrantableRoles_ProviderScopeKeepsMSPRoles(t *testing.T) {
	client, prov, ctx := newRoleProvFixture(t)
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-1", "internal", "active")
	provider := mkProvTenant(t, client, ctx, "Provider", "provider-1", "msp_provider", "active")

	mkProvRole(t, client, ctx, provider.ID, "msp_manager", true)
	mkProvRole(t, client, ctx, provider.ID, "provider_tenant_admin", true)
	mkProvRole(t, client, ctx, provider.ID, "prov_only", true)
	mkProvRole(t, client, ctx, provider.ID, "super_admin", true)

	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}
	got, err := prov.GrantableRoles(ctx, actor, provider.ID)
	require.NoError(t, err)
	codes := roleOptionCodes(got)

	assert.Contains(t, codes, "msp_manager")
	assert.Contains(t, codes, "provider_tenant_admin")
	assert.Contains(t, codes, "prov_only")
	assert.NotContains(t, codes, "super_admin")
}

func TestGrantableRoles_RejectsUnauthorizedAndSuspended(t *testing.T) {
	client, prov, ctx := newRoleProvFixture(t)
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-1", "internal", "active")
	customer := mkProvTenant(t, client, ctx, "Customer", "customer-1", "msp_customer", "active")
	other := mkProvTenant(t, client, ctx, "Other", "other-1", "saas_customer", "active")

	// 非平台、非本租户、无 allocation 的普通用户 → CROSS_TENANT_FORBIDDEN(403)
	_, err := prov.GrantableRoles(ctx, ProvisionActor{UserID: 9, HomeTenantID: other.ID, Role: "end_user"}, customer.ID)
	pe, ok := AsProvisionError(err)
	require.True(t, ok, "应返回稳定建号错误码: %v", err)
	assert.Equal(t, ProvisionCodeCrossTenantForbidden, pe.Code)
	assert.Equal(t, http.StatusForbidden, pe.Status)

	// 目标租户 suspended → TENANT_SUSPENDED(403)
	suspended := mkProvTenant(t, client, ctx, "Susp", "susp-1", "saas_customer", "suspended")
	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}
	_, err = prov.GrantableRoles(ctx, actor, suspended.ID)
	pe, ok = AsProvisionError(err)
	require.True(t, ok)
	assert.Equal(t, ProvisionCodeTenantSuspended, pe.Code)
	assert.Equal(t, http.StatusForbidden, pe.Status)
}

func TestProvisionUser_RoleIDs_PrimaryAndMembership(t *testing.T) {
	client, prov, ctx := newRoleProvFixture(t)
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-1", "internal", "active")
	customer := mkProvTenant(t, client, ctx, "Customer", "customer-1", "msp_customer", "active")

	adminRole := mkProvRole(t, client, ctx, customer.ID, "admin", true)
	customRole := mkProvRole(t, client, ctx, customer.ID, "ops_special", true)
	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}

	mkReq := func(username string, roleIDs []int) *dto.CreateUserRequest {
		return &dto.CreateUserRequest{
			Username: username, Email: username + "@corp.example.com",
			Name: username, Password: "Str0ng-P@ssw0rd!", RoleIDs: roleIDs,
		}
	}

	// ① 枚举兼容角色：users.role 派生为 admin，membership 解析到 admin 角色行。
	created, err := prov.ProvisionUser(ctx, actor, customer.ID, mkReq("role-admin", []int{adminRole.ID}))
	require.NoError(t, err)
	assert.Equal(t, "admin", string(created.Role), "roleIds 含 admin 时应派生主角色")

	rows, err := client.UserTenantMembership.Query().All(ctx)
	require.NoError(t, err)
	var adminMembership *ent.UserTenantMembership
	for _, m := range rows {
		if m.UserID == created.ID && m.TenantID == customer.ID {
			adminMembership = m
		}
	}
	require.NotNil(t, adminMembership, "建号必须写 home membership")
	assert.Equal(t, usertenantmembership.SourceHome, adminMembership.Source)
	require.NotNil(t, adminMembership.RoleID)
	assert.Equal(t, adminRole.ID, *adminMembership.RoleID)
	assert.Equal(t, usertenantmembership.AccountKindCustomer, adminMembership.AccountKind)

	// ② 仅自定义角色：users.role 保持默认 end_user；membership 按最高 rank 角色解析到自定义角色行。
	created2, err := prov.ProvisionUser(ctx, actor, customer.ID, mkReq("role-custom", []int{customRole.ID}))
	require.NoError(t, err)
	assert.Equal(t, "end_user", string(created2.Role), "自定义角色无枚举映射时主角色保持默认")

	rows2, err := client.UserTenantMembership.Query().All(ctx)
	require.NoError(t, err)
	var customMembership *ent.UserTenantMembership
	for _, m := range rows2 {
		if m.UserID == created2.ID && m.TenantID == customer.ID {
			customMembership = m
		}
	}
	require.NotNil(t, customMembership)
	require.NotNil(t, customMembership.RoleID, "membership 应按 roleIds 解析角色而非回落占位")
	assert.Equal(t, customRole.ID, *customMembership.RoleID)
}

func TestProvisionUser_RoleIDs_RejectsPlatformRole(t *testing.T) {
	client, prov, ctx := newRoleProvFixture(t)
	platform := mkProvTenant(t, client, ctx, "Platform", "platform-1", "internal", "active")
	customer := mkProvTenant(t, client, ctx, "Customer", "customer-1", "msp_customer", "active")

	superRole := mkProvRole(t, client, ctx, customer.ID, "super_admin", true)
	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}

	_, err := prov.ProvisionUser(ctx, actor, customer.ID, &dto.CreateUserRequest{
		Username: "bad-platform", Email: "bad-platform@corp.example.com",
		Name: "bad", Password: "Str0ng-P@ssw0rd!", RoleIDs: []int{superRole.ID},
	})
	pe, ok := AsProvisionError(err)
	require.True(t, ok, "应返回稳定建号错误码: %v", err)
	assert.Equal(t, ProvisionCodeRoleNotGrantable, pe.Code, "平台角色不得经 roleIds 绕过")
	assert.Equal(t, http.StatusUnprocessableEntity, pe.Status)
}
