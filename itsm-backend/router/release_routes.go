package router

import (
	attachmentHandler "itsm-backend/handlers/attachment"
	releaseHandler "itsm-backend/handlers/release"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// SetupReleaseRoutes 注册 Release（发布管理）域路由。
// 从 router.go 集中注册块抽取，行为与原内联等价。
// attachmentHandler 非空时挂载附件域内别名（富文本第三波，§3.2），权限复用 release 宿主码。
func SetupReleaseRoutes(tenant *gin.RouterGroup, h *releaseHandler.ReleaseHandler, attachmentHandler *attachmentHandler.Handler) {
	releases := tenant.Group("/releases")
	{
		releases.GET("", middleware.RequirePermission("release", "read"), h.ListReleases)
		releases.POST("", middleware.RequirePermission("release", "write"), h.CreateRelease)
		releases.GET("/stats", middleware.RequirePermission("release", "read"), h.GetReleaseStats)
		releases.GET("/:id", middleware.RequirePermission("release", "read"), h.GetRelease)
		releases.PUT("/:id", middleware.RequirePermission("release", "write"), h.UpdateRelease)
		releases.PUT("/:id/status", middleware.RequirePermission("release", "write"), h.UpdateReleaseStatus)
		releases.POST("/:id/approve", middleware.RequirePermission("release", "approve"), h.ApproveRelease)
		releases.POST("/:id/reject", middleware.RequirePermission("release", "approve"), h.RejectRelease)
		releases.POST("/:id/rollback", middleware.RequirePermission("release", "rollback"), h.RollbackRelease)
		releases.DELETE("/:id", middleware.RequirePermission("release", "delete"), h.DeleteRelease)

		// 附件域内别名（富文本第三波，BE-5 §3.2）：静态声明宿主权限码，处理体复用通用附件 A1/A2/A4/A5。
		if attachmentHandler != nil {
			releases.GET("/:id/attachments", middleware.RequirePermission("release", "read"), attachmentHandler.AliasList(service.AttachmentBizTypeRelease))
			releases.POST("/:id/attachments", middleware.RequirePermission("release", "write"), attachmentHandler.AliasUpload(service.AttachmentBizTypeRelease))
			releases.GET("/:id/attachments/:ref", middleware.RequirePermission("release", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeRelease, false))
			releases.GET("/:id/attachments/:ref/download", middleware.RequirePermission("release", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeRelease, false))
			releases.GET("/:id/attachments/:ref/preview", middleware.RequirePermission("release", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeRelease, true))
			releases.DELETE("/:id/attachments/:ref", middleware.RequirePermission("release", "delete"), attachmentHandler.AliasDelete(service.AttachmentBizTypeRelease))
		}
	}
}
