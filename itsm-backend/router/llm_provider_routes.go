package router

import (
	aiHandler "itsm-backend/handlers/ai"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupLLMProviderAdminRoutes 注册多 LLM Provider 管理 API（主计划《多 LLM Provider
// 支持与可切换方案》v1.6 BE-4 §3.4）。
//
// 门禁：
//   - h 为 nil（灰度开关关闭）时整组不注册——端点不可达即回滚语义（§3.5）；
//   - 10 个端点统一挂 RequirePermission("system","write")（分组级挂载，含读端点，D12）：
//     super_admin 走既有旁路，sysadmin 持 *:* 数据授权，其余角色一律拒绝。
//
// 路径与 handlers/ai/llm_provider_handler.go 的注释一一对应。
func SetupLLMProviderAdminRoutes(tenant *gin.RouterGroup, h *aiHandler.LLMProviderAdminHandler) {
	if h == nil {
		return
	}
	grp := tenant.Group("/ai", middleware.RequirePermission("system", "write"))
	{
		// 实例 CRUD
		grp.GET("/providers", h.ListProviders)
		grp.POST("/providers", h.CreateProvider)
		grp.PUT("/providers/:id", h.UpdateProvider)
		grp.DELETE("/providers/:id", h.DeleteProvider)
		// 连通性测试 / 默认实例 / 静态配置导入 / 可用列表
		grp.POST("/providers/:id/test", h.TestProvider)
		grp.POST("/providers/:id/default", h.SetDefaultProvider)
		grp.POST("/providers/import-static", h.ImportStatic)
		grp.GET("/providers/available", h.ListAvailable)
		// 个人默认偏好（读写）
		grp.GET("/user-preference", h.GetUserPreference)
		grp.PUT("/user-preference", h.SetUserPreference)
	}
}
