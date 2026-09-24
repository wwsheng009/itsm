package router

import (
	attachmentHandler "itsm-backend/handlers/attachment"
	changeHandler "itsm-backend/handlers/change"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// SetupChangeRoutes 注册变更管理相关路由。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
// attachmentHandler 非空时挂载附件域内别名（富文本第二波，§3.2），权限复用 change 宿主码。
func SetupChangeRoutes(tenant *gin.RouterGroup, h *changeHandler.Handler, attachmentHandler *attachmentHandler.Handler) {
	changes := tenant.Group("/changes")
	{
		changes.GET("", middleware.RequirePermission("change", "read"), h.ListChanges)
		changes.POST("", middleware.RequirePermission("change", "write"), h.CreateChange)
		changes.GET("/stats", middleware.RequirePermission("change", "read"), h.GetStats)
		changes.GET("/:id", middleware.RequirePermission("change", "read"), h.GetChange)
		changes.PUT("/:id", middleware.RequirePermission("change", "write"), h.UpdateChange)
		changes.DELETE("/:id", middleware.RequirePermission("change", "delete"), h.DeleteChange)
		changes.POST("/:id/submit", middleware.RequirePermission("change", "write"), h.SubmitChange)
		changes.POST("/:id/assign", middleware.RequirePermission("change", "write"), h.AssignChange)
		// 状态转换：approve/reject 需要独立审批权限，rollback 需要独立回滚权限（H-15 修复：禁止 write 权限泛化为审批/回滚）
		changes.POST("/:id/approve", middleware.RequirePermission("change", "approve"), h.TransitionStatus)
		changes.POST("/:id/reject", middleware.RequirePermission("change", "approve"), h.TransitionStatus)
		changes.POST("/:id/schedule", middleware.RequirePermission("change", "write"), h.TransitionStatus)
		changes.POST("/:id/start", middleware.RequirePermission("change", "write"), h.TransitionStatus)
		changes.POST("/:id/complete", middleware.RequirePermission("change", "write"), h.TransitionStatus)
		changes.POST("/:id/close", middleware.RequirePermission("change", "write"), h.TransitionStatus)
		changes.POST("/:id/rollback", middleware.RequirePermission("change", "rollback"), h.TransitionStatus)
		changes.POST("/:id/cancel", middleware.RequirePermission("change", "write"), h.TransitionStatus)
		// 审批
		changes.GET("/:id/approvals", middleware.RequirePermission("change", "read"), h.GetApprovals)
		changes.POST("/:id/approvals", middleware.RequirePermission("change", "write"), h.SubmitApproval)
		changes.GET("/:id/approval-summary", middleware.RequirePermission("change", "read"), h.GetApprovalSummary)
		// 风险评估（同时支持 /risk 和 /risk-assessment 两个路径）
		changes.GET("/:id/risk-assessment", middleware.RequirePermission("change", "read"), h.GetRiskAssessment)
		changes.GET("/:id/risk", middleware.RequirePermission("change", "read"), h.GetRiskAssessment)
		changes.PUT("/:id/risk", middleware.RequirePermission("change", "write"), h.UpdateRisk)
		changes.GET("/:id/cmdb-impact", middleware.RequirePermission("change", "read"), h.GetCMDBImpactSummary)
		// 日历视图
		changes.GET("/calendar", middleware.RequirePermission("change", "read"), h.GetCalendar)
		// PIR (Post-Implementation Review)
		changes.GET("/pirs", middleware.RequirePermission("change", "read"), h.ListPIRs)
		changes.GET("/:id/pir", middleware.RequirePermission("change", "read"), h.GetPIR)
		changes.POST("/:id/pir", middleware.RequirePermission("change", "write"), h.CreatePIR)
		changes.PUT("/pir/:id", middleware.RequirePermission("change", "write"), h.UpdatePIR)
		changes.DELETE("/pir/:id", middleware.RequirePermission("change", "delete"), h.DeletePIR)

		// 附件域内别名（富文本第二波，BE-5 §3.2）：静态声明宿主权限码，处理体复用通用附件 A1/A2/A4/A5。
		if attachmentHandler != nil {
			changes.GET("/:id/attachments", middleware.RequirePermission("change", "read"), attachmentHandler.AliasList(service.AttachmentBizTypeChange))
			changes.POST("/:id/attachments", middleware.RequirePermission("change", "write"), attachmentHandler.AliasUpload(service.AttachmentBizTypeChange))
			changes.GET("/:id/attachments/:ref", middleware.RequirePermission("change", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeChange, false))
			changes.GET("/:id/attachments/:ref/download", middleware.RequirePermission("change", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeChange, false))
			changes.GET("/:id/attachments/:ref/preview", middleware.RequirePermission("change", "read"), attachmentHandler.AliasDownload(service.AttachmentBizTypeChange, true))
			changes.DELETE("/:id/attachments/:ref", middleware.RequirePermission("change", "delete"), attachmentHandler.AliasDelete(service.AttachmentBizTypeChange))
		}
	}
}
