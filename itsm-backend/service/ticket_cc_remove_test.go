package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestTicketWorkflowService_RemoveTicketCCRecordPermissions(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:remove_ticket_cc?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant, err := client.Tenant.Create().
		SetName("CC Remove Tenant").
		SetCode("cc-remove").
		SetDomain("cc-remove.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	requester, err := client.User.Create().
		SetUsername("cc-requester").
		SetEmail("cc-requester@test.com").
		SetName("CC Requester").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	ccUser, err := client.User.Create().
		SetUsername("cc-target").
		SetEmail("cc-target@test.com").
		SetName("CC Target").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	outsider, err := client.User.Create().
		SetUsername("cc-outsider").
		SetEmail("cc-outsider@test.com").
		SetName("CC Outsider").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	ticketEntity, err := client.Ticket.Create().
		SetTitle("抄送移除").
		SetStatus("open").
		SetPriority("medium").
		SetTicketNumber("CC-REMOVE-1").
		SetRequesterID(requester.ID).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	record, err := client.TicketCC.Create().
		SetTicketID(ticketEntity.ID).
		SetUserID(ccUser.ID).
		SetAddedBy(requester.ID).
		SetTenantID(tenant.ID).
		SetIsActive(true).
		Save(ctx)
	require.NoError(t, err)

	service := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, outsider.ID, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无权管理")

	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, requester.ID, tenant.ID)
	require.NoError(t, err)
	removed, err := client.TicketCC.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.False(t, removed.IsActive)

	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, requester.ID, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "抄送记录不存在")

	second, err := client.TicketCC.Create().
		SetTicketID(ticketEntity.ID).
		SetUserID(ccUser.ID).
		SetAddedBy(requester.ID).
		SetTenantID(tenant.ID).
		SetIsActive(true).
		Save(ctx)
	require.NoError(t, err)
	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, second.ID, ccUser.ID, tenant.ID)
	require.NoError(t, err)
	selfRemoved, err := client.TicketCC.Get(ctx, second.ID)
	require.NoError(t, err)
	assert.False(t, selfRemoved.IsActive)
}

func TestTicketCCRevokedHistoricalIdentityCannotAuthorize(t *testing.T) {
	for _, state := range []string{"active", "disabled", "suspended", "expired", "revoked", "foreign-admin"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			client := enttest.Open(t, "sqlite3", "file:cc-lifecycle-"+state+"?mode=memory&cache=shared&_fk=1")
			t.Cleanup(func() { _ = client.Close() })
			tenant := client.Tenant.Create().SetCode("tenant").SetName("tenant").SaveX(ctx)
			newUser := func(name string, home int) *ent.User {
				return client.User.Create().SetUsername(name).SetName(name).
					SetEmail(name + "@example.com").SetPasswordHash("hash").SetTenantID(home).SetActive(true).SaveX(ctx)
			}
			requester := newUser("requester", tenant.ID)
			target := newUser("cc-target", tenant.ID)
			home := tenant.ID
			if state == "foreign-admin" {
				home = client.Tenant.Create().SetCode("foreign").SetName("foreign").SaveX(ctx).ID
			}
			actor := newUser("old-assignee", home)
			actorMembershipID := 0
			if state == "foreign-admin" {
				// A stored role name must not manufacture customer-tenant access.
				actor.Update().SetRole("super_admin").ExecX(ctx)
			} else {
				m := client.UserTenantMembership.Create().SetUserID(actor.ID).SetTenantID(tenant.ID).
					SetAccountKind(usertenantmembership.AccountKindCustomer).SetSource(usertenantmembership.SourceHome).SaveX(ctx)
				actorMembershipID = m.ID
				switch state {
				case "disabled":
					actor.Update().SetActive(false).ExecX(ctx)
				case "suspended":
					m.Update().SetStatus(usertenantmembership.StatusSuspended).ExecX(ctx)
				case "expired":
					m.Update().SetExpiresAt(time.Now().Add(-time.Hour)).ExecX(ctx)
				case "revoked":
					m.Update().SetDeletedAt(time.Now()).ExecX(ctx)
				}
			}
			builder := client.Ticket.Create().SetTicketNumber("CC-HISTORICAL").SetTitle("CC historical").
				SetRequesterID(requester.ID).SetAssigneeID(actor.ID).SetTenantID(tenant.ID)
			if actorMembershipID > 0 {
				builder.SetAssigneeMembershipID(actorMembershipID)
			}
			tkt := builder.SaveX(ctx)
			svc := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
			for _, recipient := range []int{target.ID, actor.ID} {
				record := client.TicketCC.Create().SetTicketID(tkt.ID).SetUserID(recipient).
					SetAddedBy(requester.ID).SetTenantID(tenant.ID).SetIsActive(true).SaveX(ctx)
				err := svc.RemoveTicketCCRecord(ctx, tkt.ID, record.ID, actor.ID, tenant.ID)
				if state == "active" {
					require.NoError(t, err)
					require.False(t, client.TicketCC.GetX(ctx, record.ID).IsActive)
				} else {
					require.Error(t, err)
					require.True(t, client.TicketCC.GetX(ctx, record.ID).IsActive)
				}
			}
			if state != "active" {
				_, err := svc.ListTicketCCRecords(ctx, tkt.ID, actor.ID, tenant.ID)
				require.Error(t, err, "historical ownership must not bypass read revocation either")
				_, err = svc.ListMyCCRecords(ctx, actor.ID, tenant.ID)
				require.Error(t, err)
			}
		})
	}
}
