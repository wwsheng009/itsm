package service

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/mspguard"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
)

// MSPAllocationService MSP 分配业务服务
type MSPAllocationService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewMSPAllocationService 创建 MSP 分配服务实例
func NewMSPAllocationService(client *ent.Client, logger *zap.SugaredLogger) *MSPAllocationService {
	return &MSPAllocationService{
		client: client,
		logger: logger,
	}
}

// Create 创建新的 MSP 分配
// operatorRole: 操作者角色，如果为 "super_admin" 或 "sysadmin" 则跳过租户类型验证
func (s *MSPAllocationService) Create(
	ctx context.Context,
	mspUserID int,
	customerTenantID int,
	role string,
	operatorRole ...string,
) (*dto.MSPAllocationDTO, error) {
	// 检查是否是管理员操作
	isAdmin := len(operatorRole) > 0 && (operatorRole[0] == "super_admin" || operatorRole[0] == "sysadmin")

	// 1. 载入 MSP 员工并派生 provider（IP-P2-1 §5.0-A：admin 不豁免归属校验）。
	u, err := s.client.User.Query().
		Where(user.IDEQ(mspUserID)).
		WithTenant().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("MSP用户不存在: %w", err)
	}
	if u.Edges.Tenant == nil || !tenantmode.IsMSPProviderTenantType(string(u.Edges.Tenant.Type)) {
		return nil, fmt.Errorf("用户不属于MSP租户（allocation 必须属于服务商员工）")
	}
	providerTenantID := u.Edges.Tenant.ID

	// 2. 验证客户租户并校验归属一致性（R2：跨 provider 分配拒绝）。
	cust, err := s.client.Tenant.Query().
		Where(tenant.IDEQ(customerTenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("客户租户不存在: %w", err)
	}
	if !isAdmin && !tenantmode.IsCustomerTenantType(string(cust.Type)) {
		return nil, fmt.Errorf("目标租户不是客户类型")
	}
	if cust.MspProviderID == 0 || cust.MspProviderID != providerTenantID {
		return nil, fmt.Errorf("跨 provider 分配被拒绝：客户租户 %d 归属服务商 %d，员工 %d 的 provider 为 %d",
			customerTenantID, cust.MspProviderID, mspUserID, providerTenantID)
	}

	// 3. 检查是否已存在有效的未解除分配
	exists, err := s.client.MSPAllocation.Query().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID)),
			mspallocation.DeassignedAtIsNil(),
		).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询现有分配失败: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("已存在有效分配记录")
	}

	// 4. 如果有已解除的旧记录，标记为已解除（避免重复）
	// 查询可能存在的未解除记录并更新
	_, err = s.client.MSPAllocation.Update().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID)),
			mspallocation.DeassignedAtNotNil(),
		).
		SetDeassignedAt(time.Now()).
		Save(ctx)
	if err != nil {
		s.logger.Warnw("更新旧分配记录失败", "error", err)
	}

	// 5. 创建新的分配记录
	alloc, err := s.client.MSPAllocation.Create().
		SetMspUserID(mspUserID).
		SetCustomerTenantID(customerTenantID).
		SetProviderTenantID(providerTenantID).
		SetRole(role).
		SetAssignedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建分配记录失败: %w", err)
	}

	return s.toDTO(alloc)
}

// toDTO 转换为 DTO
func (s *MSPAllocationService) toDTO(a *ent.MSPAllocation) (*dto.MSPAllocationDTO, error) {
	// 查询关联的用户和租户
	ctx := context.Background()

	var mspUsername string
	var customerTenantID int
	var customerTenantName string

	mspUser, err := a.QueryMspUser().Only(ctx)
	if err == nil && mspUser != nil {
		mspUsername = mspUser.Name
	}

	customers, err := a.QueryCustomerTenant().All(ctx)
	if err == nil && len(customers) > 0 {
		customerTenantID = customers[0].ID
		customerTenantName = customers[0].Name
	}

	var deassignedAt *time.Time
	if !a.DeassignedAt.IsZero() {
		deassignedAt = &a.DeassignedAt
	}

	return &dto.MSPAllocationDTO{
		ID:                 a.ID,
		MSPUserID:          a.MspUserID,
		MSPUsername:        mspUsername,
		ProviderTenantID:   a.ProviderTenantID,
		CustomerTenantID:   customerTenantID,
		CustomerTenantName: customerTenantName,
		Role:               a.Role,
		AssignedAt:         a.AssignedAt,
		DeassignedAt:       deassignedAt,
	}, nil
}

