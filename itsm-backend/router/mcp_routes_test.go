package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"
	mcpHandler "itsm-backend/handlers/mcp"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// M0-10：MCP 管理路由的装配门禁与权限声明守卫。
//
// 与多 Provider（llm_provider_routes_test.go）同构：handler 为 nil（开关关闭/未装配）
// 时整组不注册 → 端点不可达（404）即回滚语义；注入后必须注册且**声明 mcp 权限码**
// （路由声明是 RBAC 预检映射的单一真源，改动后需 go run ./cmd/authz-gen 重新生成）。

func setupMCPRouter(t *testing.T, handler *mcpHandler.Handler) *gin.Engine {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:router_mcp_routes?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() {
		SetupRoutes(engine, &RouterConfig{
			JWTSecret:  "test-secret",
			Logger:     zaptest.NewLogger(t).Sugar(),
			Client:     client,
			MCPHandler: handler,
		})
	})
	return engine
}

func TestSetupRoutes_MCPRoutesAbsentWhenHandlerNil(t *testing.T) {
	engine := setupMCPRouter(t, nil)

	for _, route := range engine.Routes() {
		assert.NotContains(t, route.Path, "/mcp-servers", "开关关闭时不得注册 MCP 管理路由：%s", route.Path)
	}

	for _, probe := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/ai/mcp-servers"},
		{http.MethodPost, "/api/v1/ai/mcp-servers"},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(probe.method, probe.path, nil))
		assert.Equal(t, http.StatusNotFound, recorder.Code,
			"开关关闭时 %s %s 不可达（404）", probe.method, probe.path)
	}
}

func TestSetupRoutes_MCPRoutesRegisteredWhenHandlerPresent(t *testing.T) {
	engine := setupMCPRouter(t, mcpHandler.NewHandler(nil))

	registered := make(map[string]bool, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	wantRoutes := []string{
		http.MethodGet + " /api/v1/ai/mcp-servers",
		http.MethodGet + " /api/v1/ai/mcp-servers/health",
		http.MethodGet + " /api/v1/ai/mcp-servers/:id",
		http.MethodGet + " /api/v1/ai/mcp-servers/:id/tools",
		http.MethodGet + " /api/v1/ai/mcp-servers/:id/events",
		http.MethodPost + " /api/v1/ai/mcp-servers",
		http.MethodPut + " /api/v1/ai/mcp-servers/:id",
		http.MethodDelete + " /api/v1/ai/mcp-servers/:id",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/test",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/enable",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/disable",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/reload",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/tools/bulk",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/tools/:callable/enable",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/tools/:callable/disable",
		http.MethodPut + " /api/v1/ai/mcp-servers/:id/tools/:callable/classification",
		http.MethodPost + " /api/v1/ai/mcp-servers/:id/rotate-credential",
	}
	for _, route := range wantRoutes {
		assert.True(t, registered[route], "MCP 管理路由必须注册：%s", route)
	}

	// 未带令牌：必须进入鉴权链（401/403），而不是 404——证明路由确实已注册。
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/mcp-servers", nil))
	require.NotEqual(t, http.StatusNotFound, recorder.Code,
		"注入 handler 后 /ai/mcp-servers 应已注册（未带令牌被鉴权链拦截而非 404）")
	assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, recorder.Code)
}

// TestMCPRouteDeclarationsUseMCPCodes 路由声明的 (resource, action) 必须是 mcp:read / mcp:admin。
//
// 依据 M0-10：读端点 mcp:read、治理写端点 mcp:admin；使用与治理分离。
// 若此处失败，说明有人把声明改回了 ai:read / system:write（或引入了第三个码），
// 需同步：① 本测试 ② cmd/authz-gen 重新生成预检映射 ③ 角色矩阵（internal/authz/mcp_roles_test.go）。
func TestMCPRouteDeclarationsUseMCPCodes(t *testing.T) {
	declared, err := middleware.ScanDeclaredPermissionRoutes()
	require.NoError(t, err)

	seen := 0
	for _, route := range declared {
		if !strings.Contains(route.FullPath, "/mcp-servers") {
			continue
		}
		seen++
		assert.Equal(t, "mcp", route.Resource, "%s %s 的资源必须是 mcp", route.Method, route.FullPath)
		assert.Contains(t, []string{"read", "admin"}, route.Action,
			"%s %s 的动作必须是 read（读）或 admin（治理）", route.Method, route.FullPath)

		if strings.HasPrefix(route.FullPath, "/api/v1/ai/mcp-servers") &&
			(strings.Contains(route.FullPath, "/tools") || strings.Contains(route.FullPath, "/events")) &&
			route.Method == http.MethodGet {
			assert.Equal(t, "read", route.Action, "工具/事件读端点必须是 read")
		}
	}
	require.GreaterOrEqual(t, seen, 17, "必须扫描到全部 MCP 管理路由声明（含读 5 + 治理写 12）")
}
