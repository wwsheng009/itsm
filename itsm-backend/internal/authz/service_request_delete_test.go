package authz

import "testing"

// TestAdminCanDeleteServiceRequest 锁定 D10 产品决策：租户管理员可删除服务请求。
// provider_tenant_admin 由 admin ∪ msp_admin 派生，应自动继承该权限。
func TestAdminCanDeleteServiceRequest(t *testing.T) {
	bindings := BuiltinRolePermissionCodes()
	for _, role := range []string{"admin", "provider_tenant_admin"} {
		found := false
		for _, code := range bindings[role] {
			if code == "service_request:delete" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("role %q missing service_request:delete", role)
		}
	}
}
