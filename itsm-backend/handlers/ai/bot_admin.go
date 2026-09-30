// Package ai 的 Bot 管理面 handler（B2-01）：
// 模板 CRUD 与工具授权 CRUD，路径前缀 /api/v1/admin/bots。
//
// 门禁：路由层统一挂 ai:read（读）/ ai:write（写）——管理权限复用既有 AI 权限码（BD8），
// 不新开权限码，避免角色矩阵扩散。
//
// 租户隔离：租户来自 gin 上下文（中间件注入），**不接收请求体 tenant_id**；
// 所有服务调用都带 tenantID 条件，跨租户访问表现为 404。
package ai

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/capability"
	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/service/bot"
)

// BotAdminHandler 暴露 Bot 模板/授权管理 API。
type BotAdminHandler struct {
	admin *bot.TemplateAdmin
	// M2 能力开关：运行时能力开关源（nil = 不做运行时门禁，保持既有行为）。
	capability capability.Source
}

func NewBotAdminHandler(admin *bot.TemplateAdmin) *BotAdminHandler {
	if admin == nil {
		return nil
	}
	return &BotAdminHandler{admin: admin}
}

// SetCapabilitySource 注入运行时能力开关源（M2 能力开关）。
func (h *BotAdminHandler) SetCapabilitySource(src capability.Source) { h.capability = src }

// capabilitiesFor 返回展示用能力块（页面据此渲染「已关闭」状态，与门禁同源）。
func (h *BotAdminHandler) capabilitiesFor(ctx context.Context, tenantID int) gin.H {
	view := gin.H{"botEnabled": true, "mcpWriteEnabled": true}
	if h == nil || h.capability == nil {
		return view
	}
	snap := h.capability.For(ctx, tenantID)
	view["botEnabled"] = snap.BotEnabled
	view["mcpWriteEnabled"] = snap.MCPWriteEnabled
	return view
}

// requireBotEnabled 写端运行时门禁（M2 能力开关）：关闭时 403；
// 读端保留（页面需要展示「已关闭」状态与重新启用入口）。
func (h *BotAdminHandler) requireBotEnabled(c *gin.Context, tenantID int) bool {
	if h == nil || h.capability == nil {
		return true
	}
	if h.capability.For(c.Request.Context(), tenantID).BotEnabled {
		return true
	}
	common.Fail(c, common.ForbiddenCode, "Bot 能力已在管理后台关闭（bot.enabled=false），请先在管理后台启用后再操作")
	return false
}

// 请求体（与 service.TemplateInput 同形；空值 = 不改动）。
type botTemplateRequest struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	Audience        string   `json:"audience"`
	RiskLimit       string   `json:"riskLimit"`
	Entrypoints     []string `json:"entrypoints"`
	SystemPromptRef string   `json:"systemPromptRef"`
	Status          string   `json:"status"`
}

type botGrantRequest struct {
	ToolName       string `json:"toolName"`
	RiskLimit      string `json:"riskLimit"`
	ArgsPolicyJSON string `json:"argsPolicyJson"`
}

// 响应契约：统一使用 common 包络（`{code,message,data}`）——与 MCP/LLM Provider 等
// 管理端点同源；前端 httpClient 只解包 `data`，裸 JSON 会导致列表静默为空。
func botTenantID(c *gin.Context) (int, bool) {
	value, exists := c.Get("tenant_id")
	if !exists {
		common.Fail(c, common.UnauthorizedCode, "缺少租户上下文")
		return 0, false
	}
	tenantID, ok := value.(int)
	if !ok || tenantID <= 0 {
		common.Fail(c, common.UnauthorizedCode, "租户上下文非法")
		return 0, false
	}
	return tenantID, true
}

func botPathID(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		common.Fail(c, common.BadRequestCode, name+" 必须是正整数")
		return 0, false
	}
	return id, true
}

// writeBotAdminError 把服务层稳定错误映射为统一错误包络（HTTP 状态码由 common.Fail 映射）。
func writeBotAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, bot.ErrTemplateNotFound), errors.Is(err, bot.ErrGrantNotFound):
		common.Fail(c, common.NotFoundCode, err.Error())
	case errors.Is(err, bot.ErrTemplateSlugUsed), errors.Is(err, bot.ErrValidation):
		common.Fail(c, common.BadRequestCode, err.Error())
	default:
		common.Fail(c, common.InternalErrorCode, "内部错误")
	}
}

func botTemplateView(t *ent.BotTemplate) gin.H {
	return gin.H{
		"id":              t.ID,
		"slug":            t.Slug,
		"name":            t.Name,
		"audience":        t.Audience,
		"riskLimit":       t.RiskLimit,
		"entrypointsJson": t.EntrypointsJSON,
		"systemPromptRef": t.SystemPromptRef,
		"status":          t.Status,
		"createdAt":       t.CreatedAt,
		"updatedAt":       t.UpdatedAt,
	}
}

func botGrantView(g *ent.BotToolGrant) gin.H {
	return gin.H{
		"id":             g.ID,
		"botId":          g.BotID,
		"toolName":       g.ToolName,
		"riskLimit":      g.RiskLimit,
		"argsPolicyJson": g.ArgsPolicyJSON,
		"createdAt":      g.CreatedAt,
		"updatedAt":      g.UpdatedAt,
	}
}

// ListBotTemplates GET /api/v1/admin/bots
func (h *BotAdminHandler) ListBotTemplates(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	templates, err := h.admin.ListTemplates(c.Request.Context(), tenantID)
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	items := make([]gin.H, 0, len(templates))
	for _, tpl := range templates {
		items = append(items, botTemplateView(tpl))
	}
	common.Success(c, gin.H{"items": items, "total": len(items), "capabilities": h.capabilitiesFor(c.Request.Context(), tenantID)})
}

