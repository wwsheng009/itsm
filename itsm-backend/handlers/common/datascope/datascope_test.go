package datascope

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entrole "itsm-backend/ent/role"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsDataScopeAllRole 锁定角色→可见范围映射，防止回归导致越权收窄或放宽。
func TestIsDataScopeAllRole(t *testing.T) {
	allRoles := []string{"super_admin", "admin", "manager", "sysadmin"}
	for _, r := range allRoles {
		if !IsDataScopeAllRole(r) {
			t.Errorf("role %q 应为 DataScopeAll（全租户可见）", r)
		}
	}

	// 非管理角色（含空串）必须收窄到 OwnedOrAssigned，安全默认是“收窄而非放宽”。
	notAll := []string{"end_user", "agent", "", "guest", "unknown"}
	for _, r := range notAll {
		if IsDataScopeAllRole(r) {
			t.Errorf("role %q 不应为 DataScopeAll", r)
		}
	}
}

// TestDataScopeConstants 确保两个枚举值互不相同。
func TestDataScopeConstants(t *testing.T) {
	if DataScopeAll == DataScopeOwnedOrAssigned {
		t.Fatal("DataScopeAll 必须与 DataScopeOwnedOrAssigned 不同")
	}
}

// TestResolveForActor_CanonicalFallback 锁定无 DB 时的角色规范值：
// provider 员工（end_user + msp_tech）取最宽 all；原生 end_user 仍为 owner。
func TestResolveForActor_CanonicalFallback(t *testing.T) {
	cases := []struct {
		name        string
		primaryRole string
		roleCodes   []string
		want        DataScope
	}{
		{"super_admin", "super_admin", nil, DataScopeAll},
		{"admin", "admin", nil, DataScopeAll},
		{"end_user", "end_user", nil, DataScopeOwnedOrAssigned},
		{"agent", "agent", nil, DataScopeOwnedOrAssigned},
		{"msp_tech", "msp_tech", nil, DataScopeAll},
		{"provider switch: end_user + msp_tech", "end_user", []string{"msp_tech"}, DataScopeAll},
		{"native customer membership end_user", "end_user", []string{"end_user"}, DataScopeOwnedOrAssigned},
		{"department role", "l1_support", nil, DataScopeDepartment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveForActor(context.Background(), nil, 1, tc.primaryRole, tc.roleCodes)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveForActor_UsesRoleDataScope 锁定 DB 优先 + 内置角色安全收敛：
//   - end_user 旧默认 all 必须收敛为 owner；
//   - msp_tech all 允许 provider 员工看到客户全部单据；
//   - 管理员把 msp_tech 收紧为 owner 时尊重收紧；
//   - 自定义角色以 DB data_scope 为准。
func TestResolveForActor_UsesRoleDataScope(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:datascope_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantEntity, err := client.Tenant.Create().
		SetName("DataScope Customer").
		SetCode("datascope-customer").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	createRole := func(code, scope string, system bool) *ent.Role {
		entity, err := client.Role.Create().
			SetName(code).
			SetCode(code).
			SetTenantID(tenantEntity.ID).
			SetIsSystem(system).
			SetDataScope(entrole.DataScope(scope)).
			Save(ctx)
		require.NoError(t, err)
		return entity
	}

	createRole("end_user", "all", true)
	tech := createRole("msp_tech", "all", true)
	createRole("custom_all", "all", false)
	createRole("custom_owner", "owner", false)

	// 内置 end_user 的旧默认 all 收敛为 owner。
	assert.Equal(t, DataScopeOwnedOrAssigned,
		ResolveForActor(ctx, client, tenantEntity.ID, "end_user", nil))

	// provider 员工：end_user + msp_tech → all。
	assert.Equal(t, DataScopeAll,
		ResolveForActor(ctx, client, tenantEntity.ID, "end_user", []string{"msp_tech"}))

	// 管理员把 msp_tech 收紧为 owner → 尊重收紧。
	_, err = tech.Update().SetDataScope(entrole.DataScopeOwner).Save(ctx)
	require.NoError(t, err)
	assert.Equal(t, DataScopeOwnedOrAssigned,
		ResolveForActor(ctx, client, tenantEntity.ID, "end_user", []string{"msp_tech"}))

	// 自定义角色以 DB 为准（all 不被内置规范收窄）。
	assert.Equal(t, DataScopeAll,
		ResolveForActor(ctx, client, tenantEntity.ID, "custom_all", nil))
	assert.Equal(t, DataScopeOwnedOrAssigned,
		ResolveForActor(ctx, client, tenantEntity.ID, "custom_owner", nil))

	// 平台超管直通。
	assert.Equal(t, DataScopeAll,
		ResolveForActor(ctx, client, tenantEntity.ID, "super_admin", nil))
}

func TestCanWriteResourceWithScope(t *testing.T) {
	assignee := 42
	other := 7

	assert.True(t, CanWriteResourceWithScope(0, DataScopeAll, 999, nil))
	assert.True(t, CanWriteResourceWithScope(other, DataScopeOwnedOrAssigned, other, nil))
	assert.True(t, CanWriteResourceWithScope(assignee, DataScopeOwnedOrAssigned, 999, &assignee))
	assert.False(t, CanWriteResourceWithScope(other, DataScopeOwnedOrAssigned, 999, &assignee))
	assert.False(t, CanWriteResourceWithScope(0, DataScopeOwnedOrAssigned, 999, nil))
}

// TestCanWriteResource_AdminLikeRolesAlwaysAllowed 管理角色全租户可写。
func TestCanWriteResource_AdminLikeRolesAlwaysAllowed(t *testing.T) {
	for _, role := range []string{"super_admin", "admin", "manager", "sysadmin"} {
		if !CanWriteResource(1, role, 999, nil) {
			t.Errorf("role %q 应全租户可写", role)
		}
		if !CanWriteResource(0, role, 999, nil) {
			t.Errorf("role %q 即使无 actorID 仍应可写（管理角色绕过行级 scope）", role)
		}
	}
}

// TestCanWriteResource_OwnerOrAssignee 普通角色仅 owner/受理人可写。
func TestCanWriteResource_OwnerOrAssignee(t *testing.T) {
	assignee := 42
	other := 7

	// owner 命中
	if !CanWriteResource(other, "agent", other, nil) {
		t.Error("owner 应可写")
	}
	// 受理人命中
	if !CanWriteResource(assignee, "agent", 999, &assignee) {
		t.Error("受理人应可写")
	}
	// 均不命中 → 拒绝
	if CanWriteResource(other, "agent", 999, &assignee) {
		t.Error("非 owner 且非受理人必须拒绝")
	}
	// 未分配时仅 owner 可写
	if CanWriteResource(assignee, "agent", 999, nil) {
		t.Error("未分配时非 owner 必须拒绝")
	}
}

// TestCanWriteResource_SafetyNarrowing 安全收窄：无效身份/未知角色按拒绝处理。
func TestCanWriteResource_SafetyNarrowing(t *testing.T) {
	// actorID 无效 → 拒绝（即使 role 是 agent）
	if CanWriteResource(0, "agent", 0, nil) {
		t.Error("actorID=0 必须拒绝（无有效身份）")
	}
	if CanWriteResource(-1, "agent", -1, nil) {
		t.Error("actorID<0 必须拒绝")
	}
	// 未知/空角色 + 非 owner → 拒绝
	if CanWriteResource(5, "unknown_role", 999, nil) {
		t.Error("未知角色非 owner 必须拒绝")
	}
	if CanWriteResource(5, "", 999, nil) {
		t.Error("空角色非 owner 必须拒绝")
	}
	// 受理人指针指向 0 不可越权
	zero := 0
	if CanWriteResource(5, "agent", 999, &zero) {
		t.Error("受理人 ID=0 必须拒绝")
	}
}
