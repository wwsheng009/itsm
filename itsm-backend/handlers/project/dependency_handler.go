package project

import (
	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ListDependencies 依赖列表
// @Summary 项目任务依赖列表
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=[]ProjectTaskDependencyResponse}
// @Router /api/v1/projects/{id}/dependencies [get]
func (h *Handler) ListDependencies(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListDependencies(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToDependencyResponseList(items))
}

// AddDependency 添加任务依赖
// @Summary 添加任务依赖（v1 仅 FS；同项目/无自依赖/无环）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Param body body DependencyCreateRequest true "依赖请求"
// @Success 200 {object} common.Response{data=ProjectTaskDependencyResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId}/dependencies [post]
func (h *Handler) AddDependency(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	taskID, ok := parseID(c, "taskId")
	if !ok {
		return
	}
	var req DependencyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	dep, err := h.service.AddDependency(c.Request.Context(), tenantID, id, taskID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToDependencyResponse(dep))
}

// RemoveDependency 删除任务依赖
// @Summary 删除任务依赖
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Param dependsOnTaskId path int true "前置任务ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/tasks/{taskId}/dependencies/{dependsOnTaskId} [delete]
func (h *Handler) RemoveDependency(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	taskID, ok := parseID(c, "taskId")
	if !ok {
		return
	}
	dependsOnID, ok := parseID(c, "dependsOnTaskId")
	if !ok {
		return
	}
	if err := h.service.RemoveDependency(c.Request.Context(), tenantID, id, taskID, dependsOnID); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}
