package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"
)

// TestListPermissions_AssignableFlag（IP-P2-7 收口补丁 8 / R5）：权限目录 assignable 标注与写面护栏同口径——
// 非平台根租户：通配（*:*）与平台治理家族（system）标注不可分配；其余可分配；平台根（含 code=default 兜底）全可分配。
func TestListPermissions_AssignableFlag(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	permSvc := NewPermissionService(client, zaptest.NewLogger(t).Sugar())

	customer := mkProvTenant(t, client, ctx, "Cust", "cust-assignable", "msp_customer", "active")
	root := mkProvTenant(t, client, ctx, "Root", "default", "msp_provider", "active") // code=default 兜底判为平台根（D4=B）

	mkPerm := func(tenantID int, code, resource, action string) {
		t.Helper()
		_, err := client.Permission.Create().
			SetCode(code).SetName(code).SetResource(resource).SetAction(action).
			SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
	}
	mkPerm(customer.ID, "system:read", "system", "read")
	mkPerm(customer.ID, "ticket:read", "ticket", "read")
	mkPerm(customer.ID, "*:*", "*", "*")

	items, err := permSvc.ListPermissions(ctx, customer.ID, "")
	require.NoError(t, err)
	got := map[string]bool{}
	for _, it := range items {
		got[it.Code] = it.Assignable
	}
	require.False(t, got["system:read"], "平台治理家族：非平台根不可分配")
	require.False(t, got["*:*"], "通配：非平台根不可分配")
	require.True(t, got["ticket:read"], "普通租户面码：可分配")

	// 平台根（code=default 兜底）：全部可分配。
	mkPerm(root.ID, "system:read", "system", "read")
	items, err = permSvc.ListPermissions(ctx, root.ID, "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].Assignable, "平台根不受限")
}
