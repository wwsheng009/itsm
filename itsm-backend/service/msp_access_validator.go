package service

import (
	"context"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/mspguard"
)

// MSPAccessValidator validates MSP user access to customer data
type MSPAccessValidator struct {
	client *ent.Client
}

// NewMSPAccessValidator creates a new MSP access validator
func NewMSPAccessValidator(client *ent.Client) *MSPAccessValidator {
	return &MSPAccessValidator{client: client}
}

// ValidateCustomerAccess verifies MSP user can access the specified customer tenant
func (v *MSPAccessValidator) ValidateCustomerAccess(ctx context.Context, mspUserID, customerTenantID int) error {
	// Check if MSP user has active allocation to this customer
	// An active allocation means: msp_user_id matches AND deassigned_at is nil AND customer_tenant matches
	allocations, err := v.client.MSPAllocation.Query().
		Where(mspallocation.MspUserIDEQ(mspUserID)).
		Where(mspallocation.DeassignedAtIsNil()).
		Where(mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID))).
		All(ctx)
	if err != nil {
		return fmt.Errorf("access denied: failed to query allocations: %w", err)
	}

	if len(allocations) == 0 {
		return fmt.Errorf("access denied: no active allocation for customer tenant %d", customerTenantID)
	}

	return nil
}

// GetAllowedCustomerIDs returns list of customer tenant IDs the MSP user can access
func (v *MSPAccessValidator) GetAllowedCustomerIDs(ctx context.Context, mspUserID int) ([]int, error) {
	allocations, err := v.client.MSPAllocation.Query().
		Where(mspallocation.MspUserIDEQ(mspUserID)).
		Where(mspallocation.DeassignedAtIsNil()).
		WithCustomerTenant().
		All(ctx)
	if err != nil {
		return nil, err
	}

	customerIDs := make([]int, 0)
	seen := make(map[int]bool)
	for _, a := range allocations {
		if a.Edges.CustomerTenant != nil && !seen[a.Edges.CustomerTenant.ID] {
			customerIDs = append(customerIDs, a.Edges.CustomerTenant.ID)
			seen[a.Edges.CustomerTenant.ID] = true
		}
	}
	return customerIDs, nil
}

// FilterByMSPAllocation filters a list of tenant IDs to only allowed customers
func (v *MSPAccessValidator) FilterByMSPAllocation(ctx context.Context, mspUserID int, tenantIDs []int) ([]int, error) {
	allowed, err := v.GetAllowedCustomerIDs(ctx, mspUserID)
	if err != nil {
		return nil, err
	}

	allowedSet := make(map[int]bool)
	for _, id := range allowed {
		allowedSet[id] = true
	}

	filtered := make([]int, 0)
	for _, id := range tenantIDs {
		if allowedSet[id] {
			filtered = append(filtered, id)
		}
	}
	return filtered, nil
}

// ==================== 统一授权入口（IP-P0-2 / R9/R10） ====================
//
// 实现位于 pkg/mspguard（middleware 与 service 共用，避免 middleware ↔ service 循环依赖）；
// 本类型是 service 层门面。所有通道（请求头 / 路径参数 / 请求体）必须经由同一实现判定，
// 禁止再出现第二套 allocation 校验（历史 MSPFilterByCustomer / GetTicketsForCustomer 已删除）。

// 跨租户授权错误码（权威：实施方案 §3.0-A）。
const (
	CodeMSPAllocationRequired  = mspguard.CodeMSPAllocationRequired
	CodeMSPAllocationExists    = mspguard.CodeMSPAllocationExists
	CodeCustomerTenantNotFound = mspguard.CodeCustomerTenantNotFound
	CodeCustomerInactive       = mspguard.CodeCustomerInactive
	CodeResourceTenantMismatch = mspguard.CodeResourceTenantMismatch
)

// CustomerAccessError 保持 service 层既有错误类型签名（= mspguard.AccessError）。
type CustomerAccessError = mspguard.AccessError

// AsCustomerAccessError 提取稳定错误码（handler 用它映射 HTTP 状态与 reasonCode）。
func AsCustomerAccessError(err error) (*CustomerAccessError, bool) {
	return mspguard.AsAccessError(err)
}

// NewCustomerAccessError 构造带错误码的授权失败错误。
func NewCustomerAccessError(code, format string, args ...interface{}) *CustomerAccessError {
	return mspguard.NewAccessError(code, format, args...)
}

// CanAccessCustomer 是 MSP 跨租户访问的唯一授权入口（R9/R10、canon A3）。
func (v *MSPAccessValidator) CanAccessCustomer(ctx context.Context, mspUserID, customerTenantID int) error {
	return mspguard.New(v.client).CanAccessCustomer(ctx, mspUserID, customerTenantID)
}

// ListAccessibleCustomerIDs 返回分配有效、归属本 provider 且客户 active 的客户租户 ID 集合。
func (v *MSPAccessValidator) ListAccessibleCustomerIDs(ctx context.Context, mspUserID int) ([]int, error) {
	return mspguard.New(v.client).ListAccessibleCustomerIDs(ctx, mspUserID)
}
