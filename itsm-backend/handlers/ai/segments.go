package ai

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/service"
)

// GetAssistantSegments handles GET /api/v1/ai/observability/segments?dimension=role|pageKey&days=
//
// O2-01 分群对比（§13.6）：按角色/页面分组输出采用/建议/任务/Inline/错误子集指标；
// 租户为隐式条件（平台侧"租户维度"视图不在本端点范围）。只读。
func (h *Handler) GetAssistantSegments(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	dimension := service.AssistantSegmentDimensionRole
	if raw := strings.TrimSpace(c.Query("dimension")); raw != "" {
		dimension = raw
	}
	days := 0
	if raw := c.Query("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			days = parsed
		}
	}

	report, err := h.svc.GetAssistantSegments(c.Request.Context(), tenantID, days, dimension)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAssistantSegmentsDimension):
			common.Fail(c, common.ParamErrorCode, "dimension 仅支持 role|pageKey")
		case errors.Is(err, service.ErrAssistantTelemetryUnavailable):
			common.Fail(c, common.ServiceUnavailableCode, "埋点服务未启用")
		default:
			h.svc.logger.Warnw("生成分群对比失败", "error", err, "tenantID", tenantID, "dimension", dimension)
			common.Fail(c, common.InternalErrorCode, "生成分群对比失败")
		}
		return
	}
	common.Success(c, report)
}
