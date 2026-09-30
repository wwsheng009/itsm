package mcp

import (
	"net/http"
	"strconv"

	"itsm-backend/capability"
	"itsm-backend/common"
	"itsm-backend/mcp/admin"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 本文件是 M0-08 的 gin 处理器（分析报告 §5.5）：薄层——身份提取 → JSON 绑定 → 服务调用 → 契约错误映射 → 统一响应。
//
// 鉴权不在本层：路由注册时统一挂权限中间件（读 = ai:read；写 = system:write；
// M0-10 切换为 mcp:read / mcp:admin 并补权限码）。
//
// 错误响应形状：HTTP 状态码按 §5.5 映射；body 保留 common 既有封套（int 业务码 + message），
// 额外给出 errorCode（§5.5 字符串码）供前端分支；绝不回显凭据明文（服务层已掩码）。
type Handler struct {
	svc *admin.Service
	// M2 能力开关：运行时能力开关源（nil = 不做运行时门禁，保持既有行为）。
	capability capability.Source
}

// NewHandler 构造管理面 handler（svc 为 nil 时所有方法返回 503）。
func NewHandler(svc *admin.Service) *Handler { return &Handler{svc: svc} }

// SetCapabilitySource 注入运行时能力开关源（M2 能力开关）。
func (h *Handler) SetCapabilitySource(src capability.Source) { h.capability = src }

// RequireEnabled 是治理写端的运行时门禁（M2 能力开关）：mcp.enabled=false → 403。
//
// 读端不挂：管理页需要展示「已关闭」状态与重新开启入口（开关面板本身必须可达）。
func (h *Handler) RequireEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h == nil || h.capability == nil {
			c.Next()
			return
		}
		tenantID := c.GetInt("tenant_id")
		if h.capability.For(c.Request.Context(), tenantID).MCPEnabled {
			c.Next()
			return
		}
		common.Fail(c, common.ForbiddenCode, "MCP 能力已在管理后台关闭（mcp.enabled=false），请先在管理后台启用后再操作")
		c.Abort()
	}
}

// capabilitiesView 返回展示用能力块（页面据此渲染「已关闭」状态，与门禁同源）。
func (h *Handler) capabilitiesView(c *gin.Context) *admin.CapabilityView {
	view := &admin.CapabilityView{MCPEnabled: true, BotEnabled: true}
	if h == nil || h.capability == nil {
		return view
	}
	snap := h.capability.For(c.Request.Context(), c.GetInt("tenant_id"))
	view.MCPEnabled = snap.MCPEnabled
	view.MCPWriteEnabled = snap.MCPWriteEnabled
	view.BotEnabled = snap.BotEnabled
	return view
}

