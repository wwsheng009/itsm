package ticket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entticket "itsm-backend/ent/ticket"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	ticketrepo "itsm-backend/repository/ticket"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type ticketIdentityFixture struct {
	ctx        context.Context
	client     *ent.Client
	tenant     *ent.Tenant
	foreign    *ent.Tenant
	actor      *ent.User
	membership *ent.UserTenantMembership
	handler    *Handler
}

func newTicketIdentityFixture(t *testing.T) *ticketIdentityFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:ticket-http-identity-"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	f := &ticketIdentityFixture{ctx: ctx, client: client}
	f.tenant = client.Tenant.Create().SetCode("customer").SetName("customer").SaveX(ctx)
	f.foreign = client.Tenant.Create().SetCode("foreign").SetName("foreign").SaveX(ctx)
	f.actor = f.account("actor", f.tenant.ID)
	f.membership = f.member(f.actor.ID)
	logger := zap.NewNop().Sugar()
	repo := ticketrepo.NewEntRepository(client, logger)
	production := service.NewTicketService(&service.TicketServiceConfig{Repository: repo, Client: client, Logger: logger})
	production.EnableWorkflowOutbox()
	production.EnableSideEffectOutbox()
	f.handler = NewHandler(NewService(NewEntRepository(repo), production, logger))
	return f
}

func (f *ticketIdentityFixture) account(name string, home int) *ent.User {
	return f.client.User.Create().SetUsername(name).SetName(name).SetEmail(name + "@example.com").
		SetPasswordHash("hash").SetTenantID(home).SetActive(true).SaveX(f.ctx)
}

func (f *ticketIdentityFixture) member(userID int) *ent.UserTenantMembership {
	return f.client.UserTenantMembership.Create().SetUserID(userID).SetTenantID(f.tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).SetSource(usertenantmembership.SourceHome).SaveX(f.ctx)
}

func (f *ticketIdentityFixture) routes() *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_id", f.tenant.ID)
		c.Set("user_id", f.actor.ID)
		c.Set("role", "end_user")
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: f.tenant.ID})
		middleware.SetMembershipContext(c, f.membership)
		c.Request = c.Request.WithContext(tenantctx.WithTenantID(c.Request.Context(), f.tenant.ID))
	})
	r.PATCH("/api/v1/tickets/:id", f.handler.UpdateTicket)
	r.POST("/api/v1/tickets/:id/subtasks", f.handler.CreateSubtask)
	r.PATCH("/api/v1/tickets/:id/subtasks/:subtask_id", f.handler.UpdateSubtask)
	return r
}

func ticketIdentityRequest(t *testing.T, r *gin.Engine, method, path, body string, wantStatus int) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, wantStatus, w.Code, w.Body.String())
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	if wantStatus == http.StatusOK {
		require.Equal(t, float64(0), response["code"])
	} else {
		require.NotEqual(t, float64(0), response["code"])
	}
	require.NotContains(t, w.Body.String(), "passwordHash")
	require.NotContains(t, w.Body.String(), "password_hash")
	return response
}

