package ai_test

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// V0-04 试点对照报告端点契约：200（gate 判定 + 窗口 clamp）、401、503。

func newPilotTestRouter(t *testing.T, telemetry *service.AITelemetryService, withTenant bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	h := ai.NewHandler(svc)

	r := gin.New()
	r.GET("/api/v1/ai/observability/pilot-report", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
			c.Set("user_id", 9)
			c.Set("role", "agent")
		}
		h.GetPilotReport(c)
	})
	return r
}

var pilotCounterColumns = []string{
	"inline_preview", "inline_applied", "inline_undone",
	"plan_preview", "plan_applied", "plan_completed", "plan_p95",
	"self_answered", "self_resolved", "self_ticket_created",
	"field_corrected", "ai_create_completed", "ai_create_p95",
	"suggestion_shown", "suggestion_ignored",
	"proactive_shown", "proactive_dismissed",
	"error_shown", "error_recovered", "task_started", "task_failed",
}

func pilotRow(values []driver.Value) *sqlmock.Rows {
	return sqlmock.NewRows(pilotCounterColumns).AddRow(values...)
}

func expectPilotCounters(mock sqlmock.Sqlmock, values []driver.Value) {
	mock.ExpectQuery(regexp.QuoteMeta("-- ai_pilot_counters")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(pilotRow(values))
}

func TestGetPilotReport_SuccessAndClamps(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	// 本期（改善）→ 基线（对照）。
	expectPilotCounters(mock, []driver.Value{
		100, 80, 8, 20, 10, 20, 2000.0,
		100, 60, 35, 2, 40, 12000.0,
		100, 20, 50, 5, 10, 9, 100, 5,
	})
	expectPilotCounters(mock, []driver.Value{
		100, 50, 10, 20, 5, 20, 4000.0,
		100, 30, 50, 10, 40, 30000.0,
		100, 30, 50, 10, 10, 9, 100, 10,
	})

	r := newPilotTestRouter(t, svc, true)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/observability/pilot-report?currentDays=999&baselineDays=7", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Current struct {
				Days int `json:"days"`
			} `json:"current"`
			Baseline struct {
				Days int `json:"days"`
			} `json:"baseline"`
			Sections []struct {
				Key string `json:"key"`
				Met bool   `json:"met"`
			} `json:"sections"`
			Gate struct {
				Passed    bool     `json:"passed"`
				PilotsMet int      `json:"pilotsMet"`
				Required  int      `json:"required"`
				Reasons   []string `json:"reasons"`
			} `json:"gate"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, service.PilotReportMaxDays, resp.Data.Current.Days, "窗口上限 clamp")
	assert.Equal(t, 7, resp.Data.Baseline.Days)
	require.Len(t, resp.Data.Sections, 3)
	assert.True(t, resp.Data.Gate.Passed)
	assert.Equal(t, 3, resp.Data.Gate.PilotsMet)
	assert.Equal(t, 2, resp.Data.Gate.Required, "§11.5：门槛要求 ≥2 项试点达标")
	assert.NotEmpty(t, resp.Data.Gate.Reasons)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetPilotReport_MissingTenantAndUnavailable(t *testing.T) {
	// 缺租户上下文 → 401。
	r := newPilotTestRouter(t, service.NewAITelemetryService(nil), false)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/observability/pilot-report", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 埋点未装配 → 503。
	r = newPilotTestRouter(t, service.NewAITelemetryService(nil), true)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/observability/pilot-report", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
