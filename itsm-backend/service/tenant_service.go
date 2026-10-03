package service

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/pkg/tenantquota"

	"go.uber.org/zap"
)

type TenantService struct {
	client   *ent.Client
	logger   *zap.SugaredLogger
	quotaSvc *TenantQuotaService // IP-P2-6 收尾：治理页用量展示（nil 时返回空用量）
}

func NewTenantService(client *ent.Client, logger *zap.SugaredLogger) *TenantService {
	return &TenantService{
		client: client,
		logger: logger,
	}
}

// SetTenantQuotaService 注入租户配额服务（IP-P2-6 收尾；nil 关闭用量汇总）。
func (s *TenantService) SetTenantQuotaService(q *TenantQuotaService) {
	s.quotaSvc = q
}

// CreateTenant 创建租户
func (s *TenantService) CreateTenant(ctx context.Context, req *dto.CreateTenantRequest) (*ent.Tenant, error) {
	// 检查租户代码是否已存在
	exists, err := s.client.Tenant.Query().
		Where(tenant.CodeEQ(req.Code)).
		Exist(ctx)
	if err != nil {
		s.logger.Errorf("检查租户代码失败: %v", err)
		return nil, fmt.Errorf("检查租户代码失败: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("租户代码已存在: %s", req.Code)
	}

	// IP-P0-4 / A1：写入拒绝 legacy 类型（msp/customer/standard），只接受目标集合。
	if err := tenantmode.ValidateTenantTypeForWrite(req.Type); err != nil {
		return nil, err
	}
	// IP-P0-4 / A2（D2/R3）：归属形状 + 目标必须是有效 msp_provider。
	// P0 起停止 parent_tenant_id 双写：旧字段仅作为兼容输入，统一落 msp_provider_id。
	providerID := req.MSPProviderID
	if providerID == nil {
		providerID = req.ParentTenantID
	}
	if err := s.validateTenantOwnership(ctx, 0, req.Type, providerID); err != nil {
		return nil, err
	}

	// IP-P2-6：租户硬配额（limits）——写入按显式模型校验（未知键/负值拒绝）。
	limits, err := tenantquota.Parse(req.Quota)
	if err != nil {
		return nil, err
	}

	// 创建租户
	create := s.client.Tenant.Create()
	if req.Quota != nil {
		create = create.SetQuota(limits)
	}
	tenantEntity, err := create.
		SetName(req.Name).
		SetCode(req.Code).
		SetNillableDomain(req.Domain).
		SetType(tenant.Type(req.Type)).
		SetStatus(defaultTenantStatus(req.Status)).
		SetNillableExpiresAt(req.ExpiresAt).
		SetNillableMspProviderID(providerID).
		SetNillablePlanCode(req.PlanCode).
		SetNillableBillingEnabled(req.BillingEnabled).
		SetNillableCostCenterCode(req.CostCenterCode).
		SetNillableLegalEntityCode(req.LegalEntityCode).
		SetNillableCurrency(req.Currency).
		SetNillableServiceTier(req.ServiceTier).
		SetNillableOwnerContact(req.OwnerContact).
		Save(ctx)
	if err != nil {
		s.logger.Errorf("创建租户失败: %v", err)
		return nil, fmt.Errorf("创建租户失败: %w", err)
	}

	s.logger.Infof("成功创建租户: %s (%s)", tenantEntity.Name, tenantEntity.Code)
	return tenantEntity, nil
}

// GetTenantByCode 根据代码获取租户
func (s *TenantService) GetTenantByCode(ctx context.Context, code string) (*ent.Tenant, error) {
	tenantEntity, err := s.client.Tenant.
		Query().
		Where(tenant.CodeEQ(code)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("租户不存在: %s", code)
		}
		s.logger.Errorf("查询租户失败: %v", err)
		return nil, fmt.Errorf("查询租户失败: %w", err)
	}
	return tenantEntity, nil
}

// UpdateTenantStatus 更新租户状态
func (s *TenantService) UpdateTenantStatus(ctx context.Context, tenantID int, status string) error {
	err := s.client.Tenant.
		UpdateOneID(tenantID).
		SetStatus(status).
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		s.logger.Errorf("更新租户状态失败: %v", err)
		return fmt.Errorf("更新租户状态失败: %w", err)
	}

	s.logger.Infof("成功更新租户状态: %d -> %s", tenantID, status)
	return nil
}

