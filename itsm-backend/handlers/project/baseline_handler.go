package project

import (
	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ListBaselines 基线列表
// @Summary 项目计划基线列表（version 倒序）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=[]ProjectBaselineResponse}
// @Router /api/v1/projects/{id}/baselines [get]
func (h *Handler) ListBaselines(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListBaselines(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToBaselineResponseList(items))
}

// CreateBaseline 冻结计划基线
// @Summary 冻结计划基线（不可变；变更 = 新版本 + reason）
// @Description 快照当前任务计划/里程碑/项目计划面；终态项目拒绝；首版 reason 可选、后续版本必填
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body BaselineCreateRequest true "冻结请求（reason 可选）"
// @Success 200 {object} common.Response{data=ProjectBaselineResponse}
// @Router /api/v1/projects/{id}/baselines [post]
func (h *Handler) CreateBaseline(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req BaselineCreateRequest
	// body 允许为空（首版冻结可不填 reason）。
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
			return
		}
	}
	created, err := h.service.CreateBaseline(c.Request.Context(), tenantID, id, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToBaselineResponse(created))
}

// BaselineDiff 基线偏差
// @Summary 最新基线 vs 当前状态偏差（实时计算）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=BaselineDiffResponse}
// @Router /api/v1/projects/{id}/baselines/diff [get]
func (h *Handler) BaselineDiff(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	diff, err := h.service.BaselineDiff(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, diff)
}
