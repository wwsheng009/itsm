package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/handlers/ai"
)

// P2 LLM 动态建议端点契约：未知页面降级 / 生成与缓存 / 未装配 503 / 参数错误。

type stubDynamicGenerator struct {
	calls  int
	output string
}

func (s *stubDynamicGenerator) Generate(_ context.Context, _ ai.DynamicSuggestionRequest) (string, string, error) {
	s.calls++
	return "gpt-4o", s.output, nil
}

type stubDynamicAudit struct{}

func (stubDynamicAudit) Record(_ context.Context, _ ai.DynamicSuggestionAuditEntry) error { return nil }

const stubDynamicOutput = `[{"kind":"task","label":"汇总待处理","prompt":"汇总我当前待处理的工单"}]`

func newDynamicSuggestionsRouter(t *testing.T, dynamic *ai.DynamicSuggestionService, withTenant bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	domain := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dynamic != nil {
		domain.SetDynamicSuggestions(dynamic)
	}
	h := ai.NewHandler(domain)

	r := gin.New()
	r.GET("/api/v1/ai/assistant/suggestions/dynamic", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 7)
			c.Set("user_id", 42)
			c.Set("role", "agent")
		}
		h.GetDynamicSuggestions(c)
	})
	return r
}

type dynamicSuggestionsResponse struct {
	Code int `json:"code"`
	Data struct {
		PageKey     string `json:"pageKey"`
		Known       bool   `json:"known"`
		Generated   bool   `json:"generated"`
		Cached      bool   `json:"cached"`
		Model       string `json:"model"`
		Reason      string `json:"reason"`
		Suggestions []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Label    string `json:"label"`
			Prompt   string `json:"prompt"`
			ReadOnly bool   `json:"readOnly"`
			Source   string `json:"source"`
		} `json:"suggestions"`
	} `json:"data"`
}

func TestGetDynamicSuggestions_GeneratesAndCaches(t *testing.T) {
	generator := &stubDynamicGenerator{output: stubDynamicOutput}
	svc := ai.NewDynamicSuggestionService(generator, stubDynamicAudit{}, nil)
	r := newDynamicSuggestionsRouter(t, svc, true)

	w := doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic?pageKey=ticket.list&route=/tickets", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body dynamicSuggestionsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.True(t, body.Data.Known)
	assert.True(t, body.Data.Generated)
	assert.False(t, body.Data.Cached)
	assert.Equal(t, "gpt-4o", body.Data.Model)
	require.Len(t, body.Data.Suggestions, 1)
	assert.Equal(t, "task", body.Data.Suggestions[0].Kind)
	assert.True(t, body.Data.Suggestions[0].ReadOnly)
	assert.Equal(t, "llm", body.Data.Suggestions[0].Source)

	// 缓存命中：第二次同参数请求 cached=true、生成器不再调用。
	w = doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic?pageKey=ticket.list&route=/tickets", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, body.Data.Cached)
	assert.True(t, body.Data.Generated)
	assert.Equal(t, 1, generator.calls, "缓存命中不重复调用 LLM")
}

func TestGetDynamicSuggestions_DegradesAndValidates(t *testing.T) {
	generator := &stubDynamicGenerator{output: stubDynamicOutput}
	svc := ai.NewDynamicSuggestionService(generator, stubDynamicAudit{}, nil)

	// 未知页面 → known=false（前端保持静态基线；不触发 LLM）。
	r := newDynamicSuggestionsRouter(t, svc, true)
	w := doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic?pageKey=unknown.page", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body dynamicSuggestionsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Data.Known)
	assert.False(t, body.Data.Generated)
	assert.Equal(t, "unknown_page", body.Data.Reason)
	assert.Empty(t, body.Data.Suggestions)
	assert.Zero(t, generator.calls)

	// 缺 pageKey → 400。
	w = doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 缺身份上下文 → 401。
	r = newDynamicSuggestionsRouter(t, svc, false)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic?pageKey=ticket.list", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 服务未装配（LLM 未启用）→ 503（前端静默回退静态基线）。
	r = newDynamicSuggestionsRouter(t, nil, true)
	w = doJSON(r, http.MethodGet, "/api/v1/ai/assistant/suggestions/dynamic?pageKey=ticket.list", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
