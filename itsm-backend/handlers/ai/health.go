package ai

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/service"
)

// parseAIHealthDays 解析 `?days=`（默认 7；上限 30；非法值回落默认而非 400）。
func parseAIHealthDays(c *gin.Context) int {
	days := service.AIHealthDefaultWindowDays
	if raw := c.Query("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			days = parsed
		}
	}
	if days > service.AIHealthMaxWindowDays {
		days = service.AIHealthMaxWindowDays
	}
	return days
}

// GetAIHealth handles GET /api/v1/ai/health
//
// O1-02：实时健康快照（运行/工具/确认/队列/Provider/质量）+ SLO 评估（§13.5）。
// 只读；服务未装配 → 503。
func (h *Handler) GetAIHealth(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	days := parseAIHealthDays(c)

	snapshot, slos, err := h.svc.GetAIHealth(c.Request.Context(), tenantID, days)
	if err != nil {
		if errors.Is(err, ErrHealthServiceUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "健康服务未启用")
			return
		}
		h.svc.logger.Warnw("读取 AI 健康快照失败", "error", err, "tenantID", tenantID)
		common.Fail(c, common.InternalErrorCode, "读取健康快照失败")
		return
	}
	common.Success(c, gin.H{"snapshot": snapshot, "slos": slos})
}

// DispatchAIHealthAlerts handles POST /api/v1/ai/health/alerts/dispatch
//
// O1-02：评估 SLO 并把违规项投递到通知中心（同标题 30 分钟去重；收件人=租户管理员）。
// 写面权限（ai:write）由路由中间件强制。
func (h *Handler) DispatchAIHealthAlerts(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	days := parseAIHealthDays(c)

	result, err := h.svc.DispatchAIHealthAlerts(c.Request.Context(), tenantID, days)
	if err != nil {
		if errors.Is(err, ErrHealthServiceUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "健康服务未启用")
			return
		}
		h.svc.logger.Warnw("派发 AI 健康告警失败", "error", err, "tenantID", tenantID)
		common.Fail(c, common.InternalErrorCode, "派发健康告警失败")
		return
	}
	common.Success(c, result)
}
