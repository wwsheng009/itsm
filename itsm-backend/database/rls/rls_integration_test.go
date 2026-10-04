//go:build integration_rls

// Package rls integration tests.
//
// Run with:
//
//	RLS_TEST_DSN='host=<pg_host> port=5432 user=itsm_user dbname=itsm sslmode=disable password=<pwd>' \
//	  go test -tags integration_rls -v ./database/rls/...
//
// Prerequisites:
//   - itsm_app / itsm_admin roles exist (see migrations/001_roles.sql)
//   - `changes` table has rows for at least tenant_id=1
//   - The connecting DB is the SAME one you migrated (see caveat below)
//
// Caveat on host environments:
//
//	If your macOS/Linux host runs a local PostgreSQL (Homebrew, apt) that
//	binds :5432, it may hijack `localhost` connections and route them to
//	the wrong server. Use one of:
//	  - RLS_TEST_DSN with `host=<container_ip>` when Docker network is
//	    reachable from host (Linux, or Docker Desktop w/ direct routes)
//	  - Run the test INSIDE the container (docker exec ... go test ...)
//	  - Stop the local PostgreSQL service and rely on Docker-published :5432
//	  - Or publish the dev container on a non-conflicting host port (e.g.
//	    `55432:5432`) and set RLS_TEST_DSN / RLS_SETUP_DSN to that port.
//
// Two DSNs are required because RLS DDL (ENABLE ROW LEVEL SECURITY, CREATE
// POLICY) needs the table owner / superuser, while the application
// connection uses the RLS-bound `itsm_app` role:
//
//	RLS_TEST_DSN   host=... port=... user=itsm_app   dbname=itsm password=...
//	RLS_SETUP_DSN  host=... port=... user=itsm_user  dbname=itsm password=...  (or any superuser)
//
// The test enables policy at setup and disables at teardown, so running
// it repeatedly is safe. It does NOT mutate business data other than
// its own probes, which are cleaned up.
package rls

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("RLS_TEST_DSN")
	if dsn == "" {
		t.Skip("RLS_TEST_DSN not set, skipping RLS integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	return db
}

// openOwnerDB opens a privileged connection used only for ALTER TABLE /
// CREATE POLICY setup & teardown. RLS-related DDL requires the table owner
// (or superuser), so the application user cannot perform these steps.
func openOwnerDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("RLS_SETUP_DSN")
	if dsn == "" {
		t.Skip("RLS_SETUP_DSN not set, skipping RLS integration test that requires policy setup")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open owner db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping owner db: %v", err)
	}
	return db
}

// setupPolicy enables RLS + policy on `changes`, using the NULLIF-safe form.
// Returns a teardown func that restores the table to no-RLS state.
//
// Uses `RLS_SETUP_DSN` (table-owner / superuser) for DDL because
// `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` requires owner privilege.
// The application connection (`RLS_TEST_DSN`) is only used afterwards.
func setupPolicy(t *testing.T, _ *sql.DB) func() {
	t.Helper()
	owner := openOwnerDB(t)
	defer owner.Close()
	ctx := context.Background()
	stmts := []string{
		`ALTER TABLE changes ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE changes FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON changes`,
		`CREATE POLICY tenant_isolation ON changes
			USING       (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
			WITH CHECK  (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)`,
	}
	for _, s := range stmts {
		if _, err := owner.ExecContext(ctx, s); err != nil {
			t.Fatalf("setup policy (%q): %v", s, err)
		}
	}
	return func() {
		teardown := []string{
			`DROP POLICY IF EXISTS tenant_isolation ON changes`,
			`ALTER TABLE changes NO FORCE ROW LEVEL SECURITY`,
			`ALTER TABLE changes DISABLE ROW LEVEL SECURITY`,
		}
		for _, s := range teardown {
			_, _ = owner.ExecContext(ctx, s)
		}
	}
}

