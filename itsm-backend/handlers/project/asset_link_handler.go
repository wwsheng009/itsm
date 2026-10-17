package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 采购/资产结项登记 HTTP 端点（P2，PE-26）。
// 权威设计：docs/project-management/plan/p2-procurement-design.md §4。

// ListAssetLinks 登记列表
// @Summary 项目台账登记列表（asset|asset_license|vendor|contract；可见性谓词）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=AssetLinkListResponse}
// @Router /api/v1/projects/{id}/asset-links [get]
func (h *Handler) ListAssetLinks(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.ListAssetLinks(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, result)
}

// CreateAssetLink 新增登记
// @Summary 新增台账登记（同租户弱引用校验；重复 409；终态项目拒绝）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body AssetLinkCreateRequest true "登记请求"
// @Success 200 {object} common.Response{data=AssetLinkListResponse}
// @Router /api/v1/projects/{id}/asset-links [post]
func (h *Handler) CreateAssetLink(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req AssetLinkCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	actorID := c.GetInt("user_id")
	result, err := h.service.CreateAssetLink(
		c.Request.Context(), tenantID, id, actorID,
		isAdminViewer(middleware.GetContextRoles(c)), &req,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	req.Normalize()
	h.writeAssetLinkAudit(c, tenantID, actorID, id, "asset_link_create", map[string]any{
		"linkType": req.LinkType,
		"targetId": req.TargetID,
		"label":    labelForTarget(result, req.LinkType, req.TargetID),
		"noteLen":  len([]rune(req.Note)),
	})
	common.Success(c, result)
}

// DeleteAssetLink 删除登记（软删）
// @Summary 删除台账登记（软删；已删/不存在 404；终态项目拒绝）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param linkId path int true "登记ID"
// @Success 200 {object} common.Response{data=AssetLinkListResponse}
// @Router /api/v1/projects/{id}/asset-links/{linkId} [delete]
func (h *Handler) DeleteAssetLink(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	linkID, ok := parseID(c, "linkId")
	if !ok {
		return
	}
	actorID := c.GetInt("user_id")
	result, err := h.service.DeleteAssetLink(
		c.Request.Context(), tenantID, id, linkID, actorID,
		isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeAssetLinkAudit(c, tenantID, actorID, id, "asset_link_delete", map[string]any{
		"linkId": linkID,
	})
	common.Success(c, result)
}

// ListAssetLinkCandidates 候选选择器
// @Summary 台账候选查询（type=asset|asset_license|vendor|contract；q 模糊；limit 1..50）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param type query string true "asset|asset_license|vendor|contract"
// @Param q query string false "关键词（编号/名称/标题）"
// @Param limit query int false "返回条数（默认 20，上限 50）"
// @Success 200 {object} common.Response{data=AssetLinkCandidateListResponse}
// @Router /api/v1/projects/{id}/asset-link-candidates [get]
func (h *Handler) ListAssetLinkCandidates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			common.Fail(c, common.ParamErrorCode, "limit 必须为正整数")
			return
		}
		limit = parsed
	}
	result, err := h.service.ListAssetLinkCandidates(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		c.Query("type"), c.Query("q"), limit,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, result)
}

// labelForTarget 从列表响应中取目标标签（审计用；缺失时回退类型#ID）。
func labelForTarget(result *AssetLinkListResponse, linkType string, targetID int) string {
	for _, item := range result.Items {
		if item.LinkType == linkType && item.TargetID == targetID {
			if item.Label != "" {
				return item.Label
			}
			break
		}
	}
	return fmt.Sprintf("%s#%d", linkType, targetID)
}

// writeAssetLinkAudit 登记审计（resource=project，action=asset_link_*；2s 超时、失败不阻断业务）。
func (h *Handler) writeAssetLinkAudit(
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
		SetPath(fmt.Sprintf("/api/v1/projects/%d/asset-links", projectID)).
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