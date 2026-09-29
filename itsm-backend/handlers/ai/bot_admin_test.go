package ai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"
)

// B2-01 handler 层测试：CRUD 往返、租户隔离、错误映射（400/404/401）。
// 权限门禁与路由注册在 router/bot_routes_test.go 覆盖（本文件直接调用 handler，
// 不做 RBAC 预检，避免与权限矩阵耦合）。

type botAdminHarness struct {
	engine *gin.Engine
	admin  *bot.TemplateAdmin
}

func newBotAdminHarness(t *testing.T) *botAdminHarness {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bot-admin-handler.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	admin := bot.NewTemplateAdmin(client)
	handler := NewBotAdminHandler(admin)
	require.NotNil(t, handler)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	// 测试中间件：注入租户上下文（生产由鉴权链注入）；/api/v1/notenant 用于缺上下文用例。
	group.Use(func(c *gin.Context) {
		if c.Request.Header.Get("X-Test-Tenant") != "" {
			tenantID, err := strconv.Atoi(c.Request.Header.Get("X-Test-Tenant"))
			require.NoError(t, err)
			c.Set("tenant_id", tenantID)
		}
		// B2-04：选择器端点按角色做 audience 过滤，测试用 X-Test-Role 注入角色。
		if role := c.Request.Header.Get("X-Test-Role"); role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	group.GET("/admin/bots", handler.ListBotTemplates)
	group.POST("/admin/bots", handler.CreateBotTemplate)
	group.GET("/admin/bots/:id", handler.GetBotTemplate)
	group.PUT("/admin/bots/:id", handler.UpdateBotTemplate)
	group.DELETE("/admin/bots/:id", handler.DeleteBotTemplate)
	group.GET("/admin/bots/:id/grants", handler.ListBotGrants)
	group.PUT("/admin/bots/:id/grants", handler.UpsertBotGrant)
	group.DELETE("/admin/bots/:id/grants/:grantId", handler.DeleteBotGrant)
	group.GET("/agent/bots", handler.ListVisibleBots)
	return &botAdminHarness{engine: engine, admin: admin}
}

func (h *botAdminHarness) do(t *testing.T, method, path string, tenant int, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewBuffer(raw)
	} else {
		reader = bytes.NewBuffer(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if tenant > 0 {
		request.Header.Set("X-Test-Tenant", strconv.Itoa(tenant))
	}
	recorder := httptest.NewRecorder()
	h.engine.ServeHTTP(recorder, request)
	return recorder
}

func intToString(v int) string { return strconv.Itoa(v) }

// doAs 与 do 相同，并注入调用角色（B2-04 audience 过滤用例）。
func (h *botAdminHarness) doAs(t *testing.T, method, path string, tenant int, role string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewBuffer(raw)
	} else {
		reader = bytes.NewBuffer(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if tenant > 0 {
		request.Header.Set("X-Test-Tenant", strconv.Itoa(tenant))
	}
	if role != "" {
		request.Header.Set("X-Test-Role", role)
	}
	recorder := httptest.NewRecorder()
	h.engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	// 响应契约：统一 common 包络 {code,message,data}——前端 httpClient 只解包 data。
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), "body=%s", recorder.Body.String())
	require.Equal(t, 0, envelope.Code, "成功响应包络 code 必须为 0：body=%s", recorder.Body.String())
	return envelope.Data
}

func TestBotAdminHandler_CRUDRoundtrip(t *testing.T) {
	h := newBotAdminHarness(t)

	// 空租户首次列表：自动种入默认助手（兼容默认）。
	recorder := h.do(t, http.MethodGet, "/api/v1/admin/bots", 1, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	listed := decodeBody(t, recorder)
	require.Equal(t, float64(1), listed["total"])
	items := listed["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, bot.DefaultTemplateSlug, items[0].(map[string]any)["slug"])

	// 创建：201 + 默认 draft/act_low。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", 1, map[string]any{
		"slug": "ticket-helper", "name": "工单助手", "entrypoints": []string{"chat", "ticket"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	created := decodeBody(t, recorder)
	templateID := int(created["id"].(float64))
	assert.Equal(t, "draft", created["status"])
	assert.Equal(t, "act_low", created["riskLimit"])

	// 重复 slug → 400。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", 1, map[string]any{
		"slug": "ticket-helper", "name": "重复",
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// 更新：pilot + act_medium；携带 slug → 400（不可改）。
	recorder = h.do(t, http.MethodPut, "/api/v1/admin/bots/"+intToString(templateID), 1, map[string]any{
		"name": "工单助手 v2", "status": "pilot", "riskLimit": "act_medium",
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "pilot", decodeBody(t, recorder)["status"])
	recorder = h.do(t, http.MethodPut, "/api/v1/admin/bots/"+intToString(templateID), 1, map[string]any{"slug": "x"})
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// 授权：upsert → 列表 → 超限 400 → 删除 → 详情携带授权为空。
	recorder = h.do(t, http.MethodPut, "/api/v1/admin/bots/"+intToString(templateID)+"/grants", 1, map[string]any{
		"toolName": "stub__create_note", "riskLimit": "act_medium",
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	grantID := int(decodeBody(t, recorder)["id"].(float64))

	recorder = h.do(t, http.MethodGet, "/api/v1/admin/bots/"+intToString(templateID)+"/grants", 1, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, float64(1), decodeBody(t, recorder)["total"])

	recorder = h.do(t, http.MethodPut, "/api/v1/admin/bots/"+intToString(templateID)+"/grants", 1, map[string]any{
		"toolName": "stub__rotate_secret", "riskLimit": "act_high",
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code, "授权风险超过模板上限必须 400")

	recorder = h.do(t, http.MethodDelete, "/api/v1/admin/bots/"+intToString(templateID)+"/grants/"+intToString(grantID), 1, nil)
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = h.do(t, http.MethodGet, "/api/v1/admin/bots/"+intToString(templateID), 1, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, decodeBody(t, recorder)["grants"])

	// 删除模板：200；再取 404。
	recorder = h.do(t, http.MethodDelete, "/api/v1/admin/bots/"+intToString(templateID), 1, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	recorder = h.do(t, http.MethodGet, "/api/v1/admin/bots/"+intToString(templateID), 1, nil)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestBotAdminHandler_TenantIsolationAndErrors(t *testing.T) {
	h := newBotAdminHarness(t)

	// 租户 2 建模板；租户 3 不可见（404）。
	recorder := h.do(t, http.MethodPost, "/api/v1/admin/bots", 2, map[string]any{"slug": "t2-bot", "name": "租户2"})
	require.Equal(t, http.StatusCreated, recorder.Code)
	templateID := int(decodeBody(t, recorder)["id"].(float64))

	for _, probe := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/admin/bots/" + intToString(templateID), nil},
		{http.MethodPut, "/api/v1/admin/bots/" + intToString(templateID), map[string]any{"name": "越权"}},
		{http.MethodDelete, "/api/v1/admin/bots/" + intToString(templateID), nil},
		{http.MethodGet, "/api/v1/admin/bots/" + intToString(templateID) + "/grants", nil},
	} {
		recorder := h.do(t, probe.method, probe.path, 3, probe.body)
		require.Equal(t, http.StatusNotFound, recorder.Code, "%s %s 跨租户必须 404", probe.method, probe.path)
	}

	// 缺租户上下文 → 401；非法 id → 400；非法 JSON → 400。
	require.Equal(t, http.StatusUnauthorized, h.do(t, http.MethodGet, "/api/v1/admin/bots", 0, nil).Code)
	require.Equal(t, http.StatusBadRequest, h.do(t, http.MethodGet, "/api/v1/admin/bots/abc", 2, nil).Code)
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", 2, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// 非法状态/风险 → 400。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", 2, map[string]any{"slug": "bad", "name": "bad", "status": "nope"})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", 2, map[string]any{"slug": "bad2", "name": "bad", "riskLimit": "wild"})
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// 服务层种子：租户 3 尚未访问过，其列表应自带默认助手（与租户 2 独立）。
	recorder = h.do(t, http.MethodGet, "/api/v1/admin/bots", 3, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	items := decodeBody(t, recorder)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, bot.DefaultTemplateSlug, items[0].(map[string]any)["slug"])
}
