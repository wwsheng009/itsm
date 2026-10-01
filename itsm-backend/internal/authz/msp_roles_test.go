package authz

import "testing"

// mspC1Matrix 权威矩阵：实施方案 §3.0-C1（等价于 middleware/rbac.go 硬编码
// msp_* 段）。此处不含 task:read/update 基线——该基线由 BuiltinRolePermissionCodes
// 统一派生，测试先剥离再比对。
var mspC1Matrix = map[string][]string{
	"msp_viewer": {
		"msp:read", "msp_customer:read", "msp_ticket:read", "msp_allocation:read", "msp_report:read",
	},
	"msp_tech": {
		"msp:read", "msp_customer:read", "msp_ticket:read", "msp_ticket:write",
		"msp_allocation:read", "msp_report:read",
	},
	"msp_specialist": {
		"msp:read", "msp_customer:read", "msp_customer:write", "msp_ticket:read", "msp_ticket:write",
		"msp_allocation:read", "msp_report:read",
	},
	"msp_manager": {
		"msp:read", "msp:write", "msp_customer:read", "msp_customer:write",
		"msp_ticket:read", "msp_ticket:write", "msp_allocation:read", "msp_allocation:write",
		"msp_report:read", "msp_report:write",
	},
	"msp_admin": {
		"msp:read", "msp:write", "msp_customer:read", "msp_customer:write",
		"msp_ticket:read", "msp_ticket:write", "msp_allocation:read", "msp_allocation:write",
		"msp_report:read", "msp_report:write",
	},
}

func mspCodeSet(codes []string) map[string]bool {
	set := make(map[string]bool, len(codes))
	for _, c := range codes {
		if c == "task:read" || c == "task:update" {
			continue // 任务面基线，C1 矩阵不含
		}
		set[c] = true
	}
	return set
}

// TestMSPRoles_C1Matrix 锁定 IP-P0-9/K1：五角色入内置词表且权限集与 C1 全等。
func TestMSPRoles_C1Matrix(t *testing.T) {
	bindings := BuiltinRolePermissionCodes()
	for role, want := range mspC1Matrix {
		got, ok := bindings[role]
		if !ok {
			t.Fatalf("内置词表缺少 %s（K1 未关闭：常规 seed 不会生成其权限行）", role)
		}
		gotSet, wantSet := mspCodeSet(got), mspCodeSet(want)
		for c := range wantSet {
			if !gotSet[c] {
				t.Errorf("%s 缺少权限码 %s（C1 矩阵）", role, c)
			}
		}
		for c := range gotSet {
			if !wantSet[c] {
				t.Errorf("%s 多出权限码 %s（C1 矩阵外，需评审）", role, c)
			}
		}
	}
}
