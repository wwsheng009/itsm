package seeder

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/tenant"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var rolePermSyncDBCounter int64

// TestSyncBuiltinRolePermissionsForTenant 锁定跨租户权限同步语义：
// 增缺（workflow:read/bpmn:read）、删词表管理范围内的过期授予（msp:write）、
// 保留词表外自定义权限（custom:perm）、dry-run 不落库、复跑幂等。
func TestSyncBuiltinRolePermissionsForTenant(t *testing.T) {
	dsn := fmt.Sprintf("file:role_perm_sync_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&rolePermSyncDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	ctx := context.Background()

	tn, err := client.Tenant.Create().SetName("T").SetCode("t-sync").SetType(tenant.TypeMspCustomer).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	r, err := client.Role.Create().SetCode("admin").SetName("管理员").SetTenantID(tn.ID).Save(ctx)
	require.NoError(t, err)

	mkPerm := func(code string) *ent.Permission {
		p, err := client.Permission.Create().
			SetCode(code).SetName(code).SetResource("x").SetAction("read").SetTenantID(tn.ID).Save(ctx)
		require.NoError(t, err)
		return p
	}
	wanted := mkPerm("ticket:read") // admin 词表内
	mkPerm("bpmn:read")             // admin 词表内（缺失，待增）
	mkPerm("workflow:read")         // admin 词表内（缺失，待增）
	obsolete := mkPerm("msp:write") // 词表管理范围（msp_admin 引用）但 admin 不持有 → 待删
	custom := mkPerm("custom:perm") // 词表外 → 保留

	for _, pid := range []int{wanted.ID, obsolete.ID, custom.ID} {
		_, err := client.RolePermission.Create().SetRoleID(r.ID).SetPermissionID(pid).SetTenantID(tn.ID).Save(ctx)
		require.NoError(t, err)
	}

	// dry-run：只报差异，不落库。
	res, err := SyncBuiltinRolePermissionsForTenant(ctx, client, tn.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Roles)
	assert.Equal(t, 2, res.Created, "bpmn:read + workflow:read 待增")
	assert.Equal(t, 1, res.Deleted, "msp:write 待删")
	grants, err := client.RolePermission.Query().Where(rolepermission.TenantIDEQ(tn.ID)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, grants, 3, "dry-run 不得写库")

	// apply：落库并校验结果。
	res, err = SyncBuiltinRolePermissionsForTenant(ctx, client, tn.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Created)
	assert.Equal(t, 1, res.Deleted)

	codes := map[string]bool{}
	rows, err := client.RolePermission.Query().Where(rolepermission.TenantIDEQ(tn.ID)).All(ctx)
	require.NoError(t, err)
	permCodes := map[int]string{}
	perms, err := client.Permission.Query().Where(permission.TenantIDEQ(tn.ID)).All(ctx)
	require.NoError(t, err)
	for _, p := range perms {
		permCodes[p.ID] = p.Code
	}
	for _, g := range rows {
		codes[permCodes[g.PermissionID]] = true
	}
	assert.True(t, codes["ticket:read"])
	assert.True(t, codes["bpmn:read"], "缺失词表码应被补齐")
	assert.True(t, codes["workflow:read"], "workflow:read 应被补齐（P1 缺口修复）")
	assert.True(t, codes["custom:perm"], "词表外自定义权限必须保留")
	assert.False(t, codes["msp:write"], "词表管理范围内的过期授予应被删除")

	// 幂等：复跑 0/0。
	res, err = SyncBuiltinRolePermissionsForTenant(ctx, client, tn.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Created)
	assert.Equal(t, 0, res.Deleted)

	// 角色查询路径不因新增角色而漂移（守卫词表角色命中）。
	roles, err := client.Role.Query().Where(role.TenantIDEQ(tn.ID)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, roles, 1)
}
