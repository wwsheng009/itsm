package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserPreferences_Roundtrip IP-P1-6c：白名单合并读写、非白名单拒绝、超限拒绝、nil 删除。
func TestUserPreferences_Roundtrip(t *testing.T) {
	client, svc, _, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	prefs, err := svc.GetUserPreferences(ctx, u.ID)
	require.NoError(t, err)
	require.Empty(t, prefs, "默认应为空偏好")

	want := map[string]any{"customerTenantIds": []any{1, 2}, "mode": "subset"}
	prefs, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": want})
	require.NoError(t, err)
	require.NotNil(t, prefs["workbenchFilter"])

	got, err := svc.GetUserPreferences(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, want["mode"], got["workbenchFilter"].(map[string]any)["mode"])

	// 非白名单键整体拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"evil": 1})
	require.ErrorIs(t, err, ErrPreferenceKeyNotAllowed)

	// 超限拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": strings.Repeat("x", maxPreferenceBytes+1)})
	require.ErrorIs(t, err, ErrPreferencePayloadTooLarge)

	// nil 删除键。
	prefs, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": nil})
	require.NoError(t, err)
	require.NotContains(t, prefs, "workbenchFilter")
}
