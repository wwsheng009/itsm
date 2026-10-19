package ai

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/service/bot"
)

// GetConversationTrace handles GET /api/v1/ai/conversations/:id/trace
//
// O1-01 条件项闭环：会话维度全链路轨迹（消息回合 → （若有）运行 → 工具调用与确认
// → 产物）；纯问答无 bot_runs 同样可回放。
// 只读；跨租户与不存在同口径 404（不区分，避免探测）；轨迹服务未装配 → 503。
func (h *Handler) GetConversationTrace(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid conversation id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	trace, err := h.svc.GetConversationTrace(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, bot.ErrConversationNotFound) {
			common.Fail(c, common.NotFoundCode, "conversation not found")
			return
		}
		if errors.Is(err, ErrTraceServiceUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "轨迹服务未启用")
			return
		}
		h.svc.logger.Warnw("读取会话轨迹失败", "error", err, "tenantID", tenantID, "conversationID", id)
		common.Fail(c, common.InternalErrorCode, "查询会话轨迹失败")
		return
	}
	common.Success(c, trace)
}
