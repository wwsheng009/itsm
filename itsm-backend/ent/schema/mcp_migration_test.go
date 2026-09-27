package schema_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// M0-03 迁移证据：空库建表、列清单、安全默认值、唯一约束与旧行兼容读取。
// 说明：本文件放在 ent/schema 目录但使用外部测试包（schema_test），
// 避免引入整个应用的编译依赖；`ent generate ./schema` 不会读取 _test.go 文件。

// mcpSchemaDSN 为每个用例返回独立的 sqlite 内存库（shared cache，供 ent 与 raw SQL 双连接访问）。
func mcpSchemaDSN(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", "\\", "_").Replace(t.Name())
	return fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name)
}

func openMCPSchemaDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mcpTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	require.NoError(t, err)
	return name == table
}

func mcpTableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
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

// TestMCPSchemaMigration_CreatesTablesAndAddsColumns 覆盖「空库建表」路径：
// 新表存在、tool_invocations 联合字段齐备、迁移可重复执行（幂等）。
func TestMCPSchemaMigration_CreatesTablesAndAddsColumns(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	db := openMCPSchemaDB(t, dsn)

	require.True(t, mcpTableExists(t, db, "mcp_servers"), "缺少 mcp_servers 表")
	require.True(t, mcpTableExists(t, db, "mcp_server_tools"), "缺少 mcp_server_tools 表")
	require.True(t, mcpTableExists(t, db, "tool_invocations"), "缺少 tool_invocations 表")

	columns := mcpTableColumns(t, db, "tool_invocations")
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

// TestMCPSchemaMigration_LegacyRowReadableWithDefaults 覆盖「旧数据兼容」：
// 仅按旧列插入的行在新代码下可读，且新列取默认值（不破坏既有数据）。
func TestMCPSchemaMigration_LegacyRowReadableWithDefaults(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	db := openMCPSchemaDB(t, dsn)

	_, err := db.Exec(`INSERT INTO tool_invocations
		(created_at, tenant_id, tool_name, arguments, status, needs_approval, approval_state,
		 approval_reason, dry_run, permission_check, permission_reason, role_snapshot)
		VALUES (CURRENT_TIMESTAMP, ?, ?, ?, 'success', 0, 'none', '', 0, 'passed', '', '')`,
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

// TestMCPSchemaDefaults_LeastPrivilege 锁定安全默认（D7 默认拒绝）。
func TestMCPSchemaDefaults_LeastPrivilege(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", mcpSchemaDSN(t))
	defer client.Close()

	server := client.MCPServer.Create().SetTenantID(1).SetName("github").SaveX(ctx)
	require.False(t, server.Enabled, "服务器默认必须关闭")
	require.Equal(t, "untrusted", server.TrustLevel, "默认不可信")
	require.Equal(t, "streamable", server.Transport)
	require.Equal(t, "none", server.CredentialType)
	require.Equal(t, "configured", server.Status)
	require.Equal(t, 30000, server.TimeoutMs)
	require.Equal(t, 4, server.MaxParallelCalls)
	require.Equal(t, 1, server.MaxRetry)
	require.Equal(t, 1, server.Version)

	tool := client.MCPServerTool.Create().
		SetTenantID(1).
		SetServerID(server.ID).
		SetRawName("create_issue").
		SetCallableName("mcp__github__create_issue").
		SaveX(ctx)
	require.False(t, tool.Enabled, "工具管理位默认必须关闭")
	require.False(t, tool.Healthy)
	require.False(t, tool.Quarantined)
	require.False(t, tool.ReadOnly)
	require.Equal(t, "high", tool.Risk, "未标注工具默认按最高风险")
}

// TestMCPSchemaMigration_UniqueConstraints 锁定唯一约束：
// 服务器 (tenant,name)、工具 (tenant,server,raw_name) 与 (tenant,callable_name)、幂等键 (tenant,hash)。
func TestMCPSchemaMigration_UniqueConstraints(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", mcpSchemaDSN(t))
	defer client.Close()

	_, err := client.MCPServer.Create().SetTenantID(1).SetName("github").Save(ctx)
	require.NoError(t, err)
	_, err = client.MCPServer.Create().SetTenantID(1).SetName("github").Save(ctx)
	require.Error(t, err, "同租户同名服务器必须唯一")

	server := client.MCPServer.Create().SetTenantID(1).SetName("gitlab").SaveX(ctx)

	_, err = client.MCPServerTool.Create().SetTenantID(1).SetServerID(server.ID).
		SetRawName("list_issues").SetCallableName("mcp__gitlab__list_issues").Save(ctx)
	require.NoError(t, err)
	_, err = client.MCPServerTool.Create().SetTenantID(1).SetServerID(server.ID).
		SetRawName("list_issues").SetCallableName("mcp__gitlab__list_issues_dup").Save(ctx)
	require.Error(t, err, "(tenant, server, raw_name) 必须唯一")
	_, err = client.MCPServerTool.Create().SetTenantID(1).SetServerID(server.ID).
		SetRawName("other").SetCallableName("mcp__gitlab__list_issues").Save(ctx)
	require.Error(t, err, "(tenant, callable_name) 必须唯一")

	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").SetIdempotencyKeyHash("hash-1").Save(ctx)
	require.NoError(t, err)
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").SetIdempotencyKeyHash("hash-1").Save(ctx)
	require.Error(t, err, "同租户同幂等键必须唯一")
	// 未设置幂等键（NULL）不互相冲突：读工具可多次写入。
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").Save(ctx)
	require.NoError(t, err)
	_, err = client.ToolInvocation.Create().SetTenantID(1).SetToolName("t").Save(ctx)
	require.NoError(t, err)
}

// TestMCPSchemaMigration_ServerVersionOptimisticLock 覆盖管理端乐观锁字段可用性。
func TestMCPSchemaMigration_ServerVersionOptimisticLock(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", mcpSchemaDSN(t))
	defer client.Close()

	server := client.MCPServer.Create().SetTenantID(1).SetName("srv").SaveX(ctx)
	require.Equal(t, 1, server.Version)

	updated := client.MCPServer.UpdateOneID(server.ID).SetVersion(2).SaveX(ctx)
	require.Equal(t, 2, updated.Version)
}
