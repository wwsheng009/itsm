package ai

import (
	"bytes"
	"context"
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

	"itsm-backend/capability"
	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"
)

// M2 能力开关（2026-09-30 方案 §2.3 G3/G4）：Bot 管理面的运行时门禁契约。
//
//  1. bot.enabled=false（管理后台关闭）→ 写端 403（含 grants），读端保留（页面需展示「已关闭」）；
//  2. 读端响应带 capabilities 块（与门禁同源）；
//  3. /agent/bots 在选择器侧返回空列表 + capabilities（前端据此隐藏入口）；
//  4. 能力开启时行为与既有完全一致。

type stubCapabilitySource struct {
	snap capability.Snapshot
}

func (s stubCapabilitySource) For(context.Context, int) capability.Snapshot { return s.snap }
func (s stubCapabilitySource) Invalidate(int)                               {}

func newBotCapabilityHarness(t *testing.T, caps capability.Source) *gin.Engine {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bot-capability-handler.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	handler := NewBotAdminHandler(bot.NewTemplateAdmin(client))
	require.NotNil(t, handler)
	handler.SetCapabilitySource(caps)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(func(c *gin.Context) {
		if raw := c.Request.Header.Get("X-Test-Tenant"); raw != "" {
			tenantID, err := strconv.Atoi(raw)
			require.NoError(t, err)
			c.Set("tenant_id", tenantID)
		}
		c.Next()
	})
	group.GET("/admin/bots", handler.ListBotTemplates)
	group.POST("/admin/bots", handler.CreateBotTemplate)
	group.PUT("/admin/bots/:id/grants", handler.UpsertBotGrant)
	group.GET("/agent/bots", handler.ListVisibleBots)
	return engine
}

func doBotCapability(t *testing.T, engine *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewBuffer(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewBuffer(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Tenant", "1")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestBotAdminHandler_CapabilityDisabledGatesWrites(t *testing.T) {
	caps := stubCapabilitySource{snap: capability.Snapshot{BotEnabled: false, MCPEnabled: true}}
	engine := newBotCapabilityHarness(t, caps)

	// 读端保留：列表 200 + capabilities 块（页面据此渲染「已关闭」）。
	recorder := doBotCapability(t, engine, http.MethodGet, "/api/v1/admin/bots", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	listed := decodeBody(t, recorder)
	capBlock, ok := listed["capabilities"].(map[string]any)
	require.True(t, ok, "读端必须下发 capabilities 块: body=%s", recorder.Body.String())
	assert.Equal(t, false, capBlock["botEnabled"])
	assert.Equal(t, false, capBlock["mcpWriteEnabled"])

	// 写端 403：创建模板。
	recorder = doBotCapability(t, engine, http.MethodPost, "/api/v1/admin/bots", map[string]any{
		"slug": "blocked-bot", "name": "被关闭期间创建的 Bot",
	})
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "bot.enabled=false")

	// 写端 403：授权保存（模板已由默认助手种子存在，id=1）。
	recorder = doBotCapability(t, engine, http.MethodPut, "/api/v1/admin/bots/1/grants", map[string]any{
		"toolName": "stub__list_notes", "riskLimit": "read",
	})
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())

	// 选择器：空列表 + capabilities（前端隐藏/禁用入口）。
	recorder = doBotCapability(t, engine, http.MethodGet, "/api/v1/agent/bots", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	selector := decodeBody(t, recorder)
	assert.Equal(t, float64(0), selector["total"])
	selectorCaps, ok := selector["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, selectorCaps["botEnabled"])
}

func TestBotAdminHandler_CapabilityEnabledKeepsBehavior(t *testing.T) {
	caps := stubCapabilitySource{snap: capability.Snapshot{BotEnabled: true, MCPEnabled: true, MCPWriteEnabled: true}}
	engine := newBotCapabilityHarness(t, caps)

	recorder := doBotCapability(t, engine, http.MethodPost, "/api/v1/admin/bots", map[string]any{
		"slug": "enabled-bot", "name": "正常创建的 Bot",
	})
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

	recorder = doBotCapability(t, engine, http.MethodGet, "/api/v1/agent/bots", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	selectorCaps, ok := decodeBody(t, recorder)["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, selectorCaps["botEnabled"])
	assert.Equal(t, true, selectorCaps["mcpWriteEnabled"])
}
