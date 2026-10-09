package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"
)

// deploymentShapeMismatch 判断部署门控与租户形态是否一致（IP-P0-1 启动自检；纯函数便于单测）。
// 返回 nil 表示一致；否则返回可读的不一致原因（调用方仅告警，不阻断启动）。
func deploymentShapeMismatch(mode string, providerTenantCount int, mspEnabled bool) error {
	mspMode := mode == tenantmode.DeploymentModeSaaSMSP
	switch {
	case mspMode && providerTenantCount == 0:
		return fmt.Errorf("saas_msp 但未发现 provider 租户（seed/迁移可能未完成）")
	case mspMode && !mspEnabled:
		return fmt.Errorf("saas_msp 但 MSP 门控未开启")
	case !mspMode && mspEnabled:
		return fmt.Errorf("非 saas_msp 模式但 MSP 门控已开启（/api/v1/msp/* 应 404）")
	case !mspMode && providerTenantCount > 0:
		return fmt.Errorf("非 saas_msp 模式但已存在 provider 租户（模式与租户形态不一致）")
	}
	return nil
}

// verifyDeploymentTenantShape 统计 provider 租户并执行一致性判断（IP-P0-1）。
func verifyDeploymentTenantShape(ctx context.Context, client *ent.Client, mode string, mspEnabled bool) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("ent client 不可用")
	}
	count, err := client.Tenant.Query().
		Where(tenant.Or(
			tenant.TypeEQ(tenantmode.TenantTypeMSPProvider),
			tenant.TypeEQ(tenantmode.TenantTypeLegacyMSP),
		)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计 provider 租户失败: %w", err)
	}

	// 存量孤儿形态（模式 × 类型校验落地前的遗留数据）：仅告警，不阻断启动。
	saasCustomers, err := client.Tenant.Query().
		Where(tenant.TypeEQ(tenantmode.TenantTypeSaaSCustomer)).
		Count(ctx)
	if err != nil {
		return count, fmt.Errorf("统计 saas_customer 租户失败: %w", err)
	}
	mspCustomers, err := client.Tenant.Query().
		Where(tenant.Or(
			tenant.TypeEQ(tenantmode.TenantTypeMSPCustomer),
			tenant.TypeEQ(tenantmode.TenantTypeLegacyCustomer),
		)).
		Count(ctx)
	if err != nil {
		return count, fmt.Errorf("统计 msp_customer 租户失败: %w", err)
	}

	return count, errors.Join(
		deploymentShapeMismatch(mode, count, mspEnabled),
		deploymentOrphanTenantWarning(mode, saasCustomers, mspCustomers),
	)
}

// deploymentOrphanTenantWarning 检查模式与存量租户类型的"孤儿"形态（warning-only）：
//   - saas_msp 下的 saas_customer：无 provider 归属、MSP 侧不可见（写路径校验落地前的遗留）；
//   - 非 saas_msp 下的 msp_customer：MSP 路由整族关闭，托管能力不可用。
//
// 写路径（TenantService）已阻止新增；本检查负责把存量不一致暴露给运维。
func deploymentOrphanTenantWarning(mode string, saasCustomerCount, mspCustomerCount int) error {
	switch {
	case mode == tenantmode.DeploymentModeSaaSMSP && saasCustomerCount > 0:
		return fmt.Errorf(
			"saas_msp 部署存在 %d 个 saas_customer 直客租户（无 provider 归属、MSP 侧不可见）；请将其改为 msp_customer 或迁移",
			saasCustomerCount,
		)
	case mode != tenantmode.DeploymentModeSaaSMSP && mspCustomerCount > 0:
		return fmt.Errorf(
			"非 saas_msp 模式但已存在 %d 个 msp_customer 租户（MSP 路由关闭，托管能力不可用）",
			mspCustomerCount,
		)
	}
	return nil
}
