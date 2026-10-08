package seeder

import (
	"sort"
	"strings"
	"testing"

	domainrole "itsm-backend/domain/role"
	"itsm-backend/internal/authz"
	"itsm-backend/middleware"
)

// role_code_classification_guard_test.go：角色码归类完整性守卫（IP-P2-7 收口补丁 7 / 设计文档 §5.2 P2）。
//
// 背景：角色可见性/可授予/可写三面由「词表单一源」驱动（middleware.PlatformRoleCodes /
// MSPOnlyRoleCodes + service.hiddenRoleCodesForTenant）。新增角色码若漏登记词表，会被当作
// 「租户本地角色」默认放行（客户侧可见/可授）——扩张面。本守卫扫描角色码定义源并锁四条契约：
//  1. 前缀归类：msp_* / provider_* 前缀的角色码必须已登记 MSP 词表（IsMSPOnlyRole）；
//  2. 平台保留码：super_admin / sysadmin 必须已登记平台词表（IsPlatformRole）；
//  3. 反防腐化：词表内的每个码必须存在于定义源（防「词表挂着已删除角色」）；
//  4. 兜底表键归类：middleware.RolePermissions 的键必须落在
//     domainrole.All ∪ 平台词表 ∪ MSP 词表 ∪ 显式登记的遗留白名单（legacyFallbackRoleKeys）内。
//
// 定义源（五路并集）：domainrole.All（users.role 词表）、middleware.RolePermissions（兜底表）、
// authz.BuiltinRolePermissionCodes()（内置绑定）、authz.MSPCustomerScopeRolePermissionCodes()（客户侧 C2 基线）、
// seeder.BuiltinRoles()（角色种子）。非前缀的岗位/本地角色（it_director、guest 等）按设计默认放行，
// 不在「归类」范围内——本守卫只拦「看起来属于平台/MSP 面但未登记」的码。
//
// 扩展纪律：新增角色码时按《新增角色/权限/路由评审清单》§1 先归类、后登记。

// roleCodeSources 汇总角色码定义源：code → 来源列表。
func roleCodeSources() map[string][]string {
	out := map[string][]string{}
	add := func(source string, codes []string) {
		for _, c := range codes {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			out[c] = append(out[c], source)
		}
	}

	add("domainrole.All", domainrole.All)

	fallback := make([]string, 0, len(middleware.RolePermissions))
	for code := range middleware.RolePermissions {
		fallback = append(fallback, code)
	}
	sort.Strings(fallback)
	add("middleware.RolePermissions", fallback)

	bindings := authz.BuiltinRolePermissionCodes()
	bindingCodes := make([]string, 0, len(bindings))
	for code := range bindings {
		bindingCodes = append(bindingCodes, code)
	}
	sort.Strings(bindingCodes)
	add("authz.BuiltinRolePermissionCodes", bindingCodes)

	c2 := authz.MSPCustomerScopeRolePermissionCodes()
	c2Codes := make([]string, 0, len(c2))
	for code := range c2 {
		c2Codes = append(c2Codes, code)
	}
	sort.Strings(c2Codes)
	add("authz.MSPCustomerScopeRolePermissionCodes", c2Codes)

	seeds := BuiltinRoles()
	seedCodes := make([]string, 0, len(seeds))
	for _, s := range seeds {
		seedCodes = append(seedCodes, s.Code)
	}
	sort.Strings(seedCodes)
	add("seeder.BuiltinRoles", seedCodes)

	return out
}

// legacyFallbackRoleKeys 兜底表（middleware.RolePermissions）中非内置词表、非平台/MSP 词表的
// 历史键白名单：值 = 理由（必填）。条目必须仍在兜底表中存在（反防腐化），否则请删除。
var legacyFallbackRoleKeys = map[string]string{
	// 历史键：domain/role/role.go 头部记录的「三套词汇漂移」遗留（词表口径为 security_admin）；
	// 兜底表保留原键以避免影响存量环境，登记为待收敛项。
	"security": "历史兜底键（词表口径 security_admin），待收敛",
}

func TestRoleCodeClassificationGuard(t *testing.T) {
	sources := roleCodeSources()

	// ① 前缀归类：msp_* / provider_* 必须登记 MSP 词表。
	for code, from := range sources {
		if !strings.HasPrefix(code, "msp_") && !strings.HasPrefix(code, "provider_") {
			continue
		}
		if !middleware.IsMSPOnlyRole(code) {
			t.Errorf("角色码 %q（来源：%s）未登记 MSP 词表（middleware.MSPOnlyRoleCodes）：客户租户侧将按「租户本地角色」默认放行（可见/可授）。请按《扩展评审清单》§1 归类后登记词表", code, strings.Join(from, ", "))
		}
	}

	// ② 平台保留码归类。
	for code := range sources {
		if code == "super_admin" || code == "sysadmin" {
			if !middleware.IsPlatformRole(code) {
				t.Errorf("平台保留角色码 %q 未登记 PlatformRoleCodes", code)
			}
		}
	}

	// ③ 反防腐化：词表条目必须存在于定义源。
	for _, code := range append(middleware.PlatformRoleCodes(), middleware.MSPOnlyRoleCodes()...) {
		if _, ok := sources[code]; !ok {
			t.Errorf("词表角色码 %q 未出现在任何定义源（domainrole.All / RolePermissions / 内置绑定 / C2 基线 / BuiltinRoles）：词表挂着已删除或从未定义的角色", code)
		}
	}

	// ④ 兜底表键归类：必须是内置词表 / 平台词表 / MSP 词表 / 显式遗留白名单之一。
	classified := func(code string) bool {
		for _, c := range domainrole.All {
			if c == code {
				return true
			}
		}
		return middleware.IsPlatformRole(code) || middleware.IsMSPOnlyRole(code)
	}
	for code := range middleware.RolePermissions {
		if classified(code) {
			continue
		}
		if _, ok := legacyFallbackRoleKeys[code]; ok {
			continue
		}
		t.Errorf("兜底表键 %q 未归类：请归入 domainrole.All / 平台词表 / MSP 词表，或登记进 legacyFallbackRoleKeys 并写明理由", code)
	}

	// ④-反防腐化：遗留白名单条目必须仍存在于兜底表且写明理由。
	for code, reason := range legacyFallbackRoleKeys {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("legacyFallbackRoleKeys[%q] 缺少理由", code)
		}
		if _, ok := middleware.RolePermissions[code]; !ok {
			t.Errorf("legacyFallbackRoleKeys[%q] 已不在兜底表中：请删除该白名单条目", code)
		}
	}
}
