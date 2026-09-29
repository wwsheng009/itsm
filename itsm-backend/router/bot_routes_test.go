package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"
	aiHandler "itsm-backend/handlers/ai"
	"itsm-backend/middleware"
	"itsm-backend/service/bot"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// B2-01：Bot 管理路由的装配门禁与权限声明守卫（与 mcp_routes_test.go 同构）。
//
// - handler 为 nil（bot.enabled=false / 未装配）时整组不注册 → 端点 404（开关关闭即回滚语义）；
// - 注入后必须注册且声明 ai:read / ai:write（BD8：复用既有权限码）；
// - 路由声明是 RBAC 预检映射的单一真源，改动后需 go run ./cmd/authz-gen 重新生成。

func setupBotRouter(t *testing.T, handler *aiHandler.BotAdminHandler) *gin.Engine {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:router_bot_routes?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() {
		SetupRoutes(engine, &RouterConfig{
			JWTSecret:       "test-secret",
			Logger:          zaptest.NewLogger(t).Sugar(),
			Client:          client,
			BotAdminHandler: handler,
		})
	})
	return engine
}

func TestSetupRoutes_BotRoutesAbsentWhenHandlerNil(t *testing.T) {
	engine := setupBotRouter(t, nil)

	for _, route := range engine.Routes() {
		assert.NotContains(t, route.Path, "/admin/bots", "开关关闭时不得注册 Bot 管理路由：%s", route.Path)
	}
	for _, probe := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/bots"},
		{http.MethodPost, "/api/v1/admin/bots"},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(probe.method, probe.path, nil))
		assert.Equal(t, http.StatusNotFound, recorder.Code,
			"开关关闭时 %s %s 不可达（404）", probe.method, probe.path)
	}
}

func TestSetupRoutes_BotRoutesRegisteredWhenHandlerPresent(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:router_bot_routes_present?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	engine := setupBotRouter(t, aiHandler.NewBotAdminHandler(bot.NewTemplateAdmin(client)))

	registered := make(map[string]bool, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	wantRoutes := []string{
		http.MethodGet + " /api/v1/admin/bots",
		http.MethodPost + " /api/v1/admin/bots",
		http.MethodGet + " /api/v1/admin/bots/:id",
		http.MethodPut + " /api/v1/admin/bots/:id",
		http.MethodDelete + " /api/v1/admin/bots/:id",
		http.MethodGet + " /api/v1/admin/bots/:id/grants",
		http.MethodPut + " /api/v1/admin/bots/:id/grants",
		http.MethodDelete + " /api/v1/admin/bots/:id/grants/:grantId",
	}
	for _, route := range wantRoutes {
		assert.True(t, registered[route], "Bot 管理路由必须注册：%s", route)
	}

	// 未带令牌：必须进入鉴权链（401/403），而不是 404——证明路由确实已注册。
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/bots", nil))
	require.NotEqual(t, http.StatusNotFound, recorder.Code,
		"注入 handler 后 /admin/bots 应已注册（未带令牌被鉴权链拦截而非 404）")
	assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, recorder.Code)
}

// TestBotRouteDeclarationsUseAICodes 路由声明的 (resource, action) 必须是 ai:read / ai:write（BD8）。
//
// 若此处失败，说明有人为 Bot 管理面新开了权限码（角色矩阵扩散），
// 需同步：① 本测试 ② internal/authz/catalog.go 与角色矩阵 ③ cmd/authz-gen 重新生成预检映射。
func TestBotRouteDeclarationsUseAICodes(t *testing.T) {
	declared, err := middleware.ScanDeclaredPermissionRoutes()
	require.NoError(t, err)

	seen := 0
	for _, route := range declared {
		if !strings.Contains(route.FullPath, "/admin/bots") {
			continue
		}
		seen++
		assert.Equal(t, "ai", route.Resource, "%s %s 的资源必须是 ai", route.Method, route.FullPath)
		if route.Method == http.MethodGet {
			assert.Equal(t, "read", route.Action, "%s %s 的读端点必须声明 read", route.Method, route.FullPath)
		} else {
			assert.Equal(t, "write", route.Action, "%s %s 的写端点必须声明 write", route.Method, route.FullPath)
		}
	}
	require.GreaterOrEqual(t, seen, 8, "必须扫描到全部 Bot 管理路由声明（读 3 + 写 5）")
}
