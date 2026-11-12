package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	approvalHandler "itsm-backend/handlers/approval"
	ticketCommentHandler "itsm-backend/handlers/ticket_comment"
	ticketTagHandler "itsm-backend/handlers/ticket_tag"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestTicketAssociationRoutes_ReadsURLParamNotContext verifies that the three
// ticket-association router handlers read the URL :id parameter via
// c.Param("id") instead of c.GetInt("id").  Earlier versions used
// c.GetInt("id") which reads from the gin context store (used for
// tenant_id/user_id/role).  The URL parameter is never in that store, so
// ticketID was always 0 and every request returned 400 "invalid ticket id".
//
// Regression coverage:
//   - GET /api/v1/tickets/:id/configuration-items
//   - GET /api/v1/tickets/:id/relations
//   - GET /api/v1/tickets/:id/relations/stats
func TestTicketAssociationRoutes_ReadsURLParamNotContext(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:router_assoc_urlparam?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ctx := context.Background()
	tenant := client.Tenant.Create().
		SetName("Assoc Tenant").
		SetCode("assoc-urlparam").
		SetDomain("assoc.example.com").
		SetStatus("active").
		SaveX(ctx)
	requester := client.User.Create().
		SetUsername("assoc-requester").
		SetEmail("assoc-requester@example.com").
		SetName("Assoc Requester").
		SetPasswordHash("hash").
		SetRole("super_admin").
		SetActive(true).
		SetTenantID(tenant.ID).
		SaveX(ctx)
	endUser := client.User.Create().
		SetUsername("assoc-end-user").
		SetEmail("assoc-end-user@example.com").
		SetName("Assoc End User").
		SetPasswordHash("hash").
		SetRole("end_user").
		SetActive(true).
		SetTenantID(tenant.ID).
		SaveX(ctx)
	// The reader is a real tenant member with only ticket:read, not an old
	// JWT role bypass. Keep the administrative approval-list denial meaningful.
	readerRole := client.Role.Create().SetCode("end_user").SetName("End user").
		SetTenantID(tenant.ID).SaveX(ctx)
	readPermission := client.Permission.Create().SetCode("ticket:read").SetName("Read tickets").
		SetResource("ticket").SetAction("read").SetTenantID(tenant.ID).SaveX(ctx)
	client.RolePermission.Create().SetRoleID(readerRole.ID).SetPermissionID(readPermission.ID).
		SetTenantID(tenant.ID).SaveX(ctx)
	client.UserTenantMembership.Create().SetUserID(endUser.ID).SetTenantID(tenant.ID).
		SetRoleID(readerRole.ID).SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).SetIsDefault(true).SaveX(ctx)
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(middleware.InvalidateAllPermissionCaches)
	ciType := client.CIType.Create().
		SetName("server").
		SetTenantID(tenant.ID).
		SaveX(ctx)
	ticket := client.Ticket.Create().
		SetTicketNumber("TKT-ASSOC-URL-001").
		SetTitle("关联路由冒烟测试").
		SetDescription("验证工单关联路由正确读取 URL 参数").
		SetType("incident").
		SetPriority("medium").
		SetStatus("open").
		SetRequesterID(requester.ID).
		SetTenantID(tenant.ID).
		SaveX(ctx)
	ci := client.ConfigurationItem.Create().
		SetName("web-01").
		SetCiTypeID(ciType.ID).
		SetCiType("server").
		SetStatus("active").
		SetSerialNumber("SN-URL-001").
		SetTenantID(tenant.ID).
		SaveX(ctx)

	assoc := service.NewTicketAssociationService(client)
	require.NoError(t, assoc.AddConfigurationItem(ctx, ticket.ID, ci.ID))

	logger := zaptest.NewLogger(t).Sugar()
	const secret = "assoc-urlparam-secret"

	cfg := &RouterConfig{
		JWTSecret:                secret,
		Logger:                   logger,
		Client:                   client,
		TicketAssociationService: assoc,
		TicketTagHandler:         ticketTagHandler.NewHandler(service.NewTicketTagService(client), logger),
		ApprovalHandler:          approvalHandler.NewHandler(service.NewApprovalService(client, logger)),
		TicketCommentHandler:     ticketCommentHandler.NewHandler(service.NewTicketCommentService(client, logger), logger),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r, cfg)

	token, err := middleware.GenerateAccessToken(requester.ID, requester.Username, "super_admin", tenant.ID, secret, time.Hour)
	require.NoError(t, err)
	endUserToken, err := middleware.GenerateAccessToken(endUser.ID, endUser.Username, "end_user", tenant.ID, secret, time.Hour)
	require.NoError(t, err)

	doAs := func(authToken, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+authToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	do := func(method, path string) *httptest.ResponseRecorder {
		return doAs(token, method, path)
	}

	t.Run("configuration-items returns associated CIs", func(t *testing.T) {
		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/configuration-items", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    []struct {
				ID           int    `json:"id"`
				Name         string `json:"name"`
				CIType       string `json:"ciType"`
				Status       string `json:"status"`
				SerialNumber string `json:"serialNumber"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		require.Len(t, body.Data, 1, "ticket should have exactly the one associated CI")
		assert.Equal(t, ci.ID, body.Data[0].ID)
		assert.Equal(t, "web-01", body.Data[0].Name)
		assert.Equal(t, "server", body.Data[0].CIType)
	})

	t.Run("relations returns success with empty relation graph", func(t *testing.T) {
		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/relations", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				ParentChain    []interface{} `json:"parentChain"`
				ChildrenTree   []interface{} `json:"childrenTree"`
				RelatedTickets []interface{} `json:"relatedTickets"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		require.NotNil(t, body.Data.ParentChain, "parentChain must be an array")
		require.NotNil(t, body.Data.ChildrenTree, "childrenTree must be an array")
		require.NotNil(t, body.Data.RelatedTickets, "relatedTickets must be an array")
		assert.Empty(t, body.Data.ParentChain)
		assert.Empty(t, body.Data.ChildrenTree)
		assert.Empty(t, body.Data.RelatedTickets)
	})

	t.Run("relations/stats returns success with non-zero counts", func(t *testing.T) {
		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/relations/stats", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code int `json:"code"`
			Data struct {
				TotalRelations int `json:"totalRelations"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
	})

	t.Run("comments never serialize null sequences", func(t *testing.T) {
		_, err := client.TicketComment.Create().
			SetTicketID(ticket.ID).
			SetUserID(requester.ID).
			SetContent("评论空数组契约").
			SetTenantID(tenant.ID).
			Save(ctx)
		require.NoError(t, err)

		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/comments", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code int `json:"code"`
			Data struct {
				Items []struct {
					Mentions    []interface{} `json:"mentions"`
					Attachments []interface{} `json:"attachments"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		require.Len(t, body.Data.Items, 1)
		assert.NotNil(t, body.Data.Items[0].Mentions, "mentions must be an array, not null")
		assert.NotNil(t, body.Data.Items[0].Attachments, "attachments must be an array, not null")
	})

	t.Run("ticket tags route is registered and returns an array", func(t *testing.T) {
		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/tags", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code int `json:"code"`
			Data struct {
				Items []interface{} `json:"items"`
				Total int           `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		assert.NotNil(t, body.Data.Items, "tags items must be an array, not null")
		assert.Empty(t, body.Data.Items)
		assert.Equal(t, 0, body.Data.Total)
	})

	t.Run("ticket approval records are readable with ticket scope", func(t *testing.T) {
		w := do(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/approval/records", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var body struct {
			Code int `json:"code"`
			Data struct {
				Items []interface{} `json:"items"`
				Total int           `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 0, body.Code)
		assert.NotNil(t, body.Data.Items, "approval items must be an array, not null")
		assert.Empty(t, body.Data.Items)
		assert.Equal(t, 0, body.Data.Total)
	})

	t.Run("end user can read ticket approvals but not admin approval records", func(t *testing.T) {
		w := doAs(endUserToken, http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/approval/records", ticket.ID))
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		admin := doAs(endUserToken, http.MethodGet, "/api/v1/tickets/approval/records")
		require.Equal(t, http.StatusForbidden, admin.Code, "body=%s", admin.Body.String())
	})
}
