package middleware

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
)

// =============================================================================
// IP-P2-9 惰性判定：expires_at 已过的 membership 在读路径一律视为不存在（fail-closed）
// =============================================================================

// TestResolvePermissions_ExpiredMembershipFailsClosed：
// 到期 membership → 权限解析 fail-closed（空集合 + source none，未开静态回退不得回退旧权限）；
// 续期（清空 expires_at）→ 同源解析恢复。
func TestResolvePermissions_ExpiredMembershipFailsClosed(t *testing.T) {
	resetAuthzFlag(t)
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "exp-tenant")
	userEntity := membershipTestUser(t, ctx, client, "exp-user", tenantEntity.ID, user.RoleEndUser)
	roleEntity := seedMembershipRole(t, ctx, client, tenantEntity.ID, "exp_role", [][2]string{{"ticket", "read"}})

	mb, err := client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(tenantEntity.ID).
		SetRoleID(roleEntity.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	codes, source, err := ResolvePermissions(ctx, client, userEntity.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Empty(t, codes, "到期 membership 必须 fail-closed（不得回退旧权限）")
	assert.Equal(t, PermissionSourceNone, source)

	// 续期（清空到期）→ 同源解析恢复。
	_, err = mb.Update().ClearExpiresAt().Save(ctx)
	require.NoError(t, err)
	codes2, source2, err := ResolvePermissions(ctx, client, userEntity.ID, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"ticket:read"}, codes2)
	assert.Equal(t, PermissionSourceMembership, source2)
}

// TestMembershipNotExpired_PredicateSemantics：NULL=永不过期；未来=有效；过去=排除。
func TestMembershipNotExpired_PredicateSemantics(t *testing.T) {
	ctx := context.Background()
	client := membershipTestClient(t)

	tenantEntity := membershipTestTenant(t, ctx, client, "pred-tenant")
	forever := membershipTestUser(t, ctx, client, "pred-forever", tenantEntity.ID, user.RoleEndUser)
	future := membershipTestUser(t, ctx, client, "pred-future", tenantEntity.ID, user.RoleEndUser)
	past := membershipTestUser(t, ctx, client, "pred-past", tenantEntity.ID, user.RoleEndUser)

	_, err := client.UserTenantMembership.Create().
		SetUserID(forever.ID).SetTenantID(tenantEntity.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserTenantMembership.Create().
		SetUserID(future.ID).SetTenantID(tenantEntity.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).
		SetExpiresAt(time.Now().Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserTenantMembership.Create().
		SetUserID(past.ID).SetTenantID(tenantEntity.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).
		SetExpiresAt(time.Now().Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	rows, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.TenantIDEQ(tenantEntity.ID),
			usertenantmembership.DeletedAtIsNil(),
			MembershipNotExpired(),
		).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "仅 NULL 与未来到期行有效")
	ids := map[int]bool{}
	for _, r := range rows {
		ids[r.UserID] = true
	}
	assert.True(t, ids[forever.ID])
	assert.True(t, ids[future.ID])
	assert.False(t, ids[past.ID])
}
