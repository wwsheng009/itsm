package project

import (
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// 单据挂接/解除（PE-5；包装方法供各域路由注册，见 router/common_system_routes.go）
// ---------------------------------------------------------------------------

func (h *Handler) attach(c *gin.Context, domain string) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req RecordAttachRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	if req.ProjectID <= 0 {
		common.Fail(c, common.ParamErrorCode, "projectId 不能为空")
		return
	}
	if err := h.service.AttachRecord(c.Request.Context(), tenantID, domain, id, req.ProjectID, middleware.GetContextRoles(c)); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"attached": true, "projectId": req.ProjectID})
}

func (h *Handler) detach(c *gin.Context, domain string) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	// detach 语义：清除单据当前项目挂接；可选 projectId 仅作提示（不参与判定）。
	if err := h.service.DetachRecord(c.Request.Context(), tenantID, domain, id, middleware.GetContextRoles(c)); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"detached": true})
}

// AttachTicket 工单挂接项目
// @Summary 工单挂接项目（idempotent；project:write 动态复核）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "工单ID"
// @Param body body RecordAttachRequest true "{projectId}"
// @Success 200 {object} common.Response
// @Router /api/v1/tickets/{id}/project [post]
func (h *Handler) AttachTicket(c *gin.Context) { h.attach(c, "ticket") }

// DetachTicket 工单解除项目
// @Summary 工单解除项目挂接
// @Tags 项目管理
// @Produce json
// @Param id path int true "工单ID"
// @Success 200 {object} common.Response
// @Router /api/v1/tickets/{id}/project [delete]
func (h *Handler) DetachTicket(c *gin.Context) { h.detach(c, "ticket") }

// AttachIncident 事件挂接项目
// @Summary 事件挂接项目
// @Tags 项目管理
// @Accept json
// @Produce json
// @Router /api/v1/incidents/{id}/project [post]
func (h *Handler) AttachIncident(c *gin.Context) { h.attach(c, "incident") }

// DetachIncident 事件解除项目
// @Summary 事件解除项目挂接
// @Tags 项目管理
// @Produce json
// @Router /api/v1/incidents/{id}/project [delete]
func (h *Handler) DetachIncident(c *gin.Context) { h.detach(c, "incident") }

// AttachProblem 问题挂接项目
// @Summary 问题挂接项目
// @Tags 项目管理
// @Accept json
// @Produce json
// @Router /api/v1/problems/{id}/project [post]
func (h *Handler) AttachProblem(c *gin.Context) { h.attach(c, "problem") }

// DetachProblem 问题解除项目
// @Summary 问题解除项目挂接
// @Tags 项目管理
// @Produce json
// @Router /api/v1/problems/{id}/project [delete]
func (h *Handler) DetachProblem(c *gin.Context) { h.detach(c, "problem") }

// AttachChange 变更挂接项目
// @Summary 变更挂接项目
// @Tags 项目管理
// @Accept json
// @Produce json
// @Router /api/v1/changes/{id}/project [post]
func (h *Handler) AttachChange(c *gin.Context) { h.attach(c, "change") }

// DetachChange 变更解除项目
// @Summary 变更解除项目挂接
// @Tags 项目管理
// @Produce json
// @Router /api/v1/changes/{id}/project [delete]
func (h *Handler) DetachChange(c *gin.Context) { h.detach(c, "change") }

// AttachServiceRequest 服务请求挂接项目
// @Summary 服务请求挂接项目
// @Tags 项目管理
// @Accept json
// @Produce json
// @Router /api/v1/service-requests/{id}/project [post]
func (h *Handler) AttachServiceRequest(c *gin.Context) { h.attach(c, "service_request") }

// DetachServiceRequest 服务请求解除项目
// @Summary 服务请求解除项目挂接
// @Tags 项目管理
// @Produce json
// @Router /api/v1/service-requests/{id}/project [delete]
func (h *Handler) DetachServiceRequest(c *gin.Context) { h.detach(c, "service_request") }

