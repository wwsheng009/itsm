//go:build rlsapply
// +build rlsapply

// cmd/rls-apply — RLS 迁移与校验工具（无 psql 环境的替代路径）。
//
// 背景：database/rls/migrations/001_roles.sql 与 002_pilot_policies.sql 以
// psql 方式编写；本工具用管理连接（.env DB_USER，联调库为 superuser）直接
// 执行这些脚本，并提供角色/策略状态校验与 itsm_app 低权探针。
//
// 用法（在 itsm-backend 目录执行，读取 .env / config）：
//
//	go run -tags rlsapply ./cmd/rls-apply -files 001_roles.sql,002_pilot_policies.sql
//	go run -tags rlsapply ./cmd/rls-apply -rollback
//	go run -tags rlsapply ./cmd/rls-apply -set-passwords
//	go run -tags rlsapply ./cmd/rls-apply -verify
//
// 说明：
//   - -files 按序执行 database/rls/migrations 下的脚本；
//   - -set-passwords 读取 DB_APP_ROLE_PASSWORD / DB_ADMIN_ROLE_PASSWORD 并对
//     itsm_app / itsm_admin 执行 ALTER ROLE（001 脚本内的占位密码的正式替换路径）；
//   - -verify 打印角色/策略状态；当 changes 已启用 RLS 时，以 `SET LOCAL ROLE itsm_app`
//     在事务内运行低权探针（有/无 app.current_tenant 的可见性对比），失败返回非零。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"itsm-backend/config"
	"itsm-backend/database"

	_ "github.com/lib/pq"
)

