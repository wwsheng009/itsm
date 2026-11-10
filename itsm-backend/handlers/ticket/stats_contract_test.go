package ticket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
	ticketrepo "itsm-backend/repository/ticket"
)

func TestTicketStatsCountsOverdueWithinTenant(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ticket_stats_contract?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	for _, tid := range []int{1, 2} {
		suffix := strconv.Itoa(tid)
		tenant := client.Tenant.Create().SetName(suffix).SetCode(suffix).SetDomain(suffix + ".test").SaveX(ctx)
		user := client.User.Create().SetUsername(suffix).SetEmail(suffix + "@example.com").SetName("test").SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)
		for _, status := range []string{"open", "resolved", "closed", "cancelled"} {
			client.Ticket.Create().SetTitle(status).SetTicketNumber(suffix + status).SetStatus(status).SetRequesterID(user.ID).SetTenantID(tenant.ID).SetSLAResolutionDeadline(time.Now().Add(-time.Hour)).SaveX(ctx)
		}
	}
	h := NewHandler(NewService(NewEntRepository(ticketrepo.NewEntRepository(client, zap.NewNop().Sugar())), nil, zap.NewNop().Sugar()))
	r := gin.New()
	r.GET("/api/v1/tickets/stats", func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 1)
		c.Set("role", "admin")
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: 1})
		h.GetTicketStats(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tickets/stats", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	var response struct {
		Code int `json:"code"`
		Data struct {
			Overdue int `json:"overdue"`
			Total   int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Zero(t, response.Code)
	require.Equal(t, 1, response.Data.Overdue)
	require.Equal(t, 4, response.Data.Total)
}

// TestTicketStatsScopedForEndUser 锁定统计与列表同 DataScope 口径：
// end_user 只统计本人创建/被指派的工单；管理角色统计全租户。
func TestTicketStatsScopedForEndUser(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ticket_stats_scoped?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("scoped").SetCode("scoped").SetDomain("scoped.test").SaveX(ctx)
	owner := client.User.Create().SetUsername("owner").SetEmail("owner@example.com").SetName("owner").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)
	other := client.User.Create().SetUsername("other").SetEmail("other@example.com").SetName("other").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	createTicket := func(number, status string, requesterID int) {
		client.Ticket.Create().
			SetTitle(number).
			SetTicketNumber(number).
			SetStatus(status).
			SetRequesterID(requesterID).
			SetTenantID(tenant.ID).
			SetSLAResolutionDeadline(time.Now().Add(-time.Hour)).
			SaveX(ctx)
	}
	createTicket("owner-open", "open", owner.ID)
	createTicket("owner-resolved", "resolved", owner.ID)
	createTicket("other-open", "open", other.ID)

	h := NewHandler(NewService(NewEntRepository(ticketrepo.NewEntRepository(client, zap.NewNop().Sugar())), nil, zap.NewNop().Sugar()))

	statsAs := func(role string, userID int) (total, overdue int) {
		r := gin.New()
		r.GET("/api/v1/tickets/stats", func(c *gin.Context) {
			c.Set("tenant_id", tenant.ID)
			c.Set("user_id", userID)
			c.Set("role", role)
			c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenant.ID})
			h.GetTicketStats(c)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tickets/stats", nil))
		require.Equal(t, 200, w.Code, w.Body.String())
		var response struct {
			Code int `json:"code"`
			Data struct {
				Overdue int `json:"overdue"`
				Total   int `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Zero(t, response.Code)
		return response.Data.Total, response.Data.Overdue
	}

	// end_user 仅本人：2 条（1 open + 1 resolved），overdue 仅 open=1。
	total, overdue := statsAs("end_user", owner.ID)
	require.Equal(t, 2, total)
	require.Equal(t, 1, overdue)

	// admin 全租户：3 条，overdue 2 条 open。
	total, overdue = statsAs("admin", owner.ID)
	require.Equal(t, 3, total)
	require.Equal(t, 2, overdue)
}