// getRecordProject 单据→项目反向查询（详情页展示"所属项目"；五域共用）。
func (h *Handler) getRecordProject(c *gin.Context, domain string) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	view, err := h.service.GetRecordProject(
		c.Request.Context(), tenantID, domain, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, RecordProjectResponse{Project: view})
}

// GetTicketProject 工单所属项目
// @Summary 工单所属项目（未挂接/项目不可见时 project=null）
// @Tags 项目管理
// @Produce json
// @Param id path int true "工单ID"
// @Success 200 {object} common.Response{data=RecordProjectResponse}
// @Router /api/v1/tickets/{id}/project [get]
func (h *Handler) GetTicketProject(c *gin.Context) { h.getRecordProject(c, "ticket") }

// GetIncidentProject 事件所属项目
// @Summary 事件所属项目（未挂接/项目不可见时 project=null）
// @Tags 项目管理
// @Produce json
// @Router /api/v1/incidents/{id}/project [get]
func (h *Handler) GetIncidentProject(c *gin.Context) { h.getRecordProject(c, "incident") }

// GetProblemProject 问题所属项目
// @Summary 问题所属项目（未挂接/项目不可见时 project=null）
// @Tags 项目管理
// @Produce json
// @Router /api/v1/problems/{id}/project [get]
func (h *Handler) GetProblemProject(c *gin.Context) { h.getRecordProject(c, "problem") }

// GetChangeProject 变更所属项目
// @Summary 变更所属项目（未挂接/项目不可见时 project=null）
// @Tags 项目管理
// @Produce json
// @Router /api/v1/changes/{id}/project [get]
func (h *Handler) GetChangeProject(c *gin.Context) { h.getRecordProject(c, "change") }

// GetServiceRequestProject 服务请求所属项目
// @Summary 服务请求所属项目（未挂接/项目不可见时 project=null）
// @Tags 项目管理
// @Produce json
// @Router /api/v1/service-requests/{id}/project [get]
func (h *Handler) GetServiceRequestProject(c *gin.Context) { h.getRecordProject(c, "service_request") }

// ListProjectRecords 项目关联单据聚合读
// @Summary 项目关联单据（五域聚合；type 可选，空=合并）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param type query string false "ticket|incident|problem|change|service_request"
// @Param status query string false "状态过滤"
// @Success 200 {object} common.Response{data=common.ListResponse}
// @Router /api/v1/projects/{id}/records [get]
func (h *Handler) ListProjectRecords(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	page := common.GetPaginationFromQuery(c)
	items, total, err := h.service.ListRecords(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		strings.TrimSpace(c.Query("type")), strings.TrimSpace(c.Query("status")),
		page.GetOffset(), page.GetLimit(),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, common.NewListResponse(items, common.NewPaginationResponse(page.Page, page.PageSize, total)))
}

// GenerateTaskTicket 任务生成工单
// @Summary 任务生成工单（一任务一主工单；重复 409）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Param body body TaskTicketRequest false "生成参数"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId}/tickets [post]
func (h *Handler) GenerateTaskTicket(c *gin.Context) {
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
	req := &TaskTicketRequest{}
	_ = c.ShouldBindJSON(req)
	task, err := h.service.GenerateTaskTicket(c.Request.Context(), tenantID, id, taskID, c.GetInt("user_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}

// UnbindTaskTicket 任务解绑工单
// @Summary 任务解绑主工单（解绑后可重建）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param taskId path int true "任务ID"
// @Success 200 {object} common.Response{data=ProjectTaskResponse}
// @Router /api/v1/projects/{id}/tasks/{taskId}/tickets [delete]
func (h *Handler) UnbindTaskTicket(c *gin.Context) {
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
	task, err := h.service.UnbindTaskTicket(c.Request.Context(), tenantID, id, taskID)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToTaskResponse(task))
}
