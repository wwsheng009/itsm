package ai_test

import (
	"bytes"
	"database/sql/driver"
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

	"itsm-backend/common"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// O0-02 埋点接入/聚合 handler 测试（sqlmock；不依赖真实数据库）。

func newTelemetryTestRouter(t *testing.T, telemetry *service.AITelemetryService, withTenant bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	h := ai.NewHandler(svc)

	r := gin.New()
	r.POST("/api/v1/ai/telemetry/events", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
			c.Set("user_id", 9)
			c.Set("role", "agent")
		}
		h.IngestTelemetryEvents(c)
	})
	r.GET("/api/v1/ai/telemetry/summary", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
		}
		h.GetTelemetrySummary(c)
	})
	return r
}

func newTelemetryMockService(t *testing.T) (*service.AITelemetryService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return service.NewAITelemetryService(db), mock
}

func anyArgs(n int) []driver.Value {
	args := make([]driver.Value, n)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	return args
}

func doJSON(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var payload []byte
	switch v := body.(type) {
	case nil:
		payload = nil
	case string:
		payload = []byte(v)
	default:
		payload, _ = json.Marshal(v)
	}
	req := httptest.NewRequest(method, path, bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestIngestTelemetryEvents_MissingTenant(t *testing.T) {
	r := newTelemetryTestRouter(t, service.NewAITelemetryService(nil), false)
	w := doJSON(r, http.MethodPost, "/api/v1/ai/telemetry/events", map[string]interface{}{
		"events": []map[string]interface{}{{"name": "assistant_opened"}},
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	resp := decodeResponse(t, w)
	assert.Equal(t, float64(common.AuthFailedCode), resp["code"])
}

func TestIngestTelemetryEvents_ServiceUnavailable(t *testing.T) {
	r := newTelemetryTestRouter(t, service.NewAITelemetryService(nil), true)
	w := doJSON(r, http.MethodPost, "/api/v1/ai/telemetry/events", map[string]interface{}{
		"events": []map[string]interface{}{{"name": "assistant_opened"}},
	})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	resp := decodeResponse(t, w)
	assert.Equal(t, float64(common.ServiceUnavailableCode), resp["code"])
}

func TestIngestTelemetryEvents_InvalidBody(t *testing.T) {
	svc, _ := newTelemetryMockService(t)
	r := newTelemetryTestRouter(t, svc, true)
	w := doJSON(r, http.MethodPost, "/api/v1/ai/telemetry/events", "{not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	resp := decodeResponse(t, w)
	assert.Equal(t, float64(common.ParamErrorCode), resp["code"])
}

func TestIngestTelemetryEvents_BatchTooLarge(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	r := newTelemetryTestRouter(t, svc, true)

	events := make([]map[string]interface{}, service.AssistantTelemetryMaxBatchSize+1)
	for i := range events {
		events[i] = map[string]interface{}{"name": "message_sent"}
	}
	w := doJSON(r, http.MethodPost, "/api/v1/ai/telemetry/events", map[string]interface{}{"events": events})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// 超限必须在触库前拒绝
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIngestTelemetryEvents_AcceptsAndDrops(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	r := newTelemetryTestRouter(t, svc, true)

	// 2 条白名单事件 + 1 条未知事件：未知静默丢弃并计数（不阻断整批）。
	// 28 个参数（2 条事件 × 14 列）；role 位于每条事件第 8 列（索引 7/21），来自服务端上下文。
	execArgs := anyArgs(28)
	execArgs[7] = "agent"
	execArgs[21] = "agent"
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO ai_telemetry_events")).
		WithArgs(execArgs...).
		WillReturnResult(sqlmock.NewResult(0, 2))

	body := map[string]interface{}{
		"events": []map[string]interface{}{
			{
				"name":      "assistant_opened",
				"ts":        float64(1_700_000_000_000),
				"sessionId": "sid-1",
				"pageKey":   "ticket.detail",
				"route":     "/tickets/1",
				"source":    "fab",
				"extra":     map[string]interface{}{"query": "数据库连不上"},
			},
			{
				"name":           "message_sent",
				"sessionId":      "sid-1",
				"conversationId": float64(42),
				"runId":          "run-7",
				"latencyMs":      float64(180),
			},
			{"name": "unknown_event"},
		},
	}
	w := doJSON(r, http.MethodPost, "/api/v1/ai/telemetry/events", body)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), data["accepted"])
	assert.Equal(t, float64(1), data["dropped"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetTelemetrySummary_Success(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	r := newTelemetryTestRouter(t, svc, true)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"count", "sessions", "errors"}).AddRow(10, 4, 2))
	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY name")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"name", "count"}).AddRow("assistant_opened", 6))
	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "count"}).AddRow("ticket.detail", 5))
	mock.ExpectQuery(regexp.QuoteMeta("percentile_cont(0.95)")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"avg", "p95", "max"}).AddRow(120.5, 250.0, 800))

	w := doJSON(r, http.MethodGet, "/api/v1/ai/telemetry/summary?days=30", nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(10), data["totalEvents"])
	assert.Equal(t, float64(4), data["distinctSessions"])
	assert.Equal(t, float64(2), data["errorEvents"])
	assert.Equal(t, float64(30), data["days"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetTelemetrySummary_ServiceUnavailable(t *testing.T) {
	r := newTelemetryTestRouter(t, service.NewAITelemetryService(nil), true)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/telemetry/summary", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	resp := decodeResponse(t, w)
	assert.Equal(t, float64(common.ServiceUnavailableCode), resp["code"])
}

func TestGetTelemetrySummary_AppliesFilters(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	r := newTelemetryTestRouter(t, svc, true)

	expect := func(match string) *sqlmock.ExpectedQuery {
		return mock.ExpectQuery(regexp.QuoteMeta(match)).
			WithArgs(int64(1), int64(30), "ticket.detail", "agent")
	}
	expect("SELECT COUNT(*)").
		WillReturnRows(sqlmock.NewRows([]string{"count", "sessions", "errors"}).AddRow(5, 2, 1))
	expect("GROUP BY name").
		WillReturnRows(sqlmock.NewRows([]string{"name", "count"}).AddRow("assistant_opened", 5))
	expect("GROUP BY page_key").
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "count"}).AddRow("ticket.detail", 5))
	expect("percentile_cont(0.95)").
		WillReturnRows(sqlmock.NewRows([]string{"avg", "p95", "max"}).AddRow(9.0, 12.0, 15))

	w := doJSON(r, http.MethodGet, "/api/v1/ai/telemetry/summary?days=30&pageKey=ticket.detail&role=agent", nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := decodeResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), data["totalEvents"])
	assert.NoError(t, mock.ExpectationsWereMet())
}
