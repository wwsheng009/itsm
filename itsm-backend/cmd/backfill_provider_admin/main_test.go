package main

import "testing"

// TestIsRebindableMSPRole 锁定重绑面（2026-10-10 扩展 msp_manager 变体）：
// 纯 MSP 专属角色可重绑到 provider_tenant_admin；目标角色自身与非 MSP 角色不可。
func TestIsRebindableMSPRole(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"msp_admin", true},
		{"msp_manager", true},
		{"msp_tech", true},
		{"msp_specialist", true},
		{"msp_viewer", true},
		{"provider_tenant_admin", false}, // 目标角色自身：由 alreadyDone 分支处理
		{"admin", false},
		{"agent", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isRebindableMSPRole(tc.code); got != tc.want {
			t.Errorf("isRebindableMSPRole(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
}
