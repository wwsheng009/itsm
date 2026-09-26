package router

import (
	aiHandler "itsm-backend/handlers/ai"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupLLMProviderAdminRoutes 注册多 LLM Provider 管理与切换 API（主计划《多 LLM Provider
// 支持与可切换方案》§3.4；P1 演进 2026-09-26）。
//
// 门禁：
//   - h 为 nil（灰度开关关闭）时整组不注册——端点不可达即回滚语义（§3.5）；
//   - 管理端点挂 RequirePermission("system","write")（分组级挂载，D12）：
//     super_admin 走既有旁路，sysadmin 持 *:* 数据授权，其余角色一律拒绝；
//   - 选择器读端点（GET /providers/available、GET /user-preference）降为
//     RequirePermission("ai","read")：让全部 AI 使用者可读取可用实例与个人偏好，
//     支撑「全员可切换」（P1 演进；此前为 system:write 仅系统管理员）。
//     PUT /user-preference 属写操作，仍留在 system:write 组，不随 P1 放开。
//
// 路径与 handlers/ai/llm_provider_handler.go 的注释一一对应。
// 注意：本文件的权限声明是 RBAC 预检映射的单一真源，改动后必须执行
// `cd itsm-backend && go run ./cmd/authz-gen` 重新生成 middleware/rbac_precheck_gen.go
// （新鲜度由 TestPrecheckMapIsFresh 守卫）。
func SetupLLMProviderAdminRoutes(tenant *gin.RouterGroup, h *aiHandler.LLMProviderAdminHandler) {
	if h == nil {
		return
	}

	// 全员可读（P1）：选择器数据源 + 个人偏好读取。
	read := tenant.Group("/ai", middleware.RequirePermission("ai", "read"))
	{
		read.GET("/providers/available", h.ListAvailable)
		read.GET("/user-preference", h.GetUserPreference)
	}

	// 管理面（D12）：实例 CRUD / 连通性测试 / 设默认 / 静态导入 / 个人默认写入。
	admin := tenant.Group("/ai", middleware.RequirePermission("system", "write"))
	{
		// 实例 CRUD
		admin.GET("/providers", h.ListProviders)
		admin.POST("/providers", h.CreateProvider)
		admin.PUT("/providers/:id", h.UpdateProvider)
		admin.DELETE("/providers/:id", h.DeleteProvider)
		// 连通性测试 / 默认实例 / 静态配置导入
		admin.POST("/providers/:id/test", h.TestProvider)
		admin.POST("/providers/:id/default", h.SetDefaultProvider)
		admin.POST("/providers/import-static", h.ImportStatic)
		// 个人默认偏好（写：仍限系统管理员）
		admin.PUT("/user-preference", h.SetUserPreference)
	}
}
