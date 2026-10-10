package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"itsm-backend/ent"
	"itsm-backend/ent/user"
)

// TestProvisionMembershipRoles_BootstrapProviderAdminUsesCombinedRole（2026-10-10 回归修复）：
// provider 首管（is_bootstrap_admin）→ 组合角色 provider_tenant_admin + msp_admin，与
// pkg/bootstrap 首管策略（07:G11）单源；普通 provider_admin 员工保持 D10 映射 msp_manager。
// 防止建号/回填/对账（SyncHomeMembership）把首管 membership 校正回纯 MSP 角色，
// 再次触发「自身 ITSM 菜单全被权限过滤」。
func TestProvisionMembershipRoles_BootstrapProviderAdminUsesCombinedRole(t *testing.T) {
	bootstrapAdmin := &ent.User{MspRole: user.MspRoleProviderAdmin, IsBootstrapAdmin: true}
	roleCode, mspRole := provisionMembershipRoles(bootstrapAdmin)
	assert.Equal(t, "provider_tenant_admin", roleCode)
	assert.Equal(t, "msp_admin", mspRole)

	staff := &ent.User{MspRole: user.MspRoleProviderAdmin}
	roleCode, mspRole = provisionMembershipRoles(staff)
	assert.Equal(t, "msp_manager", roleCode)
	assert.Equal(t, "msp_manager", mspRole)

	agent := &ent.User{MspRole: user.MspRoleProviderAgent}
	roleCode, mspRole = provisionMembershipRoles(agent)
	assert.Equal(t, "msp_tech", roleCode)
	assert.Equal(t, "msp_tech", mspRole)

	// 客户账号：按 users.role 解析，msp_role 为空。
	customer := &ent.User{Role: "admin"}
	roleCode, mspRole = provisionMembershipRoles(customer)
	assert.Equal(t, "admin", roleCode)
	assert.Equal(t, "", mspRole)
}
