package router

import (
	attachmentHandler "itsm-backend/handlers/attachment"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupAttachmentRoutes 通用附件路由（A1-A6，契约见
// docs/plan/generic-attachment-richtext-control-plan.md §3.2）。
//
// 权限口径（§4.2/§4.3）：
//   - 通用路由静态声明使用兜底码 attachment:read/write/delete（P0-3 已登记并绑定
//     系统管理员/租户管理员），同时满足权限码三条件中的「至少一个路由引用」；
//   - 宿主资源维度（ticket / knowledge / ...）由 handlers/attachment 按 biz_type 动态
//     复核 §4.2 权威表，通用路由不会因持有兜底码而放大成宿主越权；
//   - 域内别名路由（BE-5 knowledge/service_request、BE-6 旧工单端点）静态声明各自宿主
//     权限码，静态闸门即最终口径，不依赖兜底码。
//
// 成功响应一律 HTTP 200 + code=0（不引入 201/204，§3.4）。
func SetupAttachmentRoutes(r *gin.RouterGroup, h *attachmentHandler.Handler) {
	attachments := r.Group("/attachments")
	{
		attachments.POST("", middleware.RequirePermission("attachment", "write"), h.Upload)
		attachments.GET("", middleware.RequirePermission("attachment", "read"), h.List)
		// A6 必须在 /:id 之前注册同方法不同路径无冲突；批量回填属读动作。
		attachments.POST("/batch-query", middleware.RequirePermission("attachment", "read"), h.BatchQuery)
		attachments.GET("/:id", middleware.RequirePermission("attachment", "read"), h.Get)
		attachments.GET("/:id/content", middleware.RequirePermission("attachment", "read"), h.Download)
		attachments.DELETE("/:id", middleware.RequirePermission("attachment", "delete"), h.Delete)
	}
}
