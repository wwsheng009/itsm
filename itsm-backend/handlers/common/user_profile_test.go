package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateUserProfile_WhitelistAndUniqueness 自助资料更新（B3）：
// name/email/phone 白名单生效、未提供字段不动、邮箱唯一性沿用 admin 口径、跨租户查不到。
func TestUpdateUserProfile_WhitelistAndUniqueness(t *testing.T) {
	client, svc, home, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	// 他人占用邮箱，用于唯一性校验。
	other := client.User.Create().
		SetUsername("bob").SetEmail("bob@example.com").SetName("Bob").
		SetPasswordHash("hash").SetRole("end_user").SetActive(true).SetTenantID(home.ID).
		SaveX(ctx)

	name := "Alice Renamed"
	phone := "13800000000"
	view, err := svc.UpdateUserProfile(ctx, u.ID, home.ID, &name, nil, &phone)
	require.NoError(t, err)
	require.Equal(t, "Alice Renamed", view.Name)
	require.Equal(t, "13800000000", view.Phone)
	require.Equal(t, "alice@example.com", view.Email, "未提供 email 不应改动")

	// 邮箱被他人占用 → ErrEmailTaken。
	taken := other.Email
	_, err = svc.UpdateUserProfile(ctx, u.ID, home.ID, nil, &taken, nil)
	require.ErrorIs(t, err, ErrEmailTaken)

	// 本人原邮箱重复提交（幂等）→ 放行。
	own := u.Email
	view, err = svc.UpdateUserProfile(ctx, u.ID, home.ID, nil, &own, nil)
	require.NoError(t, err)
	require.Equal(t, own, view.Email)

	// 空白姓名拒绝。
	empty := "   "
	_, err = svc.UpdateUserProfile(ctx, u.ID, home.ID, &empty, nil, nil)
	require.ErrorIs(t, err, ErrNameEmpty)

	// 空白邮箱拒绝。
	emptyEmail := " "
	_, err = svc.UpdateUserProfile(ctx, u.ID, home.ID, nil, &emptyEmail, nil)
	require.ErrorIs(t, err, ErrEmailEmpty)

	// 跨租户：同一 userID + 错误 tenantID → 查不到（不泄露存在性）。
	otherTenant := client.Tenant.Create().
		SetName("Other").SetCode("other-1").SetStatus("active").
		SaveX(ctx)
	_, err = svc.UpdateUserProfile(ctx, u.ID, otherTenant.ID, &name, nil, nil)
	require.Error(t, err)
}
