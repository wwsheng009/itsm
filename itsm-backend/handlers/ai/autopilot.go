package ai

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
)

// I2-01 Autopilot 监督面端点（运行列表 / 暂停=取消 / 回滚=冻结待审批 + 通知中心）。

// parseAutopilotRunID 解析路径上的运行 id。
func parseAutopilotRunID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid run id")
		return 0, false
	}
	return id, true
}

// bindAutopilotAction 读取可选 body（`{"reason":"..."}`）；空 body/非 JSON 按无 reason 处理。
func bindAutopilotAction(c *gin.Context) string {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	return strings.TrimSpace(req.Reason)
}

// GetAutopilotRuns handles GET /api/v1/ai/runs?days=&status=&limit=
//
// 监督面运行列表：状态 + 步骤/工具/待审批/产物计数（挑运行、挑要处理的审批）。
func (h *Handler) GetAutopilotRuns(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	days := 7
	if raw := c.Query("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			days = parsed
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	switch status {
	case "", "running", "completed", "failed", "cancelled":
	default:
		common.Fail(c, common.ParamErrorCode, "invalid status (want running|completed|failed|cancelled)")
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	runs, err := h.svc.ListAutopilotRuns(c.Request.Context(), tenantID, days, status, limit)
	if err != nil {
		if errors.Is(err, ErrTraceServiceUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "运行监督未启用")
			return
		}
		h.svc.logger.Warnw("查询运行列表失败", "error", err, "tenantID", tenantID)
		common.Fail(c, common.InternalErrorCode, "查询运行列表失败")
		return
	}
	common.Success(c, gin.H{"items": runs, "days": days, "status": status})
}

// CancelAutopilotRun handles POST /api/v1/ai/runs/:id/cancel
//
// 暂停/终止后台任务：仅 `running` 生效（条件更新，幂等）；已收口运行返回 `cancelled=false`。
func (h *Handler) CancelAutopilotRun(c *gin.Context) {
	id, ok := parseAutopilotRunID(c)
	if !ok {
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	reason := bindAutopilotAction(c)

	cancelled, err := h.svc.CancelAutopilotRun(c.Request.Context(), tenantID, id, reason)
	if err != nil {
		if errors.Is(err, ErrAutopilotUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "运行监督未启用")
			return
		}
		h.svc.logger.Warnw("取消运行失败", "error", err, "tenantID", tenantID, "runID", id)
		common.Fail(c, common.InternalErrorCode, "取消运行失败")
		return
	}
	common.Success(c, gin.H{"runId": id, "cancelled": cancelled})
}

// RollbackAutopilotRun handles POST /api/v1/ai/runs/:id/rollback
//
// 回滚：终止运行 + 冻结该运行下全部待审批写动作（cancelled，永不执行）+ 通知中心投递。
func (h *Handler) RollbackAutopilotRun(c *gin.Context) {
	id, ok := parseAutopilotRunID(c)
	if !ok {
		return
	}
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	actorID := c.GetInt("user_id")
	reason := bindAutopilotAction(c)

	result, err := h.svc.RollbackAutopilotRun(c.Request.Context(), tenantID, id, actorID, reason)
	if err != nil {
		if errors.Is(err, ErrAutopilotUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "运行监督未启用")
			return
		}
		h.svc.logger.Warnw("回滚运行失败", "error", err, "tenantID", tenantID, "runID", id)
		common.Fail(c, common.InternalErrorCode, "回滚运行失败")
		return
	}
	common.Success(c, result)
}
