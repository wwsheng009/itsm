package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/common/tenantctx"
	"itsm-backend/database"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
)

// =============================================================================
// MSW-1：membership 写路径收口（建号 home / 分配 allocation / 撤销软删 / 审计 / 幂等 / 守卫）
// =============================================================================

func msTenant(t *testing.T, ctx context.Context, client *ent.Client, name, code, typ string, providerID int) *ent.Tenant {
	t.Helper()
	tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(tenant.Type(typ)).SetStatus("active")
	if providerID > 0 {
		tb = tb.SetMspProviderID(providerID)
	}
	tn, err := tb.Save(ctx)
	require.NoError(t, err)
	return tn
}

func msRoleWithPerm(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, code, resource, action string) *ent.Role {
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

func msUser(t *testing.T, ctx context.Context, client *ent.Client, username string, tenantID int, roleCode, mspRole string) *ent.User {
	t.Helper()
	tb := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash("x").SetActive(true).SetTenantID(tenantID)
	if roleCode != "" {
		tb = tb.SetRole(user.Role(roleCode))
	}
	if mspRole != "" {
		tb = tb.SetMspRole(user.MspRole(mspRole))
	}
	u, err := tb.Save(ctx)
	require.NoError(t, err)
	return u
}

func msLiveMembership(t *testing.T, ctx context.Context, client *ent.Client, userID, tenantID int) *ent.UserTenantMembership {
	t.Helper()
	mb, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(userID),
			usertenantmembership.TenantID(tenantID),
			usertenantmembership.DeletedAtIsNil(),
		).
		Only(ctx)
	require.NoError(t, err)
	return mb
}

func msAuditCount(t *testing.T, ctx context.Context, client *ent.Client, action string) int {
	t.Helper()
	n, err := client.AuditLog.Query().Where(auditlog.ActionEQ(action)).Count(ctx)
	require.NoError(t, err)
	return n
}

func TestMembershipSyncService_HomeMembership_Idempotent(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov", "msp_provider", 0)
	roleRow := msRoleWithPerm(t, ctx, client, provider.ID, "msp_manager", "ticket", "read")
	u := msUser(t, ctx, client, "home-tech", provider.ID, "admin", "provider_admin")

	svc := NewMembershipSyncService(client, logger)
	meta := MembershipAuditMeta{
		RowTenantID: provider.ID, ActorUserID: 1, ActorAccount: "root",
		Source: middleware.AuditSourcePlatformSelected, Trigger: "user.provision:platform",
	}
	mb, err := svc.SyncHomeMembership(ctx, u.ID, provider.ID, "msp_manager", "msp_manager", meta)
	require.NoError(t, err)
	require.NotNil(t, mb)
	assert.Equal(t, usertenantmembership.SourceHome, mb.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, mb.AccountKind)
	assert.True(t, mb.IsDefault, "home membership 必须 is_default=true")
	assert.Equal(t, usertenantmembership.StatusActive, mb.Status)
	require.NotNil(t, mb.RoleID)
	assert.Equal(t, roleRow.ID, *mb.RoleID)
	require.NotNil(t, mb.MspRole)
	assert.Equal(t, "msp_manager", *mb.MspRole)

	// 幂等：重复调用不新增行、不新增审计（仅漂移时更新）。
	mb2, err := svc.SyncHomeMembership(ctx, u.ID, provider.ID, "msp_manager", "msp_manager", meta)
	require.NoError(t, err)
	assert.Equal(t, mb.ID, mb2.ID)
	n, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserID(u.ID), usertenantmembership.TenantID(provider.ID), usertenantmembership.DeletedAtIsNil()).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, msAuditCount(t, ctx, client, MembershipAuditActionSync))

	// 权限单源非空（membership → role → role_permissions）。
	codes, source, err := middleware.ResolvePermissions(ctx, client, u.ID, provider.ID)
	require.NoError(t, err)
	assert.Equal(t, middleware.PermissionSourceMembership, source)
	assert.Contains(t, codes, "ticket:read")
}

