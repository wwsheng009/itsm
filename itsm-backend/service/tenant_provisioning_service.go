package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/systemconfig"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/bootstrap"

	"go.uber.org/zap"
)

// tenant_provisioning_service.go：平台把租户从零激活到可用的 HTTP 编排层
// （模板供给 + readiness + 首管），与 cmd/provision_tenant 的 CLI 能力同口径。
//
// 依赖说明：本包不能直接 import pkg/seeder —— pkg/seeder 反向依赖 itsm-backend/service
// （seeder.go 的 BPMNTemplateService），直接引用会构成 import cycle。因此通过
// TenantTemplateProvisioner 接口消费 *seeder.Seeder（结构化满足），就绪项使用
// dto.TenantReadinessItem（seeder.TenantReadinessItem 为其类型别名）。
//
// 跨租户读写一律经 tenantctx.SystemContext 系统旁路（RLS enforce 依赖），
// 组件名固定 "tenant:provisioning"，对齐 cmd/provision_tenant 的运维审计口径。

const (
	tenantProvisioningComponent = "tenant:provisioning"

	// bootstrapAdminPasswordLength 服务端生成的首管密码长度（含大写/小写/数字）。
	bootstrapAdminPasswordLength = 16
	// 与 pkg/bootstrap.CreateFirstAdmin / ConsumeToken 的密码边界保持一致。
	bootstrapAdminPasswordMinLength = 12
	bootstrapAdminPasswordMaxLength = 128

	// bootstrapAdminAuditPath HTTP 首管通道的审计来源路径。
	bootstrapAdminAuditPath = "http:tenants:bootstrap-admin"
)

var (
	// ErrTenantNotFound 目标租户不存在（handler 映射 404）。
	ErrTenantNotFound = errors.New("tenant not found")
	// ErrUnsupportedTenantTemplateVersion 模板版本不受支持（handler 映射 400）。
	ErrUnsupportedTenantTemplateVersion = errors.New("unsupported tenant template version")
	// ErrBootstrapAdminExists 该租户已存在首个管理员（handler 映射 409）。
	ErrBootstrapAdminExists = errors.New("bootstrap admin already exists for tenant")
	// ErrInvalidBootstrapAdminPassword 首管密码长度不合法（handler 映射 400）。
	ErrInvalidBootstrapAdminPassword = errors.New("bootstrap admin password length must be between 12 and 128")
)

// TenantTemplateProvisioner 是 service 侧对 pkg/seeder 的最小依赖面。
// *seeder.Seeder 结构化满足本接口（TenantReadinessItem 为 dto 类型别名）。
type TenantTemplateProvisioner interface {
	ProvisionTenant(ctx context.Context, tenantID int, templateVersion string) error
	TenantReadiness(ctx context.Context, tenantID int) ([]dto.TenantReadinessItem, error)
}

// TenantProvisioningService 提供租户激活 HTTP 能力的业务编排。零值不可用。
type TenantProvisioningService struct {
	client      *ent.Client
	provisioner TenantTemplateProvisioner
	logger      *zap.SugaredLogger
}

// NewTenantProvisioningService 构造租户供给服务；provisioner 传 *seeder.Seeder。
func NewTenantProvisioningService(client *ent.Client, provisioner TenantTemplateProvisioner, logger *zap.SugaredLogger) *TenantProvisioningService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &TenantProvisioningService{
		client:      client,
		provisioner: provisioner,
		logger:      logger,
	}
}

// Readiness 返回租户的模板就绪度报告（只读）。
func (s *TenantProvisioningService) Readiness(ctx context.Context, tenantID int) (*dto.TenantReadinessResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	sysCtx := tenantctx.SystemContext(
		ctx,
		tenantProvisioningComponent,
		fmt.Sprintf("read tenant %d provisioning readiness", tenantID),
	)
	return s.readiness(sysCtx, tenantID)
}

// Provision 安装租户产品模板并返回供给完成后的就绪度报告；重复执行幂等。
func (s *TenantProvisioningService) Provision(ctx context.Context, tenantID int, templateVersion string) (*dto.TenantReadinessResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("%w: tenant id must be positive", ErrTenantNotFound)
	}
	templateVersion = strings.TrimSpace(templateVersion)
	if templateVersion == "" {
		templateVersion = dto.DefaultTenantTemplateVersion
	}
	if templateVersion != dto.DefaultTenantTemplateVersion {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedTenantTemplateVersion, templateVersion)
	}

	sysCtx := tenantctx.SystemContext(
		ctx,
		tenantProvisioningComponent,
		fmt.Sprintf("install product template %s for tenant %d", templateVersion, tenantID),
	)
	exists, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Exist(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("load tenant %d: %w", tenantID, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: tenant %d", ErrTenantNotFound, tenantID)
	}
	if err := s.provisioner.ProvisionTenant(sysCtx, tenantID, templateVersion); err != nil {
		return nil, fmt.Errorf("provision tenant %d: %w", tenantID, err)
	}
	return s.readiness(sysCtx, tenantID)
}

