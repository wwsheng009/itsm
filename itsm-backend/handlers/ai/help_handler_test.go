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

	"itsm-backend/common"
	"itsm-backend/handlers/ai"
)

// H0-03 帮助端点契约：租户缺失 fail-closed；空关键词 400；检索/页面帮助正常返回；
// 未知 pageKey 以 status=unknown 正常响应（不 404，面板据此降级）。

func newHelpRouter(withTenant bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := ai.NewHandler(svc)

	r := gin.New()
	r.GET("/api/v1/ai/help/search", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
		}
		h.SearchHelp(c)
	})
	r.GET("/api/v1/ai/help/page", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
		}
		h.GetPageHelp(c)
	})
	r.GET("/api/v1/ai/help/coverage", func(c *gin.Context) {
		if withTenant {
			c.Set("tenant_id", 1)
		}
		h.GetHelpCoverage(c)
	})
	return r
}

func getJSON(r *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHelpEndpoints_MissingTenant(t *testing.T) {
	r := newHelpRouter(false)
	for _, path := range []string{"/api/v1/ai/help/search?q=SLA", "/api/v1/ai/help/page?pageKey=ticket.detail"} {
		w := getJSON(r, path)
		require.Equal(t, http.StatusUnauthorized, w.Code, path)
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, float64(common.AuthFailedCode), resp["code"])
	}
}

func TestHelpSearch_ValidatesQuery(t *testing.T) {
	r := newHelpRouter(true)

	w := getJSON(r, "/api/v1/ai/help/search?q=%20%20")
	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(common.ParamErrorCode), resp["code"])
}

func TestHelpSearch_ReturnsMatches(t *testing.T) {
	r := newHelpRouter(true)

	w := getJSON(r, "/api/v1/ai/help/search?q=SLA&limit=5")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Results []struct {
				Path     string   `json:"path"`
				Title    string   `json:"title"`
				PageKeys []string `json:"pageKeys"`
				Snippet  string   `json:"snippet"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, common.SuccessCode, resp.Code)
	require.NotEmpty(t, resp.Data.Results)

	found := false
	for _, item := range resp.Data.Results {
		if item.Path == "docs/help/sla/monitor.md" {
			found = true
			assert.Contains(t, item.PageKeys, "sla.monitor")
			assert.NotEmpty(t, item.Snippet)
		}
	}
	assert.True(t, found, "SLA 检索应命中 sla/monitor.md：%+v", resp.Data.Results)
}

func TestHelpSearch_PageKeyFilter(t *testing.T) {
	r := newHelpRouter(true)

	w := getJSON(r, "/api/v1/ai/help/search?q=%E5%BD%B1%E5%93%8D&pageKey=ci.detail")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			Results []struct {
				PageKeys []string `json:"pageKeys"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.Results)
	for _, item := range resp.Data.Results {
		assert.Contains(t, item.PageKeys, "ci.detail")
	}
}

func TestGetPageHelp_KnownAndUnknownPage(t *testing.T) {
	r := newHelpRouter(true)

	// 已知页面：status=ok + 文档正文。
	w := getJSON(r, "/api/v1/ai/help/page?pageKey=ticket.detail")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			PageKey string `json:"pageKey"`
			Status  string `json:"status"`
			Title   string `json:"title"`
			Docs    []struct {
				Path string `json:"path"`
				Text string `json:"text"`
			} `json:"docs"`
			Coverage struct {
				Coverage float64        `json:"coverage"`
				Totals   map[string]int `json:"totals"`
			} `json:"coverage"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ticket.detail", resp.Data.PageKey)
	assert.Equal(t, "ok", resp.Data.Status)
	require.Len(t, resp.Data.Docs, 1)
	assert.Equal(t, "docs/help/tickets/detail.md", resp.Data.Docs[0].Path)
	assert.Contains(t, resp.Data.Docs[0].Text, "分诊建议")
	assert.Equal(t, 1.0, resp.Data.Coverage.Coverage)
	assert.Equal(t, 22, resp.Data.Coverage.Totals["pages"])

	// 未知页面：status=unknown、docs 为空（面板降级"暂无文档"）。
	w = getJSON(r, "/api/v1/ai/help/page?pageKey=no.such.page")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unknown", resp.Data.Status)
	assert.Empty(t, resp.Data.Docs)

	// 缺 pageKey → 400。
	w = getJSON(r, "/api/v1/ai/help/page")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// H1-04：覆盖率看板数据面（账本覆盖率/总计 + 全量页面清单 + orphan 文档）。

func TestHelpCoverage_MissingTenant(t *testing.T) {
	r := newHelpRouter(false)
	w := getJSON(r, "/api/v1/ai/help/coverage")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(common.AuthFailedCode), resp["code"])
}

func TestHelpCoverage_ReturnsLedgerAndPages(t *testing.T) {
	r := newHelpRouter(true)
	w := getJSON(r, "/api/v1/ai/help/coverage")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			GeneratedAt string         `json:"generatedAt"`
			Coverage    float64        `json:"coverage"`
			Totals      map[string]int `json:"totals"`
			Pages       []struct {
				PageKey string   `json:"pageKey"`
				Title   string   `json:"title"`
				Status  string   `json:"status"`
				Docs    []string `json:"docs"`
			} `json:"pages"`
			OrphanDocs []struct {
				Doc         string   `json:"doc"`
				UnknownKeys []string `json:"unknownKeys"`
			} `json:"orphanDocs"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	require.Equal(t, common.SuccessCode, resp.Code)

	assert.NotEmpty(t, resp.Data.GeneratedAt)
	assert.GreaterOrEqual(t, resp.Data.Totals["pages"], 20, "页面清单应覆盖注册表规模")
	assert.Greater(t, resp.Data.Coverage, 0.0)

	require.GreaterOrEqual(t, len(resp.Data.Pages), 20)
	for _, page := range resp.Data.Pages {
		assert.NotEmpty(t, page.PageKey)
		assert.NotEmpty(t, page.Status, page.PageKey)
	}
}