func TestHandlerPatchAssigneeMembershipIdentity(t *testing.T) {
	f := newTicketIdentityFixture(t)
	next := f.account("next", f.tenant.ID)
	nextMembership := f.member(next.ID)
	foreign := f.account("foreign", f.foreign.ID)
	disabled := f.account("disabled", f.tenant.ID)
	f.member(disabled.ID)
	disabled.Update().SetActive(false).ExecX(f.ctx)
	tkt := f.client.Ticket.Create().SetTitle("patch identity").SetTicketNumber("PATCH-IDENTITY").
		SetRequesterID(f.actor.ID).SetTenantID(f.tenant.ID).
		SetAssigneeID(f.actor.ID).SetAssigneeMembershipID(f.membership.ID).SaveX(f.ctx)
	r := f.routes()
	path := fmt.Sprintf("/api/v1/tickets/%d", tkt.ID)
	response := ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"assigneeId":%d,"version":%d}`, next.ID, tkt.Version), http.StatusOK)
	data := response["data"].(map[string]interface{})
	require.Equal(t, float64(next.ID), data["assigneeId"])
	require.NotContains(t, data, "assignee_id")
	stored := f.client.Ticket.GetX(f.ctx, tkt.ID)
	require.Equal(t, next.ID, stored.AssigneeID)
	require.Equal(t, nextMembership.ID, stored.AssigneeMembershipID)
	for _, id := range []int{foreign.ID, disabled.ID, -1} {
		ticketIdentityRequest(t, r, http.MethodPatch, path,
			fmt.Sprintf(`{"assigneeId":%d,"version":%d,"title":"must not commit"}`, id, stored.Version), http.StatusBadRequest)
		fresh := f.client.Ticket.GetX(f.ctx, tkt.ID)
		require.Equal(t, stored.Version, fresh.Version)
		require.Equal(t, stored.Title, fresh.Title)
		require.Equal(t, next.ID, fresh.AssigneeID)
		require.Equal(t, nextMembership.ID, fresh.AssigneeMembershipID)
	}
	// Omitted assignee must leave both identity columns untouched.
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"title":"title only","version":%d}`, stored.Version), http.StatusOK)
	stored = f.client.Ticket.GetX(f.ctx, tkt.ID)
	require.Equal(t, next.ID, stored.AssigneeID)
	require.Equal(t, nextMembership.ID, stored.AssigneeMembershipID)
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"assigneeId":0,"version":%d}`, stored.Version), http.StatusOK)
	cleared := f.client.Ticket.GetX(f.ctx, tkt.ID)
	require.Zero(t, cleared.AssigneeID)
	require.Zero(t, cleared.AssigneeMembershipID)
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"assigneeId":%d,"version":%d}`, next.ID, stored.Version), http.StatusConflict)
	require.Zero(t, f.client.Ticket.GetX(f.ctx, tkt.ID).AssigneeMembershipID, "stale reassignment must not undo a clear")
}

func TestHandlerPatchSubtaskAssigneeIdentity(t *testing.T) {
	f := newTicketIdentityFixture(t)
	parent := f.client.Ticket.Create().SetTitle("parent").SetTicketNumber("PATCH-PARENT").
		SetRequesterID(f.actor.ID).SetTenantID(f.tenant.ID).SaveX(f.ctx)
	child := f.client.Ticket.Create().SetTitle("child").SetTicketNumber("PATCH-CHILD").
		SetRequesterID(f.actor.ID).SetTenantID(f.tenant.ID).SetParentTicketID(parent.ID).
		SetAssigneeID(f.actor.ID).SetAssigneeMembershipID(f.membership.ID).SaveX(f.ctx)
	r := f.routes()
	path := fmt.Sprintf("/api/v1/tickets/%d/subtasks/%d", parent.ID, child.ID)
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"title":"child title","version":%d}`, child.Version), http.StatusOK)
	stored := f.client.Ticket.GetX(f.ctx, child.ID)
	require.Equal(t, f.membership.ID, stored.AssigneeMembershipID)
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"assigneeId":0,"version":%d}`, stored.Version), http.StatusOK)
	stored = f.client.Ticket.GetX(f.ctx, child.ID)
	require.Zero(t, stored.AssigneeID)
	require.Zero(t, stored.AssigneeMembershipID)
	foreign := f.account("foreign-target", f.foreign.ID)
	ticketIdentityRequest(t, r, http.MethodPatch, path,
		fmt.Sprintf(`{"assigneeId":%d,"version":%d}`, foreign.ID, stored.Version), http.StatusBadRequest)
	require.Equal(t, stored.Version, f.client.Ticket.GetX(f.ctx, child.ID).Version)
}

