package seeder

import (
	"context"
	"testing"

	"itsm-backend/config"
	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/service"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestProvisionTenant_NoPlatformGovernedFamilies 防再生守卫（2026-10-10 治理域收口 §8 D1=A）：
// 非平台根租户经 ProvisionTenant 克隆后，目录与角色授权都必须零平台治理家族（system/tenant/mcp）——
// 存量收回后不得被开通链重新引入；平台根（模板租户 code=default）保持全量（供给源不裁剪）。
//
// 触发背景：D1 决策材料 §3「关键供给事实」——只做一次性数据迁移而不改开通链路，新租户会持续
// 重新引入平台家族码（漂移再生）。本用例是防再生的可失败性证明。
func TestProvisionTenant_NoPlatformGovernedFamilies(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("SEED_USER1_PASSWORD", "user123")
	t.Setenv("SEED_SECURITY1_PASSWORD", "sec123")

	drv, err := entsql.Open("sqlite3", "file:provision-platform-family?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() {
		client.Close()
	})
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))

	cfg := &config.Config{
		Deployment: config.DeploymentConfig{
			Mode: tenantmode.DeploymentModeSaaS,
		},
	}
	s := NewSeeder(client, zap.NewNop().Sugar(), cfg)
	require.NoError(t, s.SeedProduction(ctx))

	// 模板租户（平台根）：保留平台家族码（供给源；非平台根才裁剪）。
	root, err := client.Tenant.Query().Where(tenant.CodeEQ("default")).Only(ctx)
	require.NoError(t, err)
	rootPlatformRows, err := client.Permission.Query().
		Where(permission.TenantIDEQ(root.ID), permission.ResourceIn("system", "tenant", "mcp")).
		Count(ctx)
	require.NoError(t, err)
	require.Positive(t, rootPlatformRows, "平台根模板租户必须保留平台家族码（供给源）")

	target := createProvisionTarget(t, ctx, client, "platform-family-target", tenant.TypeSaasCustomer)
	require.NoError(t, s.ProvisionTenant(ctx, target.ID, CurrentTenantTemplateVersion))
	// 幂等复跑：第二次供给也不得重新引入（防"复跑补齐"路径漏改）。
	require.NoError(t, s.ProvisionTenant(ctx, target.ID, CurrentTenantTemplateVersion))

	// ① 目录：非平台根零平台家族行。
	rows, err := client.Permission.Query().
		Where(permission.TenantIDEQ(target.ID), permission.ResourceIn("system", "tenant", "mcp")).
		All(ctx)
	require.NoError(t, err)
	for _, item := range rows {
		assert.Falsef(t, service.IsPlatformGovernedPermissionResource(item.Resource),
			"非平台根目录混入平台家族码: %s", item.Code)
	}

	// ② 授权：非平台根零平台家族授权（授权只能引用目录行，双重校验防"行被跳过、授权仍写"）。
	grants, err := client.RolePermission.Query().Where(rolepermission.TenantIDEQ(target.ID)).All(ctx)
	require.NoError(t, err)
	permIDs := make([]int, 0, len(grants))
	for _, item := range grants {
		permIDs = append(permIDs, item.PermissionID)
	}
	if len(permIDs) > 0 {
		perms, err := client.Permission.Query().Where(permission.IDIn(permIDs...)).All(ctx)
		require.NoError(t, err)
		for _, item := range perms {
			assert.Falsef(t, service.IsPlatformGovernedPermissionResource(item.Resource),
				"非平台根角色授权混入平台家族码: %s", item.Code)
		}
	}

	// ③ 供给未被整体破坏：租户面码仍然齐备。
	tenantRows, err := client.Permission.Query().Where(permission.TenantIDEQ(target.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Positive(t, tenantRows, "非平台根目录应保留租户面码（供给不得整体失效）")
}
