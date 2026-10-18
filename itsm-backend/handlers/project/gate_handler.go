package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 阶段门 HTTP 端点（PE-12）。权威设计：docs/project-management/plan/p1-gate-design.md。

// SetAuditClient 注入审计客户端（bootstrap；未注入时跳过审计写入）。
func (h *Handler) SetAuditClient(client *ent.Client) {
	h.audit = client
}

// ListGates 阶段门列表
// @Summary 项目阶段门列表（四道门 + 状态/阻塞明细）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=[]ProjectGateResponse}
// @Router /api/v1/projects/{id}/gates [get]
func (h *Handler) ListGates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.ListGates(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	// 实时预检：四道门按当前数据评估阻塞项（只读，不落库），进入页面即可看到待办。
	readiness, err := h.service.PreviewGates(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{
		"items":        ToGateResponseList(items),
		"gates":        GateOrder(),
		"projectGates": ToGateResponseList(items),
		"readiness":    readiness,
	})
}

// SubmitGate 提交阶段门
// @Summary 提交阶段门（门禁规则校验；通过后触发 BPMN 审批或降级自动通过）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param gate path string true "initiation|plan|acceptance|closure"
// @Success 200 {object} common.Response{data=ProjectGateResponse}
// @Router /api/v1/projects/{id}/gates/{gate}/submit [post]
func (h *Handler) SubmitGate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	gate := strings.ToLower(strings.TrimSpace(c.Param("gate")))
	var req GateSubmitRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
			return
		}
	}
	actorID := c.GetInt("user_id")
	item, err := h.service.SubmitGate(
		c.Request.Context(), tenantID, id, actorID, gate, c.GetString("username"),
		isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		// 门禁拒绝留痕（契约 §3.5：门禁拒绝记录原因与阻塞项摘要）。
		var blocked *GateBlockedError
		if errors.As(err, &blocked) {
			h.writeGateAudit(c, tenantID, actorID, id, gate, "gate_reject", map[string]any{
				"reason":   "gate_rules_blocked",
				"blockers": simplifyBlockersForAudit(blocked.Blockers),
			})
		}
		h.fail(c, err)
		return
	}
	action := "gate_submit"
	if item.Status == GateStatusPassed {
		action = "gate_pass"
	}
	h.writeGateAudit(c, tenantID, actorID, id, gate, action, map[string]any{
		"status":            item.Status,
		"processInstanceId": item.ProcessInstanceID,
		"autoApproved":      item.ProcessInstanceID == 0,
	})
	common.Success(c, ToGateResponse(item))
}

// DecideGate 裁决阶段门
// @Summary 裁决阶段门（approve 在流程结束/无流程后放行；reject 必须带原因）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param gate path string true "initiation|plan|acceptance|closure"
// @Param body body GateDecisionRequest true "裁决请求"
// @Success 200 {object} common.Response{data=ProjectGateResponse}
// @Router /api/v1/projects/{id}/gates/{gate}/decision [post]
func (h *Handler) DecideGate(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	gate := strings.ToLower(strings.TrimSpace(c.Param("gate")))
	var req GateDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	actorID := c.GetInt("user_id")
	item, err := h.service.DecideGate(
		c.Request.Context(), tenantID, id, actorID, gate, &req,
		isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	action := "gate_pass"
	if item.Status == GateStatusRejected {
		action = "gate_reject"
	}
	meta := map[string]any{"status": item.Status}
	if item.Status == GateStatusRejected {
		meta["reason"] = item.DecisionComment
	}
	h.writeGateAudit(c, tenantID, actorID, id, gate, action, meta)
	common.Success(c, ToGateResponse(item))
}

// writeGateAudit 阶段门审计（resource=project，action=gate_*；2s 超时、失败不阻断业务）。
func (h *Handler) writeGateAudit(c *gin.Context, tenantID, userID, projectID int, gate, action string, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["gate"] = gate
	metadata["projectId"] = projectID
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
		SetPath(fmt.Sprintf("/api/v1/projects/%d/gates/%s", projectID, gate)).
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

// simplifyBlockersForAudit 审计载荷：阻塞项摘要（最多 5 条，避免审计体膨胀）。
func simplifyBlockersForAudit(blockers []GateBlocker) []string {
	out := make([]string, 0, len(blockers))
	for i, b := range blockers {
		if i >= 5 {
			out = append(out, fmt.Sprintf("…（共 %d 项）", len(blockers)))
			break
		}
		item := b.Type
		if b.Detail != "" {
			item = fmt.Sprintf("%s: %s", b.Type, b.Detail)
		}
		out = append(out, item)
	}
	return out
}

