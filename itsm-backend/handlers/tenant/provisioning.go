package tenant

import (
	"context"
	"errors"
	"io"
	"strconv"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// ProvisioningService 是 tenant 域内自定义的租户激活接口（模板供给 + readiness + 首管）。
// 独立于既有的 Service 接口，避免破坏现有 mock 与装配。
type ProvisioningService interface {
	Readiness(ctx context.Context, tenantID int) (*dto.TenantReadinessResponse, error)
	Provision(ctx context.Context, tenantID int, templateVersion string) (*dto.TenantReadinessResponse, error)
	CreateBootstrapAdmin(ctx context.Context, tenantID int, req *dto.BootstrapAdminRequest) (*dto.BootstrapAdminResponse, error)
}

// SetProvisioningService 注入租户供给服务（app 装配调用）；未注入时相关端点返回 503。
func (h *Handler) SetProvisioningService(svc ProvisioningService) {
	h.provisioning = svc
}

// GetTenantReadiness 返回租户从零激活到可用的就绪度报告（模板 7 项基线 + 首管计数）。
func (h *Handler) GetTenantReadiness(c *gin.Context) {
	tenantID, ok := h.parseProvisioningTenantID(c)
	if !ok {
		return
	}
	svc, ok := h.requireProvisioningService(c)
	if !ok {
		return
	}
	resp, err := svc.Readiness(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Errorf("获取租户就绪度失败: %v", err)
		if errors.Is(err, service.ErrTenantNotFound) {
			common.Fail(c, common.NotFoundCode, "租户不存在")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, resp)
}

// ProvisionTenant 安装租户产品模板，返回供给完成后的就绪度报告；重复执行幂等。
func (h *Handler) ProvisionTenant(c *gin.Context) {
	tenantID, ok := h.parseProvisioningTenantID(c)
	if !ok {
		return
	}
	svc, ok := h.requireProvisioningService(c)
	if !ok {
		return
	}
	var req dto.ProvisionTenantRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	resp, err := svc.Provision(c.Request.Context(), tenantID, req.TemplateVersion)
	if err != nil {
		h.logger.Errorf("租户供给失败: %v", err)
		switch {
		case errors.Is(err, service.ErrTenantNotFound):
			common.Fail(c, common.NotFoundCode, "租户不存在")
		case errors.Is(err, service.ErrUnsupportedTenantTemplateVersion):
			common.Fail(c, common.ParamErrorCode, "不支持的租户模板版本")
		default:
			respondTenantWriteError(c, err)
		}
		return
	}
	h.recordAudit(c, "tenant.provision", tenantID, "template_version", resp.TemplateVersion)
	common.Success(c, resp)
}

// CreateBootstrapAdmin 创建租户首个管理员；未传 password 时服务端生成并仅本次回传明文。
func (h *Handler) CreateBootstrapAdmin(c *gin.Context) {
	tenantID, ok := h.parseProvisioningTenantID(c)
	if !ok {
		return
	}
	svc, ok := h.requireProvisioningService(c)
	if !ok {
		return
	}
	var req dto.BootstrapAdminRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	resp, err := svc.CreateBootstrapAdmin(c.Request.Context(), tenantID, &req)
	if err != nil {
		h.logger.Errorf("创建租户首个管理员失败: %v", err)
		switch {
		case errors.Is(err, service.ErrBootstrapAdminExists):
			common.Fail(c, common.ConflictCode, "该租户已存在首个管理员")
		case errors.Is(err, service.ErrTenantNotFound):
			common.Fail(c, common.NotFoundCode, "租户不存在")
		case errors.Is(err, service.ErrInvalidBootstrapAdminPassword):
			common.Fail(c, common.ParamErrorCode, "密码长度必须为 12-128 位")
		default:
			respondTenantWriteError(c, err)
		}
		return
	}
	h.recordAudit(c, "tenant.bootstrap_admin.create", tenantID, "username", resp.Username)
	common.Success(c, resp)
}

func (h *Handler) requireProvisioningService(c *gin.Context) (ProvisioningService, bool) {
	if h.provisioning == nil {
		common.Fail(c, common.ServiceUnavailableCode, "租户供给服务未启用")
		return nil, false
	}
	return h.provisioning, true
}

func (h *Handler) parseProvisioningTenantID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的租户ID")
		return 0, false
	}
	return id, true
}

// bindOptionalJSON 容忍空请求体（两个 POST 端点参数均可空），其余绑定错误按参数错误返回。
func bindOptionalJSON(c *gin.Context, target any) bool {
	if c.Request == nil || c.Request.Body == nil {
		return true
	}
	if err := c.ShouldBindJSON(target); err != nil && !errors.Is(err, io.EOF) {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return false
	}
	return true
}
