package ticket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
	ticketrepo "itsm-backend/repository/ticket"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// P1-3 接线：ticket Pause/Resume HTTP 面。
// 未装配 → 503 fail-closed（禁止假成功）；装配后真实计时 + 恢复追加 deadline。
func TestTicketHandler_PauseResumeSLA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:tkt_pause_resume?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	tenant := client.Tenant.Create().
		SetName("TktPauseTenant").SetCode("tkt-pause").SetDomain("tktpause.test").SaveX(ctx)
	user := client.User.Create().
		SetUsername("tkt-pause-user").SetName("u").SetEmail("u@tktpause.test").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	tkt := client.Ticket.Create().
		SetTitle("Pause Ticket").
		SetDescription("d").
		SetPriority("high").
		SetType("incident").
		SetStatus("open").
		SetTicketNumber("TKT-PR-HTTP-1").
		SetTenantID(tenant.ID).
		SetRequesterID(user.ID).
		SetSLAResponseDeadline(time.Now().Add(2 * time.Hour)).
		SetSLAResolutionDeadline(time.Now().Add(8 * time.Hour)).
		SaveX(ctx)

	h := NewHandler(NewService(NewEntRepository(ticketrepo.NewEntRepository(client, zap.NewNop().Sugar())), nil, logger))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", user.ID)
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenant.ID})
	})
	r.PUT("/api/v1/tickets/:id/sla/pause", h.PauseSLA)
	r.PUT("/api/v1/tickets/:id/sla/resume", h.ResumeSLA)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	pausePath := "/api/v1/tickets/" + strconv.Itoa(tkt.ID) + "/sla/pause"
	resumePath := "/api/v1/tickets/" + strconv.Itoa(tkt.ID) + "/sla/resume"

	// 未装配 → 503 fail-closed，数据不变。
	w := do(http.MethodPut, pausePath, `{"reason":"未装配测试"}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.NotEqual(t, "paused", client.Ticket.GetX(ctx, tkt.ID).SLAStatus)

	// 装配后 → 真实暂停。
	h.SetSLAMonitorService(service.NewSLAMonitorService(client, logger))
	w = do(http.MethodPut, pausePath, `{"reason":"客户侧停电"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := client.Ticket.GetX(ctx, tkt.ID)
	assert.Equal(t, "paused", got.SLAStatus)
	assert.Equal(t, "客户侧停电", got.SLAPauseReason)

	// 篡改 paused_at 到 10 分钟前；恢复 → deadline 追加 ~10 分钟。
	past := time.Now().Add(-10 * time.Minute)
	client.Ticket.UpdateOneID(tkt.ID).SetSLAPausedAt(past).ExecX(ctx)
	before := client.Ticket.GetX(ctx, tkt.ID)
	w = do(http.MethodPut, resumePath, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	after := client.Ticket.GetX(ctx, tkt.ID)
	assert.Equal(t, "active", after.SLAStatus)
	delta := after.SLAResolutionDeadline.Sub(before.SLAResolutionDeadline)
	assert.GreaterOrEqual(t, delta, 9*time.Minute)
	assert.LessOrEqual(t, delta, 11*time.Minute)
}
