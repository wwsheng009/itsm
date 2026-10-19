package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/service"
)

// P2 文档命中率/解决率运营报表端点：遥测聚合 + 账本覆盖率；埋点不可用时降级为仅覆盖率（200）。

func newDocMetricsRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(svc)
	r := gin.New()
	r.GET("/api/v1/ai/observability/doc-metrics", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		h.GetDocMetrics(c)
	})
	return r
}

type docMetricsPayload struct {
	Code int `json:"code"`
	Data struct {
		Days   int                `json:"days"`
		Totals map[string]int     `json:"totals"`
		Rates  map[string]float64 `json:"rates"`
		ByPage []struct {
			PageKey   string `json:"pageKey"`
			Opened    int    `json:"opened"`
			Gaps      int    `json:"gaps"`
			Drafts    int    `json:"drafts"`
			Published int    `json:"published"`
			Helpful   int    `json:"helpful"`
			Unhelpful int    `json:"unhelpful"`
		} `json:"byPage"`
		Trend []struct {
			Day    string `json:"day"`
			Opened int    `json:"opened"`
			Gaps   int    `json:"gaps"`
		} `json:"trend"`
		Coverage struct {
			Coverage float64        `json:"coverage"`
			Totals   map[string]int `json:"totals"`
		} `json:"coverage"`
		Telemetry map[string]interface{} `json:"telemetry"`
	} `json:"data"`
}

func doDocMetrics(t *testing.T, r *gin.Engine) docMetricsPayload {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/observability/doc-metrics", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp docMetricsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func newDocMetricsTelemetry(t *testing.T) (*service.AITelemetryService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return service.NewAITelemetryService(db), mock
}

func TestGetDocMetrics_EndpointWithTelemetry(t *testing.T) {
	telemetry, mock := newDocMetricsTelemetry(t)
	mock.ExpectQuery(regexp.QuoteMeta("COUNT(CASE WHEN name = 'help_opened'")).
		WithArgs(int64(7), int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{
			"opened", "gaps", "drafts", "published", "helpful", "unhelpful",
			"handoffs", "opened_sessions", "gap_sessions", "handoff_sessions", "events",
		}).AddRow(10, 2, 3, 2, 3, 1, 1, 8, 1, 1, 17))
	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
		WithArgs(int64(7), int64(30), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}).
			AddRow("ticket.list", 4, 2, 2, 1, 2, 0))
	mock.ExpectQuery(regexp.QuoteMeta("date_trunc")).
		WithArgs(int64(7), int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"day", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}).
			AddRow("2026-10-18", 4, 1, 2, 2, 1, 1))

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	resp := doDocMetrics(t, newDocMetricsRouter(svc))

	assert.Equal(t, 30, resp.Data.Days)
	assert.Equal(t, 10, resp.Data.Totals["opened"])
	assert.Equal(t, 2, resp.Data.Totals["gaps"])
	assert.Equal(t, 3, resp.Data.Totals["drafts"])
	assert.Equal(t, 2, resp.Data.Totals["published"])
	assert.InDelta(t, 0.875, resp.Data.Rates["hitRate"], 0.0001)
	assert.InDelta(t, 0.875, resp.Data.Rates["resolveRate"], 0.0001)
	assert.InDelta(t, 0.75, resp.Data.Rates["feedbackResolveRate"], 0.0001)
	assert.InDelta(t, 2.0/3.0, resp.Data.Rates["draftPublishRate"], 0.0001)
	require.Len(t, resp.Data.ByPage, 1)
	assert.Equal(t, "ticket.list", resp.Data.ByPage[0].PageKey)
	assert.Equal(t, 2, resp.Data.ByPage[0].Gaps)
	assert.Equal(t, 1, resp.Data.ByPage[0].Published)
	require.Len(t, resp.Data.Trend, 1)
	assert.Equal(t, "2026-10-18", resp.Data.Trend[0].Day)
	assert.Equal(t, true, resp.Data.Telemetry["available"])
	// 账本覆盖率与 /ai/help/coverage 同源：非空、比例在 [0,1]。
	assert.GreaterOrEqual(t, resp.Data.Coverage.Coverage, float64(0))
	assert.LessOrEqual(t, resp.Data.Coverage.Coverage, float64(1))
	assert.Greater(t, resp.Data.Coverage.Totals["pages"], 0)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDocMetrics_DegradesWithoutTelemetry(t *testing.T) {
	// 埋点服务未装配（nil）→ 200 + telemetry.available=false；覆盖率照常渲染（不 5xx）。
	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	resp := doDocMetrics(t, newDocMetricsRouter(svc))

	assert.Equal(t, false, resp.Data.Telemetry["available"])
	assert.Equal(t, 0, resp.Data.Totals["opened"])
	// 无分母 → rates 里不出现该键（前端渲染「—」，不虚报 0%）。
	_, hasHitRate := resp.Data.Rates["hitRate"]
	assert.False(t, hasHitRate)
	assert.Empty(t, resp.Data.ByPage)
	assert.Greater(t, resp.Data.Coverage.Totals["pages"], 0)
}

func TestGetDocMetrics_TenantMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewHandler(svc)
	r := gin.New()
	r.GET("/api/v1/ai/observability/doc-metrics", h.GetDocMetrics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/observability/doc-metrics", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, 0, resp.Code)
}
