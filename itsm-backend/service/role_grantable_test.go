package service

import (
	"context"
	"testing"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestListRoles_GrantableFlagByCallerRank（IP-P2-7 收口补丁 9）：角色列表 grantable 标注与写面 CanGrantRoles 同口径——
// rank ≤ 调用者角色 rank 的角色可授予；调用者角色缺省（空）时按 rank 1 收敛（fail closed）。
func TestListRoles_GrantableFlagByCallerRank(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	roleSvc := NewRoleService(client, zaptest.NewLogger(t).Sugar())

	provider := mkProvTenant(t, client, ctx, "SP", "sp-grantable", "msp_provider", "active")
	mkProvRole(t, client, ctx, provider.ID, "msp_admin", true)   // rank 4
	mkProvRole(t, client, ctx, provider.ID, "msp_manager", true) // rank 3
	mkProvRole(t, client, ctx, provider.ID, "msp_tech", true)    // rank 2
	mkProvRole(t, client, ctx, provider.ID, "custom_low", true)  // rank 0（未登记词表）

	list, total, err := roleSvc.ListRoles(ctx, provider.ID, &dto.GetRolesParams{
		Page: 1, PageSize: 50, CallerRole: "msp_manager",
	})
	require.NoError(t, err)
	require.Equal(t, 4, total)
	got := map[string]bool{}
	for _, r := range list {
		got[r.Code] = r.Grantable
	}
	require.False(t, got["msp_admin"], "高于调用者 rank 的角色不可授予")
	require.True(t, got["msp_manager"], "同级角色可授予")
	require.True(t, got["msp_tech"], "低于调用者 rank 的角色可授予")
	require.True(t, got["custom_low"], "未登记词表角色 rank 0 可授予")

	// msp_admin（rank 4）：provider 可见集内全部可授予（msp_admin 同级）。
	list, _, err = roleSvc.ListRoles(ctx, provider.ID, &dto.GetRolesParams{
		Page: 1, PageSize: 50, CallerRole: "msp_admin",
	})
	require.NoError(t, err)
	for _, r := range list {
		require.True(t, r.Grantable, r.Code)
	}

	// 缺省调用者角色（空）：fail closed，仅 rank ≤ 1 可授予。
	list, _, err = roleSvc.ListRoles(ctx, provider.ID, &dto.GetRolesParams{Page: 1, PageSize: 50})
	require.NoError(t, err)
	for _, r := range list {
		require.Equal(t, serviceRoleRank(r.Code) <= 1, r.Grantable, r.Code)
	}
}
