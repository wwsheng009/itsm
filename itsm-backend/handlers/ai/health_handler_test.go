package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// O1-02 健康端点契约：快照 + SLO（GET）、告警进通知中心（POST）、未装配降级（503）。

type aiHealthResponse struct {
	Code int `json:"code"`
	Data struct {
		Snapshot struct {
			WindowDays int `json:"windowDays"`
			Runs       struct {
				Total       int     `json:"total"`
				SuccessRate float64 `json:"successRate"`
			} `json:"runs"`
			Queue struct {
				Available bool `json:"available"`
				Length    int  `json:"length"`
			} `json:"queue"`
		} `json:"snapshot"`
		SLOs []struct {
			Rule   string `json:"rule"`
			Status string `json:"status"`
		} `json:"slos"`
	} `json:"data"`
}

type aiHealthDispatchResponse struct {
	Code int `json:"code"`
	Data struct {
		Created    int `json:"created"`
		Recipients int `json:"recipients"`
		Alerts     []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
		} `json:"alerts"`
	} `json:"data"`
}

func setupAIHealthRouter(t *testing.T, withHealth bool) (*gin.Engine, *ent.Client, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:ai-health-ep-%s-%v?mode=memory&cache=shared&_fk=1", t.Name(), withHealth))
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("health-ep").SetName("健康端点").SetDomain("health.test").SaveX(ctx)
	admin := client.User.Create().
		SetUsername("health-admin").SetEmail("health@example.com").SetName("健康管理员").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetRole(user.RoleAdmin).SaveX(ctx)

	base := time.Now().Add(-2 * time.Hour)
	runIDs := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		status := "completed"
		if i >= 8 {
			status = "failed"
		}
		runIDs = append(runIDs, client.BotRun.Create().
			SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus(status).
			SetStartedAt(base.Add(time.Duration(i)*time.Minute)).
			SetFinishedAt(base.Add(time.Duration(i)*time.Minute+30*time.Second)).
			SaveX(ctx).ID)
	}
	for i := 0; i < 6; i++ {
		status := "success"
		if i >= 4 {
			status = "failed"
		}
		client.ToolInvocation.Create().
			SetTenantID(tenant.ID).SetRunID(runIDs[0]).SetToolName("create_ticket").
			SetStatus(status).SetCreatedAt(base).SaveX(ctx)
	}

	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	if withHealth {
		health := service.NewAIHealthService(client, bot.NewMetricsService(client), nil)
		health.SetQueue(stubQueueStats{length: 2, capacity: 100})
		svc.SetHealthService(health)
	}
	h := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	withCtx := func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", admin.ID)
		c.Set("role", "admin")
	}
	r.GET("/api/v1/ai/health", func(c *gin.Context) { withCtx(c); h.GetAIHealth(c) })
	r.POST("/api/v1/ai/health/alerts/dispatch", func(c *gin.Context) { withCtx(c); h.DispatchAIHealthAlerts(c) })
	return r, client, tenant.ID
}

type stubQueueStats struct{ length, capacity int }

func (s stubQueueStats) Len() int { return s.length }
func (s stubQueueStats) Cap() int { return s.capacity }

func TestGetAIHealth_SnapshotAndSLO(t *testing.T) {
	r, _, _ := setupAIHealthRouter(t, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/health?days=7", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body aiHealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	assert.Equal(t, 7, body.Data.Snapshot.WindowDays)
	assert.Equal(t, 10, body.Data.Snapshot.Runs.Total)
	assert.InDelta(t, 0.8, body.Data.Snapshot.Runs.SuccessRate, 0.001)
	assert.True(t, body.Data.Snapshot.Queue.Available)
	assert.Equal(t, 2, body.Data.Snapshot.Queue.Length)

	byRule := map[string]string{}
	for _, item := range body.Data.SLOs {
		byRule[item.Rule] = item.Status
	}
	assert.Equal(t, "breach", byRule["ai_task_success_rate"])
	assert.Equal(t, "breach", byRule["ai_tool_error_rate"])
	assert.Equal(t, "insufficient_data", byRule["ai_llm_success_rate"])

	// 窗口上限 clamp：days=999 → 30。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/health?days=999", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 30, body.Data.Snapshot.WindowDays)
}

func TestDispatchAIHealthAlerts_NotifiesCenter(t *testing.T) {
	r, client, tenantID := setupAIHealthRouter(t, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/health/alerts/dispatch?days=7", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body aiHealthDispatchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Len(t, body.Data.Alerts, 2)
	assert.Equal(t, 1, body.Data.Recipients)
	assert.Equal(t, 2, body.Data.Created)
	for _, alert := range body.Data.Alerts {
		assert.Equal(t, "critical", alert.Severity)
	}

	rows := client.Notification.Query().Where(notification.TenantID(tenantID)).AllX(context.Background())
	require.Len(t, rows, 2)
	assert.Equal(t, "ai_health", rows[0].Type)
	assert.Equal(t, "/ai/observability", rows[0].ActionURL)
	assert.False(t, rows[0].Read)

	// 重复派发 → 去重（同窗口不重复投递）。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ai/health/alerts/dispatch?days=7", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Data.Created)
}

func TestGetAIHealth_UnavailableWithoutService(t *testing.T) {
	r, _, _ := setupAIHealthRouter(t, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/health", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
