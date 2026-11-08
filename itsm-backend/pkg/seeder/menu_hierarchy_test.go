package seeder

import (
	"context"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/menu"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertTenantMenuHierarchyMatches 按 path 对比两个租户的完整父子结构，避免仅抽查
// 个别菜单而漏掉其它扁平节点，也确保目标租户不会直接引用源租户的菜单 ID。
func assertTenantMenuHierarchyMatches(t *testing.T, ctx context.Context, client *ent.Client, sourceTenantID, targetTenantID int) {
	t.Helper()

	sourceMenus, err := client.Menu.Query().Where(menu.TenantIDEQ(sourceTenantID)).All(ctx)
	require.NoError(t, err)
	targetMenus, err := client.Menu.Query().Where(menu.TenantIDEQ(targetTenantID)).All(ctx)
	require.NoError(t, err)

	sourceByID := make(map[int]*ent.Menu, len(sourceMenus))
	for _, item := range sourceMenus {
		sourceByID[item.ID] = item
	}
	targetByPath := make(map[string]*ent.Menu, len(targetMenus))
	for _, item := range targetMenus {
		targetByPath[item.Path] = item
	}
	require.Len(t, targetByPath, len(targetMenus), "目标租户菜单 path 不应重复")

	for _, expected := range sourceMenus {
		actual, ok := targetByPath[expected.Path]
		require.True(t, ok, "目标租户缺少菜单 %s", expected.Path)
		if expected.ParentID == nil {
			assert.Nil(t, actual.ParentID, "根菜单 %s 不应挂到父节点", expected.Path)
			continue
		}

		sourceParent, ok := sourceByID[*expected.ParentID]
		require.True(t, ok, "源菜单 %s 的父节点 %d 不存在", expected.Path, *expected.ParentID)
		targetParent, ok := targetByPath[sourceParent.Path]
		require.True(t, ok, "目标租户缺少父菜单 %s", sourceParent.Path)
		if assert.NotNil(t, actual.ParentID, "子菜单 %s 应挂在 %s 下", expected.Path, sourceParent.Path) {
			assert.Equal(t, targetParent.ID, *actual.ParentID, "菜单 %s 的父节点应为目标租户自身记录", expected.Path)
		}
	}
}

func TestCloneMenusPreservesAndRepairsHierarchy(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModeSaaSMSP)
	require.NoError(t, seeder.SeedProduction(ctx))

	sourceTenant, err := seeder.client.Tenant.Query().Where(tenant.CodeEQ("default")).Only(ctx)
	require.NoError(t, err)
	targetTenant, err := seeder.client.Tenant.Create().
		SetName("菜单层级测试客户").
		SetCode("menu-hierarchy-target").
		SetType(tenant.TypeSaasCustomer).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, cloneMenus(ctx, seeder.client, sourceTenant.ID, targetTenant.ID))
	assertTenantMenuHierarchyMatches(t, ctx, seeder.client, sourceTenant.ID, targetTenant.ID)

	// 模拟旧版本产生的扁平/错挂数据，并加入租户自定义字段，验证重放只修层级。
	child, err := seeder.client.Menu.Query().Where(
		menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/tickets/types"),
	).Only(ctx)
	require.NoError(t, err)
	_, err = child.Update().
		ClearParentID().
		SetName("租户自定义工单类型").
		SetSortOrder(777).
		Save(ctx)
	require.NoError(t, err)

	ticketsRoot, err := seeder.client.Menu.Query().Where(
		menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/tickets"),
	).Only(ctx)
	require.NoError(t, err)
	wrongParentChild, err := seeder.client.Menu.Query().Where(
		menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/workflow/audit"),
	).Only(ctx)
	require.NoError(t, err)
	_, err = wrongParentChild.Update().SetParentID(ticketsRoot.ID).Save(ctx)
	require.NoError(t, err)

	countBefore, err := seeder.client.Menu.Query().Where(menu.TenantIDEQ(targetTenant.ID)).Count(ctx)
	require.NoError(t, err)
	require.NoError(t, cloneMenus(ctx, seeder.client, sourceTenant.ID, targetTenant.ID))
	assertTenantMenuHierarchyMatches(t, ctx, seeder.client, sourceTenant.ID, targetTenant.ID)

	repaired, err := seeder.client.Menu.Query().Where(
		menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/tickets/types"),
	).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, "租户自定义工单类型", repaired.Name)
	assert.Equal(t, 777, repaired.SortOrder)

	// 第三次执行不应再插入或改变已正确的层级。
	require.NoError(t, cloneMenus(ctx, seeder.client, sourceTenant.ID, targetTenant.ID))
	countAfter, err := seeder.client.Menu.Query().Where(menu.TenantIDEQ(targetTenant.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, countBefore, countAfter)
	assertTenantMenuHierarchyMatches(t, ctx, seeder.client, sourceTenant.ID, targetTenant.ID)
}

func TestSeedMenusRelocatesLegacyMasterDataPaths(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModeSaaSMSP)
	require.NoError(t, seeder.SeedProduction(ctx))

	sourceTenant, err := seeder.client.Tenant.Query().Where(tenant.CodeEQ("default")).Only(ctx)
	require.NoError(t, err)
	targetTenant, err := seeder.client.Tenant.Create().
		SetName("主数据迁移测试客户").
		SetCode("master-data-relocation-target").
		SetType(tenant.TypeSaasCustomer).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	require.NoError(t, cloneMenus(ctx, seeder.client, sourceTenant.ID, targetTenant.ID))

	masterDataParent, err := seeder.client.Menu.Query().
		Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/master-data")).
		Only(ctx)
	require.NoError(t, err)
	emailParent, err := seeder.client.Menu.Query().
		Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/email-intake")).
		Only(ctx)
	require.NoError(t, err)

	// 模拟上一版本：客户/合同/来源组织仍挂在 /email-intake 下。
	for _, relocation := range masterDataMenuRelocations {
		item, err := seeder.client.Menu.Query().
			Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ(relocation.NewPath)).
			Only(ctx)
		require.NoError(t, err)
		_, err = item.Update().
			SetPath(relocation.OldPath).
			SetName(relocation.OldName).
			SetParentID(emailParent.ID).
			Save(ctx)
		require.NoError(t, err)
	}

	seeder.seedMenus(ctx)

	for _, relocation := range masterDataMenuRelocations {
		oldCount, err := seeder.client.Menu.Query().
			Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ(relocation.OldPath)).
			Count(ctx)
		require.NoError(t, err)
		assert.Zero(t, oldCount, "旧路径应被迁移或删除: %s", relocation.OldPath)

		item, err := seeder.client.Menu.Query().
			Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ(relocation.NewPath)).
			Only(ctx)
		require.NoError(t, err)
		require.NotNil(t, item.ParentID)
		assert.Equal(t, masterDataParent.ID, *item.ParentID)
	}

	customerMenu, err := seeder.client.Menu.Query().
		Where(menu.TenantIDEQ(targetTenant.ID), menu.PathEQ("/master-data/customers")).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, "客户主数据", customerMenu.Name)
	assertTenantMenuHierarchyMatches(t, ctx, seeder.client, sourceTenant.ID, targetTenant.ID)
}