func main() {
	var (
		files    = flag.String("files", "", "要执行的迁移文件名列表（逗号分隔，位于 -dir）")
		rollback = flag.Bool("rollback", false, "按 002→001 顺序执行 *_rollback.sql")
		setPw    = flag.Bool("set-passwords", false, "按 .env 的 DB_APP_ROLE_PASSWORD / DB_ADMIN_ROLE_PASSWORD 设置角色密码")
		verify   = flag.Bool("verify", false, "校验角色/策略状态并运行 itsm_app 探针")
		dir      = flag.String("dir", filepath.Join("database", "rls", "migrations"), "迁移脚本目录")
	)
	flag.Parse()

	if *files == "" && !*rollback && !*setPw && !*verify {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fatalf("load config: %v", err)
	}
	db, err := database.InitDB(&cfg.Database)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if *files != "" {
		for _, name := range strings.Split(*files, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			runFile(ctx, db, filepath.Join(*dir, name))
		}
	}
	if *rollback {
		for _, name := range []string{"002_pilot_policies_rollback.sql", "001_roles_rollback.sql"} {
			runFile(ctx, db, filepath.Join(*dir, name))
		}
	}
	if *setPw {
		setPasswords(ctx, db, &cfg.Database)
	}
	if *verify {
		if !verifyAll(ctx, db) {
			os.Exit(1)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "rls-apply: "+format+"\n", args...)
	os.Exit(1)
}

func runFile(ctx context.Context, db *sql.DB, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	if _, err := db.ExecContext(ctx, string(raw)); err != nil {
		fatalf("exec %s: %v", path, err)
	}
	fmt.Printf("applied: %s (%d bytes)\n", path, len(raw))
}

// setPasswords 将 .env 中的角色密码写入数据库（幂等；未配置则跳过并提示）。
func setPasswords(ctx context.Context, db *sql.DB, cfg *config.DatabaseConfig) {
	pairs := []struct {
		role string
		user string
		pw   string
	}{
		{"itsm_app", cfg.AppRoleUser, cfg.AppRolePassword},
		{"itsm_admin", cfg.AdminRoleUser, cfg.AdminRolePassword},
	}
	for _, p := range pairs {
		if p.user == "" || p.pw == "" {
			fmt.Printf("skip: %s (DB_*_ROLE_USER/PASSWORD 未配置)\n", p.role)
			continue
		}
		if p.user != p.role {
			fmt.Printf("skip: %s (配置的角色名为 %q，与脚本约定不一致)\n", p.role, p.user)
			continue
		}
		// 工具语句不支持扩展协议参数：用 format() 生成带字面量的语句再执行。
		var stmt string
		if err := db.QueryRowContext(ctx,
			"SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', $1::text, $2::text)", p.role, p.pw).Scan(&stmt); err != nil {
			fatalf("build alter role %s: %v", p.role, err)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			fatalf("alter role %s: %v", p.role, err)
		}
		fmt.Printf("password set: %s\n", p.role)
	}
}

func verifyAll(ctx context.Context, db *sql.DB) bool {
	ok := true

	fmt.Println("== roles ==")
	rows, err := db.QueryContext(ctx, `SELECT rolname, rolcanlogin, rolsuper, rolbypassrls
		FROM pg_roles WHERE rolname IN ('itsm_app','itsm_admin', current_user) ORDER BY rolname`)
	if err != nil {
		fatalf("query roles: %v", err)
	}
	for rows.Next() {
		var name string
		var login, super, bypass bool
		if err := rows.Scan(&name, &login, &super, &bypass); err != nil {
			fatalf("scan roles: %v", err)
		}
		fmt.Printf("  %-10s login=%-5v super=%-5v bypassrls=%v\n", name, login, super, bypass)
	}
	rows.Close()

	var grantCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.role_table_grants
		WHERE grantee='itsm_app' AND table_schema='public'`).Scan(&grantCount); err != nil {
		fatalf("count grants: %v", err)
	}
	fmt.Printf("  itsm_app table grants: %d\n", grantCount)
	if grantCount == 0 {
		ok = false
	}

	fmt.Println("== rls state (pilot) ==")
	rows, err = db.QueryContext(ctx, `SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname='public' AND c.relname IN ('changes','vectors') ORDER BY c.relname`)
	if err != nil {
		fatalf("query rls state: %v", err)
	}
	rlsOn := map[string]bool{}
	found := map[string]bool{}
	for rows.Next() {
		var tn string
		var rs, fr bool
		if err := rows.Scan(&tn, &rs, &fr); err != nil {
			fatalf("scan rls state: %v", err)
		}
		found[tn] = true
		rlsOn[tn] = rs && fr
		fmt.Printf("  %-8s rowsecurity=%-5v forcerowsecurity=%v\n", tn, rs, fr)
	}
	rows.Close()
	if !found["changes"] {
		fmt.Println("  changes 表不存在")
		ok = false
	}

	fmt.Println("== policies ==")
	rows, err = db.QueryContext(ctx, `SELECT tablename, policyname, cmd FROM pg_policies
		WHERE schemaname='public' AND tablename IN ('changes','vectors') ORDER BY tablename`)
	if err != nil {
		fatalf("query policies: %v", err)
	}
	polCount := 0
	for rows.Next() {
		var tn, pn, cmd string
		if err := rows.Scan(&tn, &pn, &cmd); err != nil {
			fatalf("scan policies: %v", err)
		}
		polCount++
		fmt.Printf("  %-8s %-18s %s\n", tn, pn, cmd)
	}
	rows.Close()
	if polCount == 0 {
		fmt.Println("  （无策略）")
	}

	fmt.Println("== itsm_app 低权探针（SET LOCAL ROLE）==")
	if !rlsOn["changes"] {
		fmt.Println("  changes 未启用 RLS(FORCE)，跳过探针（先执行 002_pilot_policies.sql）")
		return ok
	}
	var tenantID, total int
	if err := db.QueryRowContext(ctx, `SELECT tenant_id, count(*) FROM changes GROUP BY tenant_id ORDER BY count(*) DESC LIMIT 1`).
		Scan(&tenantID, &total); err != nil {
		fmt.Printf("  changes 无数据（%v），仅做无租户拒绝探针\n", err)
	}
	scopedTid := tenantID
	strict := false
	if total == 0 {
		// 空表无法验证正例：播种两条带标记的探针行（租户 990001/990002），
		// 验证后立即清理；外键无关（tenant_id 为普通整型列）。
		if cleanup, err := seedChangeProbes(ctx, db); err != nil {
			fmt.Printf("  ✗ 播种探针失败：%v\n", err)
			ok = false
		} else {
			defer cleanup()
			scopedTid = 990001
			strict = true
			fmt.Println("  changes 无数据：已播种探针行（RLS-PROBE-*，验证后清理）")
		}
	}
	if scopedTid == 0 {
		scopedTid = 1
	}
	scoped, err := probeChanges(ctx, db, scopedTid)
	if err != nil {
		fmt.Printf("  scoped probe: %v\n", err)
		ok = false
	}
	none, err := probeChanges(ctx, db, 0)
	if err != nil {
		fmt.Printf("  no-scope probe: %v\n", err)
		ok = false
	}
	other, err := probeChanges(ctx, db, 999999)
	if err != nil {
		fmt.Printf("  other probe: %v\n", err)
		ok = false
	}
	fmt.Printf("  changes: total(top tenant %d)=%d | tenant=%d visible=%d | 无租户 visible=%d | tenant=999999 visible=%d\n",
		tenantID, total, scopedTid, scoped, none, other)
	if strict {
		second, err2 := probeChanges(ctx, db, 990002)
		if err2 != nil {
			fmt.Printf("  second probe: %v\n", err2)
			ok = false
		}
		if scoped != 1 || second != 1 {
			fmt.Printf("  ✗ 播种探针可见数异常（tenant 990001=%d, 990002=%d，各应为 1）\n", scoped, second)
			ok = false
		}
	} else if total > 0 && scoped == 0 {
		fmt.Println("  ✗ 已选租户看不到自己的行（策略变量未生效？）")
		ok = false
	}
	if none != 0 || other != 0 {
		fmt.Println("  ✗ 无租户/其他租户仍可见数据（策略未生效）")
		ok = false
	} else {
		fmt.Println("  ✓ 租户隔离生效（无租户与其他租户均 0 行）")
	}
	return ok
}

// seedChangeProbes 为空的 changes 表播种两条探针行，返回清理函数。
// tenant_id/created_by 均为普通整型列（无外键），使用 990001/990002 合成租户。
func seedChangeProbes(ctx context.Context, db *sql.DB) (func(), error) {
	for _, tid := range []int{990001, 990002} {
		_, err := db.ExecContext(ctx, `INSERT INTO changes
			(change_number, title, created_by, tenant_id, created_at, updated_at)
			VALUES ($1, 'RLS probe (auto-seeded by rls-apply)', 1, $2, now(), now())`,
			fmt.Sprintf("RLS-PROBE-%d", tid), tid)
		if err != nil {
			return nil, err
		}
	}
	return func() {
		if _, err := db.ExecContext(context.Background(),
			`DELETE FROM changes WHERE change_number LIKE 'RLS-PROBE-%'`); err != nil {
			fmt.Printf("  ✗ 探针清理失败（请手工删除 RLS-PROBE-% 行）：%v\n", err)
		} else {
			fmt.Println("  探针行已清理")
		}
	}, nil
}

// probeChanges 在事务内切换到 itsm_app 角色并统计 changes 可见行数。
// tenantID<=0 表示不设置 app.current_tenant（验证空值拒绝路径）。
func probeChanges(ctx context.Context, db *sql.DB, tenantID int) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE itsm_app"); err != nil {
		return 0, fmt.Errorf("set local role: %w", err)
	}
	if tenantID > 0 {
		if _, err := tx.ExecContext(ctx, "SELECT set_config('app.current_tenant', $1, true)", fmt.Sprintf("%d", tenantID)); err != nil {
			return 0, fmt.Errorf("set tenant var: %w", err)
		}
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM changes").Scan(&n); err != nil {
		return 0, fmt.Errorf("count changes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
