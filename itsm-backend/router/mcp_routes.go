package router

import (
	mcpHandler "itsm-backend/handlers/mcp"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupMCPServerRoutes 注册 MCP 外部工具管理 API（M0-08；形状见分析报告 §5.5）。
//
// 门禁：
//   - h 为 nil（开关关闭/未装配）时整组不注册——端点不可达即回滚语义；
//   - 读端点挂 RequirePermission("mcp","read")、写端点挂 ("mcp","admin")（M0-10）：
//     使用与治理分离——read 不含治理写、admin 不隐含工具执行；两码默认仅 sysadmin/admin 持有
//     （D7，角色矩阵见 internal/authz/roles.go 与 internal/authz/mcp_roles_test.go）；
//   - enable/disable/reload 返回 202 + 状态回读（D8，禁止 60s 同步等待）。
//
// 注意：本文件的权限声明是 RBAC 预检映射的单一真源，改动后必须执行
// `cd itsm-backend && go run ./cmd/authz-gen` 重新生成 middleware/rbac_precheck_gen.go
// （新鲜度由 TestPrecheckMapIsFresh 守卫）。
func SetupMCPServerRoutes(tenant *gin.RouterGroup, h *mcpHandler.Handler) {
	if h == nil {
		return
	}

	// 读（mcp:read）：列表/详情/工具/健康/事件。
	read := tenant.Group("/ai", middleware.RequirePermission("mcp", "read"))
	{
		// 注意：/health 为静态段，需与 /:id 同级注册（gin 支持静态优先）。
		read.GET("/mcp-servers", h.ListServers)
		read.GET("/mcp-servers/health", h.Health)
		read.GET("/mcp-servers/:id", h.GetServer)
		read.GET("/mcp-servers/:id/tools", h.ListTools)
		read.GET("/mcp-servers/:id/events", h.Events)
	}

	// 治理写（mcp:admin）：CRUD / 测试连接 / 启停重载 / 工具治理 / 凭据轮换。
	admin := tenant.Group("/ai", middleware.RequirePermission("mcp", "admin"))
	{
		admin.POST("/mcp-servers", h.CreateServer)
		admin.PUT("/mcp-servers/:id", h.UpdateServer)
		admin.DELETE("/mcp-servers/:id", h.DeleteServer)
		admin.POST("/mcp-servers/:id/test", h.TestServer)
		admin.POST("/mcp-servers/:id/enable", h.EnableServer)
		admin.POST("/mcp-servers/:id/disable", h.DisableServer)
		admin.POST("/mcp-servers/:id/reload", h.ReloadServer)
		admin.POST("/mcp-servers/:id/tools/bulk", h.BulkSetTools)
		admin.POST("/mcp-servers/:id/tools/:callable/enable", h.SetToolEnabled)
		admin.POST("/mcp-servers/:id/tools/:callable/disable", h.SetToolDisabled)
		admin.PUT("/mcp-servers/:id/tools/:callable/classification", h.SetToolClassification)
		admin.POST("/mcp-servers/:id/rotate-credential", h.RotateCredential)
	}
}
