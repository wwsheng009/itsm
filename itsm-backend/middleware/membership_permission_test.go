package middleware

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IP-P1-2 解析器单测：membership.role → role_permissions 单一真源
// =============================================================================

func membershipTestClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:membership_perm_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func membershipTestTenant(t *testing.T, ctx context.Context, client *ent.Client, code string) *ent.Tenant {
	t.Helper()
	tenantEntity, err := client.Tenant.Create().
		SetName("Tenant " + code).
		SetCode(code).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	return tenantEntity
}

func membershipTestUser(t *testing.T, ctx context.Context, client *ent.Client, username string, tenantID int, role user.Role) *ent.User {
	t.Helper()
	userEntity, err := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@example.com").
		SetName(username).
		SetPasswordHash("not-used").
		SetRole(role).
		SetActive(true).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	return userEntity
}

// seedMembershipRole 创建角色 + 权限 + role_permissions（同租户），返回角色实体。
func seedMembershipRole(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, code string, perms [][2]string) *ent.Role {
	t.Helper()
	roleEntity, err := client.Role.Create().
		SetName(code).
		SetCode(code).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	for _, p := range perms {
		permEntity, err := client.Permission.Create().
			SetCode(p[0] + ":" + p[1]).
			SetName(p[0] + ":" + p[1]).
			SetResource(p[0]).
			SetAction(p[1]).
			SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
		_, err = client.RolePermission.Create().
			SetRoleID(roleEntity.ID).
			SetPermissionID(permEntity.ID).
			SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
	}
	return roleEntity
}

func resetAuthzFlag(t *testing.T) {
	t.Helper()
	SetAuthzStaticFallback(false)
	t.Cleanup(func() { SetAuthzStaticFallback(false) })
}

// TestResolvePermissions_MembershipIsSingleSourceAcrossTenants 覆盖 A6：
// 同一账号在租户 A/B 的 membership 指向不同角色时，权限互不影响，且缓存不串租户。
func TestResolvePermissions_MembershipIsSingleSourceAcrossTenants(t *testing.T) {
	resetAuthzFlag(t)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantA := membershipTestTenant(t, ctx, client, "perm-a")
	tenantB := membershipTestTenant(t, ctx, client, "perm-b")
	userEntity := membershipTestUser(t, ctx, client, "ab-user", tenantA.ID, user.RoleEndUser)

	roleA := seedMembershipRole(t, ctx, client, tenantA.ID, "role_a", [][2]string{{"ticket", "read"}})
	roleB := seedMembershipRole(t, ctx, client, tenantB.ID, "role_b", [][2]string{{"incident", "write"}})

	_, err := client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(tenantA.ID).
		SetRoleID(roleA.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(tenantB.ID).
		SetRoleID(roleB.ID).
		SetSource(usertenantmembership.SourceAllocation).
		Save(ctx)
	require.NoError(t, err)

	codesA, sourceA, err := ResolvePermissions(ctx, client, userEntity.ID, tenantA.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceMembership, sourceA)
	assert.Equal(t, []string{"ticket:read"}, codesA)

	codesB, sourceB, err := ResolvePermissions(ctx, client, userEntity.ID, tenantB.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceMembership, sourceB)
	assert.Equal(t, []string{"incident:write"}, codesB)
	assert.NotEqual(t, codesA, codesB, "A/B 租户权限必须独立")

	// 先 B 后 A：验证缓存 key 含 tenant_id，不会把 B 的结果返回给 A。
	codesAAgain, _, err := ResolvePermissions(ctx, client, userEntity.ID, tenantA.ID)
	require.NoError(t, err)
	assert.Equal(t, codesA, codesAAgain)
}

// TestResolvePermissions_SuperAdminHardcodePass 保持现状：super_admin 直通 ["*"]。
func TestResolvePermissions_SuperAdminHardcodePass(t *testing.T) {
	resetAuthzFlag(t)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "perm-super")
	userEntity := membershipTestUser(t, ctx, client, "super-user", tenantEntity.ID, user.RoleSuperAdmin)

	codes, source, err := ResolvePermissions(ctx, client, userEntity.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceSuperAdmin, source)
	assert.Equal(t, []string{"*"}, codes)
}

