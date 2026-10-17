package project

import (
	"fmt"
	"unicode/utf8"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// PE-25 项目 AI HTTP 端点（权威设计：docs/project-management/plan/p2-ai-design.md）。
//
// 三个端点均为只读派生/生成动作（不写业务状态）：权限 project:read，可见性同详情（404）。
// 审计走 writeReportAudit（resource=project；降级同样审计，D-47）。

// GenerateAISummary 生成项目 AI 状态摘要
// @Summary 生成项目 AI 状态摘要（周报/双周报/月报；AI 不可用时降级为事实派生）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body AISummaryRequest false "摘要请求（cadence 缺省 weekly）"
// @Success 200 {object} common.Response{data=AISummaryResponse}
// @Router /api/v1/projects/{id}/ai/summary [post]
func (h *Handler) GenerateAISummary(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req AISummaryRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
			return
		}
	}
	userID := c.GetInt("user_id")
	resp, err := h.service.GenerateAISummary(
		c.Request.Context(), tenantID, id, userID, isAdminViewer(middleware.GetContextRoles(c)), &req,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, userID, "ai_summary_generate", fmt.Sprintf("/api/v1/projects/%d/ai/summary", id), map[string]any{
		"projectId":     id,
		"generatedBy":   resp.GeneratedBy,
		"degraded":      resp.Degraded,
		"degradeReason": resp.DegradeReason,
		"model":         resp.Model,
		"promptChars":   resp.PromptChars,
		"outputChars":   utf8.RuneCountInString(resp.Summary),
	})
	common.Success(c, resp)
}

// GenerateAIRiskWarnings 生成项目风险预警
// @Summary 生成项目风险预警（规则信号恒返回；AI 判断为增强段）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=AIRiskWarningsResponse}
// @Router /api/v1/projects/{id}/ai/risk-warnings [post]
func (h *Handler) GenerateAIRiskWarnings(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID := c.GetInt("user_id")
	resp, err := h.service.GenerateAIRiskWarnings(
		c.Request.Context(), tenantID, id, userID, isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, userID, "ai_risk_warnings", fmt.Sprintf("/api/v1/projects/%d/ai/risk-warnings", id), map[string]any{
		"projectId":     id,
		"generatedBy":   resp.GeneratedBy,
		"degraded":      resp.Degraded,
		"degradeReason": resp.DegradeReason,
		"model":         resp.Model,
		"signalTotal":   resp.SignalTotal,
		"truncated":     resp.Truncated,
		"warningCount":  len(resp.Warnings),
	})
	common.Success(c, resp)
}

// ExtractAIMinutesTasks 会议纪要转任务建议
// @Summary 会议纪要转任务建议（只出建议不落库；AI 不可用时返回空提案 + 降级标记）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body AIMinutesTasksRequest true "纪要内容（content ≤20000 字符）"
// @Success 200 {object} common.Response{data=AIMinutesTasksResponse}
// @Router /api/v1/projects/{id}/ai/minutes/tasks [post]
func (h *Handler) ExtractAIMinutesTasks(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req AIMinutesTasksRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	userID := c.GetInt("user_id")
	resp, err := h.service.ExtractTasksFromMinutes(
		c.Request.Context(), tenantID, id, userID, isAdminViewer(middleware.GetContextRoles(c)), &req,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, userID, "ai_minutes_extract", fmt.Sprintf("/api/v1/projects/%d/ai/minutes/tasks", id), map[string]any{
		"projectId":     id,
		"generatedBy":   resp.GeneratedBy,
		"degraded":      resp.Degraded,
		"degradeReason": resp.DegradeReason,
		"model":         resp.Model,
		"contentChars":  resp.ContentChars,
		"proposalCount": len(resp.Proposals),
	})
	common.Success(c, resp)
}