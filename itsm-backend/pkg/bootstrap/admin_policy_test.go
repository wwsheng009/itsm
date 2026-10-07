package bootstrap

import (
	"testing"

	"itsm-backend/ent/usertenantmembership"
)

// TestResolveBootstrapAdminPolicy_ProviderUsesCombinedRole 锁定 provider 首管策略
// （2026-10-06 / 07:G11）：租户内绑定组合角色 provider_tenant_admin（= admin ∪ msp_admin），
// 账号仍为 admin + msp_role=provider_admin，禁止平台通配符。
func TestResolveBootstrapAdminPolicy_ProviderUsesCombinedRole(t *testing.T) {
	p := resolveBootstrapAdminPolicy("demo01", "msp_provider")
	if p.LegacyRole != "admin" || p.UserMSPRole != "provider_admin" {
		t.Errorf("账号策略漂移：legacy=%q msp=%q", p.LegacyRole, p.UserMSPRole)
	}
	if p.RBACRoleCode != "provider_tenant_admin" {
		t.Errorf("provider 首管 RBAC 角色 = %q，want provider_tenant_admin", p.RBACRoleCode)
	}
	if p.MembershipMSPRole != "msp_admin" || !p.Scoped ||
		p.AccountKind != usertenantmembership.AccountKindProvider {
		t.Errorf("membership 策略漂移：mspRole=%q scoped=%v kind=%v", p.MembershipMSPRole, p.Scoped, p.AccountKind)
	}

	// 客户租户保持 admin（不得获得 MSP 矩阵）。
	c := resolveBootstrapAdminPolicy("B2CCUST", "msp_customer")
	if c.RBACRoleCode != "admin" || c.UserMSPRole != "customer_user" {
		t.Errorf("客户租户策略漂移：rbac=%q msp=%q", c.RBACRoleCode, c.UserMSPRole)
	}

	// 平台根租户保持 super_admin 旁路。
	d := resolveBootstrapAdminPolicy("default", "msp_provider")
	if d.LegacyRole != "super_admin" || d.Scoped {
		t.Errorf("default 租户策略漂移：legacy=%q scoped=%v", d.LegacyRole, d.Scoped)
	}
}
