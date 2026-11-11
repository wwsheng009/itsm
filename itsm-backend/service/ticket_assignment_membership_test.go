package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestTicketService_AssignTicketPersistsMembership 锁定 C 方案分配链路：
// assignee_id 与 assignee_membership_id 必须同时更新。
func TestTicketService_AssignTicketPersistsMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-assign-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("assign-tenant").SetName("assign-tenant").SaveX(ctx)
	requester := client.User.Create().
		SetUsername("assign-requester").SetEmail("assign-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	assignee := client.User.Create().
		SetUsername("assign-agent").SetEmail("assign-agent@example.com").SetName("agent").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	membership := client.UserTenantMembership.Create().
		SetUserID(assignee.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	tkt := client.Ticket.Create().
		SetTicketNumber("TKT-ASSIGN-MB-1").
		SetTitle("assign membership").
		SetStatus("open").
		SetRequesterID(requester.ID).
		SetTenantID(tenant.ID).SaveX(ctx)

	svc := NewTicketServiceForTest(client, zaptest.NewLogger(t).Sugar())
	updated, err := svc.AssignTicket(ctx, tkt.ID, assignee.ID, tenant.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeID)
	assert.Equal(t, assignee.ID, *updated.AssigneeID)
	assert.Equal(t, membership.ID, updated.AssigneeMembershipID)
}
