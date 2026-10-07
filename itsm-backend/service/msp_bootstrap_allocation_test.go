package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"
	entUser "itsm-backend/ent/user"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var bootstrapAllocDBCounter int64

// TestEnsureBootstrapAllocations 锁定 provider 首管默认分配语义：
// 幂等；覆盖 bootstrap 首管与 admin/super_admin 角色；跳过暂停客户；不越 provider；dry-run 不写库。
func TestEnsureBootstrapAllocations(t *testing.T) {
	dsn := fmt.Sprintf("file:bootstrap_alloc_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&bootstrapAllocDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop().Sugar()

	mkTenant := func(name, code, typ, status string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(entTenant.Type(typ)).SetStatus(status)
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	mkUser := func(name string, tenantID int, bootstrap bool, role string) *ent.User {
		ub := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetTenantID(tenantID).SetActive(true).
			SetIsBootstrapAdmin(bootstrap)
		if role != "" {
			ub = ub.SetRole(entUser.Role(role))
		}
		u, err := ub.Save(ctx)
		require.NoError(t, err)
		return u
	}

	provider := mkTenant("Provider", "ba-prov", "msp_provider", "active", 0)
	provider2 := mkTenant("Provider2", "ba-prov2", "msp_provider", "active", 0)
	custA := mkTenant("Customer A", "ba-a", "msp_customer", "active", provider.ID)
	custB := mkTenant("Customer B", "ba-b", "msp_customer", "active", provider.ID)
	custC := mkTenant("Customer C", "ba-c", "msp_customer", "suspended", provider.ID)
	custD := mkTenant("Customer D", "ba-d", "msp_customer", "active", provider2.ID)

	admin := mkUser("ba-admin", provider.ID, true, "")                    // bootstrap 首管
	providerAdmin := mkUser("ba-prov-admin", provider.ID, false, "admin") // seeder 场景的管理员
	mkUser("ba-normal", provider.ID, false, "agent")                      // 非管理员：不参与

	// dry-run：仅统计（首管 + admin 角色 = 2），不写库。
	n, err := EnsureBootstrapAllocationsForCustomer(ctx, client, logger, false, custA.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	rows, err := client.MSPAllocation.Query().All(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows)

	// apply：为两个管理员创建 primary allocation。
	n, err = EnsureBootstrapAllocationsForCustomer(ctx, client, logger, true, custA.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	rows, err = client.MSPAllocation.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	userIDs := map[int]bool{}
	for _, r := range rows {
		userIDs[r.MspUserID] = true
		assert.Equal(t, custA.ID, r.CustomerTenantID)
		assert.Equal(t, provider.ID, r.ProviderTenantID)
		assert.Equal(t, "primary", r.Role)
	}
	assert.True(t, userIDs[admin.ID])
	assert.True(t, userIDs[providerAdmin.ID])

	// 幂等：重复执行为 0。
	n, err = EnsureBootstrapAllocationsForCustomer(ctx, client, logger, true, custA.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// ForProvider：A 已有 → 跳过；B 创建 2；C 暂停跳过；D 属其它 provider 不在范围。
	n, err = EnsureBootstrapAllocationsForProvider(ctx, client, logger, true, provider.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	rows, err = client.MSPAllocation.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	customers := map[int]int{}
	for _, r := range rows {
		customers[r.CustomerTenantID]++
	}
	assert.Equal(t, 2, customers[custA.ID])
	assert.Equal(t, 2, customers[custB.ID])
	assert.NotContains(t, customers, custC.ID, "暂停客户不得分配")
	assert.NotContains(t, customers, custD.ID, "跨 provider 客户不得分配")

	// provider2 无管理员 → 0。
	n, err = EnsureBootstrapAllocationsForProvider(ctx, client, logger, true, provider2.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// 审计：每次创建一行 msp.allocation_auto_assigned。
	logs, err := client.AuditLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 4)
	assert.Equal(t, "msp.allocation_auto_assigned", logs[0].Action)
	assert.Equal(t, provider.ID, logs[0].TenantID)
}
