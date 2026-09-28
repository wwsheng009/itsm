package schema_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// M0-03 迁移证据（Postgres 侧）：A0-03 要求「SQLite 与 Postgres 双驱动」。
//
// 本地通常无 Postgres/Docker，故本文件全部用例按环境变量门控：
//
//	MCP_TEST_POSTGRES_DSN='postgres://user:pass@host:5432/db?sslmode=disable' go test ./ent/schema/ -run Postgres -v
//
// 未设置时整组 SKIP（不影响 SQLite 用例）。CI 侧建议以 service container 提供一次性数据库后执行，
// 参见实施方案 §2.3 P3/CI 条目与 docs/plan/evidence/mcp-m1/gap-register-2026-09-27.md（缺口 A1）。
//
// 隔离策略：每个用例在目标库内建独立 schema（`mcp_test_<用例名>`）并通过 `search_path` 连接，
// 用例结束 DROP SCHEMA CASCADE；因此 DSN 指向**可丢弃**的库即可，不要求独占实例。

const mcpPostgresDSNEnv = "MCP_TEST_POSTGRES_DSN"

// mcpPostgresTestDSN 返回绑定到本用例独占 schema 的 DSN；未配置环境变量时 SKIP。
func mcpPostgresTestDSN(t *testing.T) string {
	t.Helper()
	base := os.Getenv(mcpPostgresDSNEnv)
	if base == "" {
		t.Skipf("未设置 %s，跳过 Postgres 迁移用例（A0-03 双驱动待 CI 执行）", mcpPostgresDSNEnv)
	}

	schema := "mcp_test_" + mcpSchemaIdent(t.Name())
	admin := openMCPPostgresDB(t, base)
	_, err := admin.Exec("CREATE SCHEMA IF NOT EXISTS " + schema)
	require.NoError(t, err, "创建测试 schema 失败（DSN 需具备建 schema 权限）")
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE") })

	return mcpDSNWithSearchPath(base, schema)
}

// mcpSchemaIdent 把用例名转成合法的小写 schema 标识符（[a-z0-9_]）。
func mcpSchemaIdent(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	ident := b.String()
	if len(ident) > 60 {
		ident = ident[:60]
	}
	return ident
}

// mcpDSNWithSearchPath 在 URL 或 keyword 两种 DSN 形态上追加 search_path。
func mcpDSNWithSearchPath(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + schema
	}
	return dsn + " search_path=" + schema
}

func openMCPPostgresDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mcpPostgresTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(`SELECT count(*) FROM information_schema.tables
		WHERE table_schema = ANY (current_schemas(false)) AND table_name = $1`, table).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

func mcpPostgresTableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns
		WHERE table_schema = ANY (current_schemas(false)) AND table_name = $1`, table)
	require.NoError(t, err)
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		columns[name] = true
	}
	require.NoError(t, rows.Err())
	return columns
}

// TestMCPSchemaMigrationPostgres_CreatesTablesAndAddsColumns 覆盖 Postgres 侧「空库建表 + 联合加列 + 幂等」。
func TestMCPSchemaMigrationPostgres_CreatesTablesAndAddsColumns(t *testing.T) {
	ctx := context.Background()
	dsn := mcpPostgresTestDSN(t)
	client := enttest.Open(t, "postgres", dsn)
	defer client.Close()
	db := openMCPPostgresDB(t, dsn)

	require.True(t, mcpPostgresTableExists(t, db, "mcp_servers"), "缺少 mcp_servers 表")
	require.True(t, mcpPostgresTableExists(t, db, "mcp_server_tools"), "缺少 mcp_server_tools 表")
	require.True(t, mcpPostgresTableExists(t, db, "tool_invocations"), "缺少 tool_invocations 表")

	columns := mcpPostgresTableColumns(t, db, "tool_invocations")
	wantColumns := []string{
		// MCP 接入字段（M0-03）
		"provider", "mcp_server_name", "mcp_raw_tool_name", "mcp_callable_name",
		"args_redacted", "output_summary", "duration_ms", "error_code",
		// Bot 运行态字段（B0-02，联合迁移一次加列）
		"run_id", "step_id", "risk", "category", "target_type", "target_id", "support_ref",
		"idempotency_key_hash", "expires_at", "verify_state", "verify_note", "attempt_count", "last_error_code",
	}
	for _, column := range wantColumns {
		require.True(t, columns[column], "tool_invocations 缺少列 %s", column)
	}

	// 迁移幂等：同一 schema 再次 Create 不应报错。
	require.NoError(t, client.Schema.Create(ctx))
}

// TestMCPSchemaMigrationPostgres_LegacyRowReadableWithDefaults 覆盖 Postgres 侧「旧数据兼容」。
func TestMCPSchemaMigrationPostgres_LegacyRowReadableWithDefaults(t *testing.T) {
	ctx := context.Background()
	dsn := mcpPostgresTestDSN(t)
	client := enttest.Open(t, "postgres", dsn)
	defer client.Close()
	db := openMCPPostgresDB(t, dsn)

	_, err := db.Exec(`INSERT INTO tool_invocations
		(created_at, tenant_id, tool_name, arguments, status, needs_approval, approval_state,
		 approval_reason, dry_run, permission_check, permission_reason, role_snapshot)
		VALUES (now(), $1, $2, $3, 'success', false, 'none', '', false, 'passed', '', '')`,
		7, "create_ticket", "{}")
	require.NoError(t, err)

	row, err := client.ToolInvocation.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "builtin", row.Provider, "旧行 provider 默认必须为 builtin")
	require.Equal(t, "", row.ErrorCode)
	require.Equal(t, 0, row.DurationMs)
	require.Equal(t, 0, row.AttemptCount)
	require.Equal(t, 0, row.RunID)
	require.Equal(t, "", row.IdempotencyKeyHash)
	require.Equal(t, "", row.ArgsRedacted)
}

// TestMCPSchemaDefaultsPostgres_LeastPrivilege 锁定 Postgres 侧安全默认（D7 默认拒绝）与唯一约束。
func TestMCPSchemaDefaultsPostgres_LeastPrivilege(t *testing.T) {
	ctx := context.Background()
	dsn := mcpPostgresTestDSN(t)
	client := enttest.Open(t, "postgres", dsn)
	defer client.Close()

	server := client.MCPServer.Create().SetTenantID(1).SetName("github").SaveX(ctx)
	require.False(t, server.Enabled, "服务器默认必须关闭")
	require.Equal(t, "untrusted", server.TrustLevel, "默认不可信")
	require.Equal(t, 30000, server.TimeoutMs)

	tool := client.MCPServerTool.Create().
		SetTenantID(1).
		SetServerID(server.ID).
		SetRawName("create_issue").
		SetCallableName("mcp__github__create_issue").
		SaveX(ctx)
	require.False(t, tool.Enabled, "工具管理位默认必须关闭")
	require.False(t, tool.ReadOnly)
	require.Equal(t, "high", tool.Risk, "未标注工具默认按最高风险")

	// 唯一约束：(tenant, name) 重复必须失败（与 SQLite 侧同口径）。
	_, err := client.MCPServer.Create().SetTenantID(1).SetName("github").Save(ctx)
	require.Error(t, err, "同租户同名服务器必须被唯一约束拒绝")

	// 幂等键唯一约束：(tenant, idempotency_key_hash)；未设置（NULL）不互相冲突。
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").SetIdempotencyKeyHash("hash-1").Save(ctx)
	require.NoError(t, err)
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").SetIdempotencyKeyHash("hash-1").Save(ctx)
	require.Error(t, err, "同租户同幂等键必须唯一")
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").Save(ctx)
	require.NoError(t, err)
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").Save(ctx)
	require.NoError(t, err)
}
