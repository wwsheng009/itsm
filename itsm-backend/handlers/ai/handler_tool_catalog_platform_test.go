// 工具目录 · MCP 平台独占守卫（2026-10-10 治理域收口 §8 D7 补丁）。
//
// 不变量：目录中的 MCP 条目与能力块 MCP 位仅平台角色（super_admin/sysadmin）可见；
// 非平台角色**即使持有 mcp:read/mcp:write**（DB 授权实测路径）也不下发——
// 防"未来重新授予 mcp:read 即回潮"（旧实现只靠码过滤，属隐式保护）。
package ai_test

import (
	"context"
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
	"itsm-backend/middleware"
	"itsm-backend/service"
)

type catalogPlatformResponse struct {
	Code int `json:"code"`
	Data struct {
		Items        []catalogItem              `json:"items"`
		Total        int                        `json:"total"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	} `json:"data"`
}

func TestListToolCatalog_MCPItemsPlatformOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:tool_catalog_platform?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("PlatformCatalog").SetCode("platform-catalog").
		SetDomain("pc.test").SaveX(ctx)
	user := client.User.Create().SetUsername("pc-admin").SetEmail("pc@example.com").
		SetName("PC Admin").SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	// DB 授权：自定义角色 catalog_ops 显式持 mcp:read/write + ticket:read——
	// 证明"持码也看不到 MCP 条目"（平台独占为显式规则，不是码过滤的副作用）。
	role := client.Role.Create().SetName("Catalog Ops").SetCode("catalog_ops").SetTenantID(tenant.ID).SaveX(ctx)
	for _, spec := range []struct{ code, resource, action string }{
		{"mcp:read", "mcp", "read"},
		{"mcp:write", "mcp", "write"},
		{"ticket:read", "ticket", "read"},
	} {
		p := client.Permission.Create().SetCode(spec.code).SetName(spec.code).
			SetResource(spec.resource).SetAction(spec.action).SetTenantID(tenant.ID).SaveX(ctx)
		client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(p.ID).
			SetTenantID(tenant.ID).SaveX(ctx)
	}

	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(catalogProbeProvider{defs: catalogProbeDefs()})
	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	h := ai.NewHandler(svc)

	// DBOnly + 角色行存在（configured）：授权集合以 DB 为准（catalog_ops 确实持 mcp 码）。
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeDBOnly
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
	})

	do := func(role, query string) (*httptest.ResponseRecorder, catalogPlatformResponse) {
		t.Helper()
		r := gin.New()
		r.GET("/api/v1/agent/tools/catalog", func(c *gin.Context) {
			c.Set("tenant_id", tenant.ID)
			c.Set("user_id", user.ID)
			if role != "" {
				c.Set("role", role)
			}
			h.ListToolCatalog(c)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/agent/tools/catalog"+query, nil))
		var decoded catalogPlatformResponse
		if w.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded), "body=%s", w.Body.String())
		}
		return w, decoded
	}

	t.Run("非平台角色持 mcp:read 也不下发 MCP 条目", func(t *testing.T) {
		w, resp := do("catalog_ops", "?q=probe")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, 1, resp.Data.Total, "仅内置 ticket 工具可见（MCP 条目被平台独占规则拦下）")
		assert.Equal(t, "probe_ticket_read", resp.Data.Items[0].Name)
		for _, item := range resp.Data.Items {
			assert.NotEqual(t, "mcp", item.Provider)
		}
		// 显式 source=mcp 同样为空（规则先于码判定）。
		_, mcpOnly := do("catalog_ops", "?q=probe&source=mcp")
		assert.Zero(t, mcpOnly.Data.Total)
		// 能力块：不暴露 MCP 位；botEnabled 保留。
		_, okEnabled := resp.Data.Capabilities["mcpEnabled"]
		_, okWrite := resp.Data.Capabilities["mcpWriteEnabled"]
		assert.False(t, okEnabled, "非平台能力块不得含 mcpEnabled")
		assert.False(t, okWrite, "非平台能力块不得含 mcpWriteEnabled")
		assert.Contains(t, resp.Data.Capabilities, "botEnabled")
	})

	t.Run("平台角色全量可见（含 MCP 条目与能力块 MCP 位）", func(t *testing.T) {
		w, resp := do("super_admin", "?q=probe")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, 4, resp.Data.Total)
		providers := map[string]int{}
		for _, item := range resp.Data.Items {
			providers[item.Provider]++
		}
		assert.Equal(t, 3, providers["mcp"], "平台角色可见全部 MCP 条目")
		assert.Contains(t, resp.Data.Capabilities, "mcpEnabled")
		assert.Contains(t, resp.Data.Capabilities, "mcpWriteEnabled")
	})
}
