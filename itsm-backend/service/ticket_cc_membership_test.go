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
