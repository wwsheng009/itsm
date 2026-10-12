package middleware

import "testing"

// F7 回归（2026-10-12）：JWT 承载角色的 07:G3 提级规则必须在 Login/Refresh/Switch 共用。
// 浏览器实测：prov.agent1（users.role=end_user + msp_role=provider_agent）refresh 后
// 角色掉回 end_user → /api/v1/msp/workbench/* 403（预检 RBAC 用 token role）。
func TestEffectiveTokenRole(t *testing.T) {
	msp := func(s string) string { return s }
	cases := []struct {
		name    string
		role    string
		mspRole string
		want    string
	}{
		{"provider_agent 提级为 msp_tech", "end_user", msp("provider_agent"), "msp_tech"},
		{"provider_admin 提级为 msp_manager", "end_user", msp("provider_admin"), "msp_manager"},
		{"主角色更高时保持 admin", "admin", msp("provider_admin"), "admin"},
		{"主角色更高时保持 super_admin", "super_admin", msp("provider_agent"), "super_admin"},
		{"无 MSP 角色保持主角色", "end_user", "", "end_user"},
		{"customer_user 不改变 end_user", "end_user", "customer_user", "end_user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveTokenRole(tc.role, tc.mspRole); got != tc.want {
				t.Fatalf("EffectiveTokenRole(%q, %q) = %q, want %q", tc.role, tc.mspRole, got, tc.want)
			}
		})
	}
}
