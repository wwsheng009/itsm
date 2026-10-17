package project

import (
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ListMembers 项目成员列表
// @Summary 项目成员列表（含失效历史与派生标签 memberOrigin/expiresAt）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=[]MemberView}
// @Router /api/v1/projects/{id}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	views, err := h.service.ListMembers(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, views)
}

// AddMember 添加项目成员
// @Summary 添加项目成员（校验目标租户活跃 membership）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body MemberAddRequest true "成员请求"
// @Success 200 {object} common.Response{data=MemberView}
// @Router /api/v1/projects/{id}/members [post]
func (h *Handler) AddMember(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req MemberAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	view, err := h.service.AddMember(c.Request.Context(), tenantID, id, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, view)
}

// UpdateMemberRole 调整成员角色
// @Summary 调整成员角色（pm|member|stakeholder）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param userId path int true "用户ID"
// @Param body body MemberRoleUpdateRequest true "角色请求"
// @Success 200 {object} common.Response{data=MemberView}
// @Router /api/v1/projects/{id}/members/{userId} [patch]
func (h *Handler) UpdateMemberRole(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	var req MemberRoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	view, err := h.service.UpdateMemberRole(c.Request.Context(), tenantID, id, userID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, view)
}

// RemoveMember 移除项目成员（软失效）
// @Summary 移除项目成员（软失效；项目经理不可移除）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param userId path int true "用户ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/members/{userId} [delete]
func (h *Handler) RemoveMember(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	if err := h.service.RemoveMember(c.Request.Context(), tenantID, id, userID); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"removed": true})
}

// MemberCandidates 成员候选
// @Summary 成员候选（目标租户活跃 membership，排除在册成员；最小字段）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param q query string false "姓名/用户名关键字"
// @Param limit query int false "返回上限（≤50）"
// @Success 200 {object} common.Response{data=[]MemberCandidate}
// @Router /api/v1/projects/{id}/member-candidates [get]
func (h *Handler) MemberCandidates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	candidates, err := h.service.MemberCandidates(c.Request.Context(), tenantID, id, c.Query("q"), limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, candidates)
}
