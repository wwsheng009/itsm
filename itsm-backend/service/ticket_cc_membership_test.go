package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/ticketcc"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestCCTicketPersistsMemberships 锁定 CC 的双方身份：抄送接收人与添加人
// 都必须写租户内 membership，而 user_id 仅保留全局账号兼容。
func TestCCTicketPersistsMemberships(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-cc-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("cc-tenant").SetName("cc-tenant").SaveX(ctx)
	requester := client.User.Create().
		SetUsername("cc-requester").SetEmail("cc-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	ccUser := client.User.Create().
		SetUsername("cc-user").SetEmail("cc-user@example.com").SetName("cc-user").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	requesterMembership := client.UserTenantMembership.Create().
		SetUserID(requester.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	ccMembership := client.UserTenantMembership.Create().
		SetUserID(ccUser.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	tkt := client.Ticket.Create().
		SetTicketNumber("TKT-CC-MB-1").
		SetTitle("cc membership").
		SetStatus("open").
		SetRequesterID(requester.ID).
		SetTenantID(tenant.ID).SaveX(ctx)

	svc := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
	require.NoError(t, svc.CCTicket(ctx, &dto.CCTicketRequest{
		TicketID: tkt.ID,
		CCUsers:  []int{ccUser.ID},
	}, requester.ID, tenant.ID))

	row, err := client.TicketCC.Query().
		Where(ticketcc.TicketID(tkt.ID), ticketcc.UserID(ccUser.ID), ticketcc.TenantID(tenant.ID)).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, ccMembership.ID, row.UserMembershipID)
	assert.Equal(t, requesterMembership.ID, row.AddedByMembershipID)
}

// TestCCTicketAllowsProviderMembershipAndRejectsForeignTarget 锁定 C 方案关键语义：
// provider 不复制 users 行，凭客户租户 active membership 可被抄送；无 membership 的
// 跨租户账号必须 fail-closed，不能被写入 ticket_ccs。
func TestCCTicketAllowsProviderMembershipAndRejectsForeignTarget(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-cc-provider?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	customerTenant := client.Tenant.Create().SetCode("cc-customer").SetName("cc-customer").SaveX(ctx)
	providerTenant := client.Tenant.Create().SetCode("cc-provider").SetName("cc-provider").SaveX(ctx)
	foreignTenant := client.Tenant.Create().SetCode("cc-foreign").SetName("cc-foreign").SaveX(ctx)

	requester := client.User.Create().
		SetUsername("cc-provider-requester").SetEmail("cc-provider-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(customerTenant.ID).SetActive(true).SaveX(ctx)
	provider := client.User.Create().
		SetUsername("cc-provider-tech").SetEmail("cc-provider-tech@example.com").SetName("provider tech").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).SaveX(ctx)
	foreign := client.User.Create().
		SetUsername("cc-foreign-user").SetEmail("cc-foreign-user@example.com").SetName("foreign user").
		SetPasswordHash("hash").SetTenantID(foreignTenant.ID).SetActive(true).SaveX(ctx)

	requesterMembership := client.UserTenantMembership.Create().
		SetUserID(requester.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	providerMembership := client.UserTenantMembership.Create().
		SetUserID(provider.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetMspRole("msp_viewer").
		SetStatus(usertenantmembership.StatusActive).SaveX(ctx)

	tkt := client.Ticket.Create().
		SetTicketNumber("TKT-CC-MB-2").
		SetTitle("provider membership cc").
		SetStatus("open").
		SetRequesterID(requester.ID).
		SetTenantID(customerTenant.ID).SaveX(ctx)

	svc := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
	require.NoError(t, svc.CCTicket(ctx, &dto.CCTicketRequest{
		TicketID: tkt.ID,
		CCUsers:  []int{provider.ID},
	}, requester.ID, customerTenant.ID))

	row, err := client.TicketCC.Query().
		Where(ticketcc.TicketID(tkt.ID), ticketcc.UserID(provider.ID), ticketcc.TenantID(customerTenant.ID)).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, providerMembership.ID, row.UserMembershipID)
	assert.Equal(t, requesterMembership.ID, row.AddedByMembershipID)

	ccRecords, err := svc.ListTicketCCRecords(ctx, tkt.ID, provider.ID, customerTenant.ID)
	require.NoError(t, err)
	require.Len(t, ccRecords.Records, 1)
	assert.Equal(t, providerMembership.ID, ccRecords.Records[0].UserMembershipID)

	err = svc.CCTicket(ctx, &dto.CCTicketRequest{
		TicketID: tkt.ID,
		CCUsers:  []int{foreign.ID},
	}, requester.ID, customerTenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不属于当前租户")

	count, err := client.TicketCC.Query().
		Where(ticketcc.TicketID(tkt.ID), ticketcc.UserID(foreign.ID)).
		Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count, "无 membership 的跨租户用户不得写入 CC")
}