// ListTenants 获取租户列表
func (s *TenantService) ListTenants(ctx context.Context, req *dto.ListTenantsRequest) ([]*ent.Tenant, int, error) {
	query := s.client.Tenant.Query()

	// 状态过滤
	if req.Status != "" {
		query = query.Where(tenant.StatusEQ(req.Status))
	}

	// 类型过滤（IP-P0-4 读取兼容：legacy 与新值互相可见）
	if req.Type != "" {
		values := tenantmode.TenantTypeFilterValues(req.Type)
		typeValues := make([]tenant.Type, 0, len(values))
		for _, v := range values {
			typeValues = append(typeValues, tenant.Type(v))
		}
		query = query.Where(tenant.TypeIn(typeValues...))
	}

	// 搜索过滤
	if req.Search != "" {
		query = query.Where(
			tenant.Or(
				tenant.NameContains(req.Search),
				tenant.CodeContains(req.Search),
			),
		)
	}

	// 获取总数
	total, err := query.Count(ctx)
	if err != nil {
		s.logger.Errorf("获取租户总数失败: %v", err)
		return nil, 0, fmt.Errorf("获取租户总数失败: %w", err)
	}

	// 分页查询
	tenants, err := query.
		Offset((req.Page - 1) * req.PageSize).
		Limit(req.PageSize).
		Order(ent.Desc(tenant.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		s.logger.Errorf("获取租户列表失败: %v", err)
		return nil, 0, fmt.Errorf("获取租户列表失败: %w", err)
	}

	return tenants, total, nil
}

// GetTenant 获取租户详情
func (s *TenantService) GetTenant(ctx context.Context, tenantID int) (*ent.Tenant, error) {
	tenantEntity, err := s.client.Tenant.
		Query().
		Where(tenant.ID(tenantID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("租户不存在: %d", tenantID)
		}
		s.logger.Errorf("查询租户失败: %v", err)
		return nil, fmt.Errorf("查询租户失败: %w", err)
	}
	return tenantEntity, nil
}

// UpdateTenant 更新租户
func (s *TenantService) UpdateTenant(ctx context.Context, tenantID int, req *dto.UpdateTenantRequest) (*ent.Tenant, error) {
	// 检查租户是否存在（取现状用于归属/类型收敛校验）
	current, err := s.client.Tenant.Query().
		Where(tenant.ID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("租户不存在: %d", tenantID)
		}
		s.logger.Errorf("检查租户失败: %v", err)
		return nil, fmt.Errorf("检查租户失败: %w", err)
	}

	// 构建更新操作
	update := s.client.Tenant.UpdateOneID(tenantID).SetUpdatedAt(time.Now())

	// IP-P0-4 / A1：类型变更同样拒绝 legacy 值。
	if req.Type != nil && *req.Type != "" {
		if err := tenantmode.ValidateTenantTypeForWrite(*req.Type); err != nil {
			return nil, err
		}
	}
	// IP-P0-4 / A2：仅在类型或归属被触碰时校验；有效值 = 现状 + 本次变更。
	effectiveType := string(current.Type)
	if req.Type != nil && *req.Type != "" {
		effectiveType = *req.Type
	}
	providerID := current.MspProviderID
	providerTouched := false
	if req.ParentTenantID != nil && req.MSPProviderID == nil {
		providerID = *req.ParentTenantID
		providerTouched = true
	}
	if req.MSPProviderID != nil {
		providerID = *req.MSPProviderID
		providerTouched = true
	}
	if providerTouched || (req.Type != nil && *req.Type != "") {
		if err := s.validateTenantOwnership(ctx, tenantID, effectiveType, intPtrOrNil(providerID)); err != nil {
			return nil, err
		}
	}

	if req.Name != nil && *req.Name != "" {
		update = update.SetName(*req.Name)
	}
	if req.Domain != nil {
		update = update.SetNillableDomain(req.Domain)
	}
	if req.Type != nil && *req.Type != "" {
		update = update.SetType(tenant.Type(*req.Type))
	}
	if req.Status != nil && *req.Status != "" {
		update = update.SetStatus(*req.Status)
	}
	if req.ExpiresAt != nil {
		update = update.SetNillableExpiresAt(req.ExpiresAt)
	}
	if providerTouched {
		// P0 停止 parent_tenant_id 双写：旧字段仅作输入来源，唯一写通道 msp_provider_id。
		// 注意：SetNillableMspProviderID(nil) 是 no-op，清空需显式 Clear。
		if providerID > 0 {
			update = update.SetMspProviderID(providerID)
		} else {
			update = update.ClearMspProviderID()
		}
	}
	if req.PlanCode != nil {
		update = update.SetNillablePlanCode(req.PlanCode)
	}
	if req.BillingEnabled != nil {
		update = update.SetNillableBillingEnabled(req.BillingEnabled)
	}
	if req.CostCenterCode != nil {
		update = update.SetNillableCostCenterCode(req.CostCenterCode)
	}
	if req.LegalEntityCode != nil {
		update = update.SetNillableLegalEntityCode(req.LegalEntityCode)
	}
	if req.Currency != nil {
		update = update.SetNillableCurrency(req.Currency)
	}
	if req.ServiceTier != nil {
		update = update.SetNillableServiceTier(req.ServiceTier)
	}
	if req.OwnerContact != nil {
		update = update.SetNillableOwnerContact(req.OwnerContact)
	}
	// IP-P2-6：配额热更新；显式传 {} 表示清空（不限）。
	if req.Quota != nil {
		limits, err := tenantquota.Parse(req.Quota)
		if err != nil {
			return nil, err
		}
		update = update.SetQuota(limits)
	}

	tenantEntity, err := update.Save(ctx)
	if err != nil {
		s.logger.Errorf("更新租户失败: %v", err)
		return nil, fmt.Errorf("更新租户失败: %w", err)
	}

	s.logger.Infof("成功更新租户: %d", tenantID)
	return tenantEntity, nil
}

