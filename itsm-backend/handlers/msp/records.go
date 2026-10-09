// records.go：变更/问题/服务请求接入工作台（读面；跨租户单据流转 P2-③ / IP-P2-12）。
//
// 路由（§5 API 命名规范）：
//   - 聚合读：GET /api/v1/msp/workbench/{changes|problems|service-requests}
//   - 单条读：GET /api/v1/msp/{changes|problems|service-requests}/:id?customerTenantId=…
//
// 授权：端点级 RequireMSPPermission("msp_ticket","read")（路由声明）+ 服务层
// allocation 允许集合（越界 403 + tenant.scope_denied 审计）与声明租户一致性（400）。
package msp

import (
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// listWorkbenchRecords 聚合读（三类共用实现；recordType 由包装方法传入）。
func (h *Handler) listWorkbenchRecords(c *gin.Context, recordType string) {
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
	req := dto.MSPRecordQuery{
		CustomerTenantIDs: tenantIDs,
		Status:            c.Query("status"),
		Q:                 c.Query("q"),
		Cursor:            c.Query("cursor"),
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			common.Fail(c, common.ParamErrorCode, "limit 格式错误")
			return
		}
		req.Limit = n
	}

	resp, err := h.workbench.ListRecords(c.Request.Context(), actor, recordType, req)
	if err != nil {
		h.logger.Errorw("msp records list failed", "error", err, "record_type", recordType, "user_id", actor.UserID)
		failMSPAccess(c, err, "查询工作台单据失败")
		return
	}
	common.Success(c, resp)
}

// getWorkbenchRecord 单条详情（三类共用实现）。
func (h *Handler) getWorkbenchRecord(c *gin.Context, recordType string) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "非MSP用户")
		return
	}
	if h.workbench == nil {
		common.Fail(c, common.InternalErrorCode, "工作台服务未启用")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "单据ID无效")
		return
	}
	declaredTenantID, err := strconv.Atoi(strings.TrimSpace(c.Query("customerTenantId")))
	if err != nil || declaredTenantID <= 0 {
		common.Fail(c, common.ParamErrorCode, "缺少 customerTenantId（目标客户租户声明）")
		return
	}

	resp, err := h.workbench.GetRecord(c.Request.Context(), actor, recordType, id, declaredTenantID)
	if err != nil {
		h.logger.Errorw("msp records detail failed", "error", err, "record_type", recordType, "record_id", id, "user_id", actor.UserID)
		failMSPAccess(c, err, "查询单据详情失败")
		return
	}
	common.Success(c, resp)
}

// ListWorkbenchChanges GET /api/v1/msp/workbench/changes
func (h *Handler) ListWorkbenchChanges(c *gin.Context) {
	h.listWorkbenchRecords(c, service.MSPRecordChange)
}

// ListWorkbenchProblems GET /api/v1/msp/workbench/problems
func (h *Handler) ListWorkbenchProblems(c *gin.Context) {
	h.listWorkbenchRecords(c, service.MSPRecordProblem)
}

// ListWorkbenchServiceRequests GET /api/v1/msp/workbench/service-requests
func (h *Handler) ListWorkbenchServiceRequests(c *gin.Context) {
	h.listWorkbenchRecords(c, service.MSPRecordServiceRequest)
}

// GetWorkbenchChange GET /api/v1/msp/changes/:id
func (h *Handler) GetWorkbenchChange(c *gin.Context) {
	h.getWorkbenchRecord(c, service.MSPRecordChange)
}

// GetWorkbenchProblem GET /api/v1/msp/problems/:id
func (h *Handler) GetWorkbenchProblem(c *gin.Context) {
	h.getWorkbenchRecord(c, service.MSPRecordProblem)
}

// GetWorkbenchServiceRequest GET /api/v1/msp/service-requests/:id
func (h *Handler) GetWorkbenchServiceRequest(c *gin.Context) {
	h.getWorkbenchRecord(c, service.MSPRecordServiceRequest)
}
