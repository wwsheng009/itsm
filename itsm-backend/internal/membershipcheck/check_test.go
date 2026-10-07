package membershipcheck

import "testing"

func ptr(v int) *int { return &v }

// scenarioSeedFacts 对应 scenarios 种子集（03/04/05/06）的最小投影：
//   - 平台管理员（super_admin，platform 作用域）
//   - provider 管理员（home provider 作用域 + 1 条客户分配及对应 membership）
//   - 客户管理员 / 客户员工（customer 单作用域）
//
// 期望：0 漂移（MSW-A6 判据）。
func scenarioSeedFacts() Facts {
	return Facts{
		Tenants: []TenantFact{
			{ID: 1, Type: "platform"},
			{ID: 10, Type: "msp_provider"},
			{ID: 11, Type: "msp_customer"},
		},
		Roles: []RoleFact{
			{ID: 1, TenantID: 1, Code: "super_admin"},
			{ID: 10, TenantID: 10, Code: "provider_admin"},
			{ID: 11, TenantID: 11, Code: "admin"},
			{ID: 12, TenantID: 11, Code: "end_user"},
		},
		Users: []UserFact{
			{ID: 1, Username: "admin", Active: true, Role: "super_admin", TenantID: 1},
			{ID: 2, Username: "provider-admin", Active: true, Role: "admin", MSPRole: "provider_admin", TenantID: 10},
			{ID: 3, Username: "customer-admin", Active: true, Role: "admin", TenantID: 11},
			{ID: 4, Username: "customer-user", Active: true, Role: "end_user", TenantID: 11},
		},
		Memberships: []MembershipFact{
			{ID: 101, UserID: 1, TenantID: 1, AccountKind: "platform", Source: "home", RoleID: ptr(1), Status: "active", IsDefault: true},
			{ID: 102, UserID: 2, TenantID: 10, AccountKind: "provider", Source: "home", RoleID: ptr(10), MSPRole: "msp_manager", Status: "active", IsDefault: true},
			{ID: 103, UserID: 2, TenantID: 11, AccountKind: "provider", Source: "allocation", RoleID: ptr(10), MSPRole: "msp_tech", Status: "active"},
			{ID: 104, UserID: 3, TenantID: 11, AccountKind: "customer", Source: "home", RoleID: ptr(11), Status: "active", IsDefault: true},
			{ID: 105, UserID: 4, TenantID: 11, AccountKind: "customer", Source: "home", RoleID: ptr(12), Status: "active", IsDefault: true},
		},
		Allocations: []AllocationFact{
			{MSPUserID: 2, CustomerTenantID: 11, ProviderTenantID: 10},
		},
	}
}

func TestCheck_ScenarioSeed_ZeroDrift(t *testing.T) {
	got := Check(scenarioSeedFacts())
	if len(got) != 0 {
		t.Fatalf("种子夹具期望 0 漂移，实际 %d 条: %+v", len(got), got)
	}
}

