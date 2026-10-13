package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
)

// TestTicketService_MSPAccessGuard 锁定 IP-P0-2 的核心回归：
// 未分配客户的 MSP 通道（路径参数 / 请求体）必须在 service 层被统一入口拒绝，
// 而不是沿旧实现（无校验直查）返回 200。
func TestTicketService_MSPAccessGuard(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	svc := NewTicketServiceForTest(client, zap.NewNop().Sugar())

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("prov-guard").SetType("msp_provider").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("cust-guard").SetType("msp_customer").SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	mspUser, err := client.User.Create().
		SetUsername("msp-guard").SetEmail("msp-guard@example.com").SetName("msp-guard").
		SetPasswordHash("hash").SetTenantID(provider.ID).SetMspRole("provider_agent").
		Save(ctx)
	require.NoError(t, err)

	t.Run("path channel: unallocated customer tickets denied", func(t *testing.T) {
		_, err := svc.GetCustomerTicketsForMSP(ctx, mspUser.ID, customer.ID, nil, 1, 10)
		ae, ok := AsCustomerAccessError(err)
		require.True(t, ok, "want typed access error, got %v", err)
		assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	})

	t.Run("body channel: unallocated customer assign denied", func(t *testing.T) {
		_, err := svc.AssignMSPTechnician(ctx, 123, customer.ID, mspUser.ID)
		ae, ok := AsCustomerAccessError(err)
		require.True(t, ok, "want typed access error, got %v", err)
		assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	})

	t.Run("reports channel: unallocated customer reports are empty", func(t *testing.T) {
		reports, err := svc.GetMSPCustomerReports(ctx, mspUser.ID, time.Time{}, time.Time{})
		require.NoError(t, err)
		assert.Empty(t, reports)
	})
}

// TestGetCustomerTicketsForMSP_ResolvesAssigneeName 锁定「分配技术员」可见性回归：
// 负责人是 MSP 员工（home=provider 租户，工单在客户租户），列表必须按用户 ID 解析显示名；
// 否则前端点「分配技术员」成功（toast）后，负责人列仍显示「未分配」。
func TestGetCustomerTicketsForMSP_ResolvesAssigneeName(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	svc := NewTicketServiceForTest(client, zap.NewNop().Sugar())

	provider, err := client.Tenant.Create().
		SetName("Provider-N").SetCode("prov-name").SetType("msp_provider").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer-N").SetCode("cust-name").SetType("msp_customer").SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	mspUser, err := client.User.Create().
		SetUsername("msp-name").SetEmail("msp-name@example.com").SetName("MSP Name").
		SetPasswordHash("hash").SetTenantID(provider.ID).SetMspRole("provider_agent").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.MSPAllocation.Create().
		SetMspUserID(mspUser.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).
		SetRole("primary").Save(ctx)
	require.NoError(t, err)
	_, err = client.Ticket.Create().
		SetTicketNumber("MSP-T-NAME-1").
		SetTitle("指派可见性回归").
		SetDescription("用于验证负责人显示名回填").
		SetType("incident").
		SetPriority("medium").
		SetStatus("open").
		SetRequesterID(mspUser.ID).
		SetAssigneeID(mspUser.ID).
		SetTenantID(customer.ID).
		Save(ctx)
	require.NoError(t, err)

	items, err := svc.GetCustomerTicketsForMSP(ctx, mspUser.ID, customer.ID, nil, 1, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].AssigneeID)
	assert.Equal(t, mspUser.ID, *items[0].AssigneeID)
	assert.Equal(t, "MSP Name", items[0].AssigneeName, "跨租户负责人必须回填显示名")
}
