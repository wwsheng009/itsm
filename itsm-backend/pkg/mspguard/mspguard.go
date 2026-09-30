// Package mspguard 提供 MSP 跨租户访问的唯一授权实现（IP-P0-2；canon A3/A4、R2/R9/R10）。
//
// 为什么放在 pkg/ 而非 service/：middleware 与 service 都必须调用同一实现，
// 而 service 包已依赖 middleware（menu/role/user service），入口放 service 会形成循环依赖。
// service.MSPAccessValidator 是 service 层门面（委托本包）；头通道、路径参数、请求体等
// 所有通道必须且只能经由本实现判定，禁止再出现第二套 allocation 校验。
package mspguard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/tenantmode"
)

// 跨租户授权错误码（权威：实施方案 §3.0-A）。
const (
	CodeMSPAllocationRequired  = "MSP_ALLOCATION_REQUIRED"
	CodeCustomerTenantNotFound = "CUSTOMER_TENANT_NOT_FOUND"
	CodeCustomerInactive       = "CUSTOMER_INACTIVE"
	CodeResourceTenantMismatch = "RESOURCE_TENANT_MISMATCH"
)

// AccessError 是跨租户授权失败的稳定错误类型：调用方依据 Code 映射 HTTP 状态与 reasonCode，
// 不可依赖 Message 文案做判断。
type AccessError struct {
	Code    string
	Message string
}

func (e *AccessError) Error() string { return e.Code + ": " + e.Message }

// NewAccessError 构造带错误码的授权失败错误。
func NewAccessError(code, format string, args ...interface{}) *AccessError {
	return &AccessError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AsAccessError 提取稳定错误码；非授权类错误返回 ok=false（调用方按内部错误处理）。
func AsAccessError(err error) (*AccessError, bool) {
	var target *AccessError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// Checker 绑定一个 ent client 执行授权判定（无状态，可随时构造）。
type Checker struct {
	client *ent.Client
}

// New 创建授权判定器；client 为 nil 时所有判定按内部错误返回（fail-closed）。
func New(client *ent.Client) *Checker {
	return &Checker{client: client}
}

// CanAccessCustomer 是 MSP 跨租户访问的唯一授权入口。
// 校验链：MSP 员工身份 → 有效分配（deassigned_at IS NULL）→ 客户租户存在 →
// 客户归属该 provider（R2）→ 客户 active 且未过期。
// 返回 *AccessError 表示确定性拒绝（403/404/400 由调用方映射）；其他 error 为内部故障。
func (c *Checker) CanAccessCustomer(ctx context.Context, mspUserID, customerTenantID int) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("mspguard: ent client unavailable")
	}
	actor, err := c.loadMSPActor(ctx, mspUserID)
	if err != nil {
		return err
	}

	allocation, err := c.client.MSPAllocation.Query().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.DeassignedAtIsNil(),
			mspallocation.HasCustomerTenantWith(tenant.IDEQ(customerTenantID)),
		).
		WithCustomerTenant(customerAccessFields).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// 未分配：对存在的客户租户统一 403（不因存在性产生可枚举差异）；确实不存在才 404。
			exists, existsErr := c.client.Tenant.Query().Where(tenant.IDEQ(customerTenantID)).Exist(ctx)
			if existsErr != nil {
				return fmt.Errorf("mspguard: check customer tenant: %w", existsErr)
			}
			if !exists {
				return NewAccessError(CodeCustomerTenantNotFound, "客户租户 %d 不存在", customerTenantID)
			}
			return NewAccessError(CodeMSPAllocationRequired, "未分配客户租户 %d（需有效 allocation）", customerTenantID)
		}
		return fmt.Errorf("mspguard: query allocation: %w", err)
	}

	customer := allocation.Edges.CustomerTenant
	if customer == nil {
		return NewAccessError(CodeCustomerTenantNotFound, "客户租户 %d 不存在", customerTenantID)
	}
	return validateCustomerForProvider(customer, actor.Edges.Tenant.ID)
}

// ListAccessibleCustomerIDs 返回该 MSP 员工当前可访问的客户租户 ID 集合：
// 分配有效 + 归属本 provider + 客户 active 且未过期。供报表/工作台等聚合查询复用，
// 避免调用方各自拼接过滤条件而产生绕过路径（R9/R10）。
func (c *Checker) ListAccessibleCustomerIDs(ctx context.Context, mspUserID int) ([]int, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("mspguard: ent client unavailable")
	}
	actor, err := c.loadMSPActor(ctx, mspUserID)
	if err != nil {
		return nil, err
	}

	allocations, err := c.client.MSPAllocation.Query().
		Where(
			mspallocation.MspUserIDEQ(mspUserID),
			mspallocation.DeassignedAtIsNil(),
		).
		WithCustomerTenant(customerAccessFields).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("mspguard: query allocations: %w", err)
	}

	ids := make([]int, 0, len(allocations))
	seen := make(map[int]struct{}, len(allocations))
	for _, a := range allocations {
		customer := a.Edges.CustomerTenant
		if customer == nil {
			continue
		}
		if err := validateCustomerForProvider(customer, actor.Edges.Tenant.ID); err != nil {
			continue // 归属不符/停用/过期：从可访问集合剔除（fail-closed）
		}
		if _, ok := seen[customer.ID]; !ok {
			seen[customer.ID] = struct{}{}
			ids = append(ids, customer.ID)
		}
	}
	return ids, nil
}

// loadMSPActor 加载并校验 MSP 员工身份（provider 租户 + msp_role 非空）。
func (c *Checker) loadMSPActor(ctx context.Context, mspUserID int) (*ent.User, error) {
	actor, err := c.client.User.Query().
		Where(user.IDEQ(mspUserID)).
		WithTenant(func(q *ent.TenantQuery) {
			q.Select(tenant.FieldID, tenant.FieldType)
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, NewAccessError(CodeMSPAllocationRequired, "MSP 用户不存在")
		}
		return nil, fmt.Errorf("mspguard: load msp actor: %w", err)
	}
	if actor.Edges.Tenant == nil ||
		!tenantmode.IsMSPProviderTenantType(string(actor.Edges.Tenant.Type)) ||
		strings.TrimSpace(string(actor.MspRole)) == "" {
		return nil, NewAccessError(CodeMSPAllocationRequired, "当前账号不是 MSP 员工")
	}
	return actor, nil
}

func customerAccessFields(q *ent.TenantQuery) {
	q.Select(
		tenant.FieldID,
		tenant.FieldType,
		tenant.FieldStatus,
		tenant.FieldMspProviderID,
		tenant.FieldExpiresAt,
	)
}

// validateCustomerForProvider 校验客户租户归属与可用状态（R2：allocation 必须指向本服务商客户）。
func validateCustomerForProvider(customer *ent.Tenant, providerTenantID int) error {
	// Optional int/time 在 ent 生成代码中为零值语义（0 / 零值时间 = 未设置）。
	if customer.MspProviderID == 0 || customer.MspProviderID != providerTenantID {
		return NewAccessError(CodeCustomerTenantNotFound, "客户租户 %d 不属于当前服务商", customer.ID)
	}
	if customer.Status != "" && customer.Status != "active" {
		return NewAccessError(CodeCustomerInactive, "客户租户 %d 已停用（%s）", customer.ID, customer.Status)
	}
	if !customer.ExpiresAt.IsZero() && customer.ExpiresAt.Before(time.Now()) {
		return NewAccessError(CodeCustomerInactive, "客户租户 %d 已过期", customer.ID)
	}
	return nil
}
