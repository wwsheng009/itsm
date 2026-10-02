package router

import (
	"github.com/gin-gonic/gin"

	"itsm-backend/middleware"
)

// SetupMSPRoutes 注册 MSP（托管服务商）跨租户路由。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
// 注意：MSP 跨租户访问，不需要租户中间件，直接挂在 /api/v1/msp 根组。
func SetupMSPRoutes(r *gin.Engine, config *RouterConfig) {
	if config.MSPHandler == nil {
		return
	}
	msp := r.Group("/api/v1/msp")
	msp.Use(middleware.AuthMiddleware(config.JWTSecret))
	msp.Use(middleware.RBACMiddleware(config.Client)) // 设置 client 到 context
	msp.Use(middleware.MSPMiddleware(config.Client))
	{
		// MSP 基础信息 - 允许 MSP 员工和管理员访问
		msp.GET("/status", config.MSPHandler.GetMSPStatus)
		msp.GET("/context", middleware.RequireMSPPermission("msp", "read"), config.MSPHandler.GetMSPContext)

		// 分配管理 - 需要 msp_allocation 权限
		msp.GET("/allocations", middleware.RequireMSPPermission("msp_allocation", "read"), config.MSPHandler.GetAllocations)
		msp.POST("/allocations", middleware.RequireMSPPermission("msp_allocation", "write"), config.MSPHandler.CreateAllocation)
		msp.POST("/allocations/deallocate", middleware.RequireMSPPermission("msp_allocation", "write"), config.MSPHandler.Deallocate)

		// 客户管理 - 需要 msp_customer 权限
		msp.GET("/customers", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.GetAllCustomers)
		msp.GET("/customers/:customer_tenant_id/tickets", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetCustomerTickets)
		// 建号通道（IP-P0-5）：MSP 管理员为分配客户建号；服务端二次校验 allocation（mspguard 唯一入口）。
		if config.UserHandler != nil {
			msp.POST("/customers/:customer_tenant_id/users", middleware.RequireMSPPermission("msp_customer", "write"), config.UserHandler.ProvisionUserToCustomer)
		}

		// 工单分配 - 需要 msp_ticket:write 权限
		msp.POST("/tickets/:id/assign", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.AssignMSPTechnician)

		// 跨客户工作台（IP-P0-7）：端点级 RBAC + 条目级授权链（服务端二次校验 allocation）。
		msp.GET("/workbench/tickets", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchTickets)
		msp.GET("/workbench/summary", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchSummary)
		// 批量操作（IP-P1-6）：≤100 条 + 低危动作白名单 + 逐条授权/审计 + 每租户限流。
		msp.POST("/workbench/batch", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.BatchWorkbench)
		msp.POST("/tickets/:id/reply", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.ReplyWorkbenchTicket)
		msp.POST("/tickets/:id/status", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.ChangeWorkbenchTicketStatus)
		// 自定义视图（IP-P2-4a）：保存过滤器组合 + 同 provider 分享（灰度 WORKBENCH_VIEWS_ENABLED，默认关）。
		msp.GET("/workbench/views", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchViews)
		msp.POST("/workbench/views", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.CreateWorkbenchView)
		msp.PUT("/workbench/views/:id", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.UpdateWorkbenchView)
		msp.DELETE("/workbench/views/:id", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.DeleteWorkbenchView)
		msp.POST("/workbench/views/:id/default", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.SetDefaultWorkbenchView)

		// 报表 - 需要 msp_report 权限
		msp.GET("/reports/customers", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetCustomerReports)
		msp.GET("/reports/performance", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetPerformanceReports)

		// 审计看板（IP-P1-8）：跨租户审计聚合 + 越权尝试/冲突告警面板。
		msp.GET("/audit/summary", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetAuditSummary)
	}
}
