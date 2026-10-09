package tenantmode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateTenantTypeForWrite 锁定 A1：目标集合可写，legacy/未知/空值拒绝。
func TestValidateTenantTypeForWrite(t *testing.T) {
	for _, ok := range []string{TenantTypeInternal, TenantTypeMSPProvider, TenantTypeMSPCustomer, TenantTypeSaaSCustomer} {
		assert.NoError(t, ValidateTenantTypeForWrite(ok), ok)
	}
	for _, bad := range []string{"", TenantTypeLegacyMSP, TenantTypeLegacyCustomer, TenantTypeStandard, "platform", "owner"} {
		err := ValidateTenantTypeForWrite(bad)
		require.Error(t, err, bad)
	}
	// legacy 值提示只读，避免误用
	err := ValidateTenantTypeForWrite(TenantTypeLegacyMSP)
	assert.Contains(t, err.Error(), "legacy and read-only")
}

// TestNormalizeTenantTypeRead 锁定 A1 读取兼容映射。
func TestNormalizeTenantTypeRead(t *testing.T) {
	assert.Equal(t, TenantTypeMSPProvider, NormalizeTenantTypeRead(TenantTypeLegacyMSP))
	assert.Equal(t, TenantTypeMSPCustomer, NormalizeTenantTypeRead(TenantTypeLegacyCustomer))
	assert.Equal(t, TenantTypeInternal, NormalizeTenantTypeRead(TenantTypeStandard))
	assert.Equal(t, TenantTypeSaaSCustomer, NormalizeTenantTypeRead(TenantTypeSaaSCustomer))
	assert.Equal(t, "unknown", NormalizeTenantTypeRead("unknown"))
}

// TestTenantTypeFilterValues 锁定读取过滤兼容：任一同义值都能同时匹配新值与 legacy 值。
func TestTenantTypeFilterValues(t *testing.T) {
	assert.ElementsMatch(t, []string{TenantTypeMSPProvider, TenantTypeLegacyMSP}, TenantTypeFilterValues(TenantTypeMSPProvider))
	assert.ElementsMatch(t, []string{TenantTypeMSPProvider, TenantTypeLegacyMSP}, TenantTypeFilterValues(TenantTypeLegacyMSP))
	assert.ElementsMatch(t, []string{TenantTypeMSPCustomer, TenantTypeLegacyCustomer}, TenantTypeFilterValues(TenantTypeLegacyCustomer))
	assert.ElementsMatch(t, []string{TenantTypeInternal, TenantTypeStandard}, TenantTypeFilterValues(TenantTypeStandard))
	assert.ElementsMatch(t, []string{TenantTypeSaaSCustomer}, TenantTypeFilterValues(TenantTypeSaaSCustomer))
}

// TestValidateTenantOwnership 锁定 A2/D2 归属形状。
func TestValidateTenantOwnership(t *testing.T) {
	pid := 7
	self := 7

	assert.NoError(t, ValidateTenantOwnership(TenantTypeMSPCustomer, &pid, 0))
	require.Error(t, ValidateTenantOwnership(TenantTypeMSPCustomer, nil, 0), "msp_customer 必须带 provider")
	require.Error(t, ValidateTenantOwnership(TenantTypeMSPCustomer, &self, self), "禁自指")

	assert.NoError(t, ValidateTenantOwnership(TenantTypeSaaSCustomer, nil, 0))
	require.Error(t, ValidateTenantOwnership(TenantTypeSaaSCustomer, &pid, 0), "直客不得带 provider")
	require.Error(t, ValidateTenantOwnership(TenantTypeInternal, &pid, 0))
	require.Error(t, ValidateTenantOwnership(TenantTypeMSPProvider, &pid, 0))
	assert.NoError(t, ValidateTenantOwnership(TenantTypeInternal, nil, 0))
	assert.NoError(t, ValidateTenantOwnership(TenantTypeMSPProvider, nil, 0))
}

// TestValidateTenantTypeForDeployment 锁定部署模式 × 租户类型矩阵（canon §2.2）：
// saas_msp 拒绝直客 saas_customer；private/saas 拒绝 MSP 租户；未知模式 fail-closed。
func TestValidateTenantTypeForDeployment(t *testing.T) {
	t.Run("saas_msp 允许 provider/customer 拒绝直客", func(t *testing.T) {
		for _, ok := range []string{TenantTypeInternal, TenantTypeMSPProvider, TenantTypeMSPCustomer} {
			assert.NoError(t, ValidateTenantTypeForDeployment(DeploymentModeSaaSMSP, ok), ok)
		}
		err := ValidateTenantTypeForDeployment(DeploymentModeSaaSMSP, TenantTypeSaaSCustomer)
		require.Error(t, err, "saas_msp 不得创建无 provider 归属的直客")
		var mismatch *DeploymentTypeMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Equal(t, DeploymentModeSaaSMSP, mismatch.Mode)
		assert.Equal(t, TenantTypeSaaSCustomer, mismatch.Kind)
		assert.Contains(t, err.Error(), "internal|msp_provider|msp_customer")
	})

	for _, mode := range []string{DeploymentModeSaaS, DeploymentModePrivate} {
		t.Run(mode+" 允许直客拒绝 MSP 租户", func(t *testing.T) {
			for _, ok := range []string{TenantTypeInternal, TenantTypeSaaSCustomer} {
				assert.NoError(t, ValidateTenantTypeForDeployment(mode, ok), ok)
			}
			for _, bad := range []string{TenantTypeMSPProvider, TenantTypeMSPCustomer} {
				require.Error(t, ValidateTenantTypeForDeployment(mode, bad), bad)
			}
		})
	}

	t.Run("未知模式 fail-closed", func(t *testing.T) {
		require.Error(t, ValidateTenantTypeForDeployment("", TenantTypeInternal))
		require.Error(t, ValidateTenantTypeForDeployment("owner", TenantTypeInternal))
	})

	t.Run("legacy/未知类型拒绝", func(t *testing.T) {
		require.Error(t, ValidateTenantTypeForDeployment(DeploymentModeSaaSMSP, TenantTypeLegacyCustomer))
		require.Error(t, ValidateTenantTypeForDeployment(DeploymentModeSaaSMSP, "platform"))
	})
}

// TestDeploymentAllowsTenantType 锁定纯判定函数与写入集合的一致性。
func TestDeploymentAllowsTenantType(t *testing.T) {
	assert.True(t, DeploymentAllowsTenantType(DeploymentModeSaaSMSP, TenantTypeMSPProvider))
	assert.True(t, DeploymentAllowsTenantType(DeploymentModeSaaS, TenantTypeSaaSCustomer))
	assert.True(t, DeploymentAllowsTenantType(DeploymentModePrivate, TenantTypeInternal))
	assert.False(t, DeploymentAllowsTenantType(DeploymentModeSaaSMSP, TenantTypeSaaSCustomer))
	assert.False(t, DeploymentAllowsTenantType(DeploymentModeSaaS, TenantTypeMSPCustomer))
	assert.False(t, DeploymentAllowsTenantType(DeploymentModePrivate, TenantTypeMSPProvider))
	// legacy 值永不通过（写入集合之外）。
	assert.False(t, DeploymentAllowsTenantType(DeploymentModeSaaSMSP, TenantTypeLegacyMSP))
}