func TestHandlerSubtaskMembershipIdentity(t *testing.T) {
	for _, state := range []string{"active", "provider", "disabled-assignee", "foreign-assignee", "revoked-requester", "revoked-creator", "foreign-parent", "outbox-failure"} {
		t.Run(state, func(t *testing.T) {
			f := newTicketIdentityFixture(t)
			requester := f.actor
			requesterMembership := f.membership
			if state == "provider" || state == "revoked-requester" || state == "revoked-creator" {
				// Provider creator and customer requester are distinct identities.
				f.actor = f.actor.Update().SetTenantID(f.foreign.ID).SaveX(f.ctx)
				f.membership = f.membership.Update().SetAccountKind(usertenantmembership.AccountKindProvider).
					SetSource(usertenantmembership.SourceAllocation).SaveX(f.ctx)
				requester = f.account("requester", f.tenant.ID)
				requesterMembership = f.member(requester.ID)
			}
			assignee := f.account("assignee", f.tenant.ID)
			assigneeMembership := f.member(assignee.ID)
			parentTenant := f.tenant.ID
			wantStatus := http.StatusOK
			switch state {
			case "disabled-assignee":
				assignee.Update().SetActive(false).ExecX(f.ctx)
				wantStatus = http.StatusBadRequest
			case "foreign-assignee":
				assignee = f.account("foreign-assignee", f.foreign.ID)
				wantStatus = http.StatusBadRequest
			case "revoked-requester":
				requesterMembership.Update().SetDeletedAt(time.Now()).ExecX(f.ctx)
				wantStatus = http.StatusBadRequest
			case "revoked-creator":
				f.membership.Update().SetDeletedAt(time.Now()).ExecX(f.ctx)
				wantStatus = http.StatusForbidden
			case "foreign-parent":
				parentTenant = f.foreign.ID
				wantStatus = http.StatusNotFound
			case "outbox-failure":
				f.client.OperationalCommand.Use(func(next ent.Mutator) ent.Mutator {
					return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
						return nil, fmt.Errorf("outbox unavailable")
					})
				})
				wantStatus = http.StatusInternalServerError
			}
			parent := f.client.Ticket.Create().SetTitle("parent").SetTicketNumber("SUBTASK-PARENT").
				SetRequesterID(requester.ID).SetTenantID(parentTenant).SaveX(f.ctx)
			response := ticketIdentityRequest(t, f.routes(), http.MethodPost,
				fmt.Sprintf("/api/v1/tickets/%d/subtasks", parent.ID),
				fmt.Sprintf(`{"title":"child","assigneeId":%d,"requesterId":999999,"createdByMembershipId":999999}`, assignee.ID), wantStatus)
			children := f.client.Ticket.Query().Where(entticket.ParentTicketIDEQ(parent.ID)).AllX(f.ctx)
			if wantStatus != http.StatusOK {
				require.Empty(t, children, "failed identity checks must not persist a child")
				require.Zero(t, f.client.OperationalCommand.Query().CountX(f.ctx))
				return
			}
			require.Len(t, children, 1)
			child := children[0]
			require.Equal(t, f.tenant.ID, child.TenantID)
			require.Equal(t, requester.ID, child.RequesterID)
			require.Equal(t, requesterMembership.ID, child.RequesterMembershipID)
			require.Equal(t, f.membership.ID, child.CreatedByMembershipID)
			require.Equal(t, assignee.ID, child.AssigneeID)
			require.Equal(t, assigneeMembership.ID, child.AssigneeMembershipID)
			require.Equal(t, parent.ID, child.ParentTicketID)
			require.Equal(t, 3, f.client.OperationalCommand.Query().CountX(f.ctx), "subtasks must use the normal create outbox transaction")
			data := response["data"].(map[string]interface{})
			require.Equal(t, float64(parent.ID), data["parentTicketId"])
			require.Equal(t, float64(requester.ID), data["requesterId"])
			require.Equal(t, float64(requesterMembership.ID), data["requesterMembershipId"])
		})
	}
}
