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

// ===== 租户类型收敛与归属校验（IP-P0-4；canon A1/A2、C3、D2）=====

// tenantTypeWriteSet 目标写入集合：概念名 platform/provider/customer 对应列值
// （D2：直客使用显式标记 saas_customer）。
var tenantTypeWriteSet = map[string]bool{
	TenantTypeInternal:     true,
	TenantTypeMSPProvider:  true,
	TenantTypeMSPCustomer:  true,
	TenantTypeSaaSCustomer: true,
}

// ValidateTenantTypeForWrite 写入校验（A1）：只接受目标集合；
// legacy（msp/customer/standard）与未知值一律拒绝（读取兼容见 NormalizeTenantTypeRead）。
func ValidateTenantTypeForWrite(kind string) error {
	if kind == "" {
		return fmt.Errorf("tenant type is required: want internal|msp_provider|msp_customer|saas_customer")
	}
	if tenantTypeWriteSet[kind] {
		return nil
	}
	if kind == TenantTypeLegacyMSP || kind == TenantTypeLegacyCustomer || kind == TenantTypeStandard {
		return fmt.Errorf("tenant type %q is legacy and read-only; write internal|msp_provider|msp_customer|saas_customer instead", kind)
	}
	return fmt.Errorf("invalid tenant type %q: want internal|msp_provider|msp_customer|saas_customer", kind)
}

// NormalizeTenantTypeRead 读取兼容映射（A1）：legacy → 目标值，其余原样返回。
func NormalizeTenantTypeRead(kind string) string {
	switch kind {
	case TenantTypeLegacyMSP:
		return TenantTypeMSPProvider
	case TenantTypeLegacyCustomer:
		return TenantTypeMSPCustomer
	case TenantTypeStandard:
		return TenantTypeInternal
	}
	return kind
}

// TenantTypeFilterValues 读取过滤兼容：给定任一同义值，返回应匹配的全部列值（目标+legacy）。
func TenantTypeFilterValues(kind string) []string {
	switch NormalizeTenantTypeRead(kind) {
	case TenantTypeMSPProvider:
		return []string{TenantTypeMSPProvider, TenantTypeLegacyMSP}
	case TenantTypeMSPCustomer:
		return []string{TenantTypeMSPCustomer, TenantTypeLegacyCustomer}
	case TenantTypeInternal:
		return []string{TenantTypeInternal, TenantTypeStandard}
	default:
		return []string{kind}
	}
}

// ValidateTenantOwnership 归属形状校验（A2/D2/R3）：
//   - msp_customer ⇔ mspProviderID 非空且非自指（目标必须是 msp_provider，需调用方查库复核）；
//   - saas_customer/internal/msp_provider ⇔ mspProviderID 为空。
func ValidateTenantOwnership(kind string, providerID *int, selfID int) error {
	pid := 0
	if providerID != nil {
		pid = *providerID
	}
	switch kind {
	case TenantTypeMSPCustomer:
		if pid <= 0 {
			return fmt.Errorf("tenant type msp_customer requires mspProviderId pointing to an msp_provider tenant")
		}
		if selfID > 0 && pid == selfID {
			return fmt.Errorf("tenant ownership cannot reference the tenant itself (id=%d)", selfID)
		}
	case TenantTypeSaaSCustomer, TenantTypeInternal, TenantTypeMSPProvider:
		if pid > 0 {
			return fmt.Errorf("tenant type %s must not carry mspProviderId", kind)
		}
	}
	return nil
}

// ===== 部署模式 × 租户类型一致性（canon §2.2 部署模式矩阵；与 IP-P0-1 门控同源）=====

// DeploymentAllowsTenantType 报告部署模式是否允许写入某租户类型：
//   - saas_msp：internal / msp_provider / msp_customer
//     （拒绝 saas_customer——MSP 部署下的直客无 provider 归属，MSP 侧永不可见，形成"孤儿直客"）；
//   - private / saas：internal / saas_customer
//     （拒绝 msp_provider / msp_customer——MSP 路由整族关闭，MSP 租户形态无运行能力）；
//   - 未知模式：false（fail-closed；调用方应先经 ValidateDeploymentMode 保证模式合法）。
func DeploymentAllowsTenantType(mode, kind string) bool {
	switch mode {
	case DeploymentModeSaaSMSP:
		switch kind {
		case TenantTypeInternal, TenantTypeMSPProvider, TenantTypeMSPCustomer:
			return true
		}
	case DeploymentModePrivate, DeploymentModeSaaS:
		switch kind {
		case TenantTypeInternal, TenantTypeSaaSCustomer:
			return true
		}
	}
	return false
}

// allowedTenantTypesHint 返回模式允许的类型清单（仅用于错误消息，与 DeploymentAllowsTenantType 同源）。
func allowedTenantTypesHint(mode string) string {
	switch mode {
	case DeploymentModeSaaSMSP:
		return "internal|msp_provider|msp_customer"
	case DeploymentModePrivate, DeploymentModeSaaS:
		return "internal|saas_customer"
	default:
		return ""
	}
}

// DeploymentTypeMismatchError 部署模式与租户类型不一致（写路径 4xx，不落库）。
// 单独的 error 类型供 handler 映射为参数错误（而非 500）。
type DeploymentTypeMismatchError struct {
	Mode string
	Kind string
}

func (e *DeploymentTypeMismatchError) Error() string {
	return fmt.Sprintf(
		"deployment mode %q does not allow tenant type %q (allowed: %s)",
		e.Mode, e.Kind, allowedTenantTypesHint(e.Mode),
	)
}

// ValidateTenantTypeForDeployment 写路径联合校验：模式合法 + 类型可写 + 模式允许该类型。
// 调用方在模式未装配（空值，仅测试/独立工具）时应显式跳过本校验：
// 生产进程启动时已强制 ApplyDeploymentMode（空/未知值 fatal），服务期不会出现空模式。
func ValidateTenantTypeForDeployment(mode, kind string) error {
	if err := ValidateDeploymentMode(mode); err != nil {
		return err
	}
	if err := ValidateTenantTypeForWrite(kind); err != nil {
		return err
	}
	if !DeploymentAllowsTenantType(mode, kind) {
		return &DeploymentTypeMismatchError{Mode: mode, Kind: kind}
	}
	return nil
}
