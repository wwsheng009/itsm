// 工具目录端点（GET /api/v1/agent/tools/catalog）契约测试。
//
// 锁定三件事：
//  1. 可见性与 ListTools 同源——租户工具面 + 当前角色 RBAC（resource/action）过滤；
//  2. 查询能力——q（名称/描述/原始工具名/服务器名）、source、readOnly、risk、limit；
//  3. 紧凑投影——MCP 工具带 provider/serverName/rawToolName，不下发 schema。
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

// catalogProbeProvider 提供 4 个可控定义：
//   - probe_ticket_read：内置风格只读（ticket:read）；
//   - mcp__probe__echo：MCP 只读；
//   - mcp__probe__update：MCP 写（act_low）；
//   - mcp__probe__danger：MCP 写（act_high，风险过滤用）。
type catalogProbeProvider struct{ defs []service.ToolDefinition }

func (p catalogProbeProvider) ProviderName() string { return "catalog-probe" }

func (p catalogProbeProvider) ListTools(context.Context, int) []service.ToolDefinition {
	return p.defs
}

func (p catalogProbeProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	for _, def := range p.defs {
		if def.Name == name {
			copied := def
			return &copied, true
		}
	}
	return nil, false
}

func (p catalogProbeProvider) Execute(context.Context, int, string, map[string]interface{}) (*service.ToolExecution, error) {
	return &service.ToolExecution{Provider: "probe"}, nil
}

func catalogProbeDefs() []service.ToolDefinition {
	return []service.ToolDefinition{
		{
			Name: "probe_ticket_read", Description: "工单只读探针", ReadOnly: true,
			Resource: "ticket", Action: "read", Risk: service.ToolRiskRead,
			Category: "ticket", Provider: "builtin",
		},
		{
			Name: "mcp__probe__echo", Description: "回显工具", ReadOnly: true,
			Resource: "mcp", Action: "read", Risk: service.ToolRiskRead,
			Category: "mcp", Provider: "mcp", ServerName: "probe", RawToolName: "echo",
		},
		{
			Name: "mcp__probe__update", Description: "更新工具", ReadOnly: false,
			Resource: "mcp", Action: "write", Risk: service.ToolRiskActLow,
			Category: "mcp", Provider: "mcp", ServerName: "probe", RawToolName: "update",
			SupportsDryRun: true, Idempotent: true,
		},
		{
			Name: "mcp__probe__danger", Description: "高危工具", ReadOnly: false,
			Resource: "mcp", Action: "write", Risk: service.ToolRiskActHigh,
			Category: "mcp", Provider: "mcp", ServerName: "probe", RawToolName: "danger",
			Idempotent: true,
		},
	}
}

type catalogItem struct {
	Name        string `json:"name"`
	ReadOnly    bool   `json:"readOnly"`
	Risk        string `json:"risk"`
	Provider    string `json:"provider"`
	ServerName  string `json:"serverName"`
	RawToolName string `json:"rawToolName"`
}

type catalogResponse struct {
	Code int `json:"code"`
	Data struct {
		Items []catalogItem `json:"items"`
		Total int           `json:"total"`
	} `json:"data"`
}

