package invitation

import (
	"errors"
	"io"
	"strconv"

	"itsm-backend/common"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Handler 邀请生命周期 HTTP 入口（IP-P1-4b；契约 §4.0-C）。
//
// 路由：
//   - GET  /api/v1/users/invitations               邀请列表（认证 + user:write；IP-P1-4c）
//   - POST /api/v1/users/invitations                创建邀请（认证 + user:write）
//   - POST /api/v1/users/invitations/:id/revoke     撤销邀请（认证 + user:write）
//   - GET  /api/v1/auth/invitations/:token          落地页最小回显（公开）
//   - POST /api/v1/auth/invitations/:token/accept   接受邀请/设置密码（公开 + 限流）
type Handler struct {
	svc    *service.InvitationService
	logger *zap.SugaredLogger
}

// NewHandler 构造邀请 handler。
func NewHandler(svc *service.InvitationService, logger *zap.SugaredLogger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

type createInvitationRequest struct {
	TenantID     int    `json:"tenantId"`
	Email        string `json:"email" binding:"required,email"`
	RoleID       int    `json:"roleId" binding:"required"`
	MSPRole      string `json:"mspRole"`
	TargetUserID int    `json:"targetUserId"`
}

type acceptInvitationRequest struct {
	Password string `json:"password"`
	Name     string `json:"name"`
}

// Create 创建邀请：tenantId 省略时默认当前租户（租户内邀请）。
func (h *Handler) Create(c *gin.Context) {
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "邀请服务未启用")
		return
	}
	var req createInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	if req.TenantID <= 0 {
		req.TenantID = c.GetInt("tenant_id")
	}
	if req.TenantID <= 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}
	actor := actorFromContext(c)
	result, err := h.svc.Create(c.Request.Context(), actor, &service.CreateInvitationRequest{
		TenantID:     req.TenantID,
		Email:        req.Email,
		RoleID:       req.RoleID,
		MSPRole:      req.MSPRole,
		TargetUserID: req.TargetUserID,
	})
	if err != nil {
		respondInvitationError(c, err)
		return
	}
	common.Success(c, gin.H{
		"id":        result.Invitation.ID,
		"email":     result.Invitation.Email,
		"status":    result.Invitation.Status,
		"expiresAt": result.ExpiresAt,
		"inviteUrl": result.InviteURL,
		"emailSent": result.EmailSent,
	})
}

// Inspect 落地页最小回显（公开）：邮箱脱敏，不泄露 token 归属细节。
func (h *Handler) Inspect(c *gin.Context) {
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "邀请服务未启用")
		return
	}
	token := c.Param("token")
	info, err := h.svc.Inspect(c.Request.Context(), token)
	if err != nil {
		respondInvitationError(c, err)
		return
	}
	common.Success(c, info)
}

// Accept 接受邀请（公开）：设置密码并完成建号/绑定 + membership。
func (h *Handler) Accept(c *gin.Context) {
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "邀请服务未启用")
		return
	}
	var req acceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	result, err := h.svc.Accept(c.Request.Context(), c.Param("token"), &service.AcceptInvitationRequest{
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		respondInvitationError(c, err)
		return
	}
	common.Success(c, gin.H{
		"status":       "accepted",
		"userId":       result.User.ID,
		"username":     result.User.Username,
		"tenantId":     result.User.TenantID,
		"membershipId": result.Membership.ID,
	})
}

// Revoke 撤销邀请（认证 + user:write）；仅 pending 生效。
func (h *Handler) Revoke(c *gin.Context) {
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "邀请服务未启用")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "无效的邀请ID")
		return
	}
	inv, err := h.svc.Revoke(c.Request.Context(), actorFromContext(c), id)
	if err != nil {
		respondInvitationError(c, err)
		return
	}
	common.Success(c, gin.H{
		"id":        inv.ID,
		"status":    inv.Status,
		"revokedAt": inv.RevokedAt,
	})
}

// List 邀请列表（认证 + user:write）：tenantId 省略取当前租户；status/limit/offset 可选。
func (h *Handler) List(c *gin.Context) {
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "邀请服务未启用")
		return
	}
	tenantID, _ := strconv.Atoi(c.Query("tenantId"))
	if tenantID <= 0 {
		tenantID = c.GetInt("tenant_id")
	}
	if tenantID <= 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	result, err := h.svc.List(c.Request.Context(), actorFromContext(c), &service.ListInvitationsRequest{
		TenantID: tenantID,
		Status:   c.Query("status"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		respondInvitationError(c, err)
		return
	}
	common.Success(c, result)
}

// actorFromContext 从 gin context 提取邀请人身份（JWT claims 派生）。
func actorFromContext(c *gin.Context) service.InvitationActor {
	mspRole := c.GetString("msp_role")
	if mspRole == "" {
		mspRole = c.GetString("mspRole")
	}
	return service.InvitationActor{
		UserID:       c.GetInt("user_id"),
		HomeTenantID: c.GetInt("tenant_id"),
		Role:         c.GetString("role"),
		MSPRole:      mspRole,
		Username:     c.GetString("username"),
	}
}

// respondInvitationError 邀请域稳定错误码 → 响应；其余走通用失败。
func respondInvitationError(c *gin.Context, err error) {
	if ie, ok := service.AsInvitationError(err); ok {
		c.JSON(ie.Status, gin.H{"code": ie.Code, "message": ie.Message, "error": ie.Message})
		c.Abort()
		return
	}
	common.FailWithErr(c, err, "邀请操作失败")
}
