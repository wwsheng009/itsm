package ai

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/service/bot"
)

// GetRunTrace handles GET /api/v1/ai/runs/:id/trace
//
// O1-01：单任务全链路轨迹（会话 → run → 步骤/事件 → 工具调用（确认/审批）→ 产物 → 业务对象）。
// 只读；跨租户与不存在同口径 404（不区分，避免探测）；轨迹服务未装配 → 503。
func (h *Handler) GetRunTrace(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid run id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	trace, err := h.svc.GetRunTrace(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, bot.ErrRunNotFound) {
			common.Fail(c, common.NotFoundCode, "run not found")
			return
		}
		if errors.Is(err, ErrTraceServiceUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "轨迹服务未启用")
			return
		}
		h.svc.logger.Warnw("读取运行轨迹失败", "error", err, "tenantID", tenantID, "runID", id)
		common.Fail(c, common.InternalErrorCode, "查询运行轨迹失败")
		return
	}
	common.Success(c, trace)
}
