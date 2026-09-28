package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/handlers/common/knowledgeaccess"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListTools handles GET /api/v1/agent/tools
// P2-6: 按 ToolDefinition.Resource/Action 过滤，仅返回当前角色有权限的工具
func (h *Handler) ListTools(c *gin.Context) {
	role := c.GetString("role")
	tenantID := c.GetInt("tenant_id")

	if tenantID <= 0 || c.GetInt("user_id") <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}
	if h.svc.entClient == nil || h.svc.tools == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 工具权限服务未就绪")
		return
	}

	// 按租户动态化工具参数（list_cis 的 ci_type 枚举来自该租户 CIType 表）
	allTools := h.svc.ListToolsForTenant(c.Request.Context(), tenantID)

	visible := make([]service.ToolDefinition, 0, len(allTools))
	for _, t := range allTools {
		if middleware.HasResourcePermission(c.Request.Context(), h.svc.entClient, role, t.Resource, t.Action, tenantID) {
			visible = append(visible, t)
		}
	}
	common.Success(c, gin.H{"tools": visible})
}

// ExecuteTool handles POST /api/v1/agent/tools/execute
func (h *Handler) ExecuteTool(c *gin.Context) {
	var req struct {
		Name string                 `json:"name" binding:"required"`
		Args map[string]interface{} `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID <= 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	// P2-6: 校验工具存在性；写工具（!ReadOnly）交由 ExecuteTool 统一走审批流
	// （创建 pending invocation + 入队等待人工审批），与聊天路径行为一致。
	if h.svc.tools == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 工具注册表未就绪")
		return
	}
	// M0-09：解析含外部 provider（MCP）——与聊天路径同一解析入口。
	toolDef := h.svc.tools.GetToolForTenant(c.Request.Context(), tenantID, req.Name)
	if toolDef == nil {
		common.Fail(c, common.UnknownToolCode, "unknown tool: "+req.Name)
		return
	}

	if req.Args == nil {
		req.Args = map[string]interface{}{}
	}

	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.AuthFailed(c, "用户信息缺失")
		return
	}
	role := c.GetString("role")

	res, invocationID, err := h.svc.ExecuteTool(c.Request.Context(), userID, tenantID, role, req.Name, req.Args)
	if err != nil {
		if errors.Is(err, ErrToolUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "AI 工具权限服务未就绪")
			return
		}
		// P2-6: 区分权限拒绝与未知工具的错误码
		if errors.Is(err, ErrToolPermissionDenied) {
			common.Fail(c, common.ToolPermissionDeniedCode, "无权执行该工具")
			return
		}
		if errors.Is(err, ErrUnknownTool) {
			common.Fail(c, common.UnknownToolCode, "工具不存在")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	if invocationID > 0 {
		common.Success(c, dto.ToolExecutionResponse{
			Status: "pending", Summary: "工具操作已提交审批", InvocationID: invocationID,
			ApprovalState: "pending", NextActions: []string{"等待审批后执行"}, Artifacts: []string{},
		})
		return
	}
	common.Success(c, gin.H{
		"status":      "success",
		"summary":     "tool executed",
		"nextActions": []string{"If the result is incomplete, refine args and retry."},
		"artifacts":   []string{},
		"data":        res,
	})
}

// Chat handles POST /api/v1/ai/chat
func (h *Handler) Chat(c *gin.Context) {
	var req struct {
		Query          string `json:"query" binding:"required"`
		Limit          int    `json:"limit"`
		ConversationID int    `json:"conversationId"`
		// Provider 单次覆盖（BE-7，§3.4；P1 起面向全部 ai:read 使用者）：空 = 默认链。
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	role := c.GetString("role")
	if tenantID <= 0 || userID <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}

	provider := strings.TrimSpace(req.Provider)
	// §3.4（P1 演进）：provider 覆盖参数面向全部具备 ai:read 的使用者；越权显式失败（403），
	// 绝不静默忽略——静默忽略会让调用方以为指定实例已生效。
	if provider != "" && !h.canUseProviderOverride(c.Request.Context(), tenantID, role) {
		respondLLMAdminError(c, &LLMAdminError{
			Status:  http.StatusForbidden,
			Code:    "AI_PROVIDER_FORBIDDEN",
			Message: "无 AI 使用权限，不能指定 provider 实例",
		})
		return
	}

	// 注入知识访问者身份：RAG 检索据此做分类级可见性过滤（L0 权限边界）。
	// 不注入则按匿名处理，已纳管的受限分类一律不可见（fail-closed）。
	chatCtx := knowledgeaccess.WithViewer(c.Request.Context(), knowledgeaccess.Viewer{UserID: userID, Role: role})

	if provider == "" && !service.MultiProviderEnabled() {
		// 开关关闭且未显式覆盖：保持既有调用与响应形状（QA-3 零破坏门禁）。
		answers, convID, err := h.svc.Chat(chatCtx, tenantID, userID, req.Query, req.Limit, req.ConversationID)
		if err != nil {
			// RAG 失败时降级处理：返回空结果而非 500 错误，避免前端崩溃
			h.svc.logger.Warnw("AI Chat RAG 检索失败，返回降级响应", "error", err, "tenantID", tenantID)
			common.Success(c, gin.H{
				"answers":        []interface{}{},
				"conversationId": 0,
				"degraded":       true,
				"message":        "AI 服务暂时不可用，请稍后重试",
			})
			return
		}

		common.Success(c, gin.H{
			"answers":        answers,
			"conversationId": convID,
		})
		return
	}

	// 多 Provider 开启或显式覆盖：解析 provider 并回带 provider/providerSource（BE-7）。
	answers, resolution, convID, err := h.svc.ChatWithProviderInfo(chatCtx, tenantID, userID, req.Query, req.Limit, req.ConversationID, provider)
	if err != nil {
		if isProviderResolutionError(err) {
			// 显式覆盖的解析失败按 §3.4 契约可见地失败（404/409/422/503）。
			respondLLMAdminError(c, mapProviderResolutionError(err, provider))
			return
		}
		h.svc.logger.Warnw("AI Chat RAG 检索失败，返回降级响应", "error", err, "tenantID", tenantID)
		common.Success(c, gin.H{
			"answers":        []interface{}{},
			"conversationId": 0,
			"degraded":       true,
			"message":        "AI 服务暂时不可用，请稍后重试",
		})
		return
	}

	payload := gin.H{
		"answers":        answers,
		"conversationId": convID,
	}
	if resolution.Key != "" || resolution.Source != "" {
		payload["provider"] = resolution.Key
		payload["providerSource"] = resolution.Source
	}
	common.Success(c, payload)
}

// ChatStream handles POST /api/v1/ai/chat/stream and emits Server-Sent Events.
// Events:
//   - event: sources        data: [{objectType,id,title,snippet,score,...}]
//   - event: delta          data: {"content": "..."}
//   - event: tool_call_started  data: {id?,tool,provider,server?,phase,status:"started"}
//   - event: tool_call_finished data: {...,status:"done",summary,durationMs}
//   - event: tool_call_failed   data: {...,status:"failed",errorCode}
//   - event: approval_pending   data: {id,tool,provider,server?,phase:"write",status:"pending"}
//   - event: done           data: {"conversationId": <id>}（多 Provider 开启/显式覆盖时附加 provider/providerSource）
//   - event: error          data: {"message": "..."}（provider 解析失败时附加 errorCode，§3.4 契约）
//
// M1-03：工具事件为**叠加**语义——最终答案仍由 delta/done 承载；事件丢失时前端按最终消息降级渲染。
func (h *Handler) ChatStream(c *gin.Context) {
	var req struct {
		Query          string `json:"query" binding:"required"`
		Limit          int    `json:"limit"`
		ConversationID int    `json:"conversationId"`
		// Provider 单次覆盖（BE-7，§3.4；P1 起面向全部 ai:read 使用者）：空 = 默认链。
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	role := c.GetString("role")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	provider := strings.TrimSpace(req.Provider)
	if provider != "" && !h.canUseProviderOverride(c.Request.Context(), tenantID, role) {
		respondLLMAdminError(c, &LLMAdminError{
			Status:  http.StatusForbidden,
			Code:    "AI_PROVIDER_FORBIDDEN",
			Message: "无 AI 使用权限，不能指定 provider 实例",
		})
		return
	}
	useProviderInfo := provider != "" || service.MultiProviderEnabled()

	// SSE headers. Nginx-friendly: X-Accel-Buffering:no disables proxy buffering.
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		// Streaming not supported: fall back to a normal chat response so the
		// client still gets an answer.
		fallbackCtx := knowledgeaccess.WithViewer(c.Request.Context(), knowledgeaccess.Viewer{UserID: userID, Role: role})
		if !useProviderInfo {
			answers, convID, err := h.svc.Chat(fallbackCtx, tenantID, userID, req.Query, req.Limit, req.ConversationID)
			if err != nil {
				common.FailWithErr(c, err, "操作失败")
				return
			}
			common.Success(c, gin.H{"answers": answers, "conversationId": convID})
			return
		}
		answers, resolution, convID, err := h.svc.ChatWithProviderInfo(fallbackCtx, tenantID, userID, req.Query, req.Limit, req.ConversationID, provider)
		if err != nil {
			if isProviderResolutionError(err) {
				respondLLMAdminError(c, mapProviderResolutionError(err, provider))
				return
			}
			common.FailWithErr(c, err, "操作失败")
			return
		}
		payload := gin.H{"answers": answers, "conversationId": convID}
		if resolution.Key != "" || resolution.Source != "" {
			payload["provider"] = resolution.Key
			payload["providerSource"] = resolution.Source
		}
		common.Success(c, payload)
		return
	}

	writeEvent := func(event string, payload interface{}) {
		buf, err := json.Marshal(payload)
		if err != nil {
			return
		}
		// Multi-line data payloads must be prefixed on each line, but json.Marshal
		// produces a single line, so a single data: prefix is sufficient.
		_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, string(buf))
		flusher.Flush()
	}

	onSources := func(items []map[string]any) {
		writeEvent("sources", items)
	}
	onDelta := func(delta string) {
		writeEvent("delta", map[string]string{"content": delta})
	}
	// M1-03：工具事件（过程可见）。未知状态一律按 started 下发，保证事件不丢；
	// 旧客户端遇到未知事件名必须忽略（前端解析 default 分支），故叠加事件是向后兼容的。
	onTool := func(ev ToolStreamEvent) {
		writeToolEvent(writeEvent, ev)
	}

	// 注入访问者身份：AI 助手主链路，RAG 据此做知识分类可见性过滤（L0 权限边界）
	chatCtx := knowledgeaccess.WithViewer(c.Request.Context(), knowledgeaccess.Viewer{UserID: userID, Role: role})
	if !useProviderInfo {
		convID, _, err := h.svc.ChatStream(chatCtx, tenantID, userID, role, req.Query, req.Limit, req.ConversationID, onSources, onDelta, onTool)
		if err != nil {
			h.svc.logger.Warnw("AI ChatStream 失败", "error", err, "tenantID", tenantID)
			writeEvent("error", map[string]string{"message": err.Error()})
			return
		}
		writeEvent("done", map[string]int{"conversationId": convID})
		return
	}

	// BE-7：多 Provider 开启或显式覆盖——解析 provider 并把生效标注写进 done 事件；
	// 显式覆盖的解析失败在 SSE error 事件内可见地失败（带 errorCode），不回退默认 provider。
	resolution, convID, err := h.svc.ChatStreamWithProviderInfo(chatCtx, tenantID, userID, role, req.Query, req.Limit, req.ConversationID, provider, onSources, onDelta, onTool)
	if err != nil {
		h.svc.logger.Warnw("AI ChatStream 失败", "error", err, "tenantID", tenantID, "provider", provider)
		if isProviderResolutionError(err) {
			_, code, message := providerErrorContract(err, provider)
			writeEvent("error", map[string]string{"message": message, "errorCode": code})
			return
		}
		writeEvent("error", map[string]string{"message": err.Error()})
		return
	}
	done := map[string]any{"conversationId": convID}
	if resolution.Key != "" || resolution.Source != "" {
		done["provider"] = resolution.Key
		done["providerSource"] = resolution.Source
	}
	writeEvent("done", done)
}

// canUseProviderOverride 判定当前请求能否使用 provider 单次覆盖参数（BE-7 §3.4；P1 演进）：
// 与选择器读端点同权限面（ai:read），避免出现「端点看 A 权限、覆盖参数看 B 权限」的口径漂移；
// client 为 nil 时 fail-closed（super_admin 仍直通）。
func (h *Handler) canUseProviderOverride(ctx context.Context, tenantID int, role string) bool {
	if h == nil || h.svc == nil {
		return false
	}
	return middleware.HasAIReadPermission(ctx, h.svc.entClient, role, tenantID)
}

// isProviderResolutionError 判断错误是否来自 §3.3 解析链（§3.4 契约错误）。
func isProviderResolutionError(err error) bool {
	return errors.Is(err, service.ErrProviderNotFound) ||
		errors.Is(err, service.ErrProviderDisabled) ||
		errors.Is(err, service.ErrProviderKeyMissing) ||
		errors.Is(err, service.ErrProviderUnavailable)
}

// providerErrorContract 把解析链哨兵错误映射为 §3.4 契约（HTTP 状态 + 字符串码 + 消息）。
func providerErrorContract(err error, provider string) (int, string, string) {
	name := strings.TrimSpace(provider)
	if name == "" {
		name = "默认"
	}
	switch {
	case errors.Is(err, service.ErrProviderNotFound):
		return http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", fmt.Sprintf("provider %q 不存在或对本租户不可见", name)
	case errors.Is(err, service.ErrProviderDisabled):
		return http.StatusConflict, "AI_PROVIDER_DISABLED", fmt.Sprintf("provider %q 已禁用", name)
	case errors.Is(err, service.ErrProviderKeyMissing):
		return http.StatusUnprocessableEntity, "AI_PROVIDER_KEY_MISSING", fmt.Sprintf("provider %q 密钥缺失或解密失败", name)
	default:
		return http.StatusServiceUnavailable, "AI_PROVIDER_UNAVAILABLE", fmt.Sprintf("provider %q 不可用", name)
	}
}

// mapProviderResolutionError 将解析链错误包装为统一契约错误（复用管理 API 的响应封装）。
func mapProviderResolutionError(err error, provider string) error {
	status, code, message := providerErrorContract(err, provider)
	return &LLMAdminError{Status: status, Code: code, Message: message, Err: err}
}

// ListConversations handles GET /api/v1/ai/conversations
func (h *Handler) ListConversations(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	if c.GetInt("user_id") <= 0 || c.GetString("role") == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}
	userID := c.GetInt("user_id")
	convs, err := h.svc.ListConversations(c.Request.Context(), tenantID, userID)
	if err != nil {
		common.FailWithErr(c, err, "获取会话列表失败")
		return
	}
	common.Success(c, gin.H{"conversations": convs})
}

// GetConversation handles GET /api/v1/ai/conversations/:id
func (h *Handler) GetConversation(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ParamError(c, "无效的会话ID")
		return
	}
	messages, err := h.svc.GetConversationMessages(c.Request.Context(), id, tenantID)
	if err != nil {
		common.FailWithErr(c, err, "获取会话详情失败")
		return
	}
	common.Success(c, gin.H{"messages": messages})
}

// DeleteConversation handles DELETE /api/v1/ai/conversations/:id
func (h *Handler) DeleteConversation(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ParamError(c, "无效的会话ID")
		return
	}
	if err := h.svc.DeleteConversation(c.Request.Context(), id, tenantID); err != nil {
		common.FailWithErr(c, err, "删除会话失败")
		return
	}
	common.Success(c, nil)
}

// ListAIAnalysisResults handles GET /api/v1/ai/analysis-results
func (h *Handler) ListAIAnalysisResults(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	analysisType := c.Query("type")
	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	results, err := h.svc.ListAIAnalysisResults(c.Request.Context(), tenantID, analysisType, limit)
	if err != nil {
		common.FailWithErr(c, err, "获取分析历史失败")
		return
	}
	common.Success(c, gin.H{"results": results})
}

// GetAIAnalysisResult handles GET /api/v1/ai/analysis-results/:id
func (h *Handler) GetAIAnalysisResult(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ParamError(c, "无效的记录ID")
		return
	}
	result, err := h.svc.GetAIAnalysisResult(c.Request.Context(), id, tenantID)
	if err != nil {
		common.FailWithErr(c, err, "获取分析结果失败")
		return
	}
	common.Success(c, result)
}

// DeleteAIAnalysisResult handles DELETE /api/v1/ai/analysis-results/:id
func (h *Handler) DeleteAIAnalysisResult(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ParamError(c, "无效的记录ID")
		return
	}
	if err := h.svc.DeleteAIAnalysisResult(c.Request.Context(), id, tenantID); err != nil {
		common.FailWithErr(c, err, "删除分析结果失败")
		return
	}
	common.Success(c, nil)
}

// GetDeepAnalytics handles POST /api/v1/ai/analytics
func (h *Handler) GetDeepAnalytics(c *gin.Context) {
	var req dto.DeepAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	tenantID := c.GetInt("tenant_id")
	res, err := h.svc.GetDeepAnalytics(c.Request.Context(), &req, tenantID)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, res)
}

// GetTrendPrediction handles POST /api/v1/ai/predictions
func (h *Handler) GetTrendPrediction(c *gin.Context) {
	var req dto.TrendPredictionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	tenantID := c.GetInt("tenant_id")
	res, err := h.svc.GetTrendPrediction(c.Request.Context(), &req, tenantID)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, res)
}

// AnalyzeTicket handles POST /api/v1/ai/tickets/:id/analyze
func (h *Handler) AnalyzeTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid ticket id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")

	res, err := h.svc.AnalyzeTicketWithAudit(c.Request.Context(), id, tenantID, userID)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, res)
}

// AnalyzeIncident handles POST /api/v1/ai/incidents/:id/analyze.
func (h *Handler) AnalyzeIncident(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid incident id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")
	res, err := h.svc.AnalyzeIncidentWithAudit(c.Request.Context(), id, tenantID, userID)
	if err != nil {
		if errors.Is(err, service.ErrIncidentNotFound) {
			common.NotFoundWithErr(c, err, "事件不存在")
			return
		}
		if errors.Is(err, service.ErrAIAnalysisUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "AI 事件分析服务尚未就绪")
			return
		}
		common.FailWithErr(c, err, "事件分析失败")
		return
	}
	common.Success(c, res)
}

// SummarizeTicket handles GET /api/v1/ai/tickets/:id/summary
// B9: AI 工单总结 - 优先用 LLM，fallback 用字段拼接
func (h *Handler) SummarizeTicket(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid ticket id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	summary, err := h.svc.SummarizeTicket(c.Request.Context(), id, tenantID)
	if err != nil {
		h.svc.logger.Warnw("AI摘要失败，返回降级响应", "error", err, "ticketID", id)
		common.Success(c, gin.H{
			"degraded": true,
			"message":  "AI 摘要服务暂时不可用，请稍后重试",
			"summary":  "",
		})
		return
	}
	common.Success(c, summary)
}

// SaveFeedback handles POST /api/v1/ai/feedback
func (h *Handler) SaveFeedback(c *gin.Context) {
	var req struct {
		Kind     string  `json:"kind" binding:"required"`
		Query    string  `json:"query"`
		ItemType *string `json:"itemType"`
		ItemID   *int    `json:"itemId"`
		Useful   bool    `json:"useful" binding:"required"`
		Score    *int    `json:"score"`
		Notes    *string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	requestID := c.GetString("request_id")
	if requestID == "" {
		requestID = fmt.Sprintf("req_%d_%d", time.Now().Unix(), userID)
	}

	var itemTypeVal string
	if req.ItemType != nil {
		itemTypeVal = *req.ItemType
	}

	err := h.svc.SaveFeedback(c.Request.Context(), tenantID, userID, requestID, req.Kind, req.Query, itemTypeVal, req.ItemID, req.Useful, req.Score, req.Notes)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, gin.H{"message": "Feedback saved"})
}

// RecordAudit handles POST /api/v1/ai/audit.
// It records the GA AI trace contract without allowing the AI to auto-apply high-risk actions.
func (h *Handler) RecordAudit(c *gin.Context) {
	var req struct {
		Scenario      string                 `json:"scenario" binding:"required"`
		InputRef      string                 `json:"inputRef" binding:"required"`
		PromptVersion string                 `json:"promptVersion"`
		Model         string                 `json:"model"`
		Confidence    float64                `json:"confidence"`
		Suggestion    map[string]interface{} `json:"suggestion" binding:"required"`
		Accepted      bool                   `json:"accepted"`
		Notes         string                 `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")
	requestID := c.GetString("request_id")
	if requestID == "" {
		requestID = fmt.Sprintf("ai_audit_%d_%d", time.Now().Unix(), userID)
	}

	notePayload := map[string]interface{}{
		"promptVersion": req.PromptVersion,
		"model":         req.Model,
		"confidence":    req.Confidence,
		"suggestion":    req.Suggestion,
		"notes":         req.Notes,
	}
	noteBytes, _ := json.Marshal(notePayload)
	note := string(noteBytes)
	itemType := "ai_audit"
	score := int(req.Confidence * 100)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	if err := h.svc.SaveFeedback(c.Request.Context(), tenantID, userID, requestID, req.Scenario, req.InputRef, itemType, nil, req.Accepted, &score, &note); err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, gin.H{
		"requestId":     requestID,
		"scenario":      req.Scenario,
		"inputRef":      req.InputRef,
		"promptVersion": req.PromptVersion,
		"model":         req.Model,
		"confidence":    req.Confidence,
		"accepted":      req.Accepted,
	})
}

