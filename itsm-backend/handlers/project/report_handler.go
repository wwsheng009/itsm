package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// PE-17 状态报告 Handler（thin：参数解析 + 响应封装；业务在 Service）。

// ListReportTemplates 报告模板列表
// @Summary 报告模板列表（status/q 过滤 + 分页）
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=common.ListResponse}
// @Router /api/v1/projects/report-templates [get]
func (h *Handler) ListReportTemplates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	page := common.GetPaginationFromQuery(c)
	items, total, err := h.service.ListReportTemplates(c.Request.Context(), ReportTemplateListFilter{
		TenantID: tenantID,
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("q")),
		Offset:   page.GetOffset(),
		Limit:    page.GetLimit(),
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, common.NewListResponse(
		ToReportTemplateResponseList(items),
		common.NewPaginationResponse(page.Page, page.PageSize, total),
	))
}

// CreateReportTemplate 创建报告模板
// @Summary 创建报告模板（cadence: weekly|biweekly|monthly；sections: overview|tasks|milestones|gates|variance|team）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param body body ReportTemplateCreateRequest true "创建请求"
// @Success 200 {object} common.Response{data=ReportTemplateResponse}
// @Router /api/v1/projects/report-templates [post]
func (h *Handler) CreateReportTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	var req ReportTemplateCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	created, err := h.service.CreateReportTemplate(c.Request.Context(), tenantID, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, c.GetInt("user_id"), "report_template_create", "/api/v1/projects/report-templates", map[string]any{
		"templateId": created.ID, "name": created.Name, "cadence": created.Cadence,
	})
	common.Success(c, ToReportTemplateResponse(created))
}

// GetReportTemplate 报告模板详情
// @Summary 报告模板详情
// @Tags 项目管理
// @Produce json
// @Param templateId path int true "模板ID"
// @Success 200 {object} common.Response{data=ReportTemplateResponse}
// @Router /api/v1/projects/report-templates/{templateId} [get]
func (h *Handler) GetReportTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	item, err := h.service.GetReportTemplate(c.Request.Context(), tenantID, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToReportTemplateResponse(item))
}

// UpdateReportTemplate 更新报告模板
// @Summary 更新报告模板（version 乐观锁；status 显式切换）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param templateId path int true "模板ID"
// @Param body body ReportTemplateUpdateRequest true "更新请求"
// @Success 200 {object} common.Response{data=ReportTemplateResponse}
// @Router /api/v1/projects/report-templates/{templateId} [put]
func (h *Handler) UpdateReportTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	var req ReportTemplateUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.UpdateReportTemplate(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, c.GetInt("user_id"), "report_template_update", fmt.Sprintf("/api/v1/projects/report-templates/%d", id), map[string]any{
		"templateId": id, "version": updated.Version, "status": updated.Status,
	})
	common.Success(c, ToReportTemplateResponse(updated))
}

// DeleteReportTemplate 删除报告模板（软删）
// @Summary 删除报告模板（软删；名称不释放）
// @Tags 项目管理
// @Produce json
// @Param templateId path int true "模板ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/report-templates/{templateId} [delete]
func (h *Handler) DeleteReportTemplate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "templateId")
	if !ok {
		return
	}
	if err := h.service.DeleteReportTemplate(c.Request.Context(), tenantID, id); err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, c.GetInt("user_id"), "report_template_delete", fmt.Sprintf("/api/v1/projects/report-templates/%d", id), map[string]any{
		"templateId": id,
	})
	common.Success(c, gin.H{"deleted": true})
}

// GenerateProjectReport 生成项目状态报告（数据派生，先导出后 AI）
// @Summary 生成项目状态报告（markdown + data；templateId 缺省用默认章节）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body GenerateReportRequest false "生成请求"
// @Success 200 {object} common.Response{data=ReportResponse}
// @Router /api/v1/projects/{id}/reports/generate [post]
func (h *Handler) GenerateProjectReport(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req GenerateReportRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
			return
		}
	}
	report, err := h.service.GenerateProjectReport(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)), req.TemplateID,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeReportAudit(c, tenantID, c.GetInt("user_id"), "report_generate", fmt.Sprintf("/api/v1/projects/%d/reports/generate", id), map[string]any{
		"projectId": id, "templateId": req.TemplateID, "sections": report.Data.Sections,
	})
	common.Success(c, report)
}

// ReportGroups 报表分组（按归口团队）
// @Summary 报表分组：按归口团队聚合项目数与健康度分布（可见性谓词收窄）
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=[]ReportGroupResponse}
// @Router /api/v1/projects/report-groups [get]
func (h *Handler) ReportGroups(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	groups, err := h.service.ReportGroupsByOwnerTeam(
		c.Request.Context(), tenantID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, groups)
}

// writeReportAudit 报告域审计（resource=project；2s 超时、失败不阻断业务）。
func (h *Handler) writeReportAudit(
	c *gin.Context, tenantID, userID int, action, path string, metadata map[string]any,
) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	payload, _ := json.Marshal(metadata)
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if tenantID > 0 {
		auditCtx = tenantctx.WithTenantID(auditCtx, tenantID)
	} else {
		auditCtx = tenantctx.WithSystemBypass(auditCtx)
	}
	create := h.audit.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetResource("project").
		SetAction(action).
		SetPath(path).
		SetMethod(c.Request.Method).
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		SetSource("workbench").
		SetIP(c.ClientIP()).
		SetRequestID(c.GetString("request_id"))
	if userID > 0 {
		create = create.SetUserID(userID)
	}
	if tenantID > 0 {
		create = create.SetTenantID(tenantID)
	}
	if account := c.GetString("username"); account != "" {
		create = create.SetActorAccount(account)
	}
	_ = create.Exec(auditCtx)
}
