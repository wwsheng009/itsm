package tenant

// user_admin.go：平台侧租户用户管理 HTTP 层（TUM-1/TUM-2）。
//
// 契约见 docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §3.1。
// 平台面判定：路由权限（tenant:read|write）仅粗筛，服务层 super_admin 硬校验（TUM-D1）。

import (
	"context"
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// TenantUserAdminService 定义平台侧租户用户管理接口（依赖倒置）。
// 实现由 internal/bootstrap 装配的 service.TenantUserAdminService 提供；测试可注入 mock。
type TenantUserAdminService interface {
	ListUsers(ctx context.Context, actor service.TenantAdminActor, tenantID int, q *dto.TenantUserListQuery) (*dto.TenantUserListResponse, error)
	GetUser(ctx context.Context, actor service.TenantAdminActor, tenantID, userID int) (*dto.TenantUserItem, error)
	ResetPassword(ctx context.Context, actor service.TenantAdminActor, tenantID, userID int, req *dto.ResetTenantUserPasswordRequest) (*dto.ResetTenantUserPasswordResponse, error)
	SetActive(ctx context.Context, actor service.TenantAdminActor, tenantID, userID int, req *dto.SetTenantUserStatusRequest) error
	ForceLogout(ctx context.Context, actor service.TenantAdminActor, tenantID, userID int) (*dto.ForceLogoutTenantUserResponse, error)
}

// SetTenantUserAdminService 注入平台侧租户用户管理服务（app 装配调用）；未注入时相关端点返回 503。
func (h *Handler) SetTenantUserAdminService(svc TenantUserAdminService) {
	h.userAdmin = svc
}

// ListTenantUsers GET /api/v1/tenants/:id/users
func (h *Handler) ListTenantUsers(c *gin.Context) {
	tenantID, ok := h.parseProvisioningTenantID(c)
	if !ok {
		return
	}
	svc, ok := h.requireTenantUserAdmin(c)
	if !ok {
		return
	}
	var query dto.TenantUserListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	c.Set("audit_target_tenant_id", tenantID)
	resp, err := svc.ListUsers(c.Request.Context(), tenantUserAdminActorFromContext(c), tenantID, &query)
	if err != nil {
		h.logger.Errorf("查询租户用户列表失败: %v", err)
		respondTenantUserAdminError(c, err)
		return
	}
	common.Success(c, resp)
}

// GetTenantUser GET /api/v1/tenants/:id/users/:userId
func (h *Handler) GetTenantUser(c *gin.Context) {
	tenantID, userID, ok := h.parseTenantUserAdminIDs(c)
	if !ok {
		return
	}
	svc, ok := h.requireTenantUserAdmin(c)
	if !ok {
		return
	}
	c.Set("audit_target_tenant_id", tenantID)
	resp, err := svc.GetUser(c.Request.Context(), tenantUserAdminActorFromContext(c), tenantID, userID)
	if err != nil {
		h.logger.Errorf("查询租户用户详情失败: %v", err)
		respondTenantUserAdminError(c, err)
		return
	}
	common.Success(c, resp)
}

// ResetTenantUserPassword POST /api/v1/tenants/:id/users/:userId/reset-password
func (h *Handler) ResetTenantUserPassword(c *gin.Context) {
	tenantID, userID, ok := h.parseTenantUserAdminIDs(c)
	if !ok {
		return
	}
	svc, ok := h.requireTenantUserAdmin(c)
	if !ok {
		return
	}
	var req dto.ResetTenantUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	c.Set("audit_target_tenant_id", tenantID)
	resp, err := svc.ResetPassword(c.Request.Context(), tenantUserAdminActorFromContext(c), tenantID, userID, &req)
	if err != nil {
		h.logger.Errorf("重置租户用户密码失败: %v", err)
		respondTenantUserAdminError(c, err)
		return
	}
	common.Success(c, resp)
}

// SetTenantUserStatus PUT /api/v1/tenants/:id/users/:userId/status
func (h *Handler) SetTenantUserStatus(c *gin.Context) {
	tenantID, userID, ok := h.parseTenantUserAdminIDs(c)
	if !ok {
		return
	}
	svc, ok := h.requireTenantUserAdmin(c)
	if !ok {
		return
	}
	var req dto.SetTenantUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	c.Set("audit_target_tenant_id", tenantID)
	if err := svc.SetActive(c.Request.Context(), tenantUserAdminActorFromContext(c), tenantID, userID, &req); err != nil {
		h.logger.Errorf("更改租户用户状态失败: %v", err)
		respondTenantUserAdminError(c, err)
		return
	}
	common.Success(c, nil)
}

// ForceLogoutTenantUser POST /api/v1/tenants/:id/users/:userId/force-logout
func (h *Handler) ForceLogoutTenantUser(c *gin.Context) {
	tenantID, userID, ok := h.parseTenantUserAdminIDs(c)
	if !ok {
		return
	}
	svc, ok := h.requireTenantUserAdmin(c)
	if !ok {
		return
	}
	c.Set("audit_target_tenant_id", tenantID)
	resp, err := svc.ForceLogout(c.Request.Context(), tenantUserAdminActorFromContext(c), tenantID, userID)
	if err != nil {
		h.logger.Errorf("强制下线租户用户失败: %v", err)
		respondTenantUserAdminError(c, err)
		return
	}
	common.Success(c, resp)
}

func (h *Handler) requireTenantUserAdmin(c *gin.Context) (TenantUserAdminService, bool) {
	if h.userAdmin == nil {
		common.Fail(c, common.ServiceUnavailableCode, "平台侧用户治理服务未启用")
		return nil, false
	}
	return h.userAdmin, true
}

// parseTenantUserAdminIDs 解析 /:id 与 /:userId（校验必须为正整数）。
func (h *Handler) parseTenantUserAdminIDs(c *gin.Context) (int, int, bool) {
	tenantID, ok := h.parseProvisioningTenantID(c)
	if !ok {
		return 0, 0, false
	}
	userID, err := strconv.Atoi(c.Param("userId"))
	if err != nil || userID <= 0 {
		common.ParamError(c, "无效的用户ID")
		return 0, 0, false
	}
	return tenantID, userID, true
}

// tenantUserAdminActorFromContext 从 gin context 提取调用方身份（JWT claims 派生）。
func tenantUserAdminActorFromContext(c *gin.Context) service.TenantAdminActor {
	return service.TenantAdminActor{
		UserID:       c.GetInt("user_id"),
		HomeTenantID: c.GetInt("tenant_id"),
		Role:         c.GetString("role"),
		Username:     strings.TrimSpace(c.GetString("username")),
	}
}

// respondTenantUserAdminError 将稳定错误码映射为响应（对齐建号通道风格：字符串码 + 稳定 HTTP 语义）。
func respondTenantUserAdminError(c *gin.Context, err error) {
	if ae, ok := service.AsTenantUserAdminError(err); ok {
		c.JSON(ae.Status, gin.H{"code": ae.Code, "message": ae.Message, "error": ae.Message})
		c.Abort()
		return
	}
	common.FailWithErr(c, err, "操作失败")
}
