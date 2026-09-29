package bot

import (
	"testing"
)

// B2-05 黑名单规则测试（表驱动，覆盖三面 + 例外 + MCP 豁免 + 判定优先级）。
//
// 口径来源：阶段一报告 §5.8/§412、实施方案 BP6。
func TestBlacklistRule(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		provider string
		resource string
		want     string
	}{
		// ① 管理/权限面
		{"admin 前缀", "admin_list_users", "builtin", "user", "admin_surface"},
		{"permission 前缀", "permission_grant", "builtin", "permission", "admin_surface"},
		{"rbac 前缀", "rbac_set_role", "builtin", "rbac", "admin_surface"},
		{"role 前缀", "role_update", "builtin", "role", "admin_surface"},
		{"tenant 前缀", "tenant_create", "builtin", "tenant", "admin_surface"},
		{"system 前缀", "system_config_set", "builtin", "system", "admin_surface"},
		{"resource=system（名称无害也拦）", "harmless_lookup", "builtin", "system", "admin_surface"},
		{"resource=permission（名称无害也拦）", "lookup", "builtin", "permission", "admin_surface"},

		// ② 删除/破坏类
		{"delete 前缀", "delete_ticket", "builtin", "ticket", "destructive"},
		{"drop 前缀", "drop_table", "builtin", "db", "destructive"},
		{"purge 前缀", "purge_cache", "builtin", "cache", "destructive"},
		{"truncate 前缀", "truncate_logs", "builtin", "log", "destructive"},

		// ③ 任意 IO
		{"http 前缀", "http_request", "builtin", "net", "arbitrary_io"},
		{"fetch 前缀", "fetch_url", "builtin", "net", "arbitrary_io"},
		{"shell 前缀", "shell_exec", "builtin", "host", "arbitrary_io"},
		{"exec 前缀", "exec_command", "builtin", "host", "arbitrary_io"},
		{"sql 前缀", "sql_query", "builtin", "db", "arbitrary_io"},
		{"db 前缀", "db_lookup", "builtin", "db", "arbitrary_io"},
		{"resource=database（名称无害也拦）", "lookup", "builtin", "database", "arbitrary_io"},
		{"resource=filesystem", "read_any", "builtin", "filesystem", "arbitrary_io"},

		// 放行：业务工具（与 B0-01 注册处现状一致）
		{"业务只读", "get_incident_stats", "builtin", "incident", ""},
		{"业务只读 2", "list_tickets", "builtin", "ticket", ""},
		{"业务写", "create_ticket", "builtin", "ticket", ""},
		{"业务写 2", "update_ticket", "builtin", "ticket", ""},
		{"CMDB 关系写", "create_ci_relationship", "builtin", "cmdb", ""},

		// 显式例外：管理员场景（遗留写白名单成员，经 Gate3 审批）
		{"例外：delete_ci_relationship", "delete_ci_relationship", "builtin", "cmdb", ""},

		// MCP 豁免：M1 治理面（服务器/工具开关 + 隔离 + 风险标注）
		{"MCP 前缀豁免", "mcp__github__delete_repo", "mcp", "mcp", ""},
		{"MCP provider 豁免", "shell_exec", "mcp", "net", ""},

		// 大小写/空白容错
		{"大小写与空白", "  DELETE_Ticket ", "builtin", "ticket", "destructive"},

		// 边界：空名称不判定（注册层保证非空；此处不产生误报）
		{"空名称", "", "builtin", "ticket", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BlacklistRule(tc.tool, tc.provider, tc.resource)
			if got != tc.want {
				t.Fatalf("BlacklistRule(%q, %q, %q) = %q, want %q", tc.tool, tc.provider, tc.resource, got, tc.want)
			}
			if IsBlacklisted(tc.tool, tc.provider, tc.resource) != (tc.want != "") {
				t.Fatalf("IsBlacklisted 与 BlacklistRule 不一致：%q", tc.tool)
			}
		})
	}
}

// TestBlacklistRule_RegistryToolsAreNotBlocked 锁定「现行内置工具面零误伤」：
// B0-01 注册的 14 个工具（名称 + resource）必须全部放行，否则黑名单会改变既有下发面。
func TestBlacklistRule_RegistryToolsAreNotBlocked(t *testing.T) {
	registryTools := []struct {
		name     string
		resource string
	}{
		{"get_incident_stats", "incident"},
		{"list_kb", "knowledge"},
		{"list_tickets", "ticket"},
		{"list_cis", "cmdb"},
		{"get_ci_tickets", "cmdb"},
		{"link_ticket_ci", "cmdb"},
		{"create_ticket", "ticket"},
		{"update_ticket", "ticket"},
		{"create_ticket_type", "ticket_type"},
		{"get_ci", "cmdb"},
		{"get_ci_relationships", "cmdb"},
		{"create_ci_relationship", "cmdb"},
		{"delete_ci_relationship", "cmdb"},
		{"get_ci_impact", "cmdb"},
	}
	for _, tool := range registryTools {
		if rule := BlacklistRule(tool.name, "builtin", tool.resource); rule != "" {
			t.Errorf("内置工具 %s(resource=%s) 被黑名单误伤：rule=%s", tool.name, tool.resource, rule)
		}
	}
}

// TestDecide_BlacklistPrecedence 证明黑名单**优先于**授权与兼容默认：
//   - 管理员显式建了授权也不行（严格交集放行 ≠ 黑名单放行）；
//   - 只读工具在兼容默认下本可放行，命中黑名单同样拒绝；
//   - 例外工具（delete_ci_relationship）在授权范围内正常放行（证明例外未被破坏）。
func TestDecide_BlacklistPrecedence(t *testing.T) {
	// ① 严格交集：显式授权 admin 工具（read/read）仍被拒。
	snapshot := strictSnapshot("ga", "act_high", `["chat"]`, grant("admin_list_users", "act_high"))
	decision := Decide(CheckInput{
		Snapshot:   snapshot,
		Tool:       ToolMeta{Name: "admin_list_users", Provider: "builtin", ReadOnly: true, Resource: "user", Action: "read", Risk: "read"},
		Entrypoint: EntrypointChat,
	})
	if decision.Allowed || decision.Reason != ReasonToolBlacklisted+":admin_surface" {
		t.Fatalf("显式授权下的 admin 工具应被黑名单拒绝，got allowed=%v reason=%q", decision.Allowed, decision.Reason)
	}

	// ② 兼容默认（无授权快照）：只读的 http_request 也不放行。
	decision = Decide(CheckInput{
		Tool:       ToolMeta{Name: "http_request", Provider: "builtin", ReadOnly: true, Resource: "net", Action: "read", Risk: "read"},
		Entrypoint: EntrypointChat,
	})
	if decision.Allowed || decision.Reason != ReasonToolBlacklisted+":arbitrary_io" {
		t.Fatalf("兼容默认下的 http 工具应被黑名单拒绝，got allowed=%v reason=%q", decision.Allowed, decision.Reason)
	}

	// ③ 例外工具：授权内（模板上限 act_high，授权 act_high，工具风险 act_high）正常放行。
	snapshot = strictSnapshot("ga", "act_high", `["chat"]`, grant("delete_ci_relationship", "act_high"))
	decision = Decide(CheckInput{
		Snapshot:   snapshot,
		Tool:       ToolMeta{Name: "delete_ci_relationship", Provider: "builtin", ReadOnly: false, Resource: "cmdb", Action: "write", Risk: "act_high"},
		Entrypoint: EntrypointChat,
	})
	if !decision.Allowed {
		t.Fatalf("例外工具 delete_ci_relationship 应在授权内放行，got reason=%q", decision.Reason)
	}
}
