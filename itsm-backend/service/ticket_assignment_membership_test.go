package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/ticketworkflowrecord"
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

func TestTicketUpdateAssigneePresenceAcrossServices(t *testing.T) {
	for _, entry := range []string{"production", "core"} {
		t.Run(entry, func(t *testing.T) {
			ctx := context.Background()
			client := enttest.Open(t, "sqlite3", "file:ticket-update-presence-"+entry+"?mode=memory&cache=shared&_fk=1")
			t.Cleanup(func() { _ = client.Close() })
			scope := client.Tenant.Create().SetCode("customer").SetName("Customer").SaveX(ctx)
			provider := client.Tenant.Create().SetCode("provider").SetName("Provider").SaveX(ctx)
			account := client.User.Create().SetUsername("provider-agent").SetName("Agent").
				SetEmail("agent@example.test").SetPasswordHash("test").SetTenantID(provider.ID).SetActive(true).SaveX(ctx)
			membership := client.UserTenantMembership.Create().SetUserID(account.ID).SetTenantID(scope.ID).
				SetAccountKind(usertenantmembership.AccountKindProvider).SetSource(usertenantmembership.SourceAllocation).SaveX(ctx)
			tkt := client.Ticket.Create().SetTitle("Identity update").SetTicketNumber("UPDATE-PAIR").
				SetRequesterID(account.ID).SetTenantID(scope.ID).SaveX(ctx)
			logger := zaptest.NewLogger(t).Sugar()
			production := NewTicketServiceForTest(client, logger)
			core := NewTicketCoreService(client, logger)
			update := func(req *dto.UpdateTicketRequest) error {
				if entry == "core" {
					_, err := core.UpdateTicketBasic(ctx, tkt.ID, req, scope.ID)
					return err
				}
				_, err := production.UpdateTicket(ctx, tkt.ID, req, scope.ID, 0, "")
				return err
			}
			require.NoError(t, update(&dto.UpdateTicketRequest{AssigneeID: &account.ID, Version: tkt.Version}))
			stored := client.Ticket.GetX(ctx, tkt.ID)
			require.Equal(t, account.ID, stored.AssigneeID)
			require.Equal(t, membership.ID, stored.AssigneeMembershipID)
			require.NoError(t, update(&dto.UpdateTicketRequest{Title: "No assignment change", Version: stored.Version}))
			stored = client.Ticket.GetX(ctx, tkt.ID)
			require.Equal(t, membership.ID, stored.AssigneeMembershipID)
			require.NoError(t, account.Update().SetActive(false).Exec(ctx))
			require.Error(t, update(&dto.UpdateTicketRequest{AssigneeID: &account.ID, Version: stored.Version}))
			require.Equal(t, stored.Version, client.Ticket.GetX(ctx, tkt.ID).Version)
			zero := 0
			require.NoError(t, update(&dto.UpdateTicketRequest{AssigneeID: &zero, Version: stored.Version}))
			cleared := client.Ticket.GetX(ctx, tkt.ID)
			require.Zero(t, cleared.AssigneeID)
			require.Zero(t, cleared.AssigneeMembershipID)
		})
	}
}

func TestTicketWorkflowAssigneeIdentity(t *testing.T) {
	for _, action := range []string{"accept", "transfer"} {
		for _, state := range []string{"active", "legacy", "disabled", "revoked", "expired", "foreign", "record-failure"} {
			t.Run(action+"/"+state, func(t *testing.T) {
				ctx := context.Background()
				client := enttest.Open(t, "sqlite3", "file:workflow-identity-"+action+"-"+state+"?mode=memory&cache=shared&_fk=1")
				t.Cleanup(func() { _ = client.Close() })
				tenant := client.Tenant.Create().SetCode("customer").SetName("customer").SaveX(ctx)
				newUser := func(name string, home int) *ent.User {
					return client.User.Create().SetUsername(name).SetName(name).
						SetEmail(name + "@example.com").SetPasswordHash("hash").SetActive(true).SetTenantID(home).SaveX(ctx)
				}
				newMembership := func(id int) *ent.UserTenantMembership {
					return client.UserTenantMembership.Create().SetUserID(id).SetTenantID(tenant.ID).
						SetAccountKind(usertenantmembership.AccountKindCustomer).SetSource(usertenantmembership.SourceHome).SaveX(ctx)
				}
				old := newUser("old", tenant.ID)
				oldMembership := newMembership(old.ID)
				home := tenant.ID
				if state == "foreign" {
					home = client.Tenant.Create().SetCode("foreign").SetName("foreign").SaveX(ctx).ID
				}
				next := newUser("next", home)
				nextMembershipID := 0
				if state != "legacy" && state != "foreign" {
					m := newMembership(next.ID)
					nextMembershipID = m.ID
					switch state {
					case "disabled":
						client.User.UpdateOneID(next.ID).SetActive(false).ExecX(ctx)
					case "revoked":
						m.Update().SetDeletedAt(time.Now()).ExecX(ctx)
					case "expired":
						m.Update().SetExpiresAt(time.Now().Add(-time.Minute)).ExecX(ctx)
					}
				}
				tkt := client.Ticket.Create().SetTitle("workflow identity").SetTicketNumber("IDENTITY-WF").
					SetStatus("open").SetRequesterID(old.ID).SetTenantID(tenant.ID).
					SetAssigneeID(old.ID).SetAssigneeMembershipID(oldMembership.ID).SaveX(ctx)
				if state == "record-failure" {
					client.TicketWorkflowRecord.Use(func(next ent.Mutator) ent.Mutator {
						return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
							return nil, fmt.Errorf("workflow record unavailable")
						})
					})
				}
				svc := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
				var err error
				if action == "accept" {
					err = svc.AcceptTicket(ctx, &dto.AcceptTicketRequest{TicketID: tkt.ID}, next.ID, tenant.ID)
				} else {
					err = svc.ForwardTicket(ctx, &dto.ForwardTicketRequest{
						TicketID: tkt.ID, ToUserID: next.ID, TransferOwnership: true,
					}, old.ID, tenant.ID)
				}
				stored := client.Ticket.GetX(ctx, tkt.ID)
				records := client.TicketWorkflowRecord.Query().Where(ticketworkflowrecord.TicketIDEQ(tkt.ID)).CountX(ctx)
				if state == "active" || state == "legacy" {
					require.NoError(t, err)
					require.Equal(t, next.ID, stored.AssigneeID)
					require.Equal(t, nextMembershipID, stored.AssigneeMembershipID)
					require.Equal(t, tkt.Version+1, stored.Version)
					require.Equal(t, 1, records)
				} else {
					require.Error(t, err)
					require.Equal(t, old.ID, stored.AssigneeID)
					require.Equal(t, oldMembership.ID, stored.AssigneeMembershipID)
					require.Equal(t, tkt.Version, stored.Version)
					require.Equal(t, tkt.Status, stored.Status)
					require.Zero(t, records, "failed identity/record validation must leave no partial commit")
				}
			})
		}
	}
}
