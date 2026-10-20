package router

import (
	aiHandler "itsm-backend/handlers/ai"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// SetupAIRoutes 注册 AI & Analytics 相关路由。
// 从 router.go 的集中注册块抽取而来，路由路径/方法/中间件与抽取前逐行一致。
func SetupAIRoutes(tenant *gin.RouterGroup, h *aiHandler.Handler) {
	aiGrp := tenant.Group("/ai")
	{
		aiGrp.POST("/chat", middleware.RequirePermission("ai", "read"), h.Chat)
		aiGrp.POST("/chat/stream", middleware.RequirePermission("ai", "read"), h.ChatStream)
		// T5-03a：AI 对话附件（用户级草稿附件；专用路由避免通用 attachment:* 兜底码）。
		aiGrp.POST("/attachments", middleware.RequirePermission("ai", "read"), h.UploadAIAttachment)
		aiGrp.GET("/attachments/:id/content", middleware.RequirePermission("ai", "read"), h.DownloadAIAttachment)
		aiGrp.GET("/conversations", middleware.RequirePermission("ai", "read"), h.ListConversations)
		aiGrp.GET("/conversations/:id", middleware.RequirePermission("ai", "read"), h.GetConversation)
		// I2-02 合规：会话导出（Markdown 快照；租户内只读）。
		aiGrp.GET("/conversations/:id/export", middleware.RequirePermission("ai", "read"), h.ExportConversation)
		// O1-01 条件项闭环：会话维度轨迹（纯问答无 run 同样可回放；只读）。
		aiGrp.GET("/conversations/:id/trace", middleware.RequirePermission("ai", "read"), h.GetConversationTrace)
		aiGrp.DELETE("/conversations/:id", middleware.RequirePermission("ai", "write"), h.DeleteConversation)
		aiGrp.GET("/analysis-results", middleware.RequirePermission("ai", "read"), h.ListAIAnalysisResults)
		aiGrp.GET("/analysis-results/:id", middleware.RequirePermission("ai", "read"), h.GetAIAnalysisResult)
		aiGrp.DELETE("/analysis-results/:id", middleware.RequirePermission("ai", "write"), h.DeleteAIAnalysisResult)
		// I0-04 Outcome Contract：对话产物详情（只读；租户 + 发起人收敛，供结果落点「打开」）。
		aiGrp.GET("/artifacts/:id", middleware.RequirePermission("ai", "read"), h.GetArtifact)
		aiGrp.POST("/analytics", middleware.RequirePermission("ai", "read"), h.GetDeepAnalytics)
		aiGrp.POST("/predictions", middleware.RequirePermission("ai", "read"), h.GetTrendPrediction)
		aiGrp.POST("/tickets/:id/analyze", middleware.RequirePermission("ai", "read"), h.AnalyzeTicket)
		// V0-01 工单「答复草稿」Inline（只读生成 + KB 引用；采纳/提交仍由页面完成）。
		aiGrp.POST("/tickets/:id/reply-draft",
			middleware.RequirePermission("ai", "read"),
			middleware.RequirePermission("ticket", "read"),
			h.ReplyDraft,
		)
		// V1-01 列表页 AI 批量（只读生成：批量分诊建议 / 批量答复草稿；
		// 采纳与分派由列表页逐行走既有写端点，绝不自动提交）。
		aiGrp.POST("/tickets/batch-triage",
			middleware.RequirePermission("ai", "read"),
			middleware.RequirePermission("ticket", "read"),
			h.BatchTriage,
		)
		aiGrp.POST("/tickets/batch-reply-drafts",
			middleware.RequirePermission("ai", "read"),
			middleware.RequirePermission("ticket", "read"),
			h.BatchReplyDrafts,
		)
		// V1-03 口语↔术语确认（只读理解：口语 → 分类/优先级 + 证据片段 + 置信度；
		// 确认/修改在前端完成，确认后才进入 A2UI 表单生成，不写任何业务字段）。
		aiGrp.POST("/tickets/interpret",
			middleware.RequirePermission("ai", "read"),
			middleware.RequirePermission("ticket", "read"),
			h.InterpretTicketIntent,
		)
		aiGrp.POST("/incidents/:id/analyze",
			middleware.RequirePermission("ai", "read"),
			middleware.RequirePermission("incident", "read"),
			h.AnalyzeIncident,
		)
		aiGrp.POST("/feedback", middleware.RequirePermission("ai", "write"), h.SaveFeedback)
		aiGrp.POST("/audit", middleware.RequirePermission("ai", "write"), h.RecordAudit)
		aiGrp.GET("/metrics", middleware.RequirePermission("ai", "read"), h.GetMetrics)
		// 调用报表与错误观测（租户级；不做预算/金额闸门）。
		aiGrp.GET("/llm-calls", middleware.RequirePermission("ai", "read"), h.GetLLMCalls)
		aiGrp.GET("/llm-calls/summary", middleware.RequirePermission("ai", "read"), h.GetLLMCallSummary)
		// B4-02：Bot 运行维度指标（成功/确认/verify/工具错误/时延/成本代理）。
		aiGrp.GET("/bot-metrics", middleware.RequirePermission("ai", "read"), h.GetBotMetrics)
		// AI 评估报告（按场景有用率 / 置信度校准 / 平台 LLM 统计）
		aiGrp.GET("/evaluation", middleware.RequirePermission("ai", "read"), h.GetEvaluation)
		// AI 审计日志（ai_audit 记录分页查询）
		aiGrp.GET("/audit-logs", middleware.RequirePermission("ai", "read"), h.GetAuditLogs)
		aiGrp.POST("/triage", middleware.RequirePermission("ai", "read"), h.Triage)
		// RAG endpoints
		// Bug fix (2026-08-15): handler KnowledgeSearch uses ShouldBindJSON
		// to read {query,limit,type} from a request body, but the route was
		// registered as GET. Gin would refuse to parse a body on GET, so the
		// endpoint always returned ParamError. Promote to POST to match the
		// handler's binding contract.
		aiGrp.POST("/rag/search", middleware.RequirePermission("ai", "read"), h.KnowledgeSearch)
		// AI 工单智能创建
		aiGrp.POST("/ticket/create", middleware.RequirePermission("ticket", "create"), h.CreateTicketByAI)
		// O0-02 观测性：AI Copilot 埋点接入与聚合（方案 §13.7；权限复用 ai:read）。
		aiGrp.POST("/telemetry/events", middleware.RequirePermission("ai", "read"), h.IngestTelemetryEvents)
		aiGrp.GET("/telemetry/summary", middleware.RequirePermission("ai", "read"), h.GetTelemetrySummary)
		// H0-03 帮助文档（Help Agent 数据面）：内嵌索引检索与页面帮助（权限复用 ai:read）。
		aiGrp.GET("/help/search", middleware.RequirePermission("ai", "read"), h.SearchHelp)
		aiGrp.GET("/help/page", middleware.RequirePermission("ai", "read"), h.GetPageHelp)
		// I0-04：帮助文档详情（来源引用可点击；仅读取内嵌索引中存在的文档）。
		aiGrp.GET("/help/doc", middleware.RequirePermission("ai", "read"), h.GetHelpDoc)
		// D1-01 助手上下文（服务端建议过滤：可见 Bot ∩ 工具授权 ∩ RBAC；未知 pageKey 正常降级）。
		aiGrp.GET("/assistant/context", middleware.RequirePermission("ai", "read"), h.GetAssistantContext)
		// H1-01 文档待办队列（账本缺口 + help_gap 遥测聚类；埋点未启用时降级为账本缺口）。
		aiGrp.GET("/help/todos", middleware.RequirePermission("ai", "read"), h.GetHelpTodos)
		// H1-01/H1-03/H1-04 条件项：待办认领/忽略状态持久化（claimed|ignored|open，幂等）。
		aiGrp.PUT("/help/todos/state", middleware.RequirePermission("ai", "write"), h.SetHelpTodoState)
		// H1-04 帮助覆盖率看板数据面（账本覆盖率 + 页面清单 + orphan 文档）。
		aiGrp.GET("/help/coverage", middleware.RequirePermission("ai", "read"), h.GetHelpCoverage)
		// T5-09a 帮助草稿审阅队列（知识库维护者；可跨发起人读同租户草稿）。
		aiGrp.GET("/help/drafts", middleware.RequirePermission("knowledge", "write"), h.ListHelpDrafts)
		// P2 文档命中率/解决率运营报表（遥测聚合 + 账本覆盖率；埋点未装配时降级为仅覆盖率）。
		aiGrp.GET("/observability/doc-metrics", middleware.RequirePermission("ai", "read"), h.GetDocMetrics)
		// O1-01 单任务全链路轨迹（run_id 回放：会话→run→步骤/事件→工具/确认→产物→业务对象）。
		aiGrp.GET("/runs/:id/trace", middleware.RequirePermission("ai", "read"), h.GetRunTrace)
		// O1-02 实时健康与告警（快照 + SLO；告警派发写面为 ai:write）。
		aiGrp.GET("/health", middleware.RequirePermission("ai", "read"), h.GetAIHealth)
		aiGrp.POST("/health/alerts/dispatch", middleware.RequirePermission("ai", "write"), h.DispatchAIHealthAlerts)
		// V0-04 试点对照报告 + §11.5 放量门槛（本期 vs 基线窗口；只读）。
		aiGrp.GET("/observability/pilot-report", middleware.RequirePermission("ai", "read"), h.GetPilotReport)
		// O2-01 分群对比（role | pageKey 维度；只读）。
		aiGrp.GET("/observability/segments", middleware.RequirePermission("ai", "read"), h.GetAssistantSegments)
		// P2 LLM 动态建议（缓存 + 审计；失败 fail-safe 回退静态基线）。
		aiGrp.GET("/assistant/suggestions/dynamic", middleware.RequirePermission("ai", "read"), h.GetDynamicSuggestions)
		// V2-02 异步精算（先给可用答案 + 后台精算完成后通知；只读生成，不写任何业务字段）。
		aiGrp.POST("/async/answers", middleware.RequirePermission("ai", "read"), h.StartAsyncAnswer)
		aiGrp.GET("/async/answers/:id", middleware.RequirePermission("ai", "read"), h.GetAsyncAnswer)
		// I2-01 Autopilot 监督面：运行列表 + 暂停（取消）/回滚（冻结待审批 + 通知中心）。
		aiGrp.GET("/runs", middleware.RequirePermission("ai", "read"), h.GetAutopilotRuns)
		aiGrp.POST("/runs/:id/cancel", middleware.RequirePermission("ai", "write"), h.CancelAutopilotRun)
		aiGrp.POST("/runs/:id/rollback", middleware.RequirePermission("ai", "write"), h.RollbackAutopilotRun)
	}

	agentGrp := tenant.Group("/agent")
	{
		agentGrp.GET("/tools", middleware.RequirePermission("ai", "read"), h.ListTools)
		// 工具目录查询（内置 + MCP 统一投影；Bot 授权选择器与独立工具目录页共用）：
		// GET /api/v1/agent/tools/catalog?q=&source=&readOnly=&risk=&limit=
		agentGrp.GET("/tools/catalog", middleware.RequirePermission("ai", "read"), h.ListToolCatalog)
		agentGrp.POST("/tools/execute", middleware.RequirePermission("ai", "read"), h.ExecuteTool)
		agentGrp.GET("/tools/:id", middleware.RequirePermission("ai", "read"), h.GetToolInvocation)
		// 审批人待办列表：GET /api/v1/agent/tools/invocations?state=pending
		agentGrp.GET("/tools/invocations", middleware.RequirePermission("ai", "read"), h.ListToolInvocations)
		agentGrp.POST("/tools/:id/approve", middleware.RequirePermission("ai", "write"), h.ApproveTool)
	}
}
