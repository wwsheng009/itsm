package service

import (
	"context"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"
)

// resolveTicketMSPProvider 依据工单租户（客户租户）的归属派生 MSP 快照
// （IP-P0-3；canon §7.2 建单规则 / R11 / A12）。
//
// 判定：租户为客户类型 → 取 msp_provider_id（legacy `customer` 类型回退读 parent_tenant_id，
// D2 兼容读）→ provider 租户必须存在、类型为 msp_provider 且 active；客户本身必须 active 且未过期。
// 任一条件不满足 → 返回 nil：按普通工单处理，不写快照、不阻断建单。
func resolveTicketMSPProvider(ctx context.Context, client *ent.Client, tenantID int) *int {
	if client == nil || tenantID <= 0 {
		return nil
	}
	customer, err := client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil || customer == nil {
		return nil
	}
	if !tenantmode.IsCustomerTenantType(string(customer.Type)) {
		return nil
	}
	if customer.Status != "" && customer.Status != "active" {
		return nil
	}
	if !customer.ExpiresAt.IsZero() && customer.ExpiresAt.Before(time.Now()) {
		return nil
	}
	providerID := customer.MspProviderID
	if providerID <= 0 && string(customer.Type) == tenantmode.TenantTypeLegacyCustomer {
		providerID = customer.ParentTenantID
	}
	if providerID <= 0 {
		return nil
	}
	provider, err := client.Tenant.Query().Where(tenant.IDEQ(providerID)).Only(ctx)
	if err != nil || provider == nil {
		return nil
	}
	if !tenantmode.IsMSPProviderTenantType(string(provider.Type)) {
		return nil
	}
	if provider.Status != "" && provider.Status != "active" {
		return nil
	}
	return &providerID
}
