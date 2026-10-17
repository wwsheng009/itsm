package project

import (
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 项目模板 HTTP 处理器（PE-11；thin：参数解析 + 响应封装；业务在 Service）。
//
// 权威定义：docs/project-management/plan/p1-template-design.md。

// ListTemplates 模板列表
// @Summary 模板列表
// @Description 分页 + 过滤：status/type/q（列表省略 payload，仅给计数）
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=common.ListResponse}
// @Router /api/v1/projects/templates [get]
func (h *Handler) ListTemplates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	page := common.GetPaginationFromQuery(c)
	filter := TemplateListFilter{
		TenantID: tenantID,
		Status:   strings.TrimSpace(c.Query("status")),
		Type:     strings.TrimSpace(c.Query("type")),
		Keyword:  strings.TrimSpace(c.Query("q")),
		Offset:   page.GetOffset(),
		Limit:    page.GetLimit(),
	}
	items, total, err := h.service.ListTemplates(c.Request.Context(), filter)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, common.NewListResponse(
		ToTemplateResponseList(items),
		common.NewPaginationResponse(page.Page, page.PageSize, total),
	))
}

// CreateTemplate 创建模板
// @Summary 创建模板
// @Description 模板 = 阶段/任务/检查单/里程碑定义（status 缺省 draft，激活后可一键生成）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param body body TemplateCreateRequest true "创建请求"
// @Success 200 {object} common.Response{data=ProjectTemplateResponse}
// @Router /api/v1/projects/templates [post]
func (h *Handler) CreateTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	var req TemplateCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	req.TenantID = tenantID
	created, err := h.service.CreateTemplate(c.Request.Context(), tenantID, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTemplateResponse(created, true))
}

// GetTemplate 模板详情
// @Summary 模板详情（含定义体 payload）
// @Tags 项目管理
// @Produce json
// @Param templateId path int true "模板ID"
// @Success 200 {object} common.Response{data=ProjectTemplateResponse}
// @Router /api/v1/projects/templates/{templateId} [get]
func (h *Handler) GetTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	item, err := h.service.GetTemplate(c.Request.Context(), tenantID, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTemplateResponse(item, true))
}

// UpdateTemplate 更新模板
// @Summary 更新模板（nil 字段不改；version 可选乐观锁；status 显式切换）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param templateId path int true "模板ID"
// @Param body body TemplateUpdateRequest true "更新请求"
// @Success 200 {object} common.Response{data=ProjectTemplateResponse}
// @Router /api/v1/projects/templates/{templateId} [put]
func (h *Handler) UpdateTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	var req TemplateUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.UpdateTemplate(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTemplateResponse(updated, true))
}

// DeleteTemplate 删除模板（软删；被项目应用时 409）
// @Summary 删除模板
// @Tags 项目管理
// @Produce json
// @Param templateId path int true "模板ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/templates/{templateId} [delete]
func (h *Handler) DeleteTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	if err := h.service.DeleteTemplate(c.Request.Context(), tenantID, id); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// ApplyTemplate 一键生成项目计划
// @Summary 应用模板（生成阶段/任务/检查单/里程碑）
// @Description 模板须 active；一项目一模板（重复应用 409，幂等：不产生重复数据）；锚点=项目开始日期（缺失时=当日）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body TemplateApplyRequest true "应用请求"
// @Success 200 {object} common.Response{data=TemplateApplyResponse}
// @Router /api/v1/projects/{id}/templates/apply [post]
func (h *Handler) ApplyTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req TemplateApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	resp, err := h.service.ApplyTemplate(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)), &req,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, resp)
}
