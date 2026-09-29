package ai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	botpkg "itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// B4-02：`GET /api/v1/ai/bot-metrics` 的装配态契约。
//
//   - 未装配（bot.enabled=false）：503 + 明确文案（前端据此隐藏看板）；
//   - 已装配：200 + 运行维度载荷（空库零安全，tokensRecorded=false）。
func TestGetBotMetrics_DisabledAndEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := ai.NewHandler(svc)

	router := func(tenantID int) *gin.Engine {
		r := gin.New()
		r.Use(gin.Recovery())
		r.GET("/api/v1/ai/bot-metrics", func(c *gin.Context) {
			c.Set("tenant_id", tenantID)
			h.GetBotMetrics(c)
		})
		return r
	}

	t.Run("未装配返回 503", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/bot-metrics", nil)
		router(1).ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp["message"], "未启用")
	})

	t.Run("已装配返回运行维度载荷", func(t *testing.T) {
		client := enttest.Open(t, "sqlite3", "file:ai-bot-metrics?mode=memory&cache=shared&_fk=1")
		t.Cleanup(func() { _ = client.Close() })
		tenant := client.Tenant.Create().SetCode("bm-1").SetName("指标").SaveX(t.Context())
		svc.SetBotMetrics(botpkg.NewMetricsService(client))

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/bot-metrics?days=14", nil)
		router(tenant.ID).ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Code int `json:"code"`
			Data struct {
				WindowDays     int  `json:"windowDays"`
				TokensRecorded bool `json:"tokensRecorded"`
				Runs           struct {
					Total int `json:"total"`
				} `json:"runs"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 0, resp.Code)
		assert.Equal(t, 14, resp.Data.WindowDays)
		assert.False(t, resp.Data.TokensRecorded)
		assert.Zero(t, resp.Data.Runs.Total)
	})
}
