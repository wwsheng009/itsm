package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
)

// TestMSPAllocationService_ProviderDimension 覆盖 IP-P2-1 §5.0-A：
// provider 派生与落库、跨 provider 拒绝、直客拒绝、admin 不豁免、非服务商员工拒绝、N=2 收窄。
func TestMSPAllocationService_ProviderDimension(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	svc := NewMSPAllocationService(client, zaptest.NewLogger(t).Sugar())

	provA := client.Tenant.Create().SetName("Provider A").SetCode("prov-a").
		SetType(tenant.Type("msp_provider")).SetStatus("active").SaveX(ctx)
	provB := client.Tenant.Create().SetName("Provider B").SetCode("prov-b").
		SetType(tenant.Type("msp_provider")).SetStatus("active").SaveX(ctx)
	custA := client.Tenant.Create().SetName("Customer A").SetCode("cust-a").
		SetType(tenant.Type("msp_customer")).SetStatus("active").SetMspProviderID(provA.ID).SaveX(ctx)
	custB := client.Tenant.Create().SetName("Customer B").SetCode("cust-b").
		SetType(tenant.Type("msp_customer")).SetStatus("active").SetMspProviderID(provB.ID).SaveX(ctx)
	direct := client.Tenant.Create().SetName("Direct").SetCode("direct").
		SetType(tenant.Type("saas_customer")).SetStatus("active").SaveX(ctx)
	std := client.Tenant.Create().SetName("Std").SetCode("std").
		SetType(tenant.Type("standard")).SetStatus("active").SaveX(ctx)

	agentA := client.User.Create().SetUsername("agent-a").SetEmail("agent-a@example.com").
		SetName("Agent A").SetPasswordHash("h").SetTenantID(provA.ID).
		SetMspRole(user.MspRole("provider_agent")).SaveX(ctx)
	userStd := client.User.Create().SetUsername("std-user").SetEmail("std@example.com").
		SetName("Std").SetPasswordHash("h").SetTenantID(std.ID).SaveX(ctx)

	t.Run("provider derived and persisted", func(t *testing.T) {
		res, err := svc.Create(ctx, agentA.ID, custA.ID, "primary")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, provA.ID, res.ProviderTenantID)
		row, err := client.MSPAllocation.Get(ctx, res.ID)
		require.NoError(t, err)
		assert.Equal(t, provA.ID, row.ProviderTenantID)
	})

	t.Run("cross-provider rejected", func(t *testing.T) {
		_, err := svc.Create(ctx, agentA.ID, custB.ID, "primary")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "跨 provider")
	})

	t.Run("direct customer rejected", func(t *testing.T) {
		_, err := svc.Create(ctx, agentA.ID, direct.ID, "primary")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "跨 provider")
	})

	t.Run("admin does not bypass ownership", func(t *testing.T) {
		_, err := svc.Create(ctx, agentA.ID, custB.ID, "primary", "super_admin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "跨 provider")
	})

	t.Run("non-provider user rejected", func(t *testing.T) {
		_, err := svc.Create(ctx, userStd.ID, custA.ID, "primary")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "不属于MSP租户")
	})

	t.Run("GetMSPCustomers narrows to provider", func(t *testing.T) {
		// 直接注入他 provider 的错配分配（模拟回填前脏数据）——不得出现在该员工客户列表中。
		client.MSPAllocation.Create().
			SetMspUserID(agentA.ID).SetCustomerTenantID(custB.ID).
			SetProviderTenantID(provB.ID).SetRole("primary").SaveX(ctx)

		customers, err := svc.GetMSPCustomers(ctx, agentA.ID)
		require.NoError(t, err)
		ids := make([]int, 0, len(customers))
		for _, c := range customers {
			ids = append(ids, c.ID)
		}
		assert.Contains(t, ids, custA.ID)
		assert.NotContains(t, ids, custB.ID)
	})

	t.Run("ListByMSPUser narrows stray provider rows", func(t *testing.T) {
		rows, err := svc.ListByMSPUser(ctx, agentA.ID)
		require.NoError(t, err)
		for _, r := range rows {
			assert.NotEqual(t, custB.ID, r.CustomerTenantID, "他 provider 的分配不得返回")
		}
	})
}
