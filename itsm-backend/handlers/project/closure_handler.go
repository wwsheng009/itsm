package project

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 结项清单 HTTP 端点（PE-15）。权威设计：docs/project-management/plan/p1-closure-design.md。

// GetClosure 结项清单
// @Summary 结项清单（任务/单据/结项门/复盘 KB/资产登记/成员回收/CSAT）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=ProjectClosureResponse}
// @Router /api/v1/projects/{id}/closure [get]
func (h *Handler) GetClosure(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	view, err := h.service.GetClosure(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, view)
}

// GenerateRetroDraft 生成复盘 KB 草稿
// @Summary 生成复盘 KB 草稿（幂等；草稿态不自动发布）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=ProjectClosureResponse}
// @Router /api/v1/projects/{id}/closure/retro [post]
func (h *Handler) GenerateRetroDraft(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actorID := c.GetInt("user_id")
	view, err := h.service.GenerateRetroDraft(
		c.Request.Context(), tenantID, id, actorID, isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeClosureAudit(c, tenantID, actorID, id, "closure_retro_generate", map[string]any{
		"retroKbArticleId": view.RetroKbArticleID,
	})
	common.Success(c, view)
}

// ConfirmClosureAssets 资产/许可登记确认或豁免
// @Summary 资产/许可登记确认（confirm）或豁免（waive，必填原因）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body ClosureAssetsRequest true "确认/豁免请求"
// @Success 200 {object} common.Response{data=ProjectClosureResponse}
// @Router /api/v1/projects/{id}/closure/assets [post]
func (h *Handler) ConfirmClosureAssets(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req ClosureAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	actorID := c.GetInt("user_id")
	view, err := h.service.ConfirmAssets(
		c.Request.Context(), tenantID, id, actorID, &req, isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	action := "closure_assets_confirm"
	meta := map[string]any{"note": req.Note}
	if req.Action == "waive" {
		action = "closure_assets_waive"
		meta = map[string]any{"reason": req.Reason}
	}
	h.writeClosureAudit(c, tenantID, actorID, id, action, meta)
	common.Success(c, view)
}

// RecycleClosureMembers 成员回收
// @Summary 成员回收（回收 member 角色活跃成员；保留 PM 与干系人）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=ProjectClosureResponse}
// @Router /api/v1/projects/{id}/closure/members/recycle [post]
func (h *Handler) RecycleClosureMembers(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actorID := c.GetInt("user_id")
	view, err := h.service.RecycleMembers(
		c.Request.Context(), tenantID, id, actorID, isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeClosureAudit(c, tenantID, actorID, id, "closure_members_recycle", map[string]any{
		"recycledCount": view.MembersRecycledCount,
	})
	common.Success(c, view)
}

// TriggerClosureCSAT 触发结项 CSAT 调研
// @Summary 触发结项 CSAT 调研（幂等；结项时自动触发，失败可手动重试）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=ProjectClosureResponse}
// @Router /api/v1/projects/{id}/closure/csat [post]
func (h *Handler) TriggerClosureCSAT(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actorID := c.GetInt("user_id")
	view, err := h.service.TriggerCSAT(
		c.Request.Context(), tenantID, id, actorID, isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeClosureAudit(c, tenantID, actorID, id, "closure_csat_trigger", map[string]any{
		"csatSurveyId": view.CSATSurveyID,
	})
	common.Success(c, view)
}

// writeClosureAudit 结项清单审计（resource=project，action=closure_*；2s 超时、失败不阻断业务）。
func (h *Handler) writeClosureAudit(
	c *gin.Context, tenantID, userID, projectID int, action string, metadata map[string]any,
) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
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
		SetPath(fmt.Sprintf("/api/v1/projects/%d/closure", projectID)).
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

// simplifyClosureBlockersForAudit 审计载荷：结项阻塞项摘要（最多 5 条）。
func simplifyClosureBlockersForAudit(blockers []Blocker) []string {
	out := make([]string, 0, len(blockers))
	for i, b := range blockers {
		if i >= 5 {
			out = append(out, fmt.Sprintf("…（共 %d 项）", len(blockers)))
			break
		}
		item := b.Type
		if b.Title != "" {
			item = fmt.Sprintf("%s: %s", b.Type, b.Title)
		}
		out = append(out, item)
	}
	return out
}