// QuotaUsage 汇总租户硬配额的“上限 vs 当前用量”（IP-P2-6 收尾；口径与写入校验一致）。
// quotaSvc 未注入（nil）时返回空用量、不报错——装配顺序与测试装配套路保持宽松。
func (s *TenantService) QuotaUsage(ctx context.Context, tenantID int) (*dto.TenantQuotaUsageResponse, error) {
	resp := &dto.TenantQuotaUsageResponse{TenantID: tenantID}
	if s == nil || s.quotaSvc == nil {
		return resp, nil
	}
	limits, err := s.quotaSvc.LimitsOf(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	used, err := s.quotaSvc.Usage(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	resp.Limits = dto.TenantQuotaLimits{
		MaxUsers:           limits.MaxUsers,
		MaxTicketsPerMonth: limits.MaxTicketsPerMonth,
		MaxStorageMB:       limits.MaxStorageMB,
	}
	resp.Used = dto.TenantQuotaUsage{
		Users:            used.Users,
		TicketsThisMonth: used.TicketsThisMonth,
		StorageBytes:     used.StorageBytes,
	}
	return resp, nil
}

// DeleteTenant 删除租户
// 安全约束：走软删除以保留审计痕迹，状态置为 deleted。下游数据由后台清理任务接管，
// 业务要求 上层调用在执行前必须确保租户已处于无活跃用户的状态，否则拒绝。
func (s *TenantService) DeleteTenant(ctx context.Context, tenantID int) error {
	// 检查租户是否存在
	exists, err := s.client.Tenant.Query().
		Where(tenant.ID(tenantID)).
		Exist(ctx)
	if err != nil {
		s.logger.Errorf("检查租户失败: %v", err)
		return fmt.Errorf("检查租户失败: %w", err)
	}
	if !exists {
		return fmt.Errorf("租户不存在: %d", tenantID)
	}

	// 检查租户下是否有用户
	userCount, err := s.client.User.Query().
		Where(user.TenantID(tenantID)).
		Count(ctx)
	if err != nil {
		s.logger.Errorf("检查租户用户失败: %v", err)
		return fmt.Errorf("检查租户用户失败: %w", err)
	}
	if userCount > 0 {
		return fmt.Errorf("租户下还有用户，无法删除")
	}

	err = s.client.Tenant.UpdateOneID(tenantID).
		SetStatus("deleted").
		SetUpdatedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		s.logger.Errorf("软删除租户失败: %v", err)
		return fmt.Errorf("删除租户失败: %w", err)
	}

	s.logger.Infow(
		"tenant soft-deleted",
		"tenant_id", tenantID,
		"action", "tenant.delete",
	)
	return nil
}

func defaultTenantStatus(status *string) string {
	if status == nil || *status == "" {
		return "active"
	}
	return *status
}

// validateTenantOwnership 归属形状校验 + provider 目标复核（IP-P0-4 / A2）。
// 形状规则见 tenantmode.ValidateTenantOwnership；msp_customer 需再查库确认目标确为 msp_provider。
func (s *TenantService) validateTenantOwnership(ctx context.Context, selfID int, kind string, providerID *int) error {
	if err := tenantmode.ValidateTenantOwnership(kind, providerID, selfID); err != nil {
		return err
	}
	if kind != tenantmode.TenantTypeMSPCustomer || providerID == nil {
		return nil
	}
	provider, err := s.client.Tenant.Query().Where(tenant.ID(*providerID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("归属的 MSP 提供方租户不存在: %d", *providerID)
		}
		return fmt.Errorf("查询归属的 MSP 提供方租户失败: %w", err)
	}
	if !tenantmode.IsMSPProviderTenantType(string(provider.Type)) {
		return fmt.Errorf("归属目标 %d 不是 MSP 提供方租户（type=%s）", provider.ID, provider.Type)
	}
	return nil
}

// intPtrOrNil 将 <=0 的归属值归一为 nil（清空），>0 返回值指针（写入 msp_provider_id）。
func intPtrOrNil(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}
