package bootstrap

import (
	"context"
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
	return count, deploymentShapeMismatch(mode, count, mspEnabled)
}
