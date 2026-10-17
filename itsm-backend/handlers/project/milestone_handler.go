package project

import (
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// MyMilestones 我的里程碑到期（个人工作台聚合）
// @Summary 我的里程碑到期（跨可见项目；仅 planned；默认 30 天内含逾期）
// @Tags 项目管理
// @Produce json
// @Param dueWithinDays query int false "只看 N 天内到期（0=不限；默认 30）"
// @Param limit query int false "返回上限（≤200，默认 50）"
// @Success 200 {object} common.Response{data=[]MyMilestoneResponse}
// @Router /api/v1/projects/my-milestones [get]
func (h *Handler) MyMilestones(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	dueWithinDays := 30
	if raw := strings.TrimSpace(c.Query("dueWithinDays")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			common.Fail(c, common.ParamErrorCode, "dueWithinDays 须为非负整数")
			return
		}
		dueWithinDays = v
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			common.Fail(c, common.ParamErrorCode, "limit 须为正整数")
			return
		}
		limit = v
	}
	items, err := h.service.MyMilestones(
		c.Request.Context(), tenantID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		dueWithinDays, limit,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, items)
}

// ListMilestones 里程碑列表
// @Summary 项目里程碑列表（可见性谓词）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param status query string false "planned|reached|missed"
// @Success 200 {object} common.Response{data=[]ProjectMilestoneResponse}
// @Router /api/v1/projects/{id}/milestones [get]
func (h *Handler) ListMilestones(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListMilestones(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		strings.TrimSpace(c.Query("status")),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToMilestoneResponseList(items))
}

// CreateMilestone 创建里程碑
// @Summary 创建里程碑（负责人须为项目活跃成员）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body MilestoneCreateRequest true "里程碑请求"
// @Success 200 {object} common.Response{data=ProjectMilestoneResponse}
// @Router /api/v1/projects/{id}/milestones [post]
func (h *Handler) CreateMilestone(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req MilestoneCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	created, err := h.service.CreateMilestone(c.Request.Context(), tenantID, id, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToMilestoneResponse(created))
}

// UpdateMilestone 更新里程碑
// @Summary 更新里程碑（不含状态）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param milestoneId path int true "里程碑ID"
// @Param body body MilestoneUpdateRequest true "更新请求"
// @Success 200 {object} common.Response{data=ProjectMilestoneResponse}
// @Router /api/v1/projects/{id}/milestones/{milestoneId} [put]
func (h *Handler) UpdateMilestone(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	milestoneID, ok := parseID(c, "milestoneId")
	if !ok {
		return
	}
	var req MilestoneUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.UpdateMilestone(c.Request.Context(), tenantID, id, milestoneID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToMilestoneResponse(updated))
}

// TransitionMilestone 里程碑状态流转
// @Summary 里程碑状态流转（reach|miss）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param milestoneId path int true "里程碑ID"
// @Param body body MilestoneStatusRequest true "流转请求"
// @Success 200 {object} common.Response{data=ProjectMilestoneResponse}
// @Router /api/v1/projects/{id}/milestones/{milestoneId}/status [post]
func (h *Handler) TransitionMilestone(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	milestoneID, ok := parseID(c, "milestoneId")
	if !ok {
		return
	}
	var req MilestoneStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.TransitionMilestone(c.Request.Context(), tenantID, id, milestoneID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToMilestoneResponse(updated))
}

// DeleteMilestone 删除里程碑
// @Summary 删除里程碑
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param milestoneId path int true "里程碑ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/milestones/{milestoneId} [delete]
func (h *Handler) DeleteMilestone(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	milestoneID, ok := parseID(c, "milestoneId")
	if !ok {
		return
	}
	if err := h.service.DeleteMilestone(c.Request.Context(), tenantID, id, milestoneID); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}
