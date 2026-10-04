package seeder

import (
	"testing"

	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTenantReadinessReportsSevenRequiredItemsAfterProductionSeed 锁定冻结契约：
// 7 个 required 项的 key/label 顺序与命名，以及基线数据装配后计数全部 > 0。
func TestTenantReadinessReportsSevenRequiredItemsAfterProductionSeed(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModePrivate)
	require.NoError(t, seeder.SeedProduction(ctx))

	root, err := seeder.client.Tenant.Query().Where(tenant.CodeEQ("default")).Only(ctx)
	require.NoError(t, err)

	items, err := seeder.TenantReadiness(ctx, root.ID)
	require.NoError(t, err)
	require.Len(t, items, 7)

	want := []struct {
		key   string
		label string
	}{
		{"roles", "角色"},
		{"permissions", "权限"},
		{"role_permissions", "角色权限绑定"},
		{"menus", "菜单"},
		{"groups", "审批组"},
		{"sla_definitions", "SLA 定义"},
		{"ci_types", "CI 类型"},
	}
	for i, expected := range want {
		assert.Equal(t, expected.key, items[i].Key)
		assert.Equal(t, expected.label, items[i].Label)
		assert.True(t, items[i].Required, "item %s must be required", expected.key)
		assert.Positive(t, items[i].Count, "item %s should have records after SeedProduction", expected.key)
	}
}

// TestTenantReadinessZeroBeforeProvisionAndFullAfter 验证供给前后计数变化：
// 未供给租户 7 项全 0；ProvisionTenant 后 7 项全部 > 0。
func TestTenantReadinessZeroBeforeProvisionAndFullAfter(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModeSaaS)
	require.NoError(t, seeder.SeedProduction(ctx))

	target, err := seeder.client.Tenant.Create().
		SetName("Readiness Target").SetCode("readiness-target").
		SetType(tenant.TypeSaasCustomer).Save(ctx)
	require.NoError(t, err)

	items, err := seeder.TenantReadiness(ctx, target.ID)
	require.NoError(t, err)
	require.Len(t, items, 7)
	for _, item := range items {
		assert.Zero(t, item.Count, "item %s should be empty before provisioning", item.Key)
	}

	require.NoError(t, seeder.ProvisionTenant(ctx, target.ID, CurrentTenantTemplateVersion))
	items, err = seeder.TenantReadiness(ctx, target.ID)
	require.NoError(t, err)
	for _, item := range items {
		assert.Positive(t, item.Count, "item %s should have records after provisioning", item.Key)
	}
}

// TestValidateTenantReadinessPreservesNoRecordsError 锁定既有供给校验错误语义：
// 空租户仍然以 "validate tenant <check>: no records installed" 失败。
func TestValidateTenantReadinessPreservesNoRecordsError(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModePrivate)
	require.NoError(t, seeder.SeedProduction(ctx))

	target, err := seeder.client.Tenant.Create().
		SetName("Empty Target").SetCode("empty-target").
		SetType(tenant.TypeSaasCustomer).Save(ctx)
	require.NoError(t, err)

	err = validateTenantReadinessWithClient(ctx, seeder.client, target.ID)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no records installed")
	assert.ErrorContains(t, err, "validate tenant roles")
}
