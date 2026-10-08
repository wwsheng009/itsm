package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/pkg/mspguard"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
)

// MSPAllocationService MSP 分配业务服务

// ErrAllocationExists 同员工同客户已存在有效分配（幂等语义：重复分配返回 409 + reasonCode）。
var ErrAllocationExists = errors.New("allocation already exists")

type MSPAllocationService struct {
	client         *ent.Client
	logger         *zap.SugaredLogger
	membershipSync *MembershipSyncService // MSW-1：分配/撤销同步 membership（幂等）
}

// NewMSPAllocationService 创建 MSP 分配服务实例
func NewMSPAllocationService(client *ent.Client, logger *zap.SugaredLogger) *MSPAllocationService {
	return &MSPAllocationService{
		client:         client,
		logger:         logger,
		membershipSync: NewMembershipSyncService(client, logger),
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
		return nil, fmt.Errorf("%w: user=%d customer=%d", ErrAllocationExists, mspUserID, customerTenantID)
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

	// MSW-1：分配即同步客户作用域 membership（source=allocation；失败不阻断分配，靠巡检兜底）。
	s.syncMembershipForAllocation(ctx, alloc, providerTenantID)
	return s.toDTO(alloc)
}

// syncMembershipForAllocation 分配 → membership 同步（幂等；冲突/失败仅告警不阻断）。
func (s *MSPAllocationService) syncMembershipForAllocation(ctx context.Context, alloc *ent.MSPAllocation, providerTenantID int) {
	if s.membershipSync == nil || alloc == nil {
		return
	}
	meta := MembershipAuditMeta{
		RowTenantID:  providerTenantID,
		ActorAccount: fmt.Sprintf("allocation:user:%d", alloc.MspUserID),
		Source:       middleware.AuditSourceHeader,
		Trigger:      "msp.allocation.assign",
	}
	if _, err := s.membershipSync.SyncAllocationMembership(ctx, alloc, meta); err != nil {
		s.logger.Warnw("membership sync after allocation failed (allocation created; repair via inspection)",
			"error", err, "allocation_id", alloc.ID, "user_id", alloc.MspUserID, "customer_tenant_id", alloc.CustomerTenantID)
	}
}

// toDTO 转换为 DTO
func (s *MSPAllocationService) toDTO(a *ent.MSPAllocation) (*dto.MSPAllocationDTO, error) {
	// 查询关联的用户和租户
	// R2B 阴影观察（2026-10-03）：展示名解析跨 provider（MSP 员工）与客户租户两侧，
	// 属平台范围读取；显式 system 作用域（enforce 前置；此前裸 background 会 fail-closed）。
	ctx := tenantctx.SystemContext(context.Background(), "msp:allocation:toDTO", "resolve allocation display names (cross-tenant, platform scope)")

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
		// IP-P2-1 收尾（NOT NULL）：按 provider 等值收窄。
		q = q.Where(mspallocation.ProviderTenantIDEQ(providerID))
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

// ListByProviderTenant 服务商管理面：返回本 provider 的全部有效分配
// （2026-10-05 浏览器 E2E 修复：管理页此前只列“本人”分配，provider_admin
// 创建团队分配后看不到、无法核对/解除）。
func (s *MSPAllocationService) ListByProviderTenant(ctx context.Context, providerTenantID int) ([]*dto.MSPAllocationDTO, error) {
	if providerTenantID <= 0 {
		return nil, fmt.Errorf("provider tenant id must be positive")
	}
	allocations, err := s.client.MSPAllocation.Query().
		Where(
			mspallocation.ProviderTenantIDEQ(providerTenantID),
			mspallocation.DeassignedAtIsNil(),
		).
		WithCustomerTenant().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询分配失败: %w", err)
	}
	dtos := make([]*dto.MSPAllocationDTO, 0, len(allocations))
	for _, a := range allocations {
		item, err := s.toDTO(a)
		if err != nil {
			s.logger.Warnw("转换DTO失败", "error", err)
			continue
		}
		dtos = append(dtos, item)
	}
	return dtos, nil
}

// ListForCaller 管理面读取入口：provider_admin 返回本服务商全部有效分配，
// 其余 MSP 员工仍只看本人分配（无权限提升语义）。
func (s *MSPAllocationService) ListForCaller(ctx context.Context, userID int) ([]*dto.MSPAllocationDTO, error) {
	if actor, err := s.client.User.Query().Where(user.IDEQ(userID)).Only(ctx); err == nil {
		if string(actor.MspRole) == "provider_admin" {
			return s.ListByProviderTenant(ctx, actor.TenantID)
		}
	}
	return s.ListByMSPUser(ctx, userID)
}

// AllocationHistoryQuery 分配历史过滤条件（handler 解析；零值 = 不过滤）。
type AllocationHistoryQuery struct {
	MSPUserID        int
	CustomerTenantID int
	// StartDate/EndDateExclusive 按 assigned_at 过滤（[start, end) 半开区间；handler 负责把
	// YYYY-MM-DD 的 endDate 转成次日零点，保证「含当日」语义）。
	StartDate        *time.Time
	EndDateExclusive *time.Time
}

// ListHistoryForCaller 分配历史（含已解除记录；补齐前端 /msp 页已调用端点，target-architecture P1）。
//
// 作用域与 ListForCaller 同口径（无权限提升语义）：provider_admin 看本 provider 全部历史；
// 其余员工仅看本人历史（忽略传入 mspUserId）。展示名批量解析并显式 system 作用域（与 toDTO 同口径，避免 N+1）。
func (s *MSPAllocationService) ListHistoryForCaller(ctx context.Context, callerUserID int, q AllocationHistoryQuery) ([]*dto.MSPAllocationHistory, error) {
	actor, err := s.client.User.Query().Where(user.IDEQ(callerUserID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询调用者失败: %w", err)
	}

	query := s.client.MSPAllocation.Query()
	if string(actor.MspRole) == "provider_admin" {
		query = query.Where(mspallocation.ProviderTenantIDEQ(actor.TenantID))
		if q.MSPUserID > 0 {
			query = query.Where(mspallocation.MspUserIDEQ(q.MSPUserID))
		}
	} else {
		query = query.Where(mspallocation.MspUserIDEQ(callerUserID))
	}
	if q.CustomerTenantID > 0 {
		query = query.Where(mspallocation.CustomerTenantIDEQ(q.CustomerTenantID))
	}
	if q.StartDate != nil {
		query = query.Where(mspallocation.AssignedAtGTE(*q.StartDate))
	}
	if q.EndDateExclusive != nil {
		query = query.Where(mspallocation.AssignedAtLT(*q.EndDateExclusive))
	}

	// 历史读取允许含已解除记录；上限兜底防无界扫描。
	allocations, err := query.
		Order(ent.Desc(mspallocation.FieldAssignedAt), ent.Desc(mspallocation.FieldID)).
		Limit(500).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询分配历史失败: %w", err)
	}

	// 展示名批量解析：跨 provider/客户两侧，属平台范围读取 → 显式 system 作用域（与 toDTO 同口径）。
	sysCtx := tenantctx.SystemContext(context.Background(), "msp:allocation:history", "resolve allocation history display names (cross-tenant, platform scope)")
	userIDs := make([]int, 0, len(allocations))
	tenantIDs := make([]int, 0, len(allocations))
	seenUser := map[int]struct{}{}
	seenTenant := map[int]struct{}{}
	for _, a := range allocations {
		if _, ok := seenUser[a.MspUserID]; !ok {
			seenUser[a.MspUserID] = struct{}{}
			userIDs = append(userIDs, a.MspUserID)
		}
		if _, ok := seenTenant[a.CustomerTenantID]; !ok {
			seenTenant[a.CustomerTenantID] = struct{}{}
			tenantIDs = append(tenantIDs, a.CustomerTenantID)
		}
	}
	usernameByID := map[int]string{}
	if len(userIDs) > 0 {
		users, err := s.client.User.Query().Where(user.IDIn(userIDs...)).All(sysCtx)
		if err != nil {
			return nil, fmt.Errorf("解析员工姓名失败: %w", err)
		}
		for _, u := range users {
			usernameByID[u.ID] = u.Name
		}
	}
	nameByTenantID := map[int]string{}
	if len(tenantIDs) > 0 {
		tenants, err := s.client.Tenant.Query().Where(tenant.IDIn(tenantIDs...)).All(sysCtx)
		if err != nil {
			return nil, fmt.Errorf("解析客户名称失败: %w", err)
		}
		for _, tn := range tenants {
			nameByTenantID[tn.ID] = tn.Name
		}
	}

	history := make([]*dto.MSPAllocationHistory, 0, len(allocations))
	for _, a := range allocations {
		item := &dto.MSPAllocationHistory{
			ID:               a.ID,
			MSPUserID:        a.MspUserID,
			MSPUsername:      usernameByID[a.MspUserID],
			CustomerTenantID: a.CustomerTenantID,
			CustomerName:     nameByTenantID[a.CustomerTenantID],
			Role:             a.Role,
			AssignedAt:       a.AssignedAt,
		}
		if !a.DeassignedAt.IsZero() {
			deassigned := a.DeassignedAt
			item.DeassignedAt = &deassigned
		}
		history = append(history, item)
	}
	return history, nil
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

// Deactivate 解除分配；MSW-1：同步软删客户作用域 membership（deassigned_at + deleted_at）。
// 幂等：无有效分配时静默返回 nil。
func (s *MSPAllocationService) Deactivate(ctx context.Context, mspUserID int, customerTenantID int) error {
	alloc, err := s.client.MSPAllocation.Query().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID)),
			mspallocation.DeassignedAtIsNil(),
		).
		Order(ent.Desc(mspallocation.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("查询分配失败: %w", err)
	}
	if _, err := s.client.MSPAllocation.UpdateOneID(alloc.ID).
		SetDeassignedAt(time.Now()).
		Save(ctx); err != nil {
		return fmt.Errorf("解除分配失败: %w", err)
	}
	if s.membershipSync != nil {
		meta := MembershipAuditMeta{
			RowTenantID:  alloc.ProviderTenantID,
			ActorAccount: fmt.Sprintf("allocation:user:%d", mspUserID),
			Source:       middleware.AuditSourceHeader,
			Trigger:      "msp.allocation.deassign",
		}
		if err := s.membershipSync.RevokeAllocationMembership(ctx, mspUserID, customerTenantID, meta); err != nil {
			s.logger.Warnw("membership revoke after deallocation failed (allocation deassigned; repair via inspection)",
				"error", err, "user_id", mspUserID, "customer_tenant_id", customerTenantID)
		}
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
//
// 例外（2026-10-05 浏览器 E2E 修复）：provider_admin（users.msp_role）是服务商管理角色，
// 管理面必须能看到本服务商全部客户目录——否则“首次分配”因客户列表为空而不可达。
// provider_agent 仍严格按有效分配收窄。
func (s *MSPAllocationService) GetMSPCustomers(ctx context.Context, mspUserID int) ([]*ent.Tenant, error) {
	if actor, aErr := s.client.User.Query().Where(user.IDEQ(mspUserID)).Only(ctx); aErr == nil {
		if string(actor.MspRole) == "provider_admin" {
			return s.client.Tenant.Query().
				Where(
					tenant.MspProviderIDEQ(actor.TenantID),
					tenant.TypeIn(
						tenant.Type(tenantmode.TenantTypeMSPCustomer),
						tenant.Type(tenantmode.TenantTypeLegacyCustomer),
					),
				).
				Order(ent.Asc(tenant.FieldID)).
				All(ctx)
		}
	}
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