func TestMembershipSyncService_AllocationLifecycle(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov2", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "ms-cust2", "msp_customer", provider.ID)
	customerRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "incident", "write")
	u := msUser(t, ctx, client, "alloc-tech", provider.ID, "end_user", "provider_agent")

	allocSvc := NewMSPAllocationService(client, logger)
	allocDTO, err := allocSvc.Create(ctx, u.ID, customer.ID, "backup")
	require.NoError(t, err)
	require.NotNil(t, allocDTO)

	mb := msLiveMembership(t, ctx, client, u.ID, customer.ID)
	require.NotNil(t, mb)
	assert.Equal(t, usertenantmembership.SourceAllocation, mb.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, mb.AccountKind)
	assert.False(t, mb.IsDefault, "allocation 作用域必须 is_default=false")
	require.NotNil(t, mb.AllocationID)
	assert.Equal(t, allocDTO.ID, *mb.AllocationID)
	require.NotNil(t, mb.RoleID)
	assert.Equal(t, customerRole.ID, *mb.RoleID)
	require.NotNil(t, mb.MspRole)
	assert.Equal(t, "msp_tech", *mb.MspRole)
	assert.Equal(t, 1, msAuditCount(t, ctx, client, MembershipAuditActionSync))

	codes, source, err := middleware.ResolvePermissions(ctx, client, u.ID, customer.ID)
	require.NoError(t, err)
	assert.Equal(t, middleware.PermissionSourceMembership, source)
	assert.Contains(t, codes, "incident:write")

	// 撤销：软删 + deassigned_at；权限 fail-closed（缓存联动归 M2，此处显式失效）。
	probe := time.Now().Add(-time.Minute)
	homeBefore, rErr := middleware.IsUserRefreshRevokedForTenant(ctx, u.ID, provider.ID, probe)
	require.NoError(t, rErr)
	require.NoError(t, allocSvc.Deactivate(ctx, u.ID, customer.ID))
	revoked, err := client.UserTenantMembership.Get(ctx, mb.ID)
	require.NoError(t, err)
	require.NotNil(t, revoked.DeletedAt, "撤销后必须软删（回收即失效）")
	require.NotNil(t, revoked.DeassignedAt)

	// MSW-3/Q3=A：撤销立即生效——缓存已由撤销路径失效（无需手工干预）。
	codesAfter, sourceAfter, err := middleware.ResolvePermissions(ctx, client, u.ID, customer.ID)
	require.NoError(t, err)
	assert.Empty(t, codesAfter)
	assert.Equal(t, middleware.PermissionSourceNone, sourceAfter)

	// 该 user×tenant 在途 refresh 吊销；其它作用域不受影响（作用域隔离）。
	tenantRevoked, rErr := middleware.IsUserRefreshRevokedForTenant(ctx, u.ID, customer.ID, probe)
	require.NoError(t, rErr)
	assert.True(t, tenantRevoked, "撤销后目标作用域在途 refresh 必须被吊销")
	homeAfter, rErr := middleware.IsUserRefreshRevokedForTenant(ctx, u.ID, provider.ID, probe)
	require.NoError(t, rErr)
	assert.Equal(t, homeBefore, homeAfter, "撤销不得影响其它作用域")

	// 幂等撤销 + 审计
	require.NoError(t, allocSvc.Deactivate(ctx, u.ID, customer.ID))
	assert.Equal(t, 1, msAuditCount(t, ctx, client, MembershipAuditActionRevoke))
}

