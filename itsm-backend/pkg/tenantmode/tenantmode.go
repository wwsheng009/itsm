package tenantmode

import "fmt"

const (
	DeploymentModePrivate = "private"
	DeploymentModeSaaS    = "saas"
	DeploymentModeSaaSMSP = "saas_msp"
)

// ValidateDeploymentMode 校验 DEPLOYMENT_MODE 取值（封闭集合，IP-P0-1）。
// 空值与未知值一律拒绝：部署方必须显式声明模式，避免"拼写错误静默开启 MSP"（canon R12/I12）。
func ValidateDeploymentMode(mode string) error {
	switch mode {
	case DeploymentModePrivate, DeploymentModeSaaS, DeploymentModeSaaSMSP:
		return nil
	}
	return fmt.Errorf(
		"invalid DEPLOYMENT_MODE %q: want private|saas|saas_msp (config default is private; empty/unknown values are rejected to avoid silently opening MSP routes)",
		mode,
	)
}

// MSPRoutesEnabled 报告该部署模式是否开放 /api/v1/msp/*（目标态仅 saas_msp）。
func MSPRoutesEnabled(mode string) bool {
	return mode == DeploymentModeSaaSMSP
}

const (
	TenantTypeStandard     = "standard"
	TenantTypeInternal     = "internal"
	TenantTypeSaaSCustomer = "saas_customer"
	TenantTypeMSPProvider  = "msp_provider"
	TenantTypeMSPCustomer  = "msp_customer"

	// Legacy aliases kept for backward compatibility with existing data/tests.
	TenantTypeLegacyMSP      = "msp"
	TenantTypeLegacyCustomer = "customer"
)

func IsMSPProviderTenantType(kind string) bool {
	return kind == TenantTypeMSPProvider || kind == TenantTypeLegacyMSP
}

func IsCustomerTenantType(kind string) bool {
	return kind == TenantTypeMSPCustomer || kind == TenantTypeSaaSCustomer || kind == TenantTypeLegacyCustomer
}

func IsInternalTenantType(kind string) bool {
	return kind == TenantTypeInternal || kind == TenantTypeStandard
}
