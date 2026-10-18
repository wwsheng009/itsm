package router

import (
	aiHandler "itsm-backend/handlers/ai"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupBotAdminRoutes 注册 Bot 模板/授权管理 API（B2-01；前缀 /api/v1/admin/bots）。
//
// 门禁：
//   - h 为 nil（组件未装配）时整组不注册（防御性回退，正常 bootstrap 常驻装配）；
//   - 平台级运行时开关 bot.enabled 由 handler 在请求期求值：读端保留（页面可展示「已关闭」
//     并引导到「系统配置 → AI 配置」），写端关闭时 403（feature_disabled:bot 语义）；
//   - 读端点挂 RequirePermission("ai","read")、写端点挂 ("ai","write")（BD8：
//     复用既有 AI 权限码，不新开权限码，角色矩阵不扩散）。
//
// 注意：本文件的权限声明是 RBAC 预检映射的单一真源，改动后必须执行
// `cd itsm-backend && go run ./cmd/authz-gen` 重新生成 middleware/rbac_precheck_gen.go
// （新鲜度由 TestPrecheckMapIsFresh 守卫）。
func SetupBotAdminRoutes(tenant *gin.RouterGroup, h *aiHandler.BotAdminHandler) {
	if h == nil {
		return
	}

	read := tenant.Group("/admin", middleware.RequirePermission("ai", "read"))
	{
		read.GET("/bots", h.ListBotTemplates)
		read.GET("/bots/:id", h.GetBotTemplate)
		read.GET("/bots/:id/grants", h.ListBotGrants)
	}
	// B2-04 工作区选择器：agent 前缀 + ai:read（普通使用者可用），按角色做 audience 过滤。
	// 路径注册顺序：更具体的 /agent/bots 与 /admin/bots 分属不同前缀，无冲突。
	agentRead := tenant.Group("/agent", middleware.RequirePermission("ai", "read"))
	{
		agentRead.GET("/bots", h.ListVisibleBots)
	}

	write := tenant.Group("/admin", middleware.RequirePermission("ai", "write"))
	{
		write.POST("/bots", h.CreateBotTemplate)
		write.PUT("/bots/:id", h.UpdateBotTemplate)
		write.DELETE("/bots/:id", h.DeleteBotTemplate)
		write.PUT("/bots/:id/grants", h.UpsertBotGrant)
		write.DELETE("/bots/:id/grants/:grantId", h.DeleteBotGrant)
	}
}