func TestMembershipSyncService_DeactivateMembership_Invite(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov6", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "ms-cust6", "msp_customer", provider.ID)
	roleRow := msRoleWithPerm(t, ctx, client, customer.ID, "agent", "incident", "read")
	inviter := msUser(t, ctx, client, "cust-admin-y", customer.ID, "admin", "")
	staff := msUser(t, ctx, client, "invited-staff", provider.ID, "end_user", "provider_agent")

	// 邀请流产物：source=invite ∧ invited_by 非空（Q1=C 的"客户侧显式确认"载体）。
	mb, err := client.UserTenantMembership.Create().
		SetUserID(staff.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceInvite).
		SetRoleID(roleRow.ID).SetInvitedBy(inviter.ID).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).
		Save(ctx)
	require.NoError(t, err)

	// 预热权限缓存：撤销后应无需手工失效即 fail-closed。
	codes, _, err := middleware.ResolvePermissions(ctx, client, staff.ID, customer.ID)
	require.NoError(t, err)
	require.NotEmpty(t, codes)

	svc := NewMembershipSyncService(client, logger)
	meta := MembershipAuditMeta{
		RowTenantID: customer.ID, ActorUserID: inviter.ID, ActorAccount: "cust-admin-y",
		Source: middleware.AuditSourcePlatformSelected, Trigger: "tenant_admin.revoke_membership",
	}
	require.NoError(t, svc.DeactivateMembership(ctx, staff.ID, customer.ID, meta))

	got, err := client.UserTenantMembership.Get(ctx, mb.ID)
	require.NoError(t, err)
	require.NotNil(t, got.DeletedAt)
	require.NotNil(t, got.DeassignedAt)
	assert.Equal(t, usertenantmembership.StatusSuspended, got.Status)

	codesAfter, sourceAfter, err := middleware.ResolvePermissions(ctx, client, staff.ID, customer.ID)
	require.NoError(t, err)
	assert.Empty(t, codesAfter, "撤销后权限缓存必须立即失效")
	assert.Equal(t, middleware.PermissionSourceNone, sourceAfter)

	tenantRevoked, err := middleware.IsUserRefreshRevokedForTenant(ctx, staff.ID, customer.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, tenantRevoked, "撤销后目标作用域在途 refresh 必须被吊销")
	assert.Equal(t, 1, msAuditCount(t, ctx, client, MembershipAuditActionRevoke))

	// 已无存活映射 → 治理面显式反馈（非静默）。
	err = svc.DeactivateMembership(ctx, staff.ID, customer.ID, meta)
	require.ErrorIs(t, err, ErrMembershipNotFound)

	// home 映射不可撤销（账号级语义）。
	homeMB, err := svc.SyncHomeMembership(ctx, inviter.ID, customer.ID, "admin", "", MembershipAuditMeta{RowTenantID: customer.ID})
	require.NoError(t, err)
	require.NotNil(t, homeMB)
	err = svc.DeactivateMembership(ctx, inviter.ID, customer.ID, meta)
	require.ErrorIs(t, err, ErrMembershipHomeImmutable)
}

func TestMembershipSyncService_DeactivateMembership_AllocationCascade(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov7", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "ms-cust7", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	u := msUser(t, ctx, client, "cascade-tech", provider.ID, "end_user", "provider_agent")

	allocSvc := NewMSPAllocationService(client, logger)
	allocDTO, err := allocSvc.Create(ctx, u.ID, customer.ID, "primary")
	require.NoError(t, err)

	// 治理面撤销 allocation 来源映射 → 级联解除分配，避免"映射已撤销但 allocation 仍可切换"。
	svc := NewMembershipSyncService(client, logger)
	require.NoError(t, svc.DeactivateMembership(ctx, u.ID, customer.ID, MembershipAuditMeta{RowTenantID: provider.ID}))

	alloc, err := client.MSPAllocation.Get(ctx, allocDTO.ID)
	require.NoError(t, err)
	require.NotNil(t, alloc.DeassignedAt, "allocation 来源撤销必须级联解除分配")
}

func TestMembershipSyncService_SourceConflict_NotOverwritten(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov3", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "ms-cust3", "msp_customer", provider.ID)
	u := msUser(t, ctx, client, "conflict-user", provider.ID, "end_user", "provider_agent")

	svc := NewMembershipSyncService(client, logger)
	meta := MembershipAuditMeta{RowTenantID: provider.ID, ActorAccount: "test"}
	// 先在客户租户建立 home 来源行（模拟客户账号），allocation 同步必须拒绝覆盖。
	_, err := svc.SyncHomeMembership(ctx, u.ID, customer.ID, "", "", meta)
	require.NoError(t, err)

	alloc, err := client.MSPAllocation.Create().
		SetMspUserID(u.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).SetRole("backup").
		Save(ctx)
	require.NoError(t, err)
	_, err = svc.SyncAllocationMembership(ctx, alloc, meta)
	require.ErrorIs(t, err, ErrMembershipSourceConflict)
}

