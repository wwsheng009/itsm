package project

import (
	"errors"
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ListTasks 任务列表（tree|flat）
// @Summary 项目任务列表
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param view query string false "tree|flat（默认 flat）"
// @Param status query string false "状态过滤"
// @Param assigneeId query int false "负责人过滤"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/tasks [get]
func (h *Handler) ListTasks(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListTasks(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		strings.TrimSpace(c.Query("status")), atoiOrZero(c.Query("assigneeId")),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	if strings.EqualFold(c.Query("view"), "tree") {
		common.Success(c, BuildTaskTree(items))
		return
	}
	common.Success(c, ToTaskResponseList(items))
}

// GetTask 任务详情
// @Summary 项目任务详情
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId} [get]
func (h *Handler) GetTask(c *gin.Context) {
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
	task, err := h.service.GetTask(
		c.Request.Context(), tenantID, id, taskID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}

// CreateTask 创建任务
// @Summary 创建项目任务（WBS ≤3 层；负责人须为活跃成员）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body TaskCreateRequest true "任务请求"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks [post]
func (h *Handler) CreateTask(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	task, err := h.service.CreateTask(c.Request.Context(), tenantID, id, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}

// UpdateTask 更新任务
// @Summary 更新项目任务（version 可选乐观锁）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Param body body TaskUpdateRequest true "更新请求"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId} [put]
func (h *Handler) UpdateTask(c *gin.Context) {
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
	var req TaskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	task, err := h.service.UpdateTask(c.Request.Context(), tenantID, id, taskID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}

// DeleteTask 删除任务（软删；有子任务 409）
// @Summary 删除项目任务
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/tasks/{taskId} [delete]
func (h *Handler) DeleteTask(c *gin.Context) {
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
	if err := h.service.DeleteTask(c.Request.Context(), tenantID, id, taskID); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// TransitionTask 任务状态流转
// @Summary 任务状态流转（start|block|unblock|complete|cancel）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Param body body TaskStatusRequest true "流转请求"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId}/status [post]
func (h *Handler) TransitionTask(c *gin.Context) {
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
	var req TaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	task, err := h.service.TransitionTask(c.Request.Context(), tenantID, id, taskID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}

// MyTasks 我的项目任务（个人工作台聚合）
// @Summary 我的项目任务（跨可见项目；默认排除终态）
// @Tags 项目管理
// @Produce json
// @Param status query string false "状态过滤"
// @Param limit query int false "返回上限（≤200）"
// @Success 200 {object} common.Response{data=[]MyTaskResponse}
// @Router /api/v1/projects/my-tasks [get]
func (h *Handler) MyTasks(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	items, err := h.service.MyTasks(
		c.Request.Context(), tenantID, c.GetInt("user_id"),
		isAdminViewer(middleware.GetContextRoles(c)),
		strings.TrimSpace(c.Query("status")), limit,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, items)
}

// 任务域错误映射（供 fail 调用；与项目域共用同一 fail 实现）。
var _ = errors.Is
