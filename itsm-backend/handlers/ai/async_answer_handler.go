package ai

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
)

// V2-02 异步精算端点：先给可用答案（同步快速参考）+ 后台精算任务查询。

type asyncAnswerStartRequest struct {
	Prompt  string `json:"prompt"`
	PageKey string `json:"pageKey"`
	Route   string `json:"route"`
}

// StartAsyncAnswer handles POST /api/v1/ai/async/answers
//
// 请求 `{"prompt":"...","pageKey":"...","route":"..."}`；
// 立即返回 `{taskId, status, fast:{source,items}}`（先给可用答案），后台精算完成后
// 结果进任务表（GET 轮询）并投递通知中心。
func (h *Handler) StartAsyncAnswer(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户/用户信息缺失")
		return
	}
	var req asyncAnswerStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "请求体格式错误")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.PageKey = strings.TrimSpace(req.PageKey)
	req.Route = strings.TrimSpace(req.Route)

	result, err := h.svc.StartAsyncAnswer(c.Request.Context(), AsyncAnswerRequest{
		TenantID: tenantID,
		UserID:   userID,
		Prompt:   req.Prompt,
		PageKey:  req.PageKey,
		Route:    req.Route,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrAsyncAnswerPromptRequired):
			common.Fail(c, common.ParamErrorCode, "prompt 不能为空")
		case errors.Is(err, ErrAsyncAnswerPromptTooLong):
			common.Fail(c, common.ParamErrorCode, "prompt 超长（≤1000 字）")
		case errors.Is(err, ErrAsyncAnswersUnavailable):
			common.Fail(c, common.ServiceUnavailableCode, "异步精算未启用")
		default:
			h.svc.logger.Warnw("发起异步精算失败", "error", err, "tenantID", tenantID)
			common.Fail(c, common.InternalErrorCode, "发起异步精算失败")
		}
		return
	}
	common.Success(c, result)
}

// GetAsyncAnswer handles GET /api/v1/ai/async/answers/:id
//
// 任务快照（租户 + 用户双隔离；未知/过期/越权统一 404，避免枚举）。
func (h *Handler) GetAsyncAnswer(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户/用户信息缺失")
		return
	}
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		common.Fail(c, common.ParamErrorCode, "任务 id 不能为空")
		return
	}
	result, err := h.svc.GetAsyncAnswer(c.Request.Context(), tenantID, userID, taskID)
	if err != nil {
		switch {
		case errors.Is(err, ErrAsyncAnswerNotFound):
			common.Fail(c, common.NotFoundCode, "任务不存在或已过期")
		case errors.Is(err, ErrAsyncAnswersUnavailable):
			common.Fail(c, common.ServiceUnavailableCode, "异步精算未启用")
		default:
			h.svc.logger.Warnw("查询异步精算失败", "error", err, "tenantID", tenantID, "taskID", taskID)
			common.Fail(c, common.InternalErrorCode, "查询异步精算失败")
		}
		return
	}
	common.Success(c, result)
}
