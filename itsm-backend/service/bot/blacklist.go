package bot

import "strings"

// B2-05 工具黑名单（fail-closed 单点真理源）
//
// 语义：**命中即永不下发、永不可执行**，与授权（bot_tool_grants）、模板风险上限、
// 兼容默认（只读 ∪ 遗留写白名单）**无关**——管理员即使显式建了授权也不能放行。
// 这是阶段一报告 §5.8/§412 与实施方案 BP6 的硬约束：
//
//	Admin API / 权限管理 / 删除类（除 delete_ci_relationship 的管理员场景）/
//	任意 HTTP、shell、DB 工具永不进通用 Bot 工具面。
//
// 为何用「名称前缀 + resource」双通道：内置工具的名称与 resource 都由注册处（B0-01）
// 声明，是可审计的静态元数据；新增工具若取名/挂 resource 命中危险面，默认被拒绝，
// 需要人工在注册处改名或在此登记例外——**默认拒绝**（D7）。
//
// 作用面（两个调用点同源）：
//   - 下发面：handlers/ai ChatStream 装配工具面时过滤（模型看不到）；
//   - 执行面：Decide 第 ⓪ 步（即使被绕过链路直接调用也不可执行）。
//
// MCP 外部工具（`mcp__` 前缀 / provider=mcp）**不在本表判定范围**：它们由 M1 治理
// （服务器默认禁用、工具默认不启用、隔离 quarantine、风险标注），管理员启用即为显式授权。
const (
	// ReasonToolBlacklisted 是黑名单拒绝的稳定原因码（审计 permission_reason 前缀）。
	ReasonToolBlacklisted = "tool_blacklisted"
)

// BlacklistRule 返回命中的规则 id（空 = 未被拦截）。规则 id 稳定，写入审计与测试断言。
//
// 参数取 ToolMeta 的三个静态字段（不读 schema/描述，避免被描述文本绕过）。
func BlacklistRule(name, provider, resource string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return ""
	}
	// M1 治理面：MCP 工具走服务器/工具级开关与隔离，不在此判定。
	if strings.HasPrefix(n, "mcp__") || strings.EqualFold(strings.TrimSpace(provider), "mcp") {
		return ""
	}
	// 显式例外（管理员场景，见文件头）：delete_ci_relationship 属遗留写白名单，
	// 经 Gate3 审批后可用；其余删除类一律拦截。
	if n == "delete_ci_relationship" {
		return ""
	}
	res := strings.ToLower(strings.TrimSpace(resource))

	// ① 管理/权限面：Admin API、权限管理、角色/租户治理、系统配置。
	if hasAnyPrefix(n, "admin_", "permission_", "permissions_", "rbac_", "role_", "roles_", "tenant_", "system_", "auth_", "user_admin") {
		return "admin_surface"
	}
	if inSet(res, "admin", "permission", "permissions", "rbac", "role", "roles", "tenant", "system", "auth") {
		return "admin_surface"
	}

	// ② 删除/破坏类：不可逆操作不进通用 Bot。
	if hasAnyPrefix(n, "delete_", "remove_", "drop_", "purge_", "truncate_", "wipe_") {
		return "destructive"
	}

	// ③ 任意 IO：HTTP 客户端、shell/进程、SQL/DB、文件系统、脚本执行。
	if hasAnyPrefix(n, "http_", "http", "curl_", "fetch_", "shell", "exec_", "spawn_", "cmd_", "process_",
		"sql_", "query_sql", "db_", "sqlite_", "postgres_", "mysql_", "redis_", "script_", "eval_", "file_") {
		return "arbitrary_io"
	}
	if inSet(res, "http", "http_request", "shell", "exec", "command", "process", "sql", "db", "database",
		"script", "eval", "file", "filesystem") {
		return "arbitrary_io"
	}
	return ""
}

// IsBlacklisted 是 BlacklistRule 的布尔形态（调用点更简洁）。
func IsBlacklisted(name, provider, resource string) bool {
	return BlacklistRule(name, provider, resource) != ""
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

func inSet(value string, set ...string) bool {
	for _, item := range set {
		if value == item {
			return true
		}
	}
	return false
}