// countChangesAs runs SELECT COUNT(*) as itsm_app role after AcquireConn.
// This is the real end-to-end path: no manual SET, package handles it.
func countChangesAs(t *testing.T, db *sql.DB, ctx context.Context) int {
	t.Helper()
	conn, err := AcquireConn(ctx, db)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer func() {
		if err := ReleaseConn(ctx, conn); err != nil {
			t.Logf("release: %v (non-fatal)", err)
		}
	}()

	// Switch to non-superuser role so policy actually applies.
	if _, err := conn.ExecContext(ctx, "SET ROLE itsm_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}

	var n int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM changes").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestAcquireConn_TenantScopeIsolation verifies that a request-scoped
// AcquireConn correctly limits visible rows to the given tenant.
func TestAcquireConn_TenantScopeIsolation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	teardown := setupPolicy(t, db)
	defer teardown()

	// 联调/生产库的租户 id 不保证从 1 开始：动态选取一个确有 changes 数据的租户
	// 作为正例（owner 连接查询，绕开本用例刚启用的策略）。
	positive := pickTenantWithChanges(t)
	if positive == 0 {
		t.Skip("changes 表无数据，跳过租户可见性正例")
	}
	ctx1 := WithTenant(context.Background(), int64(positive))
	n1 := countChangesAs(t, db, ctx1)
	if n1 == 0 {
		t.Fatalf("tenant %d saw 0 rows; dev DB may be missing seed data", positive)
	}

	// 不存在的租户必须 0 行（若可见即为策略被绕过）。
	ctx999 := WithTenant(context.Background(), int64(999999))
	n999 := countChangesAs(t, db, ctx999)
	if n999 != 0 {
		t.Fatalf("tenant 999999 saw %d rows; expected 0 (RLS bypassed?)", n999)
	}

	t.Logf("tenant=%d visible=%d, tenant=999999 visible=%d ✓", positive, n1, n999)
}

// pickTenantWithChanges 通过 owner（superuser/BYPASSRLS）连接挑选一个
// changes 行数最多的租户 id；无数据时返回 0。
func pickTenantWithChanges(t *testing.T) int {
	t.Helper()
	owner := openOwnerDB(t)
	defer owner.Close()
	var tid int
	err := owner.QueryRowContext(context.Background(),
		`SELECT tenant_id FROM changes GROUP BY tenant_id ORDER BY count(*) DESC LIMIT 1`).Scan(&tid)
	if err != nil {
		return 0
	}
	return tid
}

// TestAcquireConn_NoTenantRejected ensures ErrNoTenant surfaces when
// caller forgets to set tenant scope AND bypass is not asserted.
func TestAcquireConn_NoTenantRejected(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	_, err := AcquireConn(context.Background(), db)
	if err != ErrNoTenant {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
}

// TestAcquireConn_SystemBypassSkipsSet verifies that WithSystemBypass
// grants access without setting the tenant variable. Because BYPASSRLS
// on the connecting role is required for actual data visibility, this
// test only proves the SET is skipped (no error) — real bypass depends
// on connecting as itsm_admin, which is out of scope here.
func TestAcquireConn_SystemBypassSkipsSet(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := WithSystemBypass(context.Background())
	conn, err := AcquireConn(ctx, db)
	if err != nil {
		t.Fatalf("bypass acquire should not error: %v", err)
	}
	if err := ReleaseConn(ctx, conn); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestReleaseConn_DiscardsSessionState verifies that ReleaseConn actually
// wipes the tenant variable, so the next borrower of the pooled connection
// cannot inherit the previous tenant.
func TestReleaseConn_DiscardsSessionState(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	teardown := setupPolicy(t, db)
	defer teardown()

	// Force a tiny pool so we deterministically reuse the same underlying
	// physical connection across two borrows.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// First borrow: tenant=1
	ctx1 := WithTenant(context.Background(), 1)
	conn1, err := AcquireConn(ctx1, db)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	// Read within same borrow to make sure SET took effect
	var tid1 sql.NullString
	if err := conn1.QueryRowContext(ctx1,
		"SELECT current_setting('app.current_tenant', true)").Scan(&tid1); err != nil {
		t.Fatalf("read var: %v", err)
	}
	if tid1.String != "1" {
		t.Fatalf("expected tenant var '1', got %q", tid1.String)
	}
	if err := ReleaseConn(ctx1, conn1); err != nil {
		t.Fatalf("release 1: %v", err)
	}

	// Second borrow: on the SAME physical conn (pool size = 1). If DISCARD
	// ALL worked, the variable must be empty now. We bypass RLS setup
	// deliberately (use db.Conn directly) to inspect raw state.
	conn2, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("raw conn: %v", err)
	}
	defer conn2.Close()

	var tid2 sql.NullString
	if err := conn2.QueryRowContext(context.Background(),
		"SELECT current_setting('app.current_tenant', true)").Scan(&tid2); err != nil {
		t.Fatalf("read var 2: %v", err)
	}
	if tid2.String != "" {
		t.Fatalf("session state leaked across borrows: got %q, want empty", tid2.String)
	}
	t.Logf("session state correctly cleared after ReleaseConn ✓")
}

// ---------------------------------------------------------------------------
// 003 批次 1（工单核心 / 组织与成员 / 工作台）逐表隔离回归
// ---------------------------------------------------------------------------

// batch1Tables 与 database/rls/migrations/003_business_tables_policies.sql 对齐。
var batch1Tables = []string{
	"tickets",
	"ticket_comments",
	"ticket_attachments",
	"ticket_ccs",
	"ticket_workflow_records",
	"user_tenant_memberships",
	"user_tenant_membership_orgs",
	"groups",
	"projects",
	"workbench_views",
}

// ensureTenantHelper 幂等创建 get_current_tenant_id()（与 019 前向修复一致）。
func ensureTenantHelper(t *testing.T) {
	t.Helper()
	owner := openOwnerDB(t) // SETUP DSN 缺失时整体 skip
	defer owner.Close()
	if _, err := owner.ExecContext(context.Background(), `
CREATE OR REPLACE FUNCTION get_current_tenant_id() RETURNS INTEGER AS $$
BEGIN
    RETURN NULLIF(current_setting('app.current_tenant', true), '')::INTEGER;
EXCEPTION
    WHEN invalid_text_representation THEN
        RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE`); err != nil {
		t.Fatalf("ensure get_current_tenant_id: %v", err)
	}
}

// setupTablePolicyFor 启用单表策略，返回 (teardown, 表存在)。
// teardown 仅在“测试前该表未启用 RLS”时真正关闭，避免破坏已落地的 003 状态。
func setupTablePolicyFor(t *testing.T, table string) (func(), bool) {
	t.Helper()
	owner := openOwnerDB(t)
	ctx := context.Background()

	var exists bool
	if err := owner.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil || !exists {
		owner.Close()
		return func() {}, false
	}
	var hadRLS bool
	_ = owner.QueryRowContext(ctx,
		`SELECT relrowsecurity FROM pg_class WHERE relname=$1 AND relnamespace='public'::regnamespace`, table).Scan(&hadRLS)

	policy := "tenant_isolation_" + table
	stmts := []string{
		fmt.Sprintf(`ALTER TABLE %s ENABLE ROW LEVEL SECURITY`, table),
		fmt.Sprintf(`DROP POLICY IF EXISTS %s ON %s`, policy, table),
		fmt.Sprintf(`CREATE POLICY %s ON %s
			USING       (tenant_id = get_current_tenant_id())
			WITH CHECK  (tenant_id = get_current_tenant_id())`, policy, table),
	}
	for _, s := range stmts {
		if _, err := owner.ExecContext(ctx, s); err != nil {
			owner.Close()
			t.Fatalf("setup policy %s (%q): %v", table, s, err)
		}
	}
	return func() {
		defer owner.Close()
		if hadRLS {
			return // 003 已落地：保留策略与启用状态
		}
		for _, s := range []string{
			fmt.Sprintf(`DROP POLICY IF EXISTS %s ON %s`, policy, table),
			fmt.Sprintf(`ALTER TABLE %s NO FORCE ROW LEVEL SECURITY`, table),
			fmt.Sprintf(`ALTER TABLE %s DISABLE ROW LEVEL SECURITY`, table),
		} {
			_, _ = owner.ExecContext(ctx, s)
		}
	}, true
}

// countTableAs 统计 table 在 ctx 租户作用域下的可见行数（真实 AcquireConn 路径）。
func countTableAs(t *testing.T, db *sql.DB, ctx context.Context, table string) int {
	t.Helper()
	conn, err := AcquireConn(ctx, db)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer func() {
		if err := ReleaseConn(ctx, conn); err != nil {
			t.Logf("release: %v (non-fatal)", err)
		}
	}()
	if _, err := conn.ExecContext(ctx, "SET ROLE itsm_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// pickTenantWithData 返回 table 中行数最多的租户；空表返回 0（owner 连接）。
func pickTenantWithData(t *testing.T, table string) int {
	t.Helper()
	owner := openOwnerDB(t)
	defer owner.Close()
	var tid int
	err := owner.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT tenant_id FROM %s GROUP BY tenant_id ORDER BY count(*) DESC LIMIT 1`, table)).Scan(&tid)
	if err != nil {
		return 0
	}
	return tid
}

// TestBatch1_TenantScopeIsolation 逐表验证 003 批次 1 的 DB 级租户隔离：
// 正例（数据最多租户可见）> 0，反例（不存在租户 999999）== 0。
func TestBatch1_TenantScopeIsolation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ensureTenantHelper(t)

	for _, table := range batch1Tables {
		table := table
		t.Run(table, func(t *testing.T) {
			teardown, ok := setupTablePolicyFor(t, table)
			if !ok {
				t.Skipf("%s 不存在，跳过", table)
			}
			defer teardown()

			positive := pickTenantWithData(t, table)
			if positive == 0 {
				// 空表：无法构造正例，仍须验证反例 fail-closed。
				if n := countTableAs(t, db, WithTenant(context.Background(), 999999), table); n != 0 {
					t.Fatalf("%s: tenant 999999 saw %d rows", table, n)
				}
				t.Skipf("%s 无数据：仅验证 fail-closed", table)
			}

			n1 := countTableAs(t, db, WithTenant(context.Background(), int64(positive)), table)
			if n1 == 0 {
				t.Fatalf("%s: tenant %d saw 0 rows", table, positive)
			}
			nOther := countTableAs(t, db, WithTenant(context.Background(), 999999), table)
			if nOther != 0 {
				t.Fatalf("%s: tenant 999999 saw %d rows (RLS bypassed?)", table, nOther)
			}
			t.Logf("%s: tenant=%d visible=%d, tenant=999999 visible=%d ✓", table, positive, n1, nOther)
		})
	}
}