// CreateBootstrapAdmin 创建租户首个管理员；password 为空时服务端生成一次性强密码。
func (s *TenantProvisioningService) CreateBootstrapAdmin(ctx context.Context, tenantID int, req *dto.BootstrapAdminRequest) (*dto.BootstrapAdminResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("%w: tenant id must be positive", ErrTenantNotFound)
	}
	if req == nil {
		req = &dto.BootstrapAdminRequest{}
	}
	sysCtx := tenantctx.SystemContext(
		ctx,
		tenantProvisioningComponent,
		fmt.Sprintf("create first admin for tenant %d", tenantID),
	)
	exists, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Exist(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("load tenant %d: %w", tenantID, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: tenant %d", ErrTenantNotFound, tenantID)
	}

	password := req.Password
	generated := false
	if password == "" {
		password, err = generateBootstrapAdminPassword(bootstrapAdminPasswordLength)
		if err != nil {
			return nil, fmt.Errorf("generate bootstrap admin password: %w", err)
		}
		generated = true
	}
	if len(password) < bootstrapAdminPasswordMinLength || len(password) > bootstrapAdminPasswordMaxLength {
		return nil, fmt.Errorf("%w: got %d characters", ErrInvalidBootstrapAdminPassword, len(password))
	}

	userID, err := bootstrap.CreateFirstAdmin(
		sysCtx,
		s.client,
		s.logger,
		tenantID,
		password,
		bootstrap.WithAdminIdentity(req.Username, req.Email),
		bootstrap.WithAuditPath(bootstrapAdminAuditPath),
		// 冻结契约：HTTP 首管必须首登改密，不受 BOOTSTRAP_ADMIN_MUST_CHANGE_PASSWORD 影响。
		bootstrap.WithMustChangePassword(true),
	)
	if err != nil {
		if errors.Is(err, bootstrap.ErrAdminExists) {
			return nil, fmt.Errorf("%w: %v", ErrBootstrapAdminExists, err)
		}
		return nil, fmt.Errorf("create bootstrap admin for tenant %d: %w", tenantID, err)
	}

	record, err := s.client.User.Get(sysCtx, userID)
	if err != nil {
		return nil, fmt.Errorf("load bootstrap admin %d: %w", userID, err)
	}
	resp := &dto.BootstrapAdminResponse{
		UserID:             record.ID,
		Username:           record.Username,
		Email:              record.Email,
		Generated:          generated,
		MustChangePassword: record.MustChangePassword,
	}
	// 明文密码只在服务端生成时随本次响应回传一次；调用方传入的密码不回显。
	if generated {
		resp.Password = password
	}
	s.logger.Infow("tenant bootstrap admin created via http",
		"tenant_id", tenantID, "user_id", record.ID, "username", record.Username, "generated", generated)
	return resp, nil
}

// readiness 为内部实现：ctx 必须已完成系统旁路包装。
func (s *TenantProvisioningService) readiness(ctx context.Context, tenantID int) (*dto.TenantReadinessResponse, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("%w: tenant id must be positive", ErrTenantNotFound)
	}
	exists, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("load tenant %d: %w", tenantID, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: tenant %d", ErrTenantNotFound, tenantID)
	}

	items, err := s.provisioner.TenantReadiness(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("read tenant %d readiness: %w", tenantID, err)
	}
	resp := &dto.TenantReadinessResponse{
		TenantID: tenantID,
		Ready:    true,
		Items:    make([]dto.TenantReadinessItem, 0, len(items)),
	}
	for _, item := range items {
		if item.Required && item.Count <= 0 {
			resp.Ready = false
		}
		resp.Items = append(resp.Items, item)
	}

	templateVersion, err := s.templateVersion(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	resp.TemplateVersion = templateVersion

	admins, err := s.client.User.Query().
		Where(user.IsBootstrapAdminEQ(true), user.TenantIDEQ(tenantID)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count tenant %d bootstrap admins: %w", tenantID, err)
	}
	resp.BootstrapAdmins = admins
	return resp, nil
}

// templateVersion 读取 systemconfig 中的租户模板版本；无记录返回 ""（非错误）。
func (s *TenantProvisioningService) templateVersion(ctx context.Context, tenantID int) (string, error) {
	key := fmt.Sprintf("tenant.bootstrap.version.%d", tenantID)
	record, err := s.client.SystemConfig.Query().
		Where(
			systemconfig.KeyEQ(key),
			systemconfig.TenantIDEQ(tenantID),
			systemconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load tenant %d template version: %w", tenantID, err)
	}
	return record.Value, nil
}

func (s *TenantProvisioningService) ensureConfigured() error {
	if s == nil || s.client == nil || s.provisioner == nil {
		return errors.New("tenant provisioning service is not configured")
	}
	return nil
}

const bootstrapPasswordCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generateBootstrapAdminPassword 用 crypto/rand 生成指定长度的随机密码，
// 并保证同时包含大写字母、小写字母与数字（去掉首管密码策略的类别空窗）。
func generateBootstrapAdminPassword(length int) (string, error) {
	if length < bootstrapAdminPasswordMinLength {
		length = bootstrapAdminPasswordMinLength
	}
	classes := []string{
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"abcdefghijklmnopqrstuvwxyz",
		"0123456789",
	}
	out := make([]byte, 0, length)
	pick := func(charset string) (byte, error) {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return 0, err
		}
		return charset[idx.Int64()], nil
	}
	for _, class := range classes {
		ch, err := pick(class)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	for len(out) < length {
		ch, err := pick(bootstrapPasswordCharset)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	// Fisher-Yates 洗牌，避免类别字符固定落在前三位。
	for i := len(out) - 1; i > 0; i-- {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := idx.Int64()
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}
