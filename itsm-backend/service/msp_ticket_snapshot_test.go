package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	ticketrepo "itsm-backend/repository/ticket"
)

// TestCreateTicket_MSPProviderSnapshot 锁定 IP-P0-3 / R11 / A12：
// 建单时按客户租户归属派生 MSP 快照；无效归属（停用/无 provider/provider 停用）按普通工单处理。
// 每个用例独立 SQLite 库（ticket_number 为全局唯一，共享库会互相碰撞）。
func TestCreateTicket_MSPProviderSnapshot(t *testing.T) {
	cases := []struct {
		name           string
		customerType   string
		customerStatus string
		providerStatus string // "" = 不建 provider
		wantManaged    bool
	}{
		{name: "managed customer snapshots provider", customerType: "msp_customer", customerStatus: "active", providerStatus: "active", wantManaged: true},
		{name: "direct customer has no snapshot", customerType: "saas_customer", customerStatus: "active", providerStatus: "", wantManaged: false},
		{name: "inactive customer falls back to normal ticket", customerType: "msp_customer", customerStatus: "suspended", providerStatus: "active", wantManaged: false},
		{name: "suspended provider falls back to normal ticket", customerType: "msp_customer", customerStatus: "active", providerStatus: "suspended", wantManaged: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := enttest.Open(t, "sqlite3", testDSN())
			defer client.Close()
			svc := NewTicketServiceForTest(client, zaptest.NewLogger(t).Sugar())
			ctx := context.Background()

			providerID := 0
			if tc.providerStatus != "" {
				provider, err := client.Tenant.Create().
					SetName("Provider").SetCode("prov-snap").
					SetType(tenant.Type("msp_provider")).SetStatus(tc.providerStatus).
					Save(ctx)
				require.NoError(t, err)
				providerID = provider.ID
			}
			ctb := client.Tenant.Create().
				SetName("Customer").SetCode("cust-snap").
				SetType(tenant.Type(tc.customerType)).SetStatus(tc.customerStatus)
			if providerID > 0 {
				ctb = ctb.SetMspProviderID(providerID)
			}
			customer, err := ctb.Save(ctx)
			require.NoError(t, err)

			user, err := client.User.Create().
				SetUsername("requester").SetEmail("requester@example.com").SetName("requester").
				SetPasswordHash("hash").SetRole("end_user").SetActive(true).SetTenantID(customer.ID).
				Save(ctx)
			require.NoError(t, err)

			tkt, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
				Title: "快照测试工单", Description: "desc", Priority: "medium", RequesterID: user.ID,
			}, customer.ID)
			require.NoError(t, err)
			require.NotNil(t, tkt)

			assert.Equal(t, tc.wantManaged, tkt.IsManagedByMSP)
			if tc.wantManaged {
				require.NotNil(t, tkt.MSPProviderID)
				assert.Equal(t, providerID, *tkt.MSPProviderID)
				assert.Nil(t, tkt.ManagedByUserID, "指派前不应写处理人")
			} else {
				assert.Nil(t, tkt.MSPProviderID)
			}
		})
	}
}

// TestRepositoryUpdate_MSPAssignmentSnapshot 锁定指派/回填补写路径（R11 四字段中的 managed_by_user_id）。
func TestRepositoryUpdate_MSPAssignmentSnapshot(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	svc := NewTicketServiceForTest(client, zaptest.NewLogger(t).Sugar())
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("prov-upd").SetType(tenant.Type("msp_provider")).SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("cust-upd").SetType(tenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("u-upd").SetEmail("u-upd@example.com").SetName("u-upd").
		SetPasswordHash("hash").SetRole("end_user").SetActive(true).SetTenantID(customer.ID).
		Save(ctx)
	require.NoError(t, err)

	tkt, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title: "指派快照测试", Description: "desc", Priority: "medium", RequesterID: user.ID,
	}, customer.ID)
	require.NoError(t, err)
	require.True(t, tkt.IsManagedByMSP, "前置：建单已落 provider 快照")

	managed := true
	updated, err := svc.repo.Update(ctx, tkt.ID, &ticketrepo.UpdateParams{
		Version:         tkt.Version,
		IsManagedByMSP:  &managed,
		MSPProviderID:   &provider.ID,
		ManagedByUserID: &user.ID,
	}, customer.ID)
	require.NoError(t, err)
	assert.True(t, updated.IsManagedByMSP)
	require.NotNil(t, updated.MSPProviderID)
	assert.Equal(t, provider.ID, *updated.MSPProviderID)
	require.NotNil(t, updated.ManagedByUserID)
	assert.Equal(t, user.ID, *updated.ManagedByUserID)
}