// ListByMSPUser 根据 MSP 用户 ID 获取其所有分配
func (s *MSPAllocationService) ListByMSPUser(ctx context.Context, mspUserID int) ([]*dto.MSPAllocationDTO, error) {
	q := s.client.MSPAllocation.Query().
		Where(mspallocation.MspUserIDEQ(mspUserID)).
		WithCustomerTenant()
	if providerID, ok := s.providerTenantID(ctx, mspUserID); ok {
		// IP-P2-1 过渡兼容：已回填行按 provider 收窄；未回填行保留（NOT NULL 收尾后收敛为等值）。
		q = q.Where(mspallocation.Or(
			mspallocation.ProviderTenantIDEQ(providerID),
			mspallocation.ProviderTenantIDIsNil(),
		))
	}
	allocations, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询分配失败: %w", err)
	}

	dtos := make([]*dto.MSPAllocationDTO, 0, len(allocations))
	for _, a := range allocations {
		dto, err := s.toDTO(a)
		if err != nil {
			s.logger.Warnw("转换DTO失败", "error", err)
			continue
		}
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

// ListByCustomer 根据客户租户 ID 获取所有分配
func (s *MSPAllocationService) ListByCustomer(ctx context.Context, customerTenantID int) ([]*dto.MSPAllocationDTO, error) {
	allocations, err := s.client.MSPAllocation.Query().
		Where(mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID))).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询分配失败: %w", err)
	}

	dtos := make([]*dto.MSPAllocationDTO, 0, len(allocations))
	for _, a := range allocations {
		dto, err := s.toDTO(a)
		if err != nil {
			s.logger.Warnw("转换DTO失败", "error", err)
			continue
		}
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

// Deactivate 解除分配
func (s *MSPAllocationService) Deactivate(ctx context.Context, mspUserID int, customerTenantID int) error {
	_, err := s.client.MSPAllocation.Update().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID)),
			mspallocation.DeassignedAtIsNil(),
		).
		SetDeassignedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("解除分配失败: %w", err)
	}
	return nil
}

// GetActiveAllocations 获取所有有效分配
func (s *MSPAllocationService) GetActiveAllocations(ctx context.Context) ([]*dto.MSPAllocationDTO, error) {
	allocations, err := s.client.MSPAllocation.Query().
		Where(mspallocation.DeassignedAtIsNil()).
		WithCustomerTenant().
		WithMspUser().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询有效分配失败: %w", err)
	}

	dtos := make([]*dto.MSPAllocationDTO, 0, len(allocations))
	for _, a := range allocations {
		var mspUsername, customerTenantName string
		var customerTenantID int
		if a.Edges.MspUser != nil {
			mspUsername = a.Edges.MspUser.Name
		}
		if a.Edges.CustomerTenant != nil {
			customerTenantID = a.Edges.CustomerTenant.ID
			customerTenantName = a.Edges.CustomerTenant.Name
		}

		var deassignedAt *time.Time
		if !a.DeassignedAt.IsZero() {
			deassignedAt = &a.DeassignedAt
		}

		dtos = append(dtos, &dto.MSPAllocationDTO{
			ID:                 a.ID,
			MSPUserID:          a.MspUserID,
			MSPUsername:        mspUsername,
			ProviderTenantID:   a.ProviderTenantID,
			CustomerTenantID:   customerTenantID,
			CustomerTenantName: customerTenantName,
			Role:               a.Role,
			AssignedAt:         a.AssignedAt,
			DeassignedAt:       deassignedAt,
		})
	}

	return dtos, nil
}

// GetMSPCustomers 获取指定 MSP 用户可访问的客户列表（IP-P2-1：统一走 mspguard 收窄，fail-closed）。
func (s *MSPAllocationService) GetMSPCustomers(ctx context.Context, mspUserID int) ([]*ent.Tenant, error) {
	ids, err := mspguard.New(s.client).ListAccessibleCustomerIDs(ctx, mspUserID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*ent.Tenant{}, nil
	}
	return s.client.Tenant.Query().
		Where(tenant.IDIn(ids...)).
		Order(ent.Asc(tenant.FieldID)).
		All(ctx)
}

// providerTenantID 解析 MSP 员工的 home provider 租户 ID（IP-P2-1；非服务商员工返回 false）。
func (s *MSPAllocationService) providerTenantID(ctx context.Context, mspUserID int) (int, bool) {
	u, err := s.client.User.Query().
		Where(user.IDEQ(mspUserID)).
		WithTenant(func(q *ent.TenantQuery) {
			q.Select(tenant.FieldID, tenant.FieldType)
		}).
		Only(ctx)
	if err != nil || u.Edges.Tenant == nil {
		return 0, false
	}
	if !tenantmode.IsMSPProviderTenantType(string(u.Edges.Tenant.Type)) {
		return 0, false
	}
	return u.Edges.Tenant.ID, true
}