// TestResolvePermissions_NoMembershipFailClosedByDefault 覆盖默认关闭语义：
// 无 membership / 成员非 active / 软删 / role_id 为空时均 fail-closed。
func TestResolvePermissions_NoMembershipFailClosedByDefault(t *testing.T) {
	resetAuthzFlag(t)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "perm-closed")
	roleEntity := seedMembershipRole(t, ctx, client, tenantEntity.ID, "role_closed", [][2]string{{"ticket", "read"}})

	// 1) 完全没有 membership
	noMembershipUser := membershipTestUser(t, ctx, client, "no-membership", tenantEntity.ID, user.RoleEndUser)
	codes, source, err := ResolvePermissions(ctx, client, noMembershipUser.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceNone, source)
	assert.Empty(t, codes)

	// 2) membership 存在但 role_id 为空
	roleMissingUser := membershipTestUser(t, ctx, client, "role-missing", tenantEntity.ID, user.RoleEndUser)
	_, err = client.UserTenantMembership.Create().
		SetUserID(roleMissingUser.ID).
		SetTenantID(tenantEntity.ID).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)
	codes, source, err = ResolvePermissions(ctx, client, roleMissingUser.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceNone, source)
	assert.Empty(t, codes)

	// 3) membership 被挂起（status=suspended）
	suspendedUser := membershipTestUser(t, ctx, client, "suspended-user", tenantEntity.ID, user.RoleEndUser)
	_, err = client.UserTenantMembership.Create().
		SetUserID(suspendedUser.ID).
		SetTenantID(tenantEntity.ID).
		SetRoleID(roleEntity.ID).
		SetStatus(usertenantmembership.StatusSuspended).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)
	codes, source, err = ResolvePermissions(ctx, client, suspendedUser.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceNone, source)
	assert.Empty(t, codes)

	// 4) membership 被软删（deleted_at 非空）
	deletedUser := membershipTestUser(t, ctx, client, "deleted-user", tenantEntity.ID, user.RoleEndUser)
	_, err = client.UserTenantMembership.Create().
		SetUserID(deletedUser.ID).
		SetTenantID(tenantEntity.ID).
		SetRoleID(roleEntity.ID).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		SetDeletedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
	codes, source, err = ResolvePermissions(ctx, client, deletedUser.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceNone, source)
	assert.Empty(t, codes)
}

// TestResolvePermissions_StaticFallbackOnlyWhenEnabled 覆盖开关打开时的迁移期回退；
// 关闭（默认）时同样的输入必须为空（见上一个用例）。
func TestResolvePermissions_StaticFallbackOnlyWhenEnabled(t *testing.T) {
	resetAuthzFlag(t)
	SetAuthzStaticFallback(true)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "perm-fallback")
	userEntity := membershipTestUser(t, ctx, client, "fallback-user", tenantEntity.ID, user.RoleEndUser)

	codes, source, err := ResolvePermissions(ctx, client, userEntity.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceStaticFallback, source)
	assert.Equal(t, staticPermissionCodesForRoles([]string{"end_user"}), codes)
	assert.Contains(t, codes, "ticket:read")

	// membership 存在但 role_id 为空：回退角色 = users.role + membership.msp_role 映射
	mspUser := membershipTestUser(t, ctx, client, "fallback-msp", tenantEntity.ID, user.RoleEndUser)
	_, err = client.UserTenantMembership.Create().
		SetUserID(mspUser.ID).
		SetTenantID(tenantEntity.ID).
		SetMspRole("provider_admin").
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)
	codes, source, err = ResolvePermissions(ctx, client, mspUser.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceStaticFallback, source)
	assert.Equal(t, staticPermissionCodesForRoles([]string{"end_user", "msp_manager"}), codes)
}

// TestResolvePermissions_ConfiguredEmptyRoleIsExplicitRevoke：
// role_id 可解析但 role_permissions 为空 = DB 显式撤销，即使开关打开也不得回退。
func TestResolvePermissions_ConfiguredEmptyRoleIsExplicitRevoke(t *testing.T) {
	resetAuthzFlag(t)
	SetAuthzStaticFallback(true)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "perm-revoke")
	emptyRole := seedMembershipRole(t, ctx, client, tenantEntity.ID, "role_empty", [][2]string{})
	userEntity := membershipTestUser(t, ctx, client, "revoked-user", tenantEntity.ID, user.RoleEndUser)
	_, err := client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(tenantEntity.ID).
		SetRoleID(emptyRole.ID).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)

	codes, source, err := ResolvePermissions(ctx, client, userEntity.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionSourceMembership, source, "角色已配置时不得回退静态表")
	assert.Empty(t, codes)
}
