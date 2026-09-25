package ai

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 本文件是主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-4 的 gin 处理器（§3.4）：
// 10 个端点全部为薄层——身份提取 → JSON 绑定 → 服务层调用 → 契约错误映射 → 统一响应。
//
// 鉴权不在本层：路由注册时统一挂 middleware.RequirePermission("system","write")（含读端点，
// D12）；super_admin 走既有旁路、sysadmin 持 *:*。
//
// 错误响应形状（§3.4「错误码统一」）：
//   - HTTP 状态码严格按 §3.4 失败态（403/404/409/422/503）；
//   - body 保留 common 既有封套（code 为既有 int 业务码 + message），并额外给出
//     errorCode（§3.4 字符串码，如 AI_PROVIDER_IS_DEFAULT）供前端分支；
//   - 绝不回显明文密钥/密文（service 层已掩码，DTO 无密钥字段）。
type LLMProviderAdminHandler struct {
	svc *LLMProviderAdminService
}

// NewLLMProviderAdminHandler 构造管理面 handler（svc 为 nil 时所有方法返回 503）。
func NewLLMProviderAdminHandler(svc *LLMProviderAdminService) *LLMProviderAdminHandler {
	return &LLMProviderAdminHandler{svc: svc}
}

// listProviders GET /api/v1/ai/providers
func (h *LLMProviderAdminHandler) ListProviders(c *gin.Context) {
	tenantID, _, ok := h.identity(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.ListProviders(c.Request.Context(), tenantID)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// createProvider POST /api/v1/ai/providers
func (h *LLMProviderAdminHandler) CreateProvider(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	var req dto.LLMCreateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.CreateProvider(c.Request.Context(), tenantID, userID, req)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// updateProvider PUT /api/v1/ai/providers/:id
func (h *LLMProviderAdminHandler) UpdateProvider(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	id, ok := h.pathID(c)
	if !ok {
		return
	}
	var req dto.LLMUpdateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.UpdateProvider(c.Request.Context(), tenantID, userID, id, req)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// deleteProvider DELETE /api/v1/ai/providers/:id
func (h *LLMProviderAdminHandler) DeleteProvider(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	id, ok := h.pathID(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.DeleteProvider(c.Request.Context(), tenantID, userID, id)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// testProvider POST /api/v1/ai/providers/:id/test
func (h *LLMProviderAdminHandler) TestProvider(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	id, ok := h.pathID(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.TestProvider(c.Request.Context(), tenantID, userID, id)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// setDefaultProvider POST /api/v1/ai/providers/:id/default
func (h *LLMProviderAdminHandler) SetDefaultProvider(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	id, ok := h.pathID(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.SetDefaultProvider(c.Request.Context(), tenantID, userID, id)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// importStatic POST /api/v1/ai/providers/import-static
func (h *LLMProviderAdminHandler) ImportStatic(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.ImportStatic(c.Request.Context(), tenantID, userID)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// listAvailable GET /api/v1/ai/providers/available
func (h *LLMProviderAdminHandler) ListAvailable(c *gin.Context) {
	tenantID, _, ok := h.identity(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.ListAvailable(c.Request.Context(), tenantID)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// getUserPreference GET /api/v1/ai/user-preference
func (h *LLMProviderAdminHandler) GetUserPreference(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.GetUserPreference(c.Request.Context(), tenantID, userID)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// setUserPreference PUT /api/v1/ai/user-preference
func (h *LLMProviderAdminHandler) SetUserPreference(c *gin.Context) {
	tenantID, userID, ok := h.identity(c)
	if !ok {
		return
	}
	var req dto.LLMUserPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	providerKey := ""
	if req.ProviderKey != nil {
		providerKey = strings.TrimSpace(*req.ProviderKey)
	}
	if h.svc == nil {
		respondLLMAdminError(c, newLLMAdminError(http.StatusServiceUnavailable, LLMAdminCodeUnavailable, "管理服务未就绪"))
		return
	}
	result, err := h.svc.SetUserPreference(c.Request.Context(), tenantID, userID, providerKey)
	if err != nil {
		respondLLMAdminError(c, err)
		return
	}
	common.Success(c, result)
}

// identity 提取租户/用户身份；缺失时按既有 AI handler 口径返回 401。
func (h *LLMProviderAdminHandler) identity(c *gin.Context) (tenantID, userID int, ok bool) {
	tenantID = c.GetInt("tenant_id")
	userID = c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.AuthFailed(c, "缺少有效身份上下文")
		return 0, 0, false
	}
	return tenantID, userID, true
}

// pathID 解析 :id；非法（非正整数）按 400 参数错误处理。
func (h *LLMProviderAdminHandler) pathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.BadRequestCode, "id 非法")
		return 0, false
	}
	return id, true
}

// respondLLMAdminError 把领域错误映射为契约响应（HTTP 状态 + int 业务码 + §3.4 字符串码）。
func respondLLMAdminError(c *gin.Context, err error) {
	var adminErr *LLMAdminError
	if !errors.As(err, &adminErr) {
		adminErr = &LLMAdminError{Status: http.StatusInternalServerError, Code: LLMAdminCodeInternal, Message: "服务内部错误", Err: err}
	}
	status := adminErr.Status
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	if status >= http.StatusInternalServerError {
		zap.S().Errorw("llm provider admin handler error",
			"error_code", adminErr.Code,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"err", errorText(adminErr.Err),
		)
	}
	c.JSON(status, gin.H{
		"code":      llmAdminStatusToAppCode(status),
		"errorCode": adminErr.Code,
		"message":   adminErr.Message,
	})
	c.Abort()
}

// llmAdminStatusToAppCode HTTP 状态 → common 既有 int 业务码（保持既有封套语义）。
func llmAdminStatusToAppCode(status int) int {
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

// errorText 安全提取错误文本（nil 安全；调用方只在 5xx 日志路径使用）。
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
