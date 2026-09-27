package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/ent/enttest"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M0-10：MCP 权限位（mcp:read / mcp:write / mcp:admin）的 Gate2 判定矩阵与路由级 403/200。
//
// 语义提醒（M1-01 收口）：通用规则「资源内 `admin` 动作是超集」**对 mcp 资源不适用**——
// 外部工具的使用与治理必须严格分离（D7 + Q2 拍板）：mcp:admin 只授予治理能力，不蕴含
// mcp:read / mcp:write；写工具执行必须显式授予 mcp:write，且仍受 Gate3 审批（M1-02）约束。
//
// 历史（真实缺陷，2026-09-27 由 M1-01 集成用例发现）：该通用规则曾让持 mcp:admin 的租户管理员
// 静默获得 mcp:write（Gate2 直通），与授权面（authz.mcp_roles_test 断言 admin 不持 mcp:write）矛盾。

func mcpPairPresent(list []Permission, resource, action string) bool {
	for _, p := range list {
		if p.Resource == resource && p.Action == action {
			return true
		}
	}
	return false
}

func TestMCPPermissionMatrix_HardcodeFallback(t *testing.T) {
	original := PermissionConfig.Mode
	PermissionConfig.Mode = PermissionConfigModeHardcodeOnly
	t.Cleanup(func() { PermissionConfig.Mode = original })

	cases := []struct {
		role                 string
		read, write, admin   bool
		wantEmptyPermissions bool
	}{
		{role: "super_admin", read: true, write: true, admin: true},
		{role: "sysadmin", read: true, write: true, admin: true},
		// 租户管理员：显式授予 read+admin；**不含** write（mcp:write 需显式授予，见 M1-01 收口说明）。
		{role: "admin", read: true, write: false, admin: true},
		// 其余角色默认零授予（D7）：含坐席/技术员/经理/最终用户。
		{role: "manager", read: false, write: false, admin: false},
		{role: "agent", read: false, write: false, admin: false},
		{role: "technician", read: false, write: false, admin: false},
		{role: "end_user", read: false, write: false, admin: false},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			permissions := loadPermissionsByMode(context.Background(), nil, tc.role, 1)
			require.NotEmpty(t, permissions, "兜底表角色 %s 应有权限行", tc.role)
			assert.Equal(t, tc.read, checkPermissionMatch(permissions, "mcp", "read"), "mcp:read 判定")
			assert.Equal(t, tc.write, checkPermissionMatch(permissions, "mcp", "write"), "mcp:write 判定")
			assert.Equal(t, tc.admin, checkPermissionMatch(permissions, "mcp", "admin"), "mcp:admin 判定")
		})
	}
}

// TestMCPUseAndGovernanceSeparation_Matcher 使用与治理分离的**双向**约束：
// read 不隐含 admin/write、write 不隐含 admin/read、admin 不隐含 read/write（M1-01 收口后无任一蕴含）。
func TestMCPUseAndGovernanceSeparation_Matcher(t *testing.T) {
	readOnly := []Permission{{Resource: "mcp", Action: "read"}}
	assert.True(t, checkPermissionMatch(readOnly, "mcp", "read"))
	assert.False(t, checkPermissionMatch(readOnly, "mcp", "admin"), "read 不得隐含治理")
	assert.False(t, checkPermissionMatch(readOnly, "mcp", "write"), "read 不得隐含写工具执行")

	writeOnly := []Permission{{Resource: "mcp", Action: "write"}}
	assert.True(t, checkPermissionMatch(writeOnly, "mcp", "write"))
	assert.False(t, checkPermissionMatch(writeOnly, "mcp", "admin"), "write 不得隐含治理")
	assert.False(t, checkPermissionMatch(writeOnly, "mcp", "read"), "write 不得隐含读取")

	governanceOnly := []Permission{{Resource: "mcp", Action: "admin"}}
	assert.True(t, checkPermissionMatch(governanceOnly, "mcp", "admin"))
	assert.False(t, checkPermissionMatch(governanceOnly, "mcp", "write"), "admin 不得蕴含写工具执行")
	assert.False(t, checkPermissionMatch(governanceOnly, "mcp", "read"), "admin 不得蕴含读取（显式授予）")
}

// TestMCPAdminGrantSurfaceIsExplicit 授予面断言：兜底表里 admin 显式持有 read+admin、**不**持 write。
func TestMCPAdminGrantSurfaceIsExplicit(t *testing.T) {
	adminPermissions := RolePermissions["admin"]
	assert.True(t, mcpPairPresent(adminPermissions, "mcp", "read"), "admin 必须能读 MCP 服务器/工具清单")
	assert.True(t, mcpPairPresent(adminPermissions, "mcp", "admin"), "admin 必须能治理 MCP 服务器")
	assert.False(t, mcpPairPresent(adminPermissions, "mcp", "write"),
		"admin 不得显式持有 mcp:write（M1-02 起按需授予，写工具仍需 Gate3 审批）")

	// 兜底表其余角色不得出现任何 mcp 授权（默认拒绝）。
	for role, permissions := range RolePermissions {
		if role == "super_admin" || role == "sysadmin" || role == "admin" {
			continue // 通配角色与治理主，见上
		}
		assert.False(t, mcpPairPresent(permissions, "mcp", "read"), "%s 不得默认持有 mcp:read", role)
		assert.False(t, mcpPairPresent(permissions, "mcp", "write"), "%s 不得默认持有 mcp:write", role)
		assert.False(t, mcpPairPresent(permissions, "mcp", "admin"), "%s 不得默认持有 mcp:admin", role)
	}
}

// TestMCPRoutes_HTTPStatusByRole 路由级 403/200：低权角色被拒（403），治理角色放行（200）。
func TestMCPRoutes_HTTPStatusByRole(t *testing.T) {
	original := PermissionConfig.Mode
	PermissionConfig.Mode = PermissionConfigModeHardcodeOnly
	t.Cleanup(func() { PermissionConfig.Mode = original })

	// RequirePermission 需要上下文注入 ent client（为 nil 时 500「客户端缺失」）；
	// 硬编码模式下不查库，空库即可。
	client := enttest.Open(t, "sqlite3", "file:mcp_matrix_http?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	withRole := func(role string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("role", role)
			c.Set("tenant_id", 1)
			c.Set("client", client)
			c.Next()
		}
	}
	engine.GET("/read", withRole("technician"), RequirePermission("mcp", "read"), okHandler)
	engine.GET("/admin", withRole("technician"), RequirePermission("mcp", "admin"), okHandler)
	engine.GET("/read-admin-role", withRole("admin"), RequirePermission("mcp", "read"), okHandler)
	engine.GET("/admin-role", withRole("admin"), RequirePermission("mcp", "admin"), okHandler)

	cases := []struct {
		path string
		want int
		why  string
	}{
		{"/read", http.StatusForbidden, "坐席无 mcp:read"},
		{"/admin", http.StatusForbidden, "坐席无 mcp:admin"},
		{"/read-admin-role", http.StatusOK, "租户管理员可读（mcp:read）"},
		{"/admin-role", http.StatusOK, "租户管理员可治理（mcp:admin）"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		assert.Equalf(t, tc.want, recorder.Code, "%s：%s", tc.path, tc.why)
	}
}

func okHandler(c *gin.Context) { c.Status(http.StatusOK) }
