package router

import (
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SetupCommonSystemRoutes 注册 Common & System 域路由（auth/me、users、groups、org、
// projects、applications、system、RBAC roles/permissions/menus、tenants、notifications）。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
// 需要 *gin.Engine 是因为 auth 子组挂在 /api/v1/auth（r 层）而非 tenant 层。
func SetupCommonSystemRoutes(r *gin.Engine, tenant *gin.RouterGroup, config *RouterConfig) {
	// PAT（个人访问令牌）管理面：身份自省面（仅 AuthMiddleware + 本人作用域）。
	// 设计：docs/plan/personal-access-token-plan-2026-10-16.md §3.9/D-PAT-10/13。
	// 独立注册（不依赖 CommonHandler）：PATHandler 非 nil 即注册（RouterConfig nil-guard 约定）。
	// 必须直接以完整子路径注册（守卫扫描器以字面 path 参数构造豁免键）；
	// 不可挂 auth 组（该组有 RBACMiddleware，自省面路径会被预检判 403）。
	// authGrp 无组级 CSRF/Tenant 中间件 → 写路由按 CSRFEnabled 显式挂 CSRF + RejectPATAuth；
	// 租户取值由 handler 读 c.GetInt("tenant_id")（AuthMiddleware 写入）。
	if config.PATHandler != nil {
		authGrp := r.Group("/api/v1/auth")
		patAuth := middleware.AuthMiddleware(config.JWTSecret)
		condCSRF := func() gin.HandlerFunc {
			if config.CSRFEnabled {
				return middleware.CSRFProtectionMiddleware(middleware.DefaultCSRFConfig())
			}
			return func(c *gin.Context) { c.Next() }
		}
		authGrp.GET("/personal-access-tokens", patAuth, config.PATHandler.List)
		// 写路由：RejectPATAuth 防自我繁殖（D-PAT-13）；条件 CSRF（浏览器会话）。
		authGrp.POST("/personal-access-tokens", patAuth, middleware.RejectPATAuth(), condCSRF(), config.PATHandler.Create)
		authGrp.POST("/personal-access-tokens/revoke-all", patAuth, middleware.RejectPATAuth(), condCSRF(), config.PATHandler.RevokeAll)
		authGrp.POST("/personal-access-tokens/:id/rotate", patAuth, middleware.RejectPATAuth(), condCSRF(), config.PATHandler.Rotate)
		authGrp.POST("/personal-access-tokens/:id/revoke", patAuth, middleware.RejectPATAuth(), condCSRF(), config.PATHandler.Revoke)
		authGrp.GET("/personal-access-tokens/:id/usages", patAuth, config.PATHandler.Usages)
		authGrp.GET("/security-events", patAuth, config.PATHandler.SecurityEvents)
	}

	if config.CommonHandler != nil {
		// Auth scoped
		// 修复 P0：auth/me, auth/tenants, auth/menus 不应在 tenant 分组内
		// 否则 tenant RBAC 中间件会拦截端用户，导致 layout 崩溃
		// 注：PAT（个人访问令牌）管理面在本函数上方独立注册（PATHandler 非 nil 即注册）。
		authGrp := r.Group("/api/v1/auth")
		{
			authGrp.GET("/me", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.GetMe)
			authGrp.GET("/tenants", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.GetUserTenants)
			authGrp.POST("/logout", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.Logout)
			// IP-P1-5 自助改密（首登强制改密收口；授权依据=持有旧密码）。
			authGrp.POST("/change-password", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.ChangePassword)
			if config.AuthHandler != nil {
				authGrp.POST("/switch-tenant", middleware.AuthMiddleware(config.JWTSecret), config.AuthHandler.SwitchTenant)
			}
		}

		// User Menu (no permission required, will be filtered by role)
		if config.RBACHandler != nil {
			authGrp.GET("/menus", middleware.AuthMiddleware(config.JWTSecret), config.RBACHandler.GetUserMenus)
		}

		// ==================== Audit Logs ====================
		SetupAuditLogRoutes(tenant, config.AuditLogHandler, config.CommonHandler)

		// Users
		if config.UserHandler != nil {
			users := tenant.Group("/users")
			{
				users.GET("", middleware.RequirePermission("user", "read"), config.UserHandler.ListUsers)
				users.POST("", middleware.RequirePermission("user", "write"), config.UserHandler.CreateUser)
				users.GET("/profile", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.GetMe) // 获取当前用户信息（需认证）
				users.GET("/me", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.GetMe)      // alias of /profile
				// 自助资料更新（IP-P1-6 补口，B3）：授权依据=已认证身份 + 仅写本人 users 行；
				// 字段白名单（name/email/phone）与邮箱唯一性由 handlers/common 收口。
				users.PUT("/me", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.UpdateMe)
				// 本人偏好（IP-P1-6c）：白名单键（workbenchFilter 等）；仅本人可读写。
				users.GET("/me/preferences", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.GetMyPreferences)
				users.PUT("/me/preferences", middleware.AuthMiddleware(config.JWTSecret), config.CommonHandler.UpdateMyPreferences)
				users.GET("/:id", middleware.RequirePermission("user", "read"), config.UserHandler.GetUser)
				users.PUT("/:id", middleware.RequirePermission("user", "write"), config.UserHandler.UpdateUser)
				users.DELETE("/:id", middleware.RequirePermission("user", "delete"), config.UserHandler.DeleteUser)
				users.PUT("/:id/status", middleware.RequirePermission("user", "write"), config.UserHandler.ChangeUserStatus)
				// 离职编排（IP-P2-8）：停用 + 全量会话失效 + 撤销全部分配 + 审计（幂等）。
				users.POST("/:id/offboard", middleware.RequirePermission("user", "write"), config.UserHandler.OffboardUser)
				users.PUT("/:id/reset-password", middleware.RequirePermission("user", "write"), config.UserHandler.ResetPassword)
				users.GET("/stats", middleware.RequirePermission("user", "read"), config.UserHandler.GetUserStats)
				users.POST("/batch", middleware.RequirePermission("user", "write"), config.UserHandler.BatchUpdateUsers)
			}
		} else {
			users := tenant.Group("/users")
			{
				users.GET("", middleware.RequirePermission("user", "read"), config.CommonHandler.ListUsers)
			}
		}

		// 邀请创建/列表/撤销（IP-P1-4b/4c §4.0-C；独立于 UserHandler 接线）
		if config.InvitationHandler != nil {
			invitations := tenant.Group("/users/invitations")
			{
				invitations.GET("", middleware.RequirePermission("user", "write"), config.InvitationHandler.List)
				invitations.POST("", middleware.RequirePermission("user", "write"), config.InvitationHandler.Create)
				invitations.POST("/:id/revoke", middleware.RequirePermission("user", "write"), config.InvitationHandler.Revoke)
			}
		}

		// Groups
		if config.GroupHandler != nil {
			groups := tenant.Group("/groups")
			{
				groups.GET("", middleware.RequirePermission("group", "read"), config.GroupHandler.ListGroups)
				groups.POST("", middleware.RequirePermission("group", "write"), config.GroupHandler.CreateGroup)
				groups.GET("/:id", middleware.RequirePermission("group", "read"), config.GroupHandler.GetGroup)
				groups.PUT("/:id", middleware.RequirePermission("group", "write"), config.GroupHandler.UpdateGroup)
				groups.DELETE("/:id", middleware.RequirePermission("group", "write"), config.GroupHandler.DeleteGroup)
				groups.POST("/:id/members", middleware.RequirePermission("group", "write"), config.GroupHandler.AddUserToGroup)
				groups.DELETE("/:id/members", middleware.RequirePermission("group", "write"), config.GroupHandler.RemoveUserFromGroup)
				groups.GET("/:id/members", middleware.RequirePermission("group", "read"), config.GroupHandler.GetGroupMembers)
			}
		}

		// Organization
		org := tenant.Group("/org")
		{
			org.GET("/departments/tree", middleware.RequirePermission("department", "read"), config.CommonHandler.GetDepartmentTree)
			org.GET("/departments/:id", middleware.RequirePermission("department", "read"), config.CommonHandler.GetDepartment)
			org.POST("/departments", middleware.RequirePermission("department", "create"), config.CommonHandler.CreateDepartment)
			org.PUT("/departments/:id", middleware.RequirePermission("department", "update"), config.CommonHandler.UpdateDepartment)
			org.DELETE("/departments/:id", middleware.RequirePermission("department", "delete"), config.CommonHandler.DeleteDepartment)
			org.GET("/teams", middleware.RequirePermission("team", "read"), config.CommonHandler.ListTeams)
			org.GET("/teams/:id", middleware.RequirePermission("team", "read"), config.CommonHandler.GetTeam)
			org.POST("/teams", middleware.RequirePermission("team", "write"), config.CommonHandler.CreateTeam)
			org.PUT("/teams/:id", middleware.RequirePermission("team", "write"), config.CommonHandler.UpdateTeam)
			org.DELETE("/teams/:id", middleware.RequirePermission("team", "write"), config.CommonHandler.DeleteTeam)
		}

		// Projects
		if config.ProjectHandler != nil {
			projects := tenant.Group("/projects")
			{
				projects.GET("", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListProjects)
				projects.POST("", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateProject)
				// P2（PE-24）：组合视图与度量（只读；与列表同过滤/同可见性，口径同源）。
				projects.GET("/portfolio", middleware.RequirePermission("project", "read"), config.ProjectHandler.Portfolio)
				projects.GET("/metrics", middleware.RequirePermission("project", "read"), config.ProjectHandler.Metrics)
				projects.GET("/:id", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetProject)
				// P2（PE-22）：任务工时汇总（estimate/actual 全量求和；可见性同详情）。
				projects.GET("/:id/hours-summary", middleware.RequirePermission("project", "read"), config.ProjectHandler.HoursSummary)
				projects.PUT("/:id/capacity", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateCapacity)
				projects.GET("/:id/resource-heatmap", middleware.RequirePermission("project", "read"), config.ProjectHandler.ResourceHeatmap)
				// A2 回归遗留：归口（ownerTeam）候选选择器（租户级字典，创建/编辑项目共用）。
				projects.GET("/owner-team-candidates", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListOwnerTeamCandidates)
				projects.PUT("/:id", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateProject)
				projects.DELETE("/:id", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteProject)
				// 状态流转专用端点（P0；complete 走结项门禁，见 docs/project-management/plan/p0-integration-contracts.md §3.3）
				projects.POST("/:id/status", middleware.RequirePermission("project", "write"), config.ProjectHandler.TransitionStatus)
				// 成员（PE-3；候选=目标租户活跃 membership，见 docs/project-management/04）
				projects.GET("/:id/members", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListMembers)
				projects.POST("/:id/members", middleware.RequirePermission("project", "write"), config.ProjectHandler.AddMember)
				projects.PATCH("/:id/members/:userId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateMemberRole)
				projects.DELETE("/:id/members/:userId", middleware.RequirePermission("project", "write"), config.ProjectHandler.RemoveMember)
				projects.GET("/:id/member-candidates", middleware.RequirePermission("project", "write"), config.ProjectHandler.MemberCandidates)
				// 任务（PE-4；WBS ≤3 层，见 docs/project-management/plan/p0-api-and-pages.md §1.3）
				projects.GET("/my-tasks", middleware.RequirePermission("project", "read"), config.ProjectHandler.MyTasks)
				// 个人工作台聚合（PE-16）：我的里程碑到期（跨可见项目；仅 planned）
				projects.GET("/my-milestones", middleware.RequirePermission("project", "read"), config.ProjectHandler.MyMilestones)
				projects.GET("/:id/tasks", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListTasks)
				projects.POST("/:id/tasks", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateTask)
				projects.GET("/:id/tasks/:taskId", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetTask)
				projects.PUT("/:id/tasks/:taskId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateTask)
				projects.DELETE("/:id/tasks/:taskId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteTask)
				projects.POST("/:id/tasks/:taskId/status", middleware.RequirePermission("project", "write"), config.ProjectHandler.TransitionTask)
				// 单据关联（PE-5；契约 §3.1/§3.2）
				projects.GET("/:id/records", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListProjectRecords)
				projects.POST("/:id/tasks/:taskId/tickets", middleware.RequirePermission("project", "write"), config.ProjectHandler.GenerateTaskTicket)
				projects.DELETE("/:id/tasks/:taskId/tickets", middleware.RequirePermission("project", "write"), config.ProjectHandler.UnbindTaskTicket)
				// P1（PE-14）：里程碑与任务依赖
				projects.GET("/:id/milestones", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListMilestones)
				projects.POST("/:id/milestones", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateMilestone)
				projects.PUT("/:id/milestones/:milestoneId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateMilestone)
				projects.POST("/:id/milestones/:milestoneId/status", middleware.RequirePermission("project", "write"), config.ProjectHandler.TransitionMilestone)
				projects.DELETE("/:id/milestones/:milestoneId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteMilestone)
				projects.GET("/:id/dependencies", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListDependencies)
				projects.POST("/:id/tasks/:taskId/dependencies", middleware.RequirePermission("project", "write"), config.ProjectHandler.AddDependency)
				projects.DELETE("/:id/tasks/:taskId/dependencies/:dependsOnTaskId", middleware.RequirePermission("project", "write"), config.ProjectHandler.RemoveDependency)
				// P1（PE-13）：计划基线（不可变快照 + 偏差对比）
				projects.GET("/:id/baselines", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListBaselines)
				projects.POST("/:id/baselines", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateBaseline)
				projects.GET("/:id/baselines/diff", middleware.RequirePermission("project", "read"), config.ProjectHandler.BaselineDiff)
				// P1（PE-12）：阶段门（四道门提交/裁决 + 门禁规则；business_type=project）
				projects.GET("/:id/gates", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListGates)
				projects.POST("/:id/gates/:gate/submit", middleware.RequirePermission("project", "write"), config.ProjectHandler.SubmitGate)
				projects.POST("/:id/gates/:gate/decision", middleware.RequirePermission("project", "write"), config.ProjectHandler.DecideGate)

				// PE-15 结项清单（复盘 KB 草稿/资产登记/成员回收/CSAT）。
				projects.GET("/:id/closure", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetClosure)
				projects.POST("/:id/closure/retro", middleware.RequirePermission("project", "write"), config.ProjectHandler.GenerateRetroDraft)
				projects.POST("/:id/closure/assets", middleware.RequirePermission("project", "write"), config.ProjectHandler.ConfirmClosureAssets)
				projects.POST("/:id/closure/members/recycle", middleware.RequirePermission("project", "write"), config.ProjectHandler.RecycleClosureMembers)
				projects.POST("/:id/closure/csat", middleware.RequirePermission("project", "write"), config.ProjectHandler.TriggerClosureCSAT)
				// P2（PE-26）：采购/资产结项登记（结构化台账链接；同租户弱引用 asset/asset_license/vendor/contract）。
				projects.GET("/:id/asset-links", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListAssetLinks)
				projects.POST("/:id/asset-links", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateAssetLink)
				projects.DELETE("/:id/asset-links/:linkId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteAssetLink)
				projects.GET("/:id/asset-link-candidates", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListAssetLinkCandidates)
				// P1（PE-11）：项目模板（CRUD + 一键生成；模板库为租户级配置）
				projects.GET("/templates", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListTemplates)
				projects.POST("/templates", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateTemplate)
				projects.GET("/templates/:templateId", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetTemplate)
				projects.PUT("/templates/:templateId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateTemplate)
				projects.DELETE("/templates/:templateId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteTemplate)
				projects.POST("/:id/templates/apply", middleware.RequirePermission("project", "write"), config.ProjectHandler.ApplyTemplate)
				// P1（PE-17）：状态报告模板与报表分组（模板库为租户级配置；报告按项目生成）。
				projects.GET("/report-templates", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListReportTemplates)
				projects.POST("/report-templates", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateReportTemplate)
				projects.GET("/report-templates/:templateId", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetReportTemplate)
				projects.PUT("/report-templates/:templateId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateReportTemplate)
				projects.DELETE("/report-templates/:templateId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteReportTemplate)
				projects.GET("/report-groups", middleware.RequirePermission("project", "read"), config.ProjectHandler.ReportGroups)
				projects.POST("/:id/reports/generate", middleware.RequirePermission("project", "read"), config.ProjectHandler.GenerateProjectReport)

				// P2（PE-21）：风险/问题台账（kind=risk|issue；可转问题单/变更）。
				projects.GET("/:id/risks", middleware.RequirePermission("project", "read"), config.ProjectHandler.ListRisks)
				projects.POST("/:id/risks", middleware.RequirePermission("project", "write"), config.ProjectHandler.CreateRisk)
				projects.GET("/:id/risks/:riskId", middleware.RequirePermission("project", "read"), config.ProjectHandler.GetRisk)
				projects.PUT("/:id/risks/:riskId", middleware.RequirePermission("project", "write"), config.ProjectHandler.UpdateRisk)
				projects.DELETE("/:id/risks/:riskId", middleware.RequirePermission("project", "write"), config.ProjectHandler.DeleteRisk)
				projects.POST("/:id/risks/:riskId/status", middleware.RequirePermission("project", "write"), config.ProjectHandler.TransitionRisk)
				projects.POST("/:id/risks/:riskId/convert", middleware.RequirePermission("project", "write"), config.ProjectHandler.ConvertRisk)

				// P2（PE-25）：项目 AI（周报摘要 / 风险预警 / 纪要转任务；只读派生 + 审计，
				// AI 不可用时降级返回 200，不阻塞主流程，见 plan/p2-ai-design.md D-44）。
				projects.POST("/:id/ai/summary", middleware.RequirePermission("project", "read"), config.ProjectHandler.GenerateAISummary)
				projects.POST("/:id/ai/risk-warnings", middleware.RequirePermission("project", "read"), config.ProjectHandler.GenerateAIRiskWarnings)
				projects.POST("/:id/ai/minutes/tasks", middleware.RequirePermission("project", "read"), config.ProjectHandler.ExtractAIMinutesTasks)
			}
		}

		// 单据 ↔ 项目挂接（PE-5）：域写码由本组路由声明，project:write 由服务层动态复核。
		if config.ProjectHandler != nil {
			tickets := tenant.Group("/tickets")
			// 单据→项目反向查询（详情页展示"所属项目"；可见性由服务层按项目谓词收窄，不可见返回 null）。
			tickets.GET("/:id/project", middleware.RequirePermission("ticket", "read"), config.ProjectHandler.GetTicketProject)
			tickets.POST("/:id/project", middleware.RequirePermission("ticket", "update"), config.ProjectHandler.AttachTicket)
			tickets.DELETE("/:id/project", middleware.RequirePermission("ticket", "update"), config.ProjectHandler.DetachTicket)

			incidents := tenant.Group("/incidents")
			incidents.GET("/:id/project", middleware.RequirePermission("incident", "read"), config.ProjectHandler.GetIncidentProject)
			incidents.POST("/:id/project", middleware.RequirePermission("incident", "write"), config.ProjectHandler.AttachIncident)
			incidents.DELETE("/:id/project", middleware.RequirePermission("incident", "write"), config.ProjectHandler.DetachIncident)

			problems := tenant.Group("/problems")
			problems.GET("/:id/project", middleware.RequirePermission("problem", "read"), config.ProjectHandler.GetProblemProject)
			problems.POST("/:id/project", middleware.RequirePermission("problem", "write"), config.ProjectHandler.AttachProblem)
			problems.DELETE("/:id/project", middleware.RequirePermission("problem", "write"), config.ProjectHandler.DetachProblem)

			changes := tenant.Group("/changes")
			changes.GET("/:id/project", middleware.RequirePermission("change", "read"), config.ProjectHandler.GetChangeProject)
			changes.POST("/:id/project", middleware.RequirePermission("change", "write"), config.ProjectHandler.AttachChange)
			changes.DELETE("/:id/project", middleware.RequirePermission("change", "write"), config.ProjectHandler.DetachChange)

			serviceRequests := tenant.Group("/service-requests")
			serviceRequests.GET("/:id/project", middleware.RequirePermission("service_request", "read"), config.ProjectHandler.GetServiceRequestProject)
			serviceRequests.POST("/:id/project", middleware.RequirePermission("service_request", "write"), config.ProjectHandler.AttachServiceRequest)
			serviceRequests.DELETE("/:id/project", middleware.RequirePermission("service_request", "write"), config.ProjectHandler.DetachServiceRequest)
		}

		// Applications
		if config.ApplicationHandler != nil {
			applications := tenant.Group("/applications")
			{
				applications.GET("", middleware.RequirePermission("application", "read"), config.ApplicationHandler.ListApplications)
				applications.POST("", middleware.RequirePermission("application", "write"), config.ApplicationHandler.CreateApplication)
				applications.GET("/microservices", middleware.RequirePermission("application", "read"), config.ApplicationHandler.ListMicroservices)
				applications.POST("/microservices", middleware.RequirePermission("application", "write"), config.ApplicationHandler.CreateMicroservice)
			}
		}

		sys := tenant.Group("/system")
		{
			sys.GET("/tags", middleware.RequirePermission("ticket_tag", "read"), config.CommonHandler.ListTags)
			sys.GET("/audit-logs", middleware.RequirePermission("audit", "read"), config.CommonHandler.GetAuditLogs)
		}

		// B7/Bug10: 顶层路由别名（兼容前端 /api/v1/{departments,teams,tags,admin/tenants} 旧路径）
		// 实际业务仍在 /org/* 与 /system/* 与 /tenants，旧路径保持只读 GET，避免破坏现有调用方
		{
			tenant.GET("/departments", middleware.RequirePermission("department", "read"), config.CommonHandler.ListDepartments)
			tenant.GET("/departments/tree", middleware.RequirePermission("department", "read"), config.CommonHandler.GetDepartmentTree)
			tenant.GET("/teams", middleware.RequirePermission("team", "read"), config.CommonHandler.ListTeams)
			tenant.GET("/tags", middleware.RequirePermission("ticket_tag", "read"), config.CommonHandler.ListTags)
		}
		if config.TenantHandler != nil {
			// 2026-10-10 收口：租户治理面为平台专属（同 IP-P2-7 口径）——tenant:read/write 会随
			// 内置角色/目录克隆被租户管理员（msp_provider/msp_customer/saas_* 的 admin）持有，
			// 仅靠 RequirePermission 无法守住平台面，需平台身份硬校验（super_admin/sysadmin）。
			admin := tenant.Group("/admin")
			{
				admin.GET("/tenants", middleware.RequirePlatformAdmin(), middleware.RequirePermission("tenant", "read"), config.TenantHandler.ListTenants)
			}
		}

		// Role & Permission Controllers (database-backed with tenant isolation)
		if config.RBACHandler != nil {
			roles := tenant.Group("/roles")
			{
				roles.GET("", middleware.RequirePermission("role", "read"), config.RBACHandler.ListRoles)
				roles.POST("", middleware.RequirePermission("role", "write"), config.RBACHandler.CreateRole)
				roles.GET("/:id", middleware.RequirePermission("role", "read"), config.RBACHandler.GetRole)
				roles.PUT("/:id", middleware.RequirePermission("role", "write"), config.RBACHandler.UpdateRole)
				roles.DELETE("/:id", middleware.RequirePermission("role", "delete"), config.RBACHandler.DeleteRole)
				roles.POST("/:id/permissions", middleware.RequirePermission("role", "write"), config.RBACHandler.AssignPermissions)
			}
		}

		if config.RBACHandler != nil {
			permissions := tenant.Group("/permissions")
			{
				permissions.GET("", middleware.RequirePermission("permission", "read"), config.RBACHandler.ListPermissions)
				// IP-P2-7 收口补丁 4：权限词表归平台治理——租户只读目录并分配既有码；
				// 若允许租户自造权限定义，即可铸出 system:* 等平台码并经角色授权穿过平台路由。
				permissions.POST("", middleware.RequirePlatformAdmin(), middleware.RequirePermission("role", "write"), config.RBACHandler.CreatePermission)
				permissions.POST("/init", middleware.RequirePlatformAdmin(), middleware.RequirePermission("role", "write"), config.RBACHandler.InitDefaultPermissions)
			}
		}

		// Menu Controllers (database-backed with tenant isolation)
		// 2026-10-10 治理域收口（设计 §8 D5）：菜单目录为平台结构资产（含 /admin/*、/msp 行）——
		// 整组挂 L1 + system:write；租户侧个性化菜单仍经 /auth/menus 按权限过滤下发，不受影响。
		if config.RBACHandler != nil {
			menus := tenant.Group("/menus", middleware.RequirePlatformAdmin(), middleware.RequirePermission("system", "write"))
			{
				menus.GET("", config.RBACHandler.ListMenus)
				menus.POST("", config.RBACHandler.CreateMenu)
				menus.GET("/:id", config.RBACHandler.GetMenu)
				menus.PUT("/:id", config.RBACHandler.UpdateMenu)
				menus.DELETE("/:id", config.RBACHandler.DeleteMenu)
				menus.POST("/init", config.RBACHandler.InitDefaultMenus)
			}
		}

		// Tenant Management（平台专属，2026-10-10 收口：非平台租户一律 403，含列表/详情/供给/建号）
		if config.TenantHandler != nil {
			tenants := tenant.Group("/tenants", middleware.RequirePlatformAdmin())
			{
				tenants.GET("", middleware.RequirePermission("tenant", "read"), config.TenantHandler.ListTenants)
				tenants.POST("", middleware.RequirePermission("tenant", "write"), config.TenantHandler.CreateTenant)
				tenants.GET("/:id", middleware.RequirePermission("tenant", "read"), config.TenantHandler.GetTenant)
				// IP-P2-6 收尾：治理页“配额 vs 用量”（口径同写入校验）。
				tenants.GET("/:id/usage", middleware.RequirePermission("tenant", "read"), config.TenantHandler.GetTenantUsage)
				tenants.PUT("/:id", middleware.RequirePermission("tenant", "write"), config.TenantHandler.UpdateTenant)
				tenants.DELETE("/:id", middleware.RequirePermission("tenant", "write"), config.TenantHandler.DeleteTenant)
				tenants.PUT("/:id/status", middleware.RequirePermission("tenant", "write"), config.TenantHandler.UpdateTenantStatus)
				// 租户激活通道：readiness（只读）+ 模板供给 + 首个管理员。
				tenants.GET("/:id/readiness", middleware.RequirePermission("tenant", "read"), config.TenantHandler.GetTenantReadiness)
				tenants.POST("/:id/provision", middleware.RequirePermission("tenant", "write"), config.TenantHandler.ProvisionTenant)
				tenants.POST("/:id/bootstrap-admin", middleware.RequirePermission("tenant", "write"), config.TenantHandler.CreateBootstrapAdmin)
				// 建号通道（IP-P0-5）：平台通道为任意 active 租户建号；通道授权与角色白名单由 service 收口。
				if config.UserHandler != nil {
					tenants.POST("/:id/users", middleware.RequirePermission("tenant", "write"), config.UserHandler.ProvisionUserToTenant)
					// IP-P2-7：建号表单角色候选（目标租户角色列表，数据驱动多选）。
					tenants.GET("/:id/roles", middleware.RequirePermission("tenant", "write"), config.UserHandler.ListTenantGrantableRoles)
				}
				// 平台侧租户用户管理（TUM-1/TUM-2）：读列表/详情 + 账号治理（重置密码/启停/强制下线）。
				// 路由权限仅粗筛；平台面硬判定（super_admin）由 service 收口（TUM-D1）。
				tenants.GET("/:id/users", middleware.RequirePermission("tenant", "read"), config.TenantHandler.ListTenantUsers)
				tenants.GET("/:id/users/:userId", middleware.RequirePermission("tenant", "read"), config.TenantHandler.GetTenantUser)
				tenants.POST("/:id/users/:userId/reset-password", middleware.RequirePermission("tenant", "write"), config.TenantHandler.ResetTenantUserPassword)
				tenants.PUT("/:id/users/:userId/status", middleware.RequirePermission("tenant", "write"), config.TenantHandler.SetTenantUserStatus)
				tenants.POST("/:id/users/:userId/force-logout", middleware.RequirePermission("tenant", "write"), config.TenantHandler.ForceLogoutTenantUser)
				// MSW-3：映射撤销面（home 不可撤销；allocation 级联解除分配；缓存失效 + 在途 refresh 吊销）。
				tenants.POST("/:id/users/:userId/membership/revoke", middleware.RequirePermission("tenant", "write"), config.TenantHandler.RevokeTenantUserMembership)
				// IP-P2-8 D3=B：平台侧离职编排（停用 + 全量会话失效 + 撤销全部分配；幂等可重跑）。
				tenants.POST("/:id/users/:userId/offboard", middleware.RequirePermission("tenant", "write"), config.TenantHandler.OffboardTenantUser)
			}
		}

		// Notification Preferences
		config.Logger.Info("NotificationHandler check:", zap.Any("handler", config.NotificationHandler))
		if config.NotificationHandler != nil {
			config.Logger.Info("Registering notification-preferences routes")
			notifPrefs := tenant.Group("/notification-preferences")
			{
				notifPrefs.GET("", middleware.RequirePermission("notification", "read"), config.NotificationHandler.ListPreferences)
				notifPrefs.GET("/event-types", middleware.RequirePermission("notification", "read"), config.NotificationHandler.ListEventTypes)
				notifPrefs.GET("/:event_type", middleware.RequirePermission("notification", "read"), config.NotificationHandler.GetPreference)
				notifPrefs.POST("", middleware.RequirePermission("notification", "create"), config.NotificationHandler.CreateOrUpdatePreference)
				notifPrefs.PUT("", middleware.RequirePermission("notification", "write"), config.NotificationHandler.BulkUpdatePreferences)
				notifPrefs.DELETE("/:event_type", middleware.RequirePermission("notification", "write"), config.NotificationHandler.DeletePreference)
				notifPrefs.POST("/reset", middleware.RequirePermission("notification", "write"), config.NotificationHandler.ResetPreferences)
				notifPrefs.POST("/init", middleware.RequirePermission("notification", "create"), config.NotificationHandler.InitializeDefaultPreferences)
			}
		}

		// Notifications
		if config.NotificationHandler != nil {
			notifications := tenant.Group("/notifications")
			{
				notifications.GET("", middleware.RequirePermission("notification", "read"), config.NotificationHandler.GetNotifications)
				notifications.GET("/unread-count", middleware.RequirePermission("notification", "read"), config.NotificationHandler.GetUnreadCount)
				notifications.PUT("/:id/read", middleware.RequirePermission("notification", "write"), config.NotificationHandler.MarkNotificationRead)
				notifications.PUT("/read-all", middleware.RequirePermission("notification", "write"), config.NotificationHandler.MarkAllNotificationsRead)
				notifications.PUT("/batch/read", middleware.RequirePermission("notification", "write"), config.NotificationHandler.MarkNotificationsRead)
				notifications.DELETE("/batch", middleware.RequirePermission("notification", "write"), config.NotificationHandler.DeleteNotifications)
				notifications.DELETE("/:id", middleware.RequirePermission("notification", "write"), config.NotificationHandler.DeleteNotification)
				notifications.POST("", middleware.RequirePermission("notification", "create"), config.NotificationHandler.CreateNotification)
			}
		}
	}
}
