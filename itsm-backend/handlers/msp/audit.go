package msp

import (
	"strconv"

	"itsm-backend/common"

	"github.com/gin-gonic/gin"
)

// GetAuditSummary 审计看板聚合（IP-P1-8）。
//
// GET /api/v1/msp/audit/summary?days=30
// 返回 provider home 租户窗口内：跨租户事件数、拒绝事件数（tenant.scope_denied /
// tenant.probe_denied）、按 source/action/目标租户/membership 聚合、最近拒绝明细。
func (h *Handler) GetAuditSummary(c *gin.Context) {
	if h.audit == nil {
		common.Fail(c, common.InternalErrorCode, "审计看板未装配")
		return
	}
	homeTenantID := c.GetInt("tenant_id")
	if homeTenantID <= 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	days := 0
	if raw := c.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			common.Fail(c, common.ParamErrorCode, "days 必须为正整数")
			return
		}
		days = parsed
	}

	summary, err := h.audit.Summary(c.Request.Context(), homeTenantID, days)
	if err != nil {
		h.logger.Errorw("msp audit summary failed", "error", err, "tenant_id", homeTenantID)
		common.Fail(c, common.InternalErrorCode, "审计聚合失败")
		return
	}
	common.Success(c, summary)
}
