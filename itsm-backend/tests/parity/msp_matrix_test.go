// Package parity：IP-P0-9 守卫——MSP 角色矩阵三处单一源（authz 词表 / seeder 角色
// 种子 / middleware 硬编码兜底）一致性，以及 rank 单一词表（07:G3）。
package parity

import (
	"testing"

	"itsm-backend/internal/authz"
	"itsm-backend/middleware"
	"itsm-backend/pkg/seeder"
)

var mspRoleCodes = []string{"msp_viewer", "msp_tech", "msp_specialist", "msp_manager", "msp_admin"}

// TestMSPRoleMatrixParity 断言 authz 词表（剥离 task 基线后）与 middleware 硬编码矩阵全等。
// middleware 的 msp_admin 用 `*` 动作表达全动作，此处展开为 read/write 后比对。
func TestMSPRoleMatrixParity(t *testing.T) {
	bindings := authz.BuiltinRolePermissionCodes()
	for _, role := range mspRoleCodes {
		hardcoded, ok := middleware.RolePermissions[role]
		if !ok {
			t.Fatalf("middleware.RolePermissions 缺少 %s（硬编码兜底矩阵被破坏）", role)
		}
		want := map[string]bool{}
		for _, p := range hardcoded {
			if p.Action == "*" {
				want[p.Resource+":read"] = true
				want[p.Resource+":write"] = true
				continue
			}
			want[p.Resource+":"+p.Action] = true
		}
		delete(want, "task:read")
		delete(want, "task:update")

		got := map[string]bool{}
		for _, c := range bindings[role] {
			if c == "task:read" || c == "task:update" {
				continue
			}
			got[c] = true
		}

		for c := range want {
			if !got[c] {
				t.Errorf("%s：authz 词表缺少 %s（与硬编码矩阵漂移）", role, c)
			}
		}
		for c := range got {
			if !want[c] {
				t.Errorf("%s：authz 词表多出 %s（与硬编码矩阵漂移）", role, c)
			}
		}
	}
}

// TestMSPRolesSeeded 断言五角色同时进入 seeder 角色种子与内置词表（K1/K2 关闭面）。
func TestMSPRolesSeeded(t *testing.T) {
	seedCodes := map[string]bool{}
	for _, r := range seeder.BuiltinRoles() {
		seedCodes[r.Code] = true
	}
	bindings := authz.BuiltinRolePermissionCodes()
	for _, role := range mspRoleCodes {
		if !seedCodes[role] {
			t.Errorf("seeder.BuiltinRoles 缺少 %s：常规 seed 不会创建该角色行", role)
		}
		if _, ok := bindings[role]; !ok {
			t.Errorf("authz 内置词表缺少 %s：权限行无法生成", role)
		}
	}
}

// TestRoleRankSingleSource 断言 rank 单调且覆盖 msp_*（07:G3 最低验收：
// msp_manager ≥ agent，同租户 API 可建 agent 用户）。
func TestRoleRankSingleSource(t *testing.T) {
	seq := []string{"msp_viewer", "msp_tech", "msp_specialist", "msp_manager", "msp_admin", "super_admin"}
	for i := 1; i < len(seq); i++ {
		if middleware.RoleRank(seq[i-1]) > middleware.RoleRank(seq[i]) {
			t.Errorf("rank 非单调：%s(%d) > %s(%d)",
				seq[i-1], middleware.RoleRank(seq[i-1]), seq[i], middleware.RoleRank(seq[i]))
		}
	}
	if middleware.RoleRank("msp_manager") < middleware.RoleRank("agent") {
		t.Errorf("msp_manager rank(%d) < agent rank(%d)：MSP 管理员无法同租户建 agent（07:G3 回归）",
			middleware.RoleRank("msp_manager"), middleware.RoleRank("agent"))
	}
}
