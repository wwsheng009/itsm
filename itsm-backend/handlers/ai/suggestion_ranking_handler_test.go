package ai_test

import (
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

// O2-01 分析增强端点契约：分群对比（200/400/401/503）+ 建议排序权重接线（weighted/registry）。

func newO201Router(t *testing.T, telemetry *service.AITelemetryService, withTenant bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	h := ai.NewHandler(svc)

	r := gin.New()
	applyCtx := func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 7)
			c.Set("user_id", 42)
			c.Set("role", "super_admin")
		}
	}
	r.GET("/api/v1/ai/observability/segments", func(c *gin.Context) {
		applyCtx(c)
		h.GetAssistantSegments(c)
	})
	r.GET("/api/v1/ai/assistant/context", func(c *gin.Context) {
		applyCtx(c)
		h.GetAssistantContext(c)
	})
	return r
}

func segmentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"segment", "events", "sessions",
		"suggestion_shown", "suggestion_clicked",
		"task_started", "task_completed",
		"inline_preview", "inline_applied", "errors",
	}).AddRow("agent", 200, 45, 40, 12, 10, 6, 20, 9, 4)
}

type assistantContextSummary struct {
	Ranking     string `json:"ranking"`
	Suggestions []struct {
		ID       string  `json:"id"`
		Rank     int     `json:"rank"`
		Score    float64 `json:"score"`
		Feedback *struct {
			Shown          int     `json:"shown"`
			Clicked        int     `json:"clicked"`
			Ignored        int     `json:"ignored"`
			DecayedShown   float64 `json:"decayedShown"`
			DecayedClicked float64 `json:"decayedClicked"`
			DecayedIgnored float64 `json:"decayedIgnored"`
		} `json:"feedback"`
	} `json:"suggestions"`
}

func TestGetAssistantSegments_Contract(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	mock.ExpectQuery(regexp.QuoteMeta("-- ai_segment_breakdown")).
		WithArgs(int64(7), int64(service.AssistantSegmentsMaxDays)).
		WillReturnRows(segmentRows())

	r := newO201Router(t, svc, true)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/observability/segments?dimension=role&days=999", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Dimension string `json:"dimension"`
			Days      int    `json:"days"`
			Segments  []struct {
				Key                string  `json:"key"`
				SuggestionCtr      float64 `json:"suggestionCtr"`
				TaskCompletionRate float64 `json:"taskCompletionRate"`
				InlineAdoptionRate float64 `json:"inlineAdoptionRate"`
				ErrorRate          float64 `json:"errorRate"`
			} `json:"segments"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, service.AssistantSegmentDimensionRole, resp.Data.Dimension)
	assert.Equal(t, service.AssistantSegmentsMaxDays, resp.Data.Days, "窗口上限 clamp")
	require.Len(t, resp.Data.Segments, 1)
	assert.Equal(t, "agent", resp.Data.Segments[0].Key)
	assert.InDelta(t, 0.3, resp.Data.Segments[0].SuggestionCtr, 1e-9)
	assert.NoError(t, mock.ExpectationsWereMet())

	// 非法维度 → 400（触库前拒绝）。
	r = newO201Router(t, service.NewAITelemetryService(nil), true)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/observability/segments?dimension=tenant", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// 缺租户 → 401；未装配 → 503。
	r = newO201Router(t, service.NewAITelemetryService(nil), false)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/observability/segments", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	r = newO201Router(t, service.NewAITelemetryService(nil), true)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/observability/segments", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetAssistantContext_WeightedRanking(t *testing.T) {
	svc, mock := newTelemetryMockService(t)
	mock.ExpectQuery(regexp.QuoteMeta("-- ai_suggestion_weights")).
		WithArgs(int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"card_id", "name", "age_days", "count"}).
			AddRow("triage_plan", "suggestion_shown", 0, 10).
			AddRow("triage_plan", "suggestion_ignored", 0, 9).
			AddRow("reply_draft", "suggestion_shown", 0, 10).
			AddRow("reply_draft", "suggestion_clicked", 0, 9))

	r := newO201Router(t, svc, true)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/assistant/context?pageKey=ticket.detail", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Code int                     `json:"code"`
		Data assistantContextSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "weighted", resp.Data.Ranking, "埋点可用 → 规则权重排序")
	require.Len(t, resp.Data.Suggestions, 3)

	// 组内重排：tasks 内 reply_draft（CTR 高）提前；问题卡仍在后（分组契约保持）。
	assert.Equal(t, "reply_draft", resp.Data.Suggestions[0].ID)
	assert.Equal(t, 0, resp.Data.Suggestions[0].Rank)
	require.NotNil(t, resp.Data.Suggestions[0].Feedback)
	assert.Equal(t, 9, resp.Data.Suggestions[0].Feedback.Clicked)
	assert.Equal(t, "triage_plan", resp.Data.Suggestions[1].ID)
	assert.Equal(t, 9, resp.Data.Suggestions[1].Feedback.Ignored, "高忽略卡仍下发（降权不否决）")
	assert.InDelta(t, 8.7799, resp.Data.Suggestions[1].Feedback.DecayedIgnored, 1e-9)
	assert.Equal(t, "kb_lookup", resp.Data.Suggestions[2].ID)
	assert.Greater(t, resp.Data.Suggestions[0].Score, resp.Data.Suggestions[1].Score)
	assert.NoError(t, mock.ExpectationsWereMet())

	// 埋点未装配 → 注册表顺序 + ranking=registry（与既有静态基线一致）。
	r = newO201Router(t, service.NewAITelemetryService(nil), true)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/assistant/context?pageKey=ticket.detail", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// 独立解码（避免复用结构体时 JSON 缺字段残留旧指针）。
	var registryResp struct {
		Code int                     `json:"code"`
		Data assistantContextSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &registryResp))
	assert.Equal(t, "registry", registryResp.Data.Ranking)
	require.Len(t, registryResp.Data.Suggestions, 3)
	assert.Equal(t, "triage_plan", registryResp.Data.Suggestions[0].ID)
	assert.Equal(t, "reply_draft", registryResp.Data.Suggestions[1].ID)
	assert.Nil(t, registryResp.Data.Suggestions[0].Feedback)
}