// GetEvaluation handles GET /api/v1/ai/evaluation.
// 输出 AI 评估报告：按场景的有用率、置信度校准、平台级 LLM 成功率/延迟。
func (h *Handler) GetEvaluation(c *gin.Context) {
	days := queryInt(c, "days", 30)
	tenantID := c.GetInt("tenant_id")
	report, err := h.svc.Evaluate(c.Request.Context(), tenantID, days)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, report)
}

// GetAuditLogs handles GET /api/v1/ai/audit-logs.
// 分页查询 AI 审计记录（item_type='ai_audit'），可按场景 kind 过滤。
func (h *Handler) GetAuditLogs(c *gin.Context) {
	page := queryInt(c, "page", 1)
	pageSize := queryInt(c, "pageSize", 20)
	days := queryInt(c, "days", 90)
	kind := c.Query("kind")
	tenantID := c.GetInt("tenant_id")
	entries, total, err := h.svc.ListAuditLogs(c.Request.Context(), tenantID, page, pageSize, kind, days)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, gin.H{"items": entries, "total": total, "page": page, "pageSize": pageSize})
}

// queryInt 解析正整数查询参数，非法/缺失时回退到默认值。
func queryInt(c *gin.Context, key string, def int) int {
	if v := c.Query(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// GetMetrics handles GET /api/v1/ai/metrics.
func (h *Handler) GetMetrics(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	lookbackDays := 7
	if daysStr := c.Query("days"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil && days > 0 && days <= 365 {
			lookbackDays = days
		}
	}
	metrics, err := h.svc.GetMetrics(c.Request.Context(), tenantID, lookbackDays)
	if err != nil {
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, metrics)
}

// KnowledgeSearch handles POST /api/v1/ai/rag/search - RAG search over knowledge base
func (h *Handler) KnowledgeSearch(c *gin.Context) {
	var req struct {
		Query string `json:"query" binding:"required"`
		Limit int    `json:"limit"`
		Type  string `json:"type"` // kb|incident
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")
	role := c.GetString("role")

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	// Use the service's RAG search capability
	// 注入访问者身份，使知识分类可见性过滤生效（L0 权限边界）
	result, err := h.svc.SearchKnowledge(
		knowledgeaccess.WithViewer(c.Request.Context(), knowledgeaccess.Viewer{UserID: userID, Role: role}),
		tenantID, req.Query, req.Type, limit)
	if err != nil {
		h.svc.logger.Warnw("AI知识搜索失败，返回降级响应", "error", err, "tenantID", tenantID)
		common.Success(c, gin.H{
			"results":  []interface{}{},
			"degraded": true,
			"message":  "AI 搜索服务暂时不可用，请稍后重试",
		})
		return
	}
	common.Success(c, gin.H{
		"results":  result,
		"degraded": false,
	})
}

// Triage handles POST /api/v1/ai/triage - Ticket classification and recommendation
func (h *Handler) Triage(c *gin.Context) {
	var req struct {
		Title       string `json:"title" binding:"required"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Priority    string `json:"priority"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.BindValidationError(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	result, err := h.svc.TriageTicket(c.Request.Context(), tenantID, req.Title, req.Description, req.Category, req.Priority)
	if err != nil {
		h.svc.logger.Warnw("AI分诊失败，返回降级响应", "error", err, "tenantID", tenantID)
		common.Success(c, gin.H{
			"title":       req.Title,
			"description": req.Description,
			"suggestions": map[string]interface{}{},
			"degraded":    true,
			"message":     "AI 分诊服务暂时不可用，请稍后重试",
		})
		return
	}
	common.Success(c, result)
}

// CreateTicketByAI handles POST /api/v1/ai/ticket/create
// 通过 AI 解析自然语言描述，智能分析描述并返回工单创建建议
func (h *Handler) CreateTicketByAI(c *gin.Context) {
	var req struct {
		Description string `json:"description" binding:"required"`
		TenantID    int    `json:"tenantId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	// 调用 AI 分析描述，返回工单创建建议
	result, err := h.svc.CreateTicketByAI(c.Request.Context(), req.Description, tenantID)
	if err != nil {
		common.FailWithErr(c, err, "AI ticket creation failed")
		return
	}
	common.Success(c, result)
}

// GetToolInvocation handles GET /api/v1/agent/tools/:id
// 查询工具执行记录（跨租户隔离）
func (h *Handler) GetToolInvocation(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid invocation id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	inv, err := h.svc.repo.GetToolInvocation(c.Request.Context(), id, tenantID)
	if err != nil {
		common.Fail(c, common.NotFoundCode, "invocation not found")
		return
	}
	// 与列表同一份字段装配：详情/列表不出现两套口径（M1-05 的对话内审批卡片依赖本接口）。
	item := h.toolInvocationItem(c.Request.Context(), tenantID, inv)
	item["error"] = inv.Error
	item["requestId"] = inv.RequestID
	common.Success(c, item)
}

// toolInvocationItem 装配工具调用记录的对内响应字段（列表与详情共用）。
//
// 口径（M1-02/M1-05）：
//   - 原始参数 `arguments` **绝不回显**（仅作执行真源留存），展示用脱敏快照 `argsRedacted`；
//   - 来源三元组来自 pending 落库快照（服务器不可达也可追溯）；
//   - `risk` 由工具面实时解析（标注变更立即反映，解析不到为空值）。
func (h *Handler) toolInvocationItem(ctx context.Context, tenantID int, inv *ToolInvocation) gin.H {
	risk := ""
	if h.svc.tools != nil {
		if def := h.svc.tools.GetToolForTenant(ctx, tenantID, inv.ToolName); def != nil {
			risk = def.Risk
		}
	}
	return gin.H{
		"id":               inv.ID,
		"toolName":         inv.ToolName,
		"argsRedacted":     inv.ArgsRedacted,
		"status":           inv.Status,
		"needsApproval":    inv.NeedsApproval,
		"approvalState":    inv.ApprovalState,
		"approvalReason":   inv.ApprovalReason,
		"permissionCheck":  inv.PermissionCheck,
		"permissionReason": inv.PermissionReason,
		"createdAt":        inv.CreatedAt,
		"conversationId":   inv.ConversationID,
		"userId":           inv.UserID,
		"provider":         inv.Provider,
		"serverName":       inv.McpServerName,
		"rawToolName":      inv.McpRawToolName,
		"callableName":     inv.McpCallableName,
		"risk":             risk,
		"roleSnapshot":     inv.RoleSnapshot,
		"approvedBy":       inv.ApprovedBy,
		"approvedAt":       inv.ApprovedAt,
		"durationMs":       inv.DurationMs,
		"errorCode":        inv.ErrorCode,
		"result":           inv.Result,
		"outputSummary":    inv.OutputSummary,
	}
}

// ListToolInvocations handles GET /api/v1/agent/tools/invocations
// 列出工具调用审批记录，供审批人查看待办。默认返回 pending（待审批），
// 支持 ?state=approved|rejected|auto 查看其它状态。跨租户隔离由 tenant_id 强制。
func (h *Handler) ListToolInvocations(c *gin.Context) {
	state := c.Query("state")
	if state == "" {
		state = "pending"
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	invs, err := h.svc.repo.ListToolInvocations(c.Request.Context(), tenantID, state)
	if err != nil {
		h.svc.logger.Warnw("查询工具调用列表失败", "error", err, "tenantID", tenantID)
		common.Fail(c, common.InternalErrorCode, "查询工具调用失败")
		return
	}

	items := make([]gin.H, 0, len(invs))
	for _, inv := range invs {
		// M1-02：审批详情必须让审批人看到「来源（内置/MCP）+ 服务器 + 原始工具名 + 投影名 + 风险」，
		// 否则在信息缺失下决策（字段口径集中在 toolInvocationItem）。
		items = append(items, h.toolInvocationItem(c.Request.Context(), tenantID, inv))
	}
	common.Success(c, gin.H{"items": items, "state": state})
}

// ApproveTool handles POST /api/v1/agent/tools/:id/approve
// 审批危险工具执行请求（RBAC 由路由中间件强制）
func (h *Handler) ApproveTool(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid invocation id")
		return
	}
	var req struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "请求参数错误")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")

	state, err := h.svc.ApproveTool(c.Request.Context(), id, tenantID, userID, req.Approve, req.Reason)
	if err != nil {
		// M1-02：错误语义分层，便于调用方区分「重试有用」与「重试无意义」。
		switch {
		case errors.Is(err, ErrInvocationNotPending):
			common.Fail(c, common.ConflictCode, "该审批已处理（仅 pending 记录可审批）")
		case errors.Is(err, ErrToolQueueUnavailable):
			common.Fail(c, common.ServiceUnavailableCode, "执行队列暂不可用，已保持待审批，请稍后重试")
		default:
			common.Fail(c, common.NotFoundCode, "invocation not found or operation failed")
		}
		return
	}
	common.Success(c, gin.H{"invocationId": id, "approvalState": state})
}