// TestCheck_ConstructedDrift_Classified 人工构造 5 类漂移 + 客户单作用域冲突，
// 断言分类与级别（MSW-A6）。
func TestCheck_ConstructedDrift_Classified(t *testing.T) {
	f := scenarioSeedFacts()

	// 1) missing_membership：活跃客户账号无任何 membership。
	f.Users = append(f.Users, UserFact{ID: 90, Username: "no-membership", Active: true, Role: "end_user", TenantID: 11})
	// 2) missing_membership：provider 有活跃分配但缺客户作用域 membership。
	f.Users = append(f.Users, UserFact{ID: 91, Username: "provider-no-alloc-member", Active: true, Role: "agent", MSPRole: "provider_agent", TenantID: 10})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 190, UserID: 91, TenantID: 10, AccountKind: "provider", Source: "home", RoleID: ptr(10), MSPRole: "msp_tech", Status: "active", IsDefault: true})
	f.Allocations = append(f.Allocations, AllocationFact{MSPUserID: 91, CustomerTenantID: 11, ProviderTenantID: 10})

	// 3) role_id_null：活跃 membership 无角色。
	f.Users = append(f.Users, UserFact{ID: 92, Username: "null-role", Active: true, Role: "end_user", TenantID: 11})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 191, UserID: 92, TenantID: 11, AccountKind: "customer", Source: "home", Status: "active", IsDefault: true})

	// 4) status_mismatch ×3：停用账号仍有活跃 membership；allocation 来源无活跃分配；软删仍 active。
	f.Users = append(f.Users, UserFact{ID: 93, Username: "disabled-with-member", Active: false, Role: "agent", MSPRole: "provider_agent", TenantID: 10})
	f.Memberships = append(f.Memberships,
		MembershipFact{ID: 192, UserID: 93, TenantID: 10, AccountKind: "provider", Source: "home", RoleID: ptr(10), MSPRole: "msp_tech", Status: "active", IsDefault: true},
		MembershipFact{ID: 193, UserID: 93, TenantID: 12, AccountKind: "provider", Source: "allocation", RoleID: ptr(10), MSPRole: "msp_tech", Status: "active"},
		MembershipFact{ID: 194, UserID: 93, TenantID: 13, AccountKind: "provider", Source: "allocation", RoleID: ptr(10), MSPRole: "msp_tech", Status: "active", DeletedAt: true},
	)

	// 5) customer_scope_conflict：客户账号两条活跃 membership。
	f.Users = append(f.Users, UserFact{ID: 94, Username: "double-scope", Active: true, Role: "admin", TenantID: 11})
	f.Memberships = append(f.Memberships,
		MembershipFact{ID: 195, UserID: 94, TenantID: 11, AccountKind: "customer", Source: "home", RoleID: ptr(11), Status: "active", IsDefault: true},
		MembershipFact{ID: 196, UserID: 94, TenantID: 12, AccountKind: "customer", Source: "invite", RoleID: ptr(11), Status: "active"},
	)

	// 6) account_kind_snapshot：快照与账号类型不一致。
	f.Users = append(f.Users, UserFact{ID: 95, Username: "stale-kind", Active: true, Role: "end_user", TenantID: 11})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 197, UserID: 95, TenantID: 11, AccountKind: "provider", Source: "home", RoleID: ptr(12), Status: "active", IsDefault: true})

	// 7) jwt_role_mismatch：客户 membership 角色码 vs users.role 不一致。
	f.Users = append(f.Users, UserFact{ID: 96, Username: "role-drift", Active: true, Role: "admin", TenantID: 11})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 198, UserID: 96, TenantID: 11, AccountKind: "customer", Source: "home", RoleID: ptr(12), Status: "active", IsDefault: true})

	// 8) jwt_role_mismatch：provider membership.msp_role vs users.msp_role 不一致。
	f.Users = append(f.Users, UserFact{ID: 97, Username: "msp-role-drift", Active: true, Role: "agent", MSPRole: "provider_agent", TenantID: 10})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 199, UserID: 97, TenantID: 10, AccountKind: "provider", Source: "home", RoleID: ptr(10), MSPRole: "provider_admin", Status: "active", IsDefault: true})

	got := Check(f)
	counts := map[string]int{}
	sev := map[string]Severity{}
	for _, g := range got {
		counts[g.Category]++
		sev[g.Category] = g.Severity
	}
	wantCounts := map[string]int{
		"missing_membership":      2,
		"role_id_null":            1,
		"status_mismatch":         3,
		"customer_scope_conflict": 1,
		"account_kind_snapshot":   1,
		"jwt_role_mismatch":       2,
	}
	for cat, want := range wantCounts {
		if counts[cat] != want {
			t.Errorf("分类 %s 期望 %d 条，实际 %d（全部: %+v）", cat, want, counts[cat], got)
		}
	}
	if len(counts) != len(wantCounts) {
		t.Errorf("出现了预期外的分类：%v（全部: %+v）", counts, got)
	}
	for _, cat := range []string{"missing_membership", "role_id_null", "status_mismatch", "customer_scope_conflict"} {
		if sev[cat] != SeverityFail {
			t.Errorf("分类 %s 期望 FAIL，实际 %s", cat, sev[cat])
		}
	}
	for _, cat := range []string{"account_kind_snapshot", "jwt_role_mismatch"} {
		if sev[cat] != SeverityWarn {
			t.Errorf("分类 %s 期望 WARN，实际 %s", cat, sev[cat])
		}
	}

	fail, warn := Summary(got)
	if fail != 7 || warn != 3 {
		t.Errorf("Summary 期望 fail=7 warn=3，实际 fail=%d warn=%d", fail, warn)
	}
}

// TestCheck_SuperAdminPlatformScope 平台账号（platform 作用域）不触发角色码/类型告警；
// 覆盖 super_admin 覆盖规则：即便 home 租户类型被标为 msp_provider（存量数据异常），
// 平台根账号仍按 platform 口径判定。
func TestCheck_SuperAdminPlatformScope(t *testing.T) {
	f := scenarioSeedFacts()
	f.Tenants[0].Type = "msp_provider"
	f.Users = append(f.Users, UserFact{ID: 98, Username: "ops", Active: true, Role: "super_admin", TenantID: 1})
	f.Memberships = append(f.Memberships, MembershipFact{ID: 200, UserID: 98, TenantID: 1, AccountKind: "platform", Source: "platform", RoleID: ptr(1), Status: "active", IsDefault: true})
	if got := Check(f); len(got) != 0 {
		t.Fatalf("平台账号期望 0 漂移，实际: %+v", got)
	}
}
