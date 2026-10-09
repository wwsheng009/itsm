package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMSPCustomerScopeRolePermissionCodes 锁定「客户租户内 msp_* 角色基线」（实施计划 §3.0-C2 / Q7）：
// 客户侧业务码、层级关系、无跨租户 C1 码残留，且所有码均在权限目录中。
func TestMSPCustomerScopeRolePermissionCodes(t *testing.T) {
	m := MSPCustomerScopeRolePermissionCodes()
	require.Len(t, m, 5)

	defs := make(map[string]bool)
	for _, d := range Definitions() {
		defs[d.Code] = true
	}
	for role, codes := range m {
		require.NotEmpty(t, codes, role)
		for _, code := range codes {
			require.Truef(t, defs[code], "%s 引用了目录外权限码 %s", role, code)
			require.Falsef(t, strings.HasPrefix(code, "msp"),
				"%s 不应包含跨租户 C1 码 %s（客户租户按 C2 客户侧基线供给）", role, code)
		}
	}

	// 层级：viewer ⊂ tech ⊆ specialist；manager ⊇ tech + user:write/report:read；admin ⊇ manager + cmdb:write/change:write。
	require.Subset(t, m["msp_tech"], m["msp_viewer"])
	require.Subset(t, m["msp_tech"], m["msp_specialist"])
	require.Subset(t, m["msp_manager"], m["msp_tech"])
	require.Subset(t, m["msp_admin"], m["msp_manager"])
	require.Contains(t, m["msp_manager"], "user:write")
	require.Contains(t, m["msp_manager"], "report:read")
	require.Contains(t, m["msp_admin"], "cmdb:write")
	require.Contains(t, m["msp_admin"], "change:write")

	// 工单处理核心动作（客户侧路由实际守卫码，见 router/ticket_routes.go）：
	// 评论/建单 = ticket:create；编辑/状态/解决 = ticket:update。
	for _, code := range []string{"ticket:read", "ticket:create", "ticket:update"} {
		require.Contains(t, m["msp_tech"], code)
	}
	// 写面门禁（IP-P2-14/15；客户侧路由守卫码，见 router/{problem,change,service_request}_routes.go）：
	// 问题/变更/服务请求处理类动作在 tech 及以上开放；审批类码（change:approve / service_request:approve）不进 C2。
	for _, code := range []string{"problem:write", "change:write", "service_request:write"} {
		require.Contains(t, m["msp_tech"], code)
	}
	require.NotContains(t, m["msp_tech"], "change:approve")
	require.NotContains(t, m["msp_tech"], "service_request:approve")
	// 任务面基线与内置词表同口径。
	require.Contains(t, m["msp_tech"], "task:read")
	require.Contains(t, m["msp_tech"], "task:update")

	// 与 C1 分离：provider 词表中的 msp_tech 仍是 C1 跨租户码（回归守护）。
	require.Contains(t, BuiltinRolePermissionCodes()["msp_tech"], "msp_ticket:read")
	require.NotContains(t, m["msp_tech"], "msp_ticket:read")
}
