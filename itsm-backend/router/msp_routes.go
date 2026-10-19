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
		// 分配历史（含已解除记录）：补齐前端 /msp 页已调用端点（target-architecture P1 契约漂移修复）。
		msp.GET("/allocations/history", middleware.RequireMSPPermission("msp_allocation", "read"), config.MSPHandler.GetAllocationHistory)
		msp.POST("/allocations", middleware.RequireMSPPermission("msp_allocation", "write"), config.MSPHandler.CreateAllocation)
		msp.POST("/allocations/deallocate", middleware.RequireMSPPermission("msp_allocation", "write"), config.MSPHandler.Deallocate)

		// 客户管理 - 需要 msp_customer 权限
		msp.GET("/customers", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.GetAllCustomers)
		msp.GET("/customers/:customer_tenant_id/tickets", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetCustomerTickets)
		// 建号通道（IP-P0-5）：MSP 管理员为分配客户建号；服务端二次校验 allocation（mspguard 唯一入口）。
		if config.UserHandler != nil {
			msp.POST("/customers/:customer_tenant_id/users", middleware.RequireMSPPermission("msp_customer", "write"), config.UserHandler.ProvisionUserToCustomer)
			// IP-P2-7：MSP 建号表单角色候选（目标客户角色列表，数据驱动多选）。
			msp.GET("/customers/:customer_tenant_id/roles", middleware.RequireMSPPermission("msp_customer", "write"), config.UserHandler.ListCustomerGrantableRoles)
		}
		// 模板分发（P2-①）：provider 已发布模板 → 客户租户草稿副本（copy-in + 来源追踪 + 升级提示）。
		msp.GET("/customers/:customer_tenant_id/workflow-templates", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.ListCustomerWorkflowTemplates)
		msp.POST("/customers/:customer_tenant_id/workflow-templates/:key/distribute", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.DistributeWorkflowTemplate)
		// 工单类型受控下发（D-TT-1，方案 §4.5a，P2-3）：copy-in + 受控锁 + 发布同步 + 解绑转 native。
		// 授权 = msp_customer read/write ∩ mspguard（分配 + 客户 active，服务端二次校验）。
		msp.POST("/customers/:customer_tenant_id/ticket-types/:id/distribute", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.DistributeTicketType)
		msp.GET("/customers/:customer_tenant_id/ticket-types/distributions", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.ListTicketTypeDistributions)
		msp.GET("/customers/:customer_tenant_id/ticket-types/distributions/:distId/preview", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.PreviewTicketTypeDistribution)
		msp.POST("/customers/:customer_tenant_id/ticket-types/distributions/:distId/publish", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.PublishTicketTypeDistribution)
		msp.POST("/customers/:customer_tenant_id/ticket-types/distributions/:distId/unbind", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.UnbindTicketTypeDistribution)

		// P2 帮助空间（provider 自有帮助库）：读 msp:read / 写 msp:write；draft → publish，仅已发布可分发。
		msp.GET("/help/docs", middleware.RequireMSPPermission("msp", "read"), config.MSPHandler.ListHelpDocs)
		msp.POST("/help/docs", middleware.RequireMSPPermission("msp", "write"), config.MSPHandler.CreateHelpDoc)
		msp.PUT("/help/docs/:id", middleware.RequireMSPPermission("msp", "write"), config.MSPHandler.UpdateHelpDoc)
		msp.POST("/help/docs/:id/publish", middleware.RequireMSPPermission("msp", "write"), config.MSPHandler.PublishHelpDoc)
		msp.DELETE("/help/docs/:id", middleware.RequireMSPPermission("msp", "write"), config.MSPHandler.DeleteHelpDoc)
		// P2 帮助分发授权（copy-in + 来源追踪 + 升级提示/解绑转原生）：
		// 授权 = msp_customer read/write ∩ mspguard（allocation + 客户 active，服务端二次校验）。
		msp.GET("/customers/:customer_tenant_id/help/distributions", middleware.RequireMSPPermission("msp_customer", "read"), config.MSPHandler.ListHelpDistributions)
		msp.POST("/customers/:customer_tenant_id/help/docs/:id/distribute", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.DistributeHelpDoc)
		msp.POST("/customers/:customer_tenant_id/help/distributions/:distId/upgrade", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.UpgradeHelpDistribution)
		msp.POST("/customers/:customer_tenant_id/help/distributions/:distId/unbind", middleware.RequireMSPPermission("msp_customer", "write"), config.MSPHandler.UnbindHelpDistribution)

		// 工单分配 - 需要 msp_ticket:write 权限
		msp.POST("/tickets/:id/assign", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.AssignMSPTechnician)

		// 跨客户工作台（IP-P0-7）：端点级 RBAC + 条目级授权链（服务端二次校验 allocation）。
		msp.GET("/workbench/tickets", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchTickets)
		msp.GET("/workbench/summary", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchSummary)
		// 批量操作（IP-P1-6）：≤100 条 + 低危动作白名单 + 逐条授权/审计 + 每租户限流。
		msp.POST("/workbench/batch", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.BatchWorkbench)
		msp.POST("/tickets/:id/reply", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.ReplyWorkbenchTicket)
		msp.POST("/tickets/:id/status", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.ChangeWorkbenchTicketStatus)
		// 跨租户详情（P1）：只读；声明租户 + allocation 双重校验，镜像列表可见范围。
		msp.GET("/tickets/:id", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchTicket)
		// 评论只读面（P2）：公开评论双向互通；客户内部备注不下发（XFD8）。
		msp.GET("/tickets/:id/comments", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchTicketComments)
		// 自定义视图（IP-P2-4a）：保存过滤器组合 + 同 provider 分享（灰度 WORKBENCH_VIEWS_ENABLED，默认关）。
		msp.GET("/workbench/views", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchViews)
		msp.POST("/workbench/views", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.CreateWorkbenchView)
		msp.PUT("/workbench/views/:id", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.UpdateWorkbenchView)
		msp.DELETE("/workbench/views/:id", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.DeleteWorkbenchView)
		msp.POST("/workbench/views/:id/default", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.SetDefaultWorkbenchView)

		// 跨租户任务投影（P2-② / IP-P2-11）：待办面 + 写回。
		// 授权链 = allocation（服务端允许集合）∩ 客户租户 RBAC task:read/update（C2 基线）∩ 分配规则；
		// 写回在客户租户内执行（engine + tenantctx=客户），逐条审计 source=workbench。
		msp.GET("/workbench/tasks", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchTasks)
		msp.POST("/workbench/tasks/:taskId/claim", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.ClaimWorkbenchTask)
		msp.POST("/workbench/tasks/:taskId/complete", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.CompleteWorkbenchTask)

		// 变更/问题/服务请求接入工作台（P2-③ / IP-P2-12）：读面（聚合列表 + 单条详情）。
		// 授权链 = 端点级 msp_ticket:read ∩ 服务端允许集合（allocation）∩ 声明租户一致性；
		// 条目级写面（状态迁移/回复/审批）随 IP-P2-13。
		msp.GET("/workbench/changes", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchChanges)
		msp.GET("/workbench/problems", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchProblems)
		msp.GET("/workbench/service-requests", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchServiceRequests)
		// 跨客户项目只读总览（P2+ MSP 客户交付 P2-A）。
		msp.GET("/workbench/projects", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.ListWorkbenchProjects)
		// 协助单据总览（MSP dashboard 第二排 KPI）：事件/问题/已知错误/变更/服务请求 计数 + 最近条目。
		msp.GET("/workbench/records-summary", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchRecordsSummary)
		msp.GET("/changes/:id", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchChange)
		msp.GET("/problems/:id", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchProblem)
		msp.GET("/service-requests/:id", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchServiceRequest)
		// 项目详情（只读；审批/状态推进保留客户治理面）。
		msp.GET("/projects/:id", middleware.RequireMSPPermission("msp_ticket", "read"), config.MSPHandler.GetWorkbenchProject)

		// 写面（IP-P2-13 问题 / IP-P2-15 变更·服务请求）：处理类动作（D8）；
		// 审批类（变更 approve/reject、服务请求 approval）保留客户治理面，不注册 MSP 端点。
		msp.POST("/problems/:id/status", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.UpdateWorkbenchProblemStatus)
		msp.POST("/changes/:id/status", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.UpdateWorkbenchChangeStatus)
		msp.POST("/service-requests/:id/status", middleware.RequireMSPPermission("msp_ticket", "write"), config.MSPHandler.UpdateWorkbenchServiceRequestStatus)

		// 报表 - 需要 msp_report 权限
		msp.GET("/reports/customers", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetCustomerReports)
		msp.GET("/reports/performance", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetPerformanceReports)

		// 审计看板（IP-P1-8）：跨租户审计聚合 + 越权尝试/冲突告警面板。
		msp.GET("/audit/summary", middleware.RequireMSPPermission("msp_report", "read"), config.MSPHandler.GetAuditSummary)
	}
}
