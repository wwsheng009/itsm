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
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/ent"
	"itsm-backend/service/bot"
)

// BotAdminHandler 暴露 Bot 模板/授权管理 API。
type BotAdminHandler struct {
	admin *bot.TemplateAdmin
}

func NewBotAdminHandler(admin *bot.TemplateAdmin) *BotAdminHandler {
	if admin == nil {
		return nil
	}
	return &BotAdminHandler{admin: admin}
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

func botTenantID(c *gin.Context) (int, bool) {
	value, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少租户上下文"})
		return 0, false
	}
	tenantID, ok := value.(int)
	if !ok || tenantID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "租户上下文非法"})
		return 0, false
	}
	return tenantID, true
}

func botPathID(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " 必须是正整数"})
		return 0, false
	}
	return id, true
}

// writeBotAdminError 把服务层稳定错误映射为 HTTP 状态码。
func writeBotAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, bot.ErrTemplateNotFound), errors.Is(err, bot.ErrGrantNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, bot.ErrTemplateSlugUsed), errors.Is(err, bot.ErrValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "内部错误"})
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
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
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
	c.JSON(http.StatusOK, view)
}

// CreateBotTemplate POST /api/v1/admin/bots
func (h *BotAdminHandler) CreateBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	var req botTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体非法: " + err.Error()})
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
	c.JSON(http.StatusCreated, botTemplateView(tpl))
}

// UpdateBotTemplate PUT /api/v1/admin/bots/:id
func (h *BotAdminHandler) UpdateBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	var req botTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体非法: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Slug) != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "slug 不可修改"})
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
	c.JSON(http.StatusOK, botTemplateView(tpl))
}

// DeleteBotTemplate DELETE /api/v1/admin/bots/:id（级联删除授权）
func (h *BotAdminHandler) DeleteBotTemplate(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
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
	c.JSON(http.StatusOK, gin.H{"deleted": true})
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
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// UpsertBotGrant PUT /api/v1/admin/bots/:id/grants
func (h *BotAdminHandler) UpsertBotGrant(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
		return
	}
	id, ok := botPathID(c, "id")
	if !ok {
		return
	}
	var req botGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体非法: " + err.Error()})
		return
	}
	grant, err := h.admin.UpsertGrant(c.Request.Context(), tenantID, id, bot.GrantInput{
		ToolName: req.ToolName, RiskLimit: req.RiskLimit, ArgsPolicyJSON: req.ArgsPolicyJSON,
	})
	if err != nil {
		writeBotAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, botGrantView(grant))
}

// DeleteBotGrant DELETE /api/v1/admin/bots/:id/grants/:grantId
func (h *BotAdminHandler) DeleteBotGrant(c *gin.Context) {
	tenantID, ok := botTenantID(c)
	if !ok {
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
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
