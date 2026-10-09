package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 工作台写门禁码 ↔ C2 基线一致性守卫（IP-P2-14；IP-P2-15 扩展变更/服务请求）。
//
// 背景：工作台跨租户写按**目标租户的客户侧业务码**判定（canon I2/I5：权限在目标租户内计算；
// 与深切换/客户侧路由守卫同码）：
//   - 工单 reply  = ticket:create（router/ticket_routes.go 评论路由守卫）
//   - 工单 status = ticket:update（状态路由守卫）
//   - 工单 assign = ticket:assign（指派路由守卫）
//   - 问题 status = problem:write（router/problem_routes.go 问题写路由守卫）
//   - 变更 status = change:write（router/change_routes.go schedule/start/complete/close/cancel 守卫）
//   - 服务请求 status = service_request:write（router/service_request_routes.go 状态路由守卫）
//
// 本守卫锁定：C2 基线必须让 msp_tech 及以上具备上述全部码（否则 DB-only RBAC 下写门禁
// fail-closed——曾导致工单/问题写面全部拒绝）；viewer 不得获得写码；客户租户 msp_* 角色
// 不得回填跨租户 msp_* 权限码（词表所有权；写门禁不再依赖 msp_ticket:write）。
func TestCustomerScopeBaselineCoversWorkbenchWriteGates(t *testing.T) {
	c2 := MSPCustomerScopeRolePermissionCodes()
	required := []string{
		"ticket:create", "ticket:update", "ticket:assign", "problem:write",
		"change:write", "service_request:write",
	}

	for _, role := range []string{"msp_tech", "msp_specialist", "msp_manager", "msp_admin"} {
		for _, code := range required {
			assert.Contains(t, c2[role], code, "%s 必须含工作台写门禁码 %s", role, code)
		}
	}
	for _, code := range []string{"ticket:update", "ticket:assign", "problem:write", "change:write", "service_request:write"} {
		assert.NotContains(t, c2["msp_viewer"], code, "viewer 不得含写码 %s", code)
	}
	assert.Contains(t, c2["msp_viewer"], "ticket:create", "viewer 保留评论能力（客户侧评论路由守卫）")
	assert.NotContains(t, c2["msp_viewer"], "change:approve", "审批类码不进 C2 写面")
	assert.NotContains(t, c2["msp_tech"], "change:approve", "审批类动作保留客户治理面（D8）")

	for role, codes := range c2 {
		for _, code := range codes {
			assert.False(t, strings.HasPrefix(code, "msp_"), "%s 不应含跨租户权限码 %s", role, code)
		}
	}
}