func TestMembershipSyncService_AllocationUnderTenantWriteGuard(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	// 注册生产同款写路径租户守卫：证明同步在 ctx=provider 时仍能安全写入 customer 行（内部重绑）。
	database.RegisterSecurityInterceptors(client, "off")

	provider := msTenant(t, ctx, client, "Provider", "ms-prov4", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "ms-cust4", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	u := msUser(t, ctx, client, "guard-tech", provider.ID, "end_user", "provider_agent")

	allocSvc := NewMSPAllocationService(client, logger)
	guardCtx := tenantctx.WithTenantID(ctx, provider.ID)
	allocDTO, err := allocSvc.Create(guardCtx, u.ID, customer.ID, "primary")
	require.NoError(t, err)
	require.NotNil(t, allocDTO)

	mb := msLiveMembership(t, ctx, client, u.ID, customer.ID)
	require.NotNil(t, mb)
	require.NotNil(t, mb.AllocationID)
	assert.Equal(t, allocDTO.ID, *mb.AllocationID)
}

func TestUserProvisioning_WritesHomeMembership(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "ms-prov5", "msp_provider", 0)
	providerRole := msRoleWithPerm(t, ctx, client, provider.ID, "msp_manager", "ticket", "read")
	customer := msTenant(t, ctx, client, "Customer", "ms-cust5", "msp_customer", provider.ID)
	agentRole := msRoleWithPerm(t, ctx, client, customer.ID, "agent", "incident", "read")

	users := NewUserService(client, logger)
	prov := NewUserProvisioningService(client, users, logger)
	prov.SetChannelsEnabled(true)

	// 平台通道：provider 员工（provider_admin）→ home membership（provider 类型、msp_manager 角色）。
	created, err := prov.ProvisionUser(ctx,
		ProvisionActor{UserID: 7, HomeTenantID: provider.ID, Role: "super_admin", Username: "platform-root"},
		provider.ID,
		&dto.CreateUserRequest{
			Username: "prov-new-admin", Email: "prov-new-admin@example.com", Name: "Prov New Admin",
			Password: "Str0ng-P@ssw0rd!", Role: "admin", MSPRole: "provider_admin",
		})
	require.NoError(t, err)
	mb := msLiveMembership(t, ctx, client, created.ID, provider.ID)
	require.NotNil(t, mb)
	assert.Equal(t, usertenantmembership.SourceHome, mb.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, mb.AccountKind)
	assert.True(t, mb.IsDefault)
	require.NotNil(t, mb.RoleID)
	assert.Equal(t, providerRole.ID, *mb.RoleID)
	require.NotNil(t, mb.MspRole)
	assert.Equal(t, "msp_manager", *mb.MspRole)
	assert.Equal(t, 1, msAuditCount(t, ctx, client, MembershipAuditActionSync))

	// 租户通道：客户租户建号 → customer 类型、agent 角色（按 users.role 解析）。
	custAdmin := msUser(t, ctx, client, "cust-admin-x", customer.ID, "admin", "")
	created2, err := prov.ProvisionUser(ctx,
		ProvisionActor{UserID: custAdmin.ID, HomeTenantID: customer.ID, Role: "admin", Username: "cust-admin-x"},
		customer.ID,
		&dto.CreateUserRequest{
			Username: "cust-agent-new", Email: "cust-agent-new@example.com", Name: "Cust Agent",
			Password: "Str0ng-P@ssw0rd!", Role: "agent",
		})
	require.NoError(t, err)
	mb2 := msLiveMembership(t, ctx, client, created2.ID, customer.ID)
	require.NotNil(t, mb2)
	assert.Equal(t, usertenantmembership.AccountKindCustomer, mb2.AccountKind)
	require.NotNil(t, mb2.RoleID)
	assert.Equal(t, agentRole.ID, *mb2.RoleID)
}
