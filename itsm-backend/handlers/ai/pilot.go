package ai

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/service"
)

// parsePilotDays 读取窗口参数（非法/缺省 → 0，由 service 回落默认并 clamp）。
func parsePilotDays(c *gin.Context, key string) int {
	if raw := c.Query(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

// GetPilotReport handles GET /api/v1/ai/observability/pilot-report
//
// V0-04：试点对照报告（本期 vs 基线窗口）+ §11.5 放量门槛判定（只读）。
func (h *Handler) GetPilotReport(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	report, err := h.svc.BuildPilotReport(c.Request.Context(), tenantID, service.PilotReportQuery{
		CurrentDays:  parsePilotDays(c, "currentDays"),
		BaselineDays: parsePilotDays(c, "baselineDays"),
	})
	if err != nil {
		if errors.Is(err, service.ErrAssistantTelemetryUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "埋点服务未启用")
			return
		}
		h.svc.logger.Warnw("生成试点对照报告失败", "error", err, "tenantID", tenantID)
		common.Fail(c, common.InternalErrorCode, "生成试点对照报告失败")
		return
	}
	common.Success(c, report)
}
