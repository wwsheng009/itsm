package pat

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"itsm-backend/common"
	"itsm-backend/middleware"
)

// Handler PAT HTTP 层（bind/validate → service → DTO）。
// 路由全部挂 authGrp（仅 AuthMiddleware，自省面）：
//   - 租户取值用 c.GetInt("tenant_id")（AuthMiddleware 写入），
//     不可用 middleware.GetTenantID（authGrp 无 TenantMiddleware）。
//   - 写路由前置 RejectPATAuth（D-PAT-13）+ 条件 CSRF（router 装配处处理）。
type Handler struct {
	svc *Service
}

// NewHandler 构造。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateRequest 创建请求。
type CreateRequest struct {
	Name          string  `json:"name" binding:"required,max=64"`
	Description   string  `json:"description" binding:"omitempty,max=255"`
	ExpiresInDays int     `json:"expiresInDays" binding:"omitempty,min=1,max=365"`
	ExpiresAt     *string `json:"expiresAt" binding:"omitempty"` // RFC3339
}

// RotateRequest 轮换请求。
type RotateRequest struct {
	GracePeriodHours int `json:"gracePeriodHours" binding:"omitempty,oneof=0 24"`
}

// RevokeRequest 吊销请求（空体）。
type RevokeRequest struct{}

// List GET /personal-access-tokens
func (h *Handler) List(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	items, err := h.svc.List(c.Request.Context(), userID, c.GetInt("tenant_id"))
	if err != nil {
		common.InternalError(c, "查询访问令牌失败")
		return
	}
	common.Success(c, gin.H{"tokens": items})
}

// Create POST /personal-access-tokens
func (h *Handler) Create(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	role, _ := c.Get("role")
	roleStr, _ := role.(string)
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	in := CreateInput{Name: req.Name, Description: req.Description, ExpiresInDays: req.ExpiresInDays}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := timeParseRFC3339(*req.ExpiresAt)
		if err != nil {
			common.ParamError(c, "expiresAt 必须为 RFC3339 时间")
			return
		}
		in.ExpiresAt = &t
	}
	res, err := h.svc.Create(c.Request.Context(), userID, c.GetInt("tenant_id"), in, roleStr)
	if err != nil {
		h.mapError(c, err)
		return
	}
	// 明文仅此一次返回（D-PAT-2/3）
	common.Success(c, res)
}

// Rotate POST /personal-access-tokens/:id/rotate
func (h *Handler) Rotate(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	tokenID, ok := h.pathID(c)
	if !ok {
		return
	}
	var req RotateRequest
	_ = c.ShouldBindJSON(&req) // 空体允许（默认 0 = 立即吊销）
	res, err := h.svc.Rotate(c.Request.Context(), userID, c.GetInt("tenant_id"), tokenID, req.GracePeriodHours)
	if err != nil {
		h.mapError(c, err)
		return
	}
	common.Success(c, res)
}

// Revoke POST /personal-access-tokens/:id/revoke
func (h *Handler) Revoke(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	tokenID, ok := h.pathID(c)
	if !ok {
		return
	}
	item, err := h.svc.Revoke(c.Request.Context(), userID, c.GetInt("tenant_id"), tokenID)
	if err != nil {
		h.mapError(c, err)
		return
	}
	common.Success(c, item)
}

// RevokeAll POST /personal-access-tokens/revoke-all
func (h *Handler) RevokeAll(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	n, err := h.svc.RevokeAll(c.Request.Context(), userID, c.GetInt("tenant_id"))
	if err != nil {
		h.mapError(c, err)
		return
	}
	common.Success(c, gin.H{"revokedCount": n})
}

// Usages GET /personal-access-tokens/:id/usages
func (h *Handler) Usages(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	tokenID, ok := h.pathID(c)
	if !ok {
		return
	}
	page := atoiDefault(c.Query("page"), 1)
	pageSize := atoiDefault(c.Query("pageSize"), 20)
	res, err := h.svc.Usages(c.Request.Context(), userID, c.GetInt("tenant_id"), tokenID, page, pageSize)
	if err != nil {
		h.mapError(c, err)
		return
	}
	common.Success(c, res)
}

// SecurityEvents GET /auth/security-events
func (h *Handler) SecurityEvents(c *gin.Context) {
	userID, ok := middleware.UserIDOrUnauthorized(c)
	if !ok {
		return
	}
	page := atoiDefault(c.Query("page"), 1)
	pageSize := atoiDefault(c.Query("pageSize"), 20)
	res, err := h.svc.SecurityEvents(c.Request.Context(), userID, c.GetInt("tenant_id"), page, pageSize, c.DefaultQuery("kind", "all"))
	if err != nil {
		common.InternalError(c, "查询安全事件失败")
		return
	}
	common.Success(c, res)
}

// pathID 解析 :id（非数字 → 404 防枚举）。
func (h *Handler) pathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.NotFound(c, "访问令牌不存在")
		return 0, false
	}
	return id, true
}

// mapError 领域错误 → HTTP 语义（统一经由 common.Fail 的状态映射）。
func (h *Handler) mapError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrConfigDisabled):
		common.NotFound(c, "访问令牌功能未启用")
	case errors.Is(err, ErrPATNotFound):
		common.NotFound(c, "访问令牌不存在")
	case errors.Is(err, ErrPATRevoked), errors.Is(err, ErrPATExpired):
		common.Fail(c, common.ParamErrorCode, err.Error())
	case errors.Is(err, ErrPATLimit):
		common.Fail(c, common.UnprocessableEntityCode, err.Error())
	case errors.Is(err, ErrPATForbidden):
		common.Forbidden(c, err.Error())
	default:
		common.ParamError(c, err.Error())
	}
}

// timeParseRFC3339 解析 RFC3339 时间。
func timeParseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
