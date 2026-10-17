package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 风险/问题台账 HTTP 端点（P2，PE-21）。
// 权威设计：docs/project-management/plan/p2-risk-design.md。

// ListRisks 风险/问题列表
// @Summary 项目风险/问题列表（kind/status/level 过滤；可见性谓词）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param kind query string false "risk|issue"
// @Param status query string false "open|mitigating|closed"
// @Param level query string false "low|medium|high"
// @Success 200 {object} common.Response{data=[]ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks [get]
func (h *Handler) ListRisks(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListRisks(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		strings.TrimSpace(c.Query("kind")),
		strings.TrimSpace(c.Query("status")),
		strings.TrimSpace(c.Query("level")),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToRiskResponseList(items, h.service.RiskOwnerNames(c.Request.Context(), items)))
}

// CreateRisk 创建风险/问题
// @Summary 创建风险/问题（负责人须为项目活跃成员；终态项目拒绝）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body RiskCreateRequest true "风险请求"
// @Success 200 {object} common.Response{data=ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks [post]
func (h *Handler) CreateRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req RiskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	created, err := h.service.CreateRisk(c.Request.Context(), tenantID, id, c.GetInt("user_id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	userID := c.GetInt("user_id")
	h.writeRiskAudit(c, tenantID, userID, id, created.ID, "risk_create", map[string]any{
		"kind":  created.Kind,
		"level": created.Level,
	})
	names := h.service.RiskOwnerNames(c.Request.Context(), []*ent.ProjectRisk{created})
	common.Success(c, ToRiskResponseList([]*ent.ProjectRisk{created}, names)[0])
}

// GetRisk 风险/问题详情
// @Summary 风险/问题详情（可见性谓词）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param riskId path int true "风险ID"
// @Success 200 {object} common.Response{data=ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks/{riskId} [get]
func (h *Handler) GetRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	riskID, ok := parseID(c, "riskId")
	if !ok {
		return
	}
	item, err := h.service.GetRiskForViewer(
		c.Request.Context(), tenantID, id, riskID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	names := h.service.RiskOwnerNames(c.Request.Context(), []*ent.ProjectRisk{item})
	common.Success(c, ToRiskResponseList([]*ent.ProjectRisk{item}, names)[0])
}

// UpdateRisk 更新风险/问题
// @Summary 更新风险/问题（不含状态；version 乐观锁）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param riskId path int true "风险ID"
// @Param body body RiskUpdateRequest true "更新请求"
// @Success 200 {object} common.Response{data=ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks/{riskId} [put]
func (h *Handler) UpdateRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	riskID, ok := parseID(c, "riskId")
	if !ok {
		return
	}
	var req RiskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.UpdateRisk(c.Request.Context(), tenantID, id, riskID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	userID := c.GetInt("user_id")
	h.writeRiskAudit(c, tenantID, userID, id, riskID, "risk_update", map[string]any{
		"kind":    updated.Kind,
		"level":   updated.Level,
		"version": updated.Version,
	})
	names := h.service.RiskOwnerNames(c.Request.Context(), []*ent.ProjectRisk{updated})
	common.Success(c, ToRiskResponseList([]*ent.ProjectRisk{updated}, names)[0])
}

// TransitionRisk 风险/问题状态流转
// @Summary 风险/问题状态流转（mitigate|close|reopen；close 必填 resolution）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param riskId path int true "风险ID"
// @Param body body RiskStatusRequest true "流转请求"
// @Success 200 {object} common.Response{data=ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks/{riskId}/status [post]
func (h *Handler) TransitionRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	riskID, ok := parseID(c, "riskId")
	if !ok {
		return
	}
	var req RiskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.TransitionRisk(c.Request.Context(), tenantID, id, riskID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	userID := c.GetInt("user_id")
	h.writeRiskAudit(c, tenantID, userID, id, riskID, "risk_status", map[string]any{
		"action": strings.TrimSpace(req.Action),
		"status": updated.Status,
	})
	names := h.service.RiskOwnerNames(c.Request.Context(), []*ent.ProjectRisk{updated})
	common.Success(c, ToRiskResponseList([]*ent.ProjectRisk{updated}, names)[0])
}

// ConvertRisk 风险/问题转换
// @Summary 风险/问题转问题单或变更单（幂等：同目标二次转换 409 + existingId）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param riskId path int true "风险ID"
// @Param body body RiskConvertRequest true "转换请求"
// @Success 200 {object} common.Response{data=ProjectRiskResponse}
// @Router /api/v1/projects/{id}/risks/{riskId}/convert [post]
func (h *Handler) ConvertRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	riskID, ok := parseID(c, "riskId")
	if !ok {
		return
	}
	var req RiskConvertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	userID := c.GetInt("user_id")
	updated, createdID, err := h.service.ConvertRisk(c.Request.Context(), tenantID, id, riskID, userID, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeRiskAudit(c, tenantID, userID, id, riskID, "risk_convert", map[string]any{
		"target":    strings.TrimSpace(req.Target),
		"createdId": createdID,
	})
	names := h.service.RiskOwnerNames(c.Request.Context(), []*ent.ProjectRisk{updated})
	common.Success(c, &RiskConvertResponse{
		Target:    strings.TrimSpace(req.Target),
		CreatedID: createdID,
		Risk:      ToRiskResponseList([]*ent.ProjectRisk{updated}, names)[0],
	})
}

// DeleteRisk 删除风险/问题（软删）
// @Summary 删除风险/问题（软删；已转换记录仍可删，关联实体不受影响）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param riskId path int true "风险ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id}/risks/{riskId} [delete]
func (h *Handler) DeleteRisk(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	riskID, ok := parseID(c, "riskId")
	if !ok {
		return
	}
	if err := h.service.DeleteRisk(c.Request.Context(), tenantID, id, riskID); err != nil {
		h.fail(c, err)
		return
	}
	userID := c.GetInt("user_id")
	h.writeRiskAudit(c, tenantID, userID, id, riskID, "risk_delete", nil)
	common.Success(c, gin.H{"deleted": true})
}

// writeRiskAudit 风险审计（resource=project，action=risk_*；2s 超时、失败不阻断业务）。
func (h *Handler) writeRiskAudit(c *gin.Context, tenantID, userID, projectID, riskID int, action string, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["projectId"] = projectID
	metadata["riskId"] = riskID
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
		SetPath(fmt.Sprintf("/api/v1/projects/%d/risks/%d", projectID, riskID)).
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