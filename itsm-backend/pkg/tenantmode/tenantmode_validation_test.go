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
