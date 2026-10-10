// Package authz M0-10 角色矩阵守卫：mcp:read / mcp:write / mcp:admin 的默认授予面。
//
// 决策依据（实施方案 §4.1 M0-10 + D5/D7 + Q2 拍板；2026-10-10 治理域收口重估）：
//   - 使用与治理分离：read/write 管工具执行，admin 管服务器与工具治理；read/write 不隐含 admin；
//   - 默认拒绝（D7）：新增权限码默认不授予任何角色；
//   - 2026-10-10 设计 §8 D7 重估：MCP 为平台资产——唯一例外为 sysadmin（平台角色）全量持有；
//     admin/provider_tenant_admin（租户角色）零持有（路由已叠加 L1，码亦不可分配/不下发）。
package authz

import (
	"sort"
	"testing"
)

// mcpCodes 是本批次新增的三个码（顺序即矩阵列顺序）。
var mcpCodes = []string{"mcp:read", "mcp:write", "mcp:admin"}

func roleCodeSet(t *testing.T, role string) map[string]bool {
	t.Helper()
	codes, ok := BuiltinRolePermissionCodes()[role]
	if !ok {
		t.Fatalf("角色 %s 不存在于内置角色绑定表（角色表改名需同步本守卫）", role)
	}
	set := make(map[string]bool, len(codes))
	for _, code := range codes {
		set[code] = true
	}
	return set
}

func TestMCPPermissionCodes_Defaults(t *testing.T) {
	// 码空间：三码必须已登记（否则路由永久 403，见 permission_code_catalog_guard_test）。
	catalog := make(map[string]bool)
	for _, code := range AllCodes() {
		catalog[code] = true
	}
	for _, code := range mcpCodes {
		if !catalog[code] {
			t.Errorf("权限码 %s 未登记到 authz.Definitions（码空间权威源）", code)
		}
	}
}

// TestMCPRoleMatrix 锁定「默认授予面」：任何角色变动的沉默扩权都会让本测试红。
func TestMCPRoleMatrix(t *testing.T) {
	want := map[string]map[string]bool{
		// 系统管理员（平台角色）：全量（含写工具执行，仍需 Gate3 审批）。
		"sysadmin": {"mcp:read": true, "mcp:write": true, "mcp:admin": true},
		// 租户管理员：2026-10-10 治理域收口（§8 D7）后零持有（MCP 为平台资产）。
		"admin": {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		// 服务商租户管理员（G11：admin ∪ msp_admin 派生）：随 admin 零持有。
		"provider_tenant_admin": {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		// 其余角色（含总监/经理/坐席/技术员/访客）：默认零授予。
		"it_director":  {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		"ops_director": {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		"ops_manager":  {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		"agent":        {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		"technician":   {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
		"end_user":     {"mcp:read": false, "mcp:write": false, "mcp:admin": false},
	}

	var violations []string
	for role, expect := range want {
		set := roleCodeSet(t, role)
		for _, code := range mcpCodes {
			got := set[code]
			if got != expect[code] {
				violations = append(violations, role+" "+code+": got "+boolText(got)+", want "+boolText(expect[code]))
			}
		}
	}
	sort.Strings(violations) // 稳定性：失败输出按字典序
	if len(violations) > 0 {
		t.Errorf("MCP 角色默认授予面漂移（D7 默认拒绝被破坏）：\n  %s\n\n"+
			"若确需放开：先评审 D7 与 Q2 决议，再同步 internal/authz/roles.go、"+
			"middleware/rbac.go（兜底表）与本矩阵。", joinLines(violations))
	}
}

// TestMCPRoleMatrix_Exhaustive 反向哨兵：除平台角色（sysadmin）外，任何角色都不得持有 mcp 码。
func TestMCPRoleMatrix_Exhaustive(t *testing.T) {
	allowed := map[string]map[string]bool{
		"sysadmin": {"mcp:read": true, "mcp:write": true, "mcp:admin": true},
	}

	codeSet := make(map[string]bool, len(mcpCodes))
	for _, code := range mcpCodes {
		codeSet[code] = true
	}

	var violations []string
	for role, codes := range BuiltinRolePermissionCodes() {
		for _, code := range codes {
			if !codeSet[code] {
				continue
			}
			if !allowed[role][code] {
				violations = append(violations, role+" → "+code)
			}
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Errorf("非白名单角色持有 MCP 权限码（默认拒绝被破坏）：\n  %s", joinLines(violations))
	}
}

// TestMCPRoleMatrix_PlatformOnly 语义守卫（2026-10-10 §8 D7）：mcp 家族为平台资产——
// 仅平台角色（sysadmin）持有；租户角色（admin/provider_tenant_admin）零持有。
// （read/write/admin 三码互不蕴含的事实由 middleware.checkPermissionMatch 的匹配测试与
// 前端 permissionGranted 测试固化，本用例只锁"默认授予面=平台独占"。）
func TestMCPRoleMatrix_PlatformOnly(t *testing.T) {
	for _, role := range []string{"admin", "provider_tenant_admin"} {
		set := roleCodeSet(t, role)
		for _, code := range mcpCodes {
			if set[code] {
				t.Errorf("%s 不应持有 %s：MCP 为平台资产（§8 D7 重估）", role, code)
			}
		}
	}
	sysSet := roleCodeSet(t, "sysadmin")
	for _, code := range mcpCodes {
		if !sysSet[code] {
			t.Errorf("sysadmin 应持 %s（平台治理面）", code)
		}
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func joinLines(lines []string) string {
	out := ""
	for index, line := range lines {
		if index > 0 {
			out += "\n  "
		}
		out += line
	}
	return out
}
