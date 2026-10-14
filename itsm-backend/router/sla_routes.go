package router

import (
	slaHandler "itsm-backend/handlers/sla"
	slaTemplateHandler "itsm-backend/handlers/sla_template"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupSLARoutes 注册 SLA 相关路由。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
func SetupSLARoutes(tenant *gin.RouterGroup, h *slaHandler.Handler) {
	slaGrp := tenant.Group("/sla")
	{
		slaGrp.GET("", middleware.RequirePermission("sla", "read"), h.ListSLADefinitions)
		// SLA Definitions
		slaGrp.GET("/stats", middleware.RequirePermission("sla", "read"), h.GetSLAStats)
		slaGrp.GET("/definitions", middleware.RequirePermission("sla", "read"), h.ListSLADefinitions)
		slaGrp.POST("/definitions", middleware.RequirePermission("sla", "write"), h.CreateSLADefinition)
		// customer 面只读：服务商下发定义（D-SLA-1；独立端点，不开列表跨租户读洞——方案 §4.1）
		slaGrp.GET("/definitions/provided", middleware.RequirePermission("sla", "read"), h.ListProvidedDefinitions)
		// 兼容旧路径：/sla/policies → /sla/definitions
		slaGrp.GET("/policies", middleware.RequirePermission("sla", "read"), h.ListSLADefinitions)
		slaGrp.POST("/policies", middleware.RequirePermission("sla", "write"), h.CreateSLADefinition)
		slaGrp.GET("/policies/:id", middleware.RequirePermission("sla", "read"), h.GetSLADefinition)
		slaGrp.PUT("/policies/:id", middleware.RequirePermission("sla", "write"), h.UpdateSLADefinition)
		slaGrp.DELETE("/policies/:id", middleware.RequirePermission("sla", "delete"), h.DeleteSLADefinition)

		// 兼容旧路径：/sla/monitor → /sla/monitoring
		slaGrp.POST("/monitor", middleware.RequirePermission("sla", "read"), h.GetSLAMonitoring)

		slaGrp.GET("/definitions/:id", middleware.RequirePermission("sla", "read"), h.GetSLADefinition)
		slaGrp.PUT("/definitions/:id", middleware.RequirePermission("sla", "write"), h.UpdateSLADefinition)
		slaGrp.DELETE("/definitions/:id", middleware.RequirePermission("sla", "delete"), h.DeleteSLADefinition)

		// SLA Alert Rules
		slaGrp.POST("/alert-rules", middleware.RequirePermission("sla", "write"), h.CreateAlertRule)
		slaGrp.GET("/alert-rules", middleware.RequirePermission("sla", "read"), h.ListAlertRules)
		slaGrp.GET("/alert-rules/:id", middleware.RequirePermission("sla", "read"), h.GetAlertRule)
		slaGrp.PUT("/alert-rules/:id", middleware.RequirePermission("sla", "write"), h.UpdateAlertRule)
		slaGrp.DELETE("/alert-rules/:id", middleware.RequirePermission("sla", "delete"), h.DeleteAlertRule)

		// SLA Metrics
		slaGrp.GET("/metrics", middleware.RequirePermission("sla", "read"), h.GetSLAMetrics)

		// SLA Violations
		slaGrp.GET("/violations", middleware.RequirePermission("sla", "read"), h.GetSLAViolations)
		slaGrp.PUT("/violations/:id", middleware.RequirePermission("sla", "write"), h.UpdateViolationStatus)
		// 违约裁决（D-SLA-8，方案 §13.1）：accepted/waived 归责终态需 sla:violation:settle
		slaGrp.PUT("/violations/:id/settle", middleware.RequirePermission("sla", "violation:settle"), h.SettleViolation)

		// SLA Monitoring
		slaGrp.POST("/monitoring", middleware.RequirePermission("sla", "read"), h.GetSLAMonitoring)
		// SLA 绩效按维度聚合（serviceType / priority），供监控大屏绩效表格使用
		slaGrp.GET("/performance", middleware.RequirePermission("sla", "read"), h.GetSLAPerformance)

		// SLA Compliance Check
		slaGrp.POST("/check-compliance/:ticketId", middleware.RequirePermission("sla", "read"), h.CheckSLACompliance)

		// SLA Alert History
		slaGrp.GET("/alert-history", middleware.RequirePermission("sla", "read"), h.GetAlertHistory)
		slaGrp.GET("/compliance-report", middleware.RequirePermission("sla", "read"), h.GetSLAComplianceReport)

		// SLA 定义下发授权（D-SLA-1，方案 §4.1）——provider 面：
		// 非 MSP 部署 404（RequireMSPDeploymentMode，canon §7 R12/I12）；MSP 身份 +
		// RBAC（sla:grant:read/write）+ 客户分配由 RequireMSPPermission 联合校验；
		// 归属一致性（customer.msp_provider_id == provider）在 repository 单事务内复核。
		grants := slaGrp.Group("/grants", middleware.RequireMSPDeploymentMode())
		{
			grants.POST("", middleware.RequireMSPPermission("sla", "grant:write"), h.CreateGrant)
			grants.GET("", middleware.RequireMSPPermission("sla", "grant:read"), h.ListGrants)
			grants.GET("/:id", middleware.RequireMSPPermission("sla", "grant:read"), h.GetGrant)
			grants.PUT("/:id", middleware.RequireMSPPermission("sla", "grant:write"), h.UpdateGrant)
			grants.DELETE("/:id", middleware.RequireMSPPermission("sla", "grant:write"), h.DeactivateGrant)
		}

		// MSP SLA 合同（D-SLA-4，方案 §4.4）——provider 面（写入口自 P1-2 起唯一化到本组）：
		// grant 写端点保留 410；合同链承载下发/条款/有效期/罚则。
		contracts := slaGrp.Group("/contracts", middleware.RequireMSPDeploymentMode())
		{
			contracts.POST("", middleware.RequireMSPPermission("sla", "contract:write"), h.CreateContract)
			contracts.GET("", middleware.RequireMSPPermission("sla", "contract:read"), h.ListContracts)
			contracts.GET("/:id", middleware.RequireMSPPermission("sla", "contract:read"), h.GetContract)
			contracts.PUT("/:id", middleware.RequireMSPPermission("sla", "contract:write"), h.UpdateContract)
		}

		// MSP SLA 结算报表（D-SLA-5/D-SLA-9，方案 §4.5/§13）——provider 面：
		// 白名单口径 + 双口径对账；客户归属校验在 service 取数前 fail-closed（SLA-A6）。
		settlement := slaGrp.Group("/settlement-report", middleware.RequireMSPDeploymentMode())
		{
			settlement.GET("", middleware.RequireMSPPermission("sla", "settlement:read"), h.GetSLASettlementReport)
			// 出快照锁定（§13.3）：锁定后该周期违约不可再变更（新争议走下一周期）。
			settlement.POST("/lock", middleware.RequireMSPPermission("sla", "violation:settle"), h.LockSettlementSnapshot)
		}
	}

	// SLA 模板（开箱即用预置模板）- 在 router.go 中单独处理 SLATemplateHandler.RegisterRoutes
}

// SetupSLATemplateRoutes 注册 SLA 模板相关路由。
func SetupSLATemplateRoutes(tenant *gin.RouterGroup, h *slaTemplateHandler.Handler) {
	h.RegisterRoutes(tenant)
}
