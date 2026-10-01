// workbench.go：MSP 跨客户工作台 HTTP 端点（IP-P0-7）。
//
// 允许集合一律来自服务端（middleware.MSPContext.AllowedCustomers），
// 请求中的 customerTenantIds 仅作筛选；显式请求未分配租户 → 403 MSP_ALLOCATION_REQUIRED。
package msp

import (
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// workbenchActorFrom 从 MSPContext 构造工作台身份快照。
func workbenchActorFrom(c *gin.Context) (service.MSPWorkbenchActor, bool) {
	mspCtx, exists := middleware.GetMSPContext(c)
	if !exists || !mspCtx.IsMSP {
		return service.MSPWorkbenchActor{}, false
	}
	return service.MSPWorkbenchActor{
		UserID:           mspCtx.MSPUserID,
		Username:         c.GetString("username"),
		HomeTenantID:     c.GetInt("tenant_id"),
		MSPRole:          mspCtx.Role,
		AllowedCustomers: mspCtx.AllowedCustomers,
	}, true
}

// parseCustomerTenantIDs 解析 customerTenantIds=all|1,2；空值视为 all。
func parseCustomerTenantIDs(raw string) ([]int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil || id <= 0 {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

// ListWorkbenchTickets GET /api/v1/msp/workbench/tickets
func (h *Handler) ListWorkbenchTickets(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.workbench == nil {
		common.Fail(c, common.InternalErrorCode, "工作台服务未启用")
		return
	}
	tenantIDs, ok := parseCustomerTenantIDs(c.Query("customerTenantIds"))
	if !ok {
		common.Fail(c, common.ParamErrorCode, "customerTenantIds 格式错误（all 或逗号分隔数字）")
		return
	}

	req := dto.WorkbenchTicketQuery{
		CustomerTenantIDs: tenantIDs,
		Status:            c.Query("status"),
		Priority:          c.Query("priority"),
		Q:                 c.Query("q"),
		Sort:              c.Query("sort"),
		Cursor:            c.Query("cursor"),
	}
	if raw := strings.TrimSpace(c.Query("assigneeId")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			common.Fail(c, common.ParamErrorCode, "assigneeId 格式错误")
			return
		}
		req.AssigneeID = id
	}
	if raw := strings.TrimSpace(c.Query("updatedAfter")); raw != "" {
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			common.Fail(c, common.ParamErrorCode, "updatedAfter 格式错误（RFC3339）")
			return
		}
		req.UpdatedAfter = &ts
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			common.Fail(c, common.ParamErrorCode, "limit 格式错误")
			return
		}
		req.Limit = n
	}

	resp, err := h.workbench.ListTickets(c.Request.Context(), actor, req)
	if err != nil {
		h.logger.Errorw("workbench list failed", "error", err, "user_id", actor.UserID)
		failMSPAccess(c, err, "查询工作台工单失败")
		return
	}
	common.Success(c, resp)
}

// GetWorkbenchSummary GET /api/v1/msp/workbench/summary
func (h *Handler) GetWorkbenchSummary(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.workbench == nil {
		common.Fail(c, common.InternalErrorCode, "工作台服务未启用")
		return
	}
	resp, err := h.workbench.Summary(c.Request.Context(), actor)
	if err != nil {
		h.logger.Errorw("workbench summary failed", "error", err, "user_id", actor.UserID)
		failMSPAccess(c, err, "查询工作台计数失败")
		return
	}
	common.Success(c, resp)
}

// ReplyWorkbenchTicket POST /api/v1/msp/tickets/:id/reply
func (h *Handler) ReplyWorkbenchTicket(c *gin.Context) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ticketID <= 0 {
		common.Fail(c, common.ParamErrorCode, "工单ID无效")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.workbench == nil {
		common.Fail(c, common.InternalErrorCode, "工作台服务未启用")
		return
	}
	var req dto.WorkbenchReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	comment, err := h.workbench.Reply(c.Request.Context(), actor, ticketID, req)
	if err != nil {
		h.logger.Errorw("workbench reply failed", "error", err, "ticket_id", ticketID)
		failMSPAccess(c, err, "回复工单失败")
		return
	}
	common.Success(c, comment)
}

// ChangeWorkbenchTicketStatus POST /api/v1/msp/tickets/:id/status
func (h *Handler) ChangeWorkbenchTicketStatus(c *gin.Context) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ticketID <= 0 {
		common.Fail(c, common.ParamErrorCode, "工单ID无效")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.workbench == nil {
		common.Fail(c, common.InternalErrorCode, "工作台服务未启用")
		return
	}
	var req dto.WorkbenchStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	updated, err := h.workbench.ChangeStatus(c.Request.Context(), actor, ticketID, req)
	if err != nil {
		h.logger.Errorw("workbench status change failed", "error", err, "ticket_id", ticketID)
		failMSPAccess(c, err, "更新工单状态失败")
		return
	}
	common.Success(c, updated)
}
