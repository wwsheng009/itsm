package router

import (
	"github.com/gin-gonic/gin"

	attachmentHandler "itsm-backend/handlers/attachment"
	"itsm-backend/handlers/provisioning"
	"itsm-backend/handlers/service_request"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// SetupServiceRequestRoutes 注册服务请求域路由。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
// 注意：provisioning 任务子路由随同搬移，保持 /service-requests 与
// /provisioning-tasks 两条路径的注册顺序不变。
// attachmentHandler 非空时挂载附件域内别名（BE-5，§3.2），权限复用 service_request 宿主码。
func SetupServiceRequestRoutes(tenant *gin.RouterGroup, h *service_request.Handler, provisioningHandler *provisioning.Handler, attachmentHandler *attachmentHandler.Handler) {
	sr := tenant.Group("/service-requests")
	{
		sr.POST("", middleware.RequirePermission("service_request", "write"), h.Create)
		sr.GET("", middleware.RequirePermission("service_request", "read"), h.List)
		sr.GET("/me", middleware.RequirePermission("service_request", "read"), h.List)
		sr.GET("/approvals/pending", middleware.RequirePermission("service_request", "read"), h.ListPending)
		sr.GET("/:id", middleware.RequirePermission("service_request", "read"), h.Get)
		sr.GET("/:id/approvals", middleware.RequirePermission("service_request", "read"), h.ListApprovals)
		sr.PUT("/:id", middleware.RequirePermission("service_request", "write"), h.Update)
		sr.PUT("/:id/status", middleware.RequirePermission("service_request", "write"), h.UpdateStatus)
		sr.DELETE("/:id", middleware.RequirePermission("service_request", "delete"), h.Delete)
		sr.POST("/:id/approval", middleware.RequirePermission("service_request", "approve"), h.ApplyApproval)
		sr.POST("/:id/approvals", middleware.RequirePermission("service_request", "approve"), h.ApplyApproval)

		// Provisioning routes
		if provisioningHandler != nil {
			sr.POST("/:id/provision", middleware.RequirePermission("service_request", "write"), provisioningHandler.StartProvisioning)
			sr.GET("/:id/provisioning-tasks", middleware.RequirePermission("service_request", "read"), provisioningHandler.ListProvisioningTasks)
		}

		// 附件域内别名（BE-5，§3.2）：静态声明宿主权限码，处理体复用通用附件 A1/A2/A4/A5。
		if attachmentHandler != nil {
			sr.GET("/:id/attachments", middleware.RequirePermission("service_request", "read"), attachmentHandler.AliasList(service.AttachmentBizTypeServiceRequest))
			sr.POST("/:id/attachments", middleware.RequirePermission("service_request", "write"), attachmentHandler.AliasUpload(service.AttachmentBizTypeServiceRequest))
			sr.GET("/:id/attachments/:ref", middleware.RequirePermission("service_request", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeServiceRequest, false))
			sr.GET("/:id/attachments/:ref/download", middleware.RequirePermission("service_request", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeServiceRequest, false))
			sr.GET("/:id/attachments/:ref/preview", middleware.RequirePermission("service_request", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeServiceRequest, true))
			sr.DELETE("/:id/attachments/:ref", middleware.RequirePermission("service_request", "delete"), attachmentHandler.AliasDelete(service.AttachmentBizTypeServiceRequest))
		}
	}

	// Provisioning task routes (separate path)
	provisioning := tenant.Group("/provisioning-tasks")
	{
		provisioning.POST("/:id/execute", middleware.RequirePermission("service_request", "write"), provisioningHandler.ExecuteProvisioningTask)
	}
}
