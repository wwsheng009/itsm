// workbench_views.go：工作台自定义视图 HTTP 端点（IP-P2-4a）。
//
// 灰度：服务层 WORKBENCH_VIEWS_ENABLED（默认关）；未开启时端点返回 404 + reasonCode。
package msp

import (
	"strconv"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// failWorkbenchView 将视图领域错误映射为稳定 HTTP 语义 + reasonCode。
func failWorkbenchView(c *gin.Context, err error, fallback string) {
	if ve, ok := service.AsWorkbenchViewError(err); ok {
		switch ve.Status {
		case 404:
			common.FailWithData(c, common.NotFoundCode, ve.Message, gin.H{"reasonCode": ve.Code})
		case 403:
			common.FailWithData(c, common.ForbiddenCode, ve.Message, gin.H{"reasonCode": ve.Code})
		case 409:
			common.FailWithData(c, common.ConflictCode, ve.Message, gin.H{"reasonCode": ve.Code})
		default:
			common.FailWithData(c, common.ParamErrorCode, ve.Message, gin.H{"reasonCode": ve.Code})
		}
		return
	}
	common.FailWithErr(c, err, fallback)
}

func workbenchViewID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "视图ID无效")
		return 0, false
	}
	return id, true
}

// ListWorkbenchViews GET /api/v1/msp/workbench/views
func (h *Handler) ListWorkbenchViews(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.views == nil {
		common.Fail(c, common.InternalErrorCode, "视图服务未启用")
		return
	}
	views, err := h.views.ListViews(c.Request.Context(), actor)
	if err != nil {
		h.logger.Errorw("workbench views list failed", "error", err, "user_id", actor.UserID)
		failWorkbenchView(c, err, "查询视图失败")
		return
	}
	common.Success(c, gin.H{"views": views, "total": len(views), "enabled": h.views.ViewsEnabled()})
}

// CreateWorkbenchView POST /api/v1/msp/workbench/views
func (h *Handler) CreateWorkbenchView(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.views == nil {
		common.Fail(c, common.InternalErrorCode, "视图服务未启用")
		return
	}
	var req dto.WorkbenchViewCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.views.CreateView(c.Request.Context(), actor, req)
	if err != nil {
		h.logger.Errorw("workbench view create failed", "error", err, "user_id", actor.UserID)
		failWorkbenchView(c, err, "创建视图失败")
		return
	}
	common.Success(c, view)
}

// UpdateWorkbenchView PUT /api/v1/msp/workbench/views/:id
func (h *Handler) UpdateWorkbenchView(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.views == nil {
		common.Fail(c, common.InternalErrorCode, "视图服务未启用")
		return
	}
	id, ok := workbenchViewID(c)
	if !ok {
		return
	}
	var req dto.WorkbenchViewUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.views.UpdateView(c.Request.Context(), actor, id, req)
	if err != nil {
		h.logger.Errorw("workbench view update failed", "error", err, "view_id", id, "user_id", actor.UserID)
		failWorkbenchView(c, err, "更新视图失败")
		return
	}
	common.Success(c, view)
}

// DeleteWorkbenchView DELETE /api/v1/msp/workbench/views/:id
func (h *Handler) DeleteWorkbenchView(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.views == nil {
		common.Fail(c, common.InternalErrorCode, "视图服务未启用")
		return
	}
	id, ok := workbenchViewID(c)
	if !ok {
		return
	}
	if err := h.views.DeleteView(c.Request.Context(), actor, id); err != nil {
		h.logger.Errorw("workbench view delete failed", "error", err, "view_id", id, "user_id", actor.UserID)
		failWorkbenchView(c, err, "删除视图失败")
		return
	}
	common.Success(c, gin.H{"message": "视图已删除"})
}

// SetDefaultWorkbenchView POST /api/v1/msp/workbench/views/:id/default
func (h *Handler) SetDefaultWorkbenchView(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.views == nil {
		common.Fail(c, common.InternalErrorCode, "视图服务未启用")
		return
	}
	id, ok := workbenchViewID(c)
	if !ok {
		return
	}
	view, err := h.views.SetDefaultView(c.Request.Context(), actor, id)
	if err != nil {
		h.logger.Errorw("workbench view set-default failed", "error", err, "view_id", id, "user_id", actor.UserID)
		failWorkbenchView(c, err, "设置默认视图失败")
		return
	}
	common.Success(c, view)
}