// ListVisibleBots GET /api/v1/agent/bots（B2-04 工作区选择器）
//
// 与管理面的区别：① 挂 agent 前缀、只要求 ai:read（普通使用者可用）；
// ② 按调用角色做 audience 过滤（end_user 看不到 internal Bot）；
// ③ 只返回非 draft 且允许 chat 入口的 Bot（与策略层下发口径一致）；
// ④ 最小字段集（选择器只需要的 id/slug/name/audience），不下发风险上限/授权等管理信息。
func (h *BotAdminHandler) ListVisibleBots(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	role := c.GetString("role")
	capabilities := h.capabilitiesFor(c.Request.Context(), tenantID)
	// M2 能力开关：Bot 能力关闭时选择器返回空列表（前端据此隐藏/禁用入口），
	// 但仍下发 capabilities 块，便于页面显示「已关闭」而不是空白。
	if enabled, _ := capabilities["botEnabled"].(bool); !enabled {
		common.Success(c, gin.H{"items": []gin.H{}, "total": 0, "capabilities": capabilities})
		return
	}
	templates, err := h.admin.ListVisibleForChat(c.Request.Context(), tenantID, role)
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	items := make([]gin.H, 0, len(templates))
	for _, tpl := range templates {
		items = append(items, gin.H{
			"id":       tpl.ID,
			"slug":     tpl.Slug,
			"name":     tpl.Name,
			"audience": tpl.Audience,
		})
	}
	common.Success(c, gin.H{"items": items, "total": len(items), "capabilities": capabilities})
}

// GetBotTemplate GET /api/v1/admin/bots/:id（含授权清单）
func (h *BotAdminHandler) GetBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	tpl, err := h.admin.GetTemplate(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	grants, err := h.admin.ListGrants(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	grantViews := make([]gin.H, 0, len(grants))
	for _, grant := range grants {
		grantViews = append(grantViews, botGrantView(grant))
	}
	view := botTemplateView(tpl)
	view["grants"] = grantViews
	view["capabilities"] = h.capabilitiesFor(c.Request.Context(), tenantID)
	common.Success(c, view)
}

// CreateBotTemplate POST /api/v1/admin/bots
func (h *BotAdminHandler) CreateBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	if !h.requireBotEnabled(c, tenantID) {
		return
	}
	var req botTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.BadRequestCode, "请求体非法: "+err.Error())
		return
	}
	tpl, err := h.admin.CreateTemplate(c.Request.Context(), tenantID, bot.TemplateInput{
		Slug: req.Slug, Name: req.Name, Audience: req.Audience, RiskLimit: req.RiskLimit,
		Entrypoints: req.Entrypoints, SystemPromptRef: req.SystemPromptRef, Status: req.Status,
	})
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	// 201 保留创建语义，同时携带统一包络（common.Success 固定 200，故此处显式构造）。
	c.JSON(http.StatusCreated, common.Response{Code: common.SuccessCode, Message: "success", Data: botTemplateView(tpl)})
}

// UpdateBotTemplate PUT /api/v1/admin/bots/:id
func (h *BotAdminHandler) UpdateBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	if !h.requireBotEnabled(c, tenantID) {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	var req botTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.BadRequestCode, "请求体非法: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Slug) != "" {
		common.Fail(c, common.BadRequestCode, "slug 不可修改")
		return
	}
	tpl, err := h.admin.UpdateTemplate(c.Request.Context(), tenantID, id, bot.TemplateInput{
		Name: req.Name, Audience: req.Audience, RiskLimit: req.RiskLimit,
		Entrypoints: req.Entrypoints, SystemPromptRef: req.SystemPromptRef, Status: req.Status,
	})
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	common.Success(c, botTemplateView(tpl))
}

// DeleteBotTemplate DELETE /api/v1/admin/bots/:id（级联删除授权）
func (h *BotAdminHandler) DeleteBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	if !h.requireBotEnabled(c, tenantID) {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	if err := h.admin.DeleteTemplate(c.Request.Context(), tenantID, id); err != nil {
		writeBotAdminError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// ListBotGrants GET /api/v1/admin/bots/:id/grants
func (h *BotAdminHandler) ListBotGrants(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	grants, err := h.admin.ListGrants(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	items := make([]gin.H, 0, len(grants))
	for _, grant := range grants {
		items = append(items, botGrantView(grant))
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

// UpsertBotGrant PUT /api/v1/admin/bots/:id/grants
func (h *BotAdminHandler) UpsertBotGrant(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	if !h.requireBotEnabled(c, tenantID) {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	var req botGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.BadRequestCode, "请求体非法: "+err.Error())
		return
	}
	grant, err := h.admin.UpsertGrant(c.Request.Context(), tenantID, id, bot.GrantInput{
		ToolName: req.ToolName, RiskLimit: req.RiskLimit, ArgsPolicyJSON: req.ArgsPolicyJSON,
	})
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	common.Success(c, botGrantView(grant))
}

// DeleteBotGrant DELETE /api/v1/admin/bots/:id/grants/:grantId
func (h *BotAdminHandler) DeleteBotGrant(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	if !h.requireBotEnabled(c, tenantID) {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	grantID, ok := botPathID(c, "grantId")
	if !ok {
		return
	}
	if err := h.admin.DeleteGrant(c.Request.Context(), tenantID, id, grantID); err != nil {
		writeBotAdminError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}
