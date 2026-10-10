package router

import (
	"strings"
	"testing"

	"itsm-backend/middleware"
	"itsm-backend/service"
)

// TestMenuRoleRestrictionGuards ③「非 elevated 角色整段剔除受限前缀」守卫（2026-10-10 收口）。
//
// 词表（elevated 角色）与前缀表（受限路径）都是身份面硬编码，本测试把它们锁成显式契约：
// 任何变更必须同时改这里并写明理由，防止静默扩大/缩小可见面。
//
// 覆盖：
//  1. 受限前缀集合锁定（service.RestrictedMenuPrefixes）；
//  2. 前缀格式（非空、以 / 开头、无尾斜杠）；
//  3. 无死前缀：每个前缀至少命中一条 seeder 菜单路径（路径改名后前缀会静默失效）；
//  4. elevated 词表锁定（middleware.ElevatedMenuRoles，全小写、无重复）；
//  5. 不变式：平台治理角色 ⊆ elevated——平台角色若不在 elevated 内，会连自己租户的管理菜单都看不到。
func TestMenuRoleRestrictionGuards(t *testing.T) {
	// 1) 受限前缀集合锁定（顺序即语义顺序，改动需同步说明）。
	expectedPrefixes := []string{"/admin", "/enterprise", "/system", "/workflow"}
	gotPrefixes := service.RestrictedMenuPrefixes()
	if len(gotPrefixes) != len(expectedPrefixes) {
		t.Fatalf("受限前缀集合变化：got=%v want=%v（新增/删除受限域必须同步本测试并写明理由）",
			gotPrefixes, expectedPrefixes)
	}
	for i, want := range expectedPrefixes {
		if gotPrefixes[i] != want {
			t.Fatalf("受限前缀[%d] 变化：got=%q want=%q（顺序变更请同步本测试）", i, gotPrefixes[i], want)
		}
	}

	// 2) 前缀格式。
	for _, prefix := range gotPrefixes {
		if strings.TrimSpace(prefix) == "" || !strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
			t.Errorf("受限前缀格式非法（需非空、以 / 开头、无尾斜杠）：%q", prefix)
		}
	}

	// 3) 无死前缀：每个受限前缀至少命中一条 seeder 菜单路径。
	//    dormantRestrictedPrefixes：seeder 与存量库均无对应行的防御性前缀——保留（历史路径回潮时
	//    仍不放行），可确认废弃后删除；删除/新增必须同步本表与理由。
	dormantRestrictedPrefixes := map[string]string{
		"/enterprise": "历史企业版路径：seeder 与 12 租户存量库均无 /enterprise* 行（2026-10-10 实测）；保留防回潮",
		"/system":     "历史系统路径：seeder 与 12 租户存量库均无 /system* 行（2026-10-10 实测）；保留防回潮",
	}
	menus := loadSeederMenuPermissions(t)
	for _, prefix := range gotPrefixes {
		if reason, dormant := dormantRestrictedPrefixes[prefix]; dormant {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("dormant 前缀 %q 缺少保留理由", prefix)
			}
			continue
		}
		hit := false
		for path := range menus {
			if strings.HasPrefix(path, prefix) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("受限前缀 %q 未命中任何 seeder 菜单路径（疑似路径已改名/删除，前缀静默失效）", prefix)
		}
	}
	// dormant 表防腐化：登记的必须是当前受限前缀，且确实无 seeder 命中（恢复命中后应移出本表）。
	for prefix := range dormantRestrictedPrefixes {
		found := false
		for _, p := range gotPrefixes {
			if p == prefix {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("dormant 前缀 %q 不在受限前缀集合中，请从本表移除", prefix)
			continue
		}
		for path := range menus {
			if strings.HasPrefix(path, prefix) {
				t.Errorf("dormant 前缀 %q 已恢复 seeder 命中（%s），请从 dormant 表移出", prefix, path)
				break
			}
		}
	}

	// 4) elevated 词表锁定。
	expectedElevated := []string{"super_admin", "sysadmin", "admin", "manager", "provider_tenant_admin"}
	gotElevated := middleware.ElevatedMenuRoles()
	elevatedSet := make(map[string]bool, len(gotElevated))
	for _, code := range gotElevated {
		if code != strings.ToLower(code) || strings.TrimSpace(code) == "" {
			t.Errorf("elevated 词表角色码需为小写非空：%q", code)
		}
		if elevatedSet[code] {
			t.Errorf("elevated 词表存在重复项：%q", code)
		}
		elevatedSet[code] = true
	}
	if len(elevatedSet) != len(expectedElevated) {
		t.Fatalf("elevated 词表变化：got=%v want=%v（新增/删除必须同步本测试并写明理由）",
			gotElevated, expectedElevated)
	}
	for _, want := range expectedElevated {
		if !elevatedSet[want] {
			t.Errorf("elevated 词表缺少 %q（got=%v）", want, gotElevated)
		}
	}

	// 5) 不变式：平台治理角色必须是 elevated 子集。
	for _, platformRole := range middleware.PlatformRoleCodes() {
		if !elevatedSet[platformRole] {
			t.Errorf("不变式破坏：平台角色 %q 不在 elevated 词表（平台用户将看不到管理菜单）", platformRole)
		}
	}
}