func TestListToolCatalog_FiltersAndRBAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:tool_catalog?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("Catalog").SetCode("catalog").SetDomain("catalog.test").SaveX(ctx)
	user := client.User.Create().SetUsername("catalog-admin").SetEmail("catalog@example.com").
		SetName("Catalog Admin").SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(catalogProbeProvider{defs: catalogProbeDefs()})
	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	h := ai.NewHandler(svc)

	// HardcodeOnly：角色权限取硬编码表（end_user 仅 ticket:read/write/create；super_admin 直通）。
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
	})

	do := func(role, query string) (*httptest.ResponseRecorder, catalogResponse) {
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
		var decoded catalogResponse
		if w.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded), "body=%s", w.Body.String())
		}
		return w, decoded
	}

	t.Run("全量：内置在前、MCP 带来源三元组", func(t *testing.T) {
		w, resp := do("super_admin", "?q=probe")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, 4, resp.Data.Total)
		require.Len(t, resp.Data.Items, 4)
		assert.Equal(t, "probe_ticket_read", resp.Data.Items[0].Name, "内置工具排在 MCP 之前")
		byName := map[string]catalogItem{}
		for _, item := range resp.Data.Items {
			byName[item.Name] = item
		}
		echo := byName["mcp__probe__echo"]
		assert.Equal(t, "mcp", echo.Provider)
		assert.Equal(t, "probe", echo.ServerName)
		assert.Equal(t, "echo", echo.RawToolName)
		assert.True(t, echo.ReadOnly)
		assert.Equal(t, service.ToolRiskRead, echo.Risk)
		// 紧凑投影：不下发 schema（防选择器大 payload）。
		assert.NotContains(t, w.Body.String(), "argsSchema")
		assert.NotContains(t, w.Body.String(), "resultSchema")
	})

	t.Run("source=mcp 仅返回外部工具", func(t *testing.T) {
		_, resp := do("super_admin", "?q=probe&source=mcp")
		assert.Equal(t, 3, resp.Data.Total)
		for _, item := range resp.Data.Items {
			assert.Equal(t, "mcp", item.Provider)
		}
	})

	t.Run("readOnly/risk/描述关键词过滤", func(t *testing.T) {
		_, resp := do("super_admin", "?q=probe&readOnly=true")
		names := make([]string, 0, len(resp.Data.Items))
		for _, item := range resp.Data.Items {
			names = append(names, item.Name)
			assert.True(t, item.ReadOnly)
		}
		assert.ElementsMatch(t, []string{"probe_ticket_read", "mcp__probe__echo"}, names)

		_, resp = do("super_admin", "?q=probe&readOnly=false&risk=act_high")
		require.Len(t, resp.Data.Items, 1)
		assert.Equal(t, "mcp__probe__danger", resp.Data.Items[0].Name)

		// 描述命中（中文关键词）+ 原始工具名命中。
		_, resp = do("super_admin", "?q=回显")
		require.Equal(t, 1, resp.Data.Total)
		assert.Equal(t, "mcp__probe__echo", resp.Data.Items[0].Name)
		_, resp = do("super_admin", "?q=danger")
		require.Equal(t, 1, resp.Data.Total)
		assert.Equal(t, "mcp__probe__danger", resp.Data.Items[0].Name)
	})

	t.Run("limit 截断但 total 为全量匹配数；上限钳制", func(t *testing.T) {
		_, resp := do("super_admin", "?q=probe&limit=1")
		assert.Equal(t, 4, resp.Data.Total, "total 反映匹配总数")
		assert.Len(t, resp.Data.Items, 1, "items 按 limit 截断")
		_, resp = do("super_admin", "?q=probe&limit=9999")
		assert.Equal(t, 4, resp.Data.Total)
		assert.Len(t, resp.Data.Items, 4, "limit 超上限按 500 钳制而非报错")
	})

	t.Run("RBAC：end_user 看不到 MCP 工具", func(t *testing.T) {
		_, resp := do("end_user", "?q=probe")
		require.Equal(t, 1, resp.Data.Total, "end_user 仅持 ticket:read/write/create")
		assert.Equal(t, "probe_ticket_read", resp.Data.Items[0].Name)
		_, resp = do("end_user", "?q=probe&source=mcp")
		assert.Zero(t, resp.Data.Total)
	})

	t.Run("参数非法与身份缺失", func(t *testing.T) {
		cases := []string{"?source=nope", "?readOnly=maybe", "?limit=0", "?limit=abc", "?risk=wild"}
		for _, query := range cases {
			w, _ := do("super_admin", query)
			assert.Equal(t, http.StatusBadRequest, w.Code, "query=%s body=%s", query, w.Body.String())
		}
		w, _ := do("", "?q=probe")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "缺少角色上下文必须 401")
	})
}
