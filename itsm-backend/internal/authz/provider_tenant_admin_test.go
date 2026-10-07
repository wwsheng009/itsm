package authz

import "testing"

// TestProviderTenantAdminIsAdminPlusMSPUnion 锁定 provider 首管组合角色口径
// （2026-10-06 / 07:G11）：provider_tenant_admin = admin ∪ msp_admin。
func TestProviderTenantAdminIsAdminPlusMSPUnion(t *testing.T) {
	bindings := BuiltinRolePermissionCodes()
	union, ok := bindings["provider_tenant_admin"]
	if !ok || len(union) == 0 {
		t.Fatalf("provider_tenant_admin 在词表中缺失或为空集")
	}

	got := make(map[string]bool, len(union))
	for _, c := range union {
		got[c] = true
	}
	allowed := make(map[string]bool)
	for _, src := range []string{"admin", "msp_admin"} {
		codes, ok := bindings[src]
		if !ok || len(codes) == 0 {
			t.Fatalf("源角色 %s 缺失或为空集", src)
		}
		for _, c := range codes {
			allowed[c] = true
			if !got[c] {
				t.Errorf("provider_tenant_admin 缺少 %s 的权限码 %s", src, c)
			}
		}
	}
	for _, c := range union {
		if !allowed[c] {
			t.Errorf("provider_tenant_admin 多出非并集权限码 %s", c)
		}
	}

	// 关键能力：自身 ITSM（ticket/incident/sla）+ MSP 写通道（工单/分配）。
	for _, want := range []string{"ticket:read", "incident:read", "sla:read", "msp_ticket:write", "msp_allocation:write"} {
		if !got[want] {
			t.Errorf("provider_tenant_admin 缺少关键权限码 %s", want)
		}
	}
	// bootstrap 策略禁止平台通配符。
	if got["*"] {
		t.Error("provider_tenant_admin 不得包含平台通配符 *")
	}
}
