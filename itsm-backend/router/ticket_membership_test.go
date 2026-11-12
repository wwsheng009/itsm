package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/handlers/ai"
	tickethandler "itsm-backend/handlers/ticket"
	workflowhandler "itsm-backend/handlers/ticket_workflow"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTicketCCRoutesMembershipLifecycle(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket_cc_route_membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	oldMode, oldFallback := middleware.PermissionConfig.Mode, middleware.AuthzStaticFallbackEnabled()
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeDBOnly
	middleware.SetAuthzStaticFallback(false)
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = oldMode
		middleware.SetAuthzStaticFallback(oldFallback)
		middleware.InvalidateAllPermissionCaches()
	})

	home := client.Tenant.Create().SetName("Provider").SetCode("cc-provider").SaveX(ctx)
	customer := client.Tenant.Create().SetName("Customer").SetCode("cc-customer").SaveX(ctx)
	foreign := client.Tenant.Create().SetName("Other").SetCode("cc-other").SaveX(ctx)
	newUser := func(name string, tenantID int) *ent.User {
		return client.User.Create().SetUsername(name).SetName(name).
			SetEmail(name + "@example.test").SetPasswordHash("not-a-password").
			SetRole("end_user").SetTenantID(tenantID).SetActive(true).SaveX(ctx)
	}
	actor := newUser("cc-provider-actor", home.ID)
	requester := newUser("cc-customer-requester", customer.ID)
	target := newUser("cc-customer-recipient", customer.ID)
	role := client.Role.Create().SetCode("msp_tech").SetName("Customer operator").SetTenantID(customer.ID).SaveX(ctx)
	for _, action := range []string{"read", "update"} {
		permission := client.Permission.Create().SetCode("ticket:" + action).
			SetName(action).SetResource("ticket").SetAction(action).SetTenantID(customer.ID).SaveX(ctx)
		client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(permission.ID).
			SetTenantID(customer.ID).SaveX(ctx)
	}
	// No workflow:* permission: the personal CC list is a ticket collaboration
	// route, not a workflow administration endpoint.
	membership := client.UserTenantMembership.Create().
		SetUserID(actor.ID).SetTenantID(customer.ID).SetRoleID(role.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).SaveX(ctx)
	ticket := client.Ticket.Create().SetTitle("Customer ticket").SetTicketNumber("CC-ROUTE-1").
		SetRequesterID(requester.ID).SetAssigneeID(actor.ID).SetAssigneeMembershipID(membership.ID).
		SetTenantID(customer.ID).SaveX(ctx)
	selfCC := client.TicketCC.Create().SetTicketID(ticket.ID).SetUserID(actor.ID).
		SetUserMembershipID(membership.ID).SetAddedBy(requester.ID).SetTenantID(customer.ID).SetIsActive(true).SaveX(ctx)
	otherCC := client.TicketCC.Create().SetTicketID(ticket.ID).SetUserID(target.ID).
		SetAddedBy(requester.ID).SetTenantID(customer.ID).SetIsActive(true).SaveX(ctx)

	logger := zap.NewNop().Sugar()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	const secret = "ticket-cc-membership-route-test"
	group := r.Group("/api/v1", middleware.AuthMiddleware(secret), middleware.RBACMiddleware(client))
	// Register the production routes and real Handler -> Service -> Ent chain.
	SetupTicketRoutes(group, &RouterConfig{
		TicketHandler:         tickethandler.NewHandler(nil),
		AIHandler:             &ai.Handler{},
		TicketWorkflowHandler: workflowhandler.NewHandler(service.NewTicketWorkflowService(client, logger), nil, logger),
	})
	token, err := middleware.GenerateAccessTokenWithSource(actor.ID, actor.Username, "end_user",
		customer.ID, "switch", secret, time.Hour)
	require.NoError(t, err)
	request := func(method, path, accessToken string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	list := request(http.MethodGet, "/api/v1/tickets/cc/my", token)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var response struct {
		Code int                      `json:"code"`
		Data dto.TicketCCListResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &response))
	require.Zero(t, response.Code)
	require.Equal(t, 1, response.Data.Total)
	require.Len(t, response.Data.Records, 1)
	require.Equal(t, selfCC.ID, response.Data.Records[0].ID)
	require.NotContains(t, list.Body.String(), "passwordHash")
	require.NotContains(t, list.Body.String(), "not-a-password")

	foreignToken, err := middleware.GenerateAccessTokenWithSource(actor.ID, actor.Username, "admin",
		foreign.ID, "switch", secret, time.Hour)
	require.NoError(t, err)
	assertDenied := func(w *httptest.ResponseRecorder) {
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		var body struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, common.ForbiddenCode, body.Code)
	}
	assertDenied(request(http.MethodGet, "/api/v1/tickets/cc/my", foreignToken))

	// Reuse the still-unexpired customer token without evicting caches or
	// blacklisting it: current membership must revoke both read and write.
	require.NoError(t, membership.Update().SetDeletedAt(time.Now()).Exec(ctx))
	assertDenied(request(http.MethodGet, "/api/v1/tickets/cc/my", token))
	for _, record := range []*ent.TicketCC{selfCC, otherCC} {
		path := fmt.Sprintf("/api/v1/tickets/%d/cc/%d", ticket.ID, record.ID)
		assertDenied(request(http.MethodDelete, path, token))
		require.True(t, client.TicketCC.GetX(ctx, record.ID).IsActive)
	}
}