// ListServers GET /api/v1/ai/mcp-servers
func (h *Handler) ListServers(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondError(c, admin.NewAdminError(http.StatusServiceUnavailable, admin.CodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.ListServers(c.Request.Context(), actor)
	if err != nil {
		respondError(c, err)
		return
	}
	// M2 能力开关：列表响应追加展示用能力块（与门禁同源；未注入能力源时为默认值）。
	result.Capabilities = h.capabilitiesView(c)
	common.Success(c, result)
}

// Health GET /api/v1/ai/mcp-servers/health
func (h *Handler) Health(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondError(c, admin.NewAdminError(http.StatusServiceUnavailable, admin.CodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.HealthSummary(c.Request.Context(), actor)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, result)
}

// GetServer GET /api/v1/ai/mcp-servers/:id
func (h *Handler) GetServer(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.GetServer(c.Request.Context(), actor, id)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// CreateServer POST /api/v1/ai/mcp-servers
func (h *Handler) CreateServer(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req admin.CreateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.svc.CreateServer(c.Request.Context(), actor, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// UpdateServer PUT /api/v1/ai/mcp-servers/:id
func (h *Handler) UpdateServer(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req admin.UpdateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.svc.UpdateServer(c.Request.Context(), actor, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// DeleteServer DELETE /api/v1/ai/mcp-servers/:id
func (h *Handler) DeleteServer(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteServer(c.Request.Context(), actor, id); err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// TestServer POST /api/v1/ai/mcp-servers/:id/test（同步 ≤10s；不落库）
func (h *Handler) TestServer(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req admin.TestServerRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ParamErrorWithErr(c, err, "请求参数错误")
			return
		}
	}
	result, err := h.svc.TestServer(c.Request.Context(), actor, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, result)
}

// EnableServer POST /api/v1/ai/mcp-servers/:id/enable（202 + 状态回读）
func (h *Handler) EnableServer(c *gin.Context) {
	h.lifecycle(c, func(actor admin.Actor, id int) (any, error) {
		return h.svc.EnableServer(c.Request.Context(), actor, id)
	})
}

// DisableServer POST /api/v1/ai/mcp-servers/:id/disable（202 + 状态回读）
func (h *Handler) DisableServer(c *gin.Context) {
	h.lifecycle(c, func(actor admin.Actor, id int) (any, error) {
		return h.svc.DisableServer(c.Request.Context(), actor, id)
	})
}

// ReloadServer POST /api/v1/ai/mcp-servers/:id/reload（202 + 状态回读）
func (h *Handler) ReloadServer(c *gin.Context) {
	h.lifecycle(c, func(actor admin.Actor, id int) (any, error) {
		return h.svc.ReloadServer(c.Request.Context(), actor, id)
	})
}

func (h *Handler) lifecycle(c *gin.Context, run func(actor admin.Actor, id int) (any, error)) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := run(actor, id)
	if err != nil {
		respondError(c, err)
		return
	}
	// D8：异步语义——202 + 状态回读（前端按 2s 间隔轮询 GET /:id）。
	c.JSON(http.StatusAccepted, gin.H{
		"code":    common.SuccessCode,
		"message": "accepted",
		"data":    view,
	})
}

// ListTools GET /api/v1/ai/mcp-servers/:id/tools
func (h *Handler) ListTools(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	tools, err := h.svc.ListTools(c.Request.Context(), actor, id)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, gin.H{"items": tools, "total": len(tools)})
}

// SetToolEnabled POST /api/v1/ai/mcp-servers/:id/tools/:callable/enable
func (h *Handler) SetToolEnabled(c *gin.Context) {
	h.setToolEnabled(c, true)
}

// SetToolDisabled POST /api/v1/ai/mcp-servers/:id/tools/:callable/disable
func (h *Handler) SetToolDisabled(c *gin.Context) {
	h.setToolEnabled(c, false)
}

func (h *Handler) setToolEnabled(c *gin.Context, enabled bool) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	callable := c.Param("callable")
	if callable == "" {
		common.ParamError(c, "callable 不能为空")
		return
	}
	view, err := h.svc.SetToolEnabled(c.Request.Context(), actor, id, callable, enabled)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// BulkSetTools POST /api/v1/ai/mcp-servers/:id/tools/bulk（tools 为空 = 全部）
func (h *Handler) BulkSetTools(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req admin.BulkToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	affected, err := h.svc.BulkSetTools(c.Request.Context(), actor, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, gin.H{"affected": affected, "enabled": req.Enabled})
}

// SetToolClassification PUT /api/v1/ai/mcp-servers/:id/tools/:callable/classification
func (h *Handler) SetToolClassification(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	callable := c.Param("callable")
	if callable == "" {
		common.ParamError(c, "callable 不能为空")
		return
	}
	var req admin.ClassificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.svc.SetToolClassification(c.Request.Context(), actor, id, callable, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// RotateCredential POST /api/v1/ai/mcp-servers/:id/rotate-credential
func (h *Handler) RotateCredential(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req admin.RotateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	view, err := h.svc.RotateCredential(c.Request.Context(), actor, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, view)
}

// Events GET /api/v1/ai/mcp-servers/:id/events
func (h *Handler) Events(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	events, err := h.svc.ServerEvents(c.Request.Context(), actor, id)
	if err != nil {
		respondError(c, err)
		return
	}
	common.Success(c, gin.H{"items": events, "total": len(events)})
}

// actor 提取租户/用户/IP；缺失按既有口径返回 401。
func (h *Handler) actor(c *gin.Context) (admin.Actor, bool) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.AuthFailed(c, "缺少有效身份上下文")
		return admin.Actor{}, false
	}
	return admin.Actor{TenantID: tenantID, UserID: userID, IP: c.ClientIP()}, true
}

func pathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "id 非法")
		return 0, false
	}
	return id, true
}

// respondError 把领域错误映射为契约响应（HTTP 状态 + int 业务码 + §5.5 字符串码）。
func respondError(c *gin.Context, err error) {
	adminErr, ok := admin.AsAdminError(err)
	if !ok {
		adminErr = admin.NewAdminError(http.StatusInternalServerError, admin.CodeInternal, "服务内部错误")
		zap.S().Errorw("mcp admin handler error",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"err", errText(err),
		)
	}
	status := adminErr.Status
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	if status >= http.StatusInternalServerError {
		zap.S().Errorw("mcp admin handler error",
			"error_code", adminErr.Code,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"err", errText(adminErr.Err),
		)
	}
	c.JSON(status, gin.H{
		"code":      statusToAppCode(status),
		"errorCode": string(adminErr.Code),
		"message":   adminErr.Message,
	})
	c.Abort()
}

func statusToAppCode(status int) int {
	switch status {
	case http.StatusBadRequest:
		return common.BadRequestCode
	case http.StatusUnauthorized:
		return common.UnauthorizedCode
	case http.StatusForbidden:
		return common.ForbiddenCode
	case http.StatusNotFound:
		return common.NotFoundCode
	case http.StatusConflict:
		return common.ConflictCode
	case http.StatusUnprocessableEntity:
		return common.UnprocessableEntityCode
	case http.StatusServiceUnavailable:
		return common.ServiceUnavailableCode
	default:
		return common.InternalErrorCode
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 300 {
		return text[:300]
	}
	return text
}
