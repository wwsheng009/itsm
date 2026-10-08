package seeder

import (
	"context"
	"strings"
	"testing"

	"itsm-backend/config"
	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func createProvisionTarget(t *testing.T, ctx context.Context, client *ent.Client, code string, typ tenant.Type) *ent.Tenant {
	t.Helper()
	item, err := client.Tenant.Create().SetName(code).SetCode(code).SetType(typ).Save(ctx)
	require.NoError(t, err)
	return item
}

func rolePermissionCodeSet(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, roleCode string) map[string]bool {
	t.Helper()
	roleRow, err := client.Role.Query().
		Where(role.TenantIDEQ(tenantID), role.CodeEQ(roleCode)).
		Only(ctx)
	require.NoError(t, err)
	grants, err := client.RolePermission.Query().
		Where(rolepermission.TenantIDEQ(tenantID), rolepermission.RoleIDEQ(roleRow.ID)).
		All(ctx)
	require.NoError(t, err)
	if len(grants) == 0 {
		return map[string]bool{}
	}
	permIDs := make([]int, 0, len(grants))
	for _, item := range grants {
		permIDs = append(permIDs, item.PermissionID)
	}
	perms, err := client.Permission.Query().Where(permission.IDIn(permIDs...)).All(ctx)
	require.NoError(t, err)
	out := make(map[string]bool, len(perms))
	for _, item := range perms {
		out[item.Code] = true
	}
	return out
}

// TestProvisionTenant_CustomerScopeMSPBaseline 锁定客户类型租户内的 msp_* 角色按 C2 客户侧基线供给
// （实施计划 §3.0-C2）：随 default 租户克隆落入的 C1 跨租户 msp_* 码被替换为客户侧业务码；
// provider 类型租户保持 C1 不受影响；复跑幂等。
func TestProvisionTenant_CustomerScopeMSPBaseline(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("SEED_USER1_PASSWORD", "user123")
	t.Setenv("SEED_SECURITY1_PASSWORD", "sec123")

	drv, err := entsql.Open("sqlite3", "file:provision-customer-scope?mode=memory&cache=shared&_fk=1")
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

	// 客户类型租户：C1 → C2 对账（含幂等复跑）。
	customer := createProvisionTarget(t, ctx, client, "customer-scope-target", tenant.TypeSaasCustomer)
	require.NoError(t, s.ProvisionTenant(ctx, customer.ID, CurrentTenantTemplateVersion))
	require.NoError(t, s.ProvisionTenant(ctx, customer.ID, CurrentTenantTemplateVersion))

	tech := rolePermissionCodeSet(t, ctx, client, customer.ID, "msp_tech")
	for _, code := range []string{
		"ticket:read", "ticket:create", "ticket:update", "knowledge:read",
		"cmdb:read", "service_catalog:read", "task:read", "task:update",
	} {
		assert.Truef(t, tech[code], "客户租户 msp_tech 缺少 C2 码 %s", code)
	}
	for code := range tech {
		assert.Falsef(t, strings.HasPrefix(code, "msp"), "客户租户 msp_tech 残留 C1 码 %s", code)
	}
	viewer := rolePermissionCodeSet(t, ctx, client, customer.ID, "msp_viewer")
	assert.True(t, viewer["ticket:read"])
	assert.False(t, viewer["msp:read"])
	manager := rolePermissionCodeSet(t, ctx, client, customer.ID, "msp_manager")
	assert.True(t, manager["user:write"])
	assert.True(t, manager["report:read"])
	admin := rolePermissionCodeSet(t, ctx, client, customer.ID, "msp_admin")
	assert.True(t, admin["cmdb:write"])
	assert.True(t, admin["change:write"])

	// provider 类型租户：保持 C1 跨租户码。
	provider := createProvisionTarget(t, ctx, client, "provider-scope-target", tenant.TypeMspProvider)
	require.NoError(t, s.ProvisionTenant(ctx, provider.ID, CurrentTenantTemplateVersion))
	providerTech := rolePermissionCodeSet(t, ctx, client, provider.ID, "msp_tech")
	assert.True(t, providerTech["msp_ticket:read"])
	assert.False(t, providerTech["ticket:read"])
}
