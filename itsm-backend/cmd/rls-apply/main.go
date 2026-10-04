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

// rlsManagedTables — -verify 的受管表清单（与 database/rls/migrations 002/003/004/005 对齐）。
// 002 试点表在前、003 批次 1、004 批次 2、005 批次 3 依次在后；存在的表必须已启用 RLS 且有策略，否则 -verify 非零退出。
var rlsManagedTables = []string{
	"changes", "vectors", // 002 试点
	"tickets", "ticket_comments", "ticket_attachments", "ticket_ccs", "ticket_workflow_records", // 003 工单核心
	"user_tenant_memberships", "user_tenant_membership_orgs", // 003 成员
	"groups", "projects", "workbench_views", // 003 组织/工作台
	"notifications", "notification_deliveries", "notification_preferences", "ticket_notifications", // 004 通知
	"sla_definitions", "sla_metrics", "sla_violations", "sla_alert_histories", // 004 SLA
	"knowledge_articles", "knowledge_article_likes", // 004 知识库
	"service_requests", "service_request_approvals", "service_catalog_items", // 004 服务请求
	"invitations",                  // 004 邀请
	"incidents", "incident_alerts", // 004 事件
	"ticket_types", "ticket_templates", // 004 工单配置
	"conversations", "messages", // 005 AI 会话
	"mcp_servers", "mcp_server_tools", "tool_invocations", // 005 AI 工具/MCP
	"connector_configs", "connector_inbound_dedups", // 005 连接器
	"email_conversations", "email_intake_analyses", "email_outbound_messages", // 005 邮件
	"inbound_email_messages", "feishu_ticket_syncs", // 005 邮件/飞书
	"domain_configs", "provisioning_tasks", // 005 域名/供给
	// 006 批次 4 流程/工作流/审批引擎
	"process_instances", "process_tasks", "process_audit_logs", "process_variables",
	"process_timers", "process_version_changelogs", "process_approval_decisions",
	"process_execution_histories",
	"workflow_instances", "workflow_tasks", "workflow_templates", "workflow_versions",
	"workflows",
	"bpmn_permissions",
	// 007 批次 5 鉴权/菜单/配置/审计
	"role_permissions", "permissions", "menus", "system_configs",
	"audit_logs", "endpoint_acls",
	// 008 批次 6 自动化/机器人与运维命令
	"bot_runs", "bot_steps", "bot_events", "bot_artifacts",
	"bot_templates", "bot_tool_grants",
	"ai_analysis_results", "ai_feedbacks", "llm_user_preferences",
	"operational_commands",
	// 009 批次 7 长尾业务域
	"configuration_items", "configuration_item_histories", "ci_attribute_definitions",
	"ci_relationships", "ci_tags", "ci_types", "applications", "microservices",
	"cloud_accounts", "cloud_resources", "cloud_services",
	"discovery_jobs", "discovery_results", "discovery_sources",
	"cmdb_export_tasks", "cmdb_import_tasks", "cmdb_identity_migration_conflicts",
	"cmdb_saved_views", "assets", "asset_licenses",
	"change_approval_chains", "change_approvals", "change_implementation_plans",
	"change_pi_rs", "change_risk_assessments", "change_rollback_executions",
	"change_rollback_plans", "standard_changes", "cab_members",
	"approval_workflows", "approval_chains", "approval_records", "ticket_approvals",
	"incident_rules", "incident_escalation_rules", "incident_rule_executions",
	"incident_metrics", "incident_events",
	"problems", "root_cause_analyses", "known_errors", "releases",
	"teams", "departments", "customer_branches", "source_organizations",
	"relationship_types", "service_customers", "contracts", "support_contracts",
	"external_contract_references", "vendors", "on_call_schedules", "on_call_shifts",
	"engineer_skills",
	"ticket_categories", "ticket_assignment_rules", "ticket_automation_rules",
	"ticket_views", "ticket_tags", "tags",
	"surveys", "survey_responses",
	"sla_policies", "sla_alert_rules",
	"service_catalogs",
	"process_definitions", "process_deployments", "process_bindings",
	"llm_provider_configs",
	"attachments",
	"alerts", "ai_analysis_result", "endpoint_ac_ls",
	// 010 批次 8 平台面收口
	"bootstrap_tokens", "tenant_installations",
}

// rlsProbeTables — 逐表低权探针清单（003 批次 1 + 004 批次 2 + 005 批次 3；changes 走带播种的特殊探针）。
var rlsProbeTables = []string{
	"tickets", "ticket_comments", "ticket_attachments", "ticket_ccs", "ticket_workflow_records",
	"user_tenant_memberships", "user_tenant_membership_orgs", "groups", "projects", "workbench_views",
	"notifications", "notification_deliveries", "notification_preferences", "ticket_notifications",
	"sla_definitions", "sla_metrics", "sla_violations", "sla_alert_histories",
	"knowledge_articles", "knowledge_article_likes",
	"service_requests", "service_request_approvals", "service_catalog_items",
	"invitations", "incidents", "incident_alerts",
	"ticket_types", "ticket_templates",
	"conversations", "messages",
	"mcp_servers", "mcp_server_tools", "tool_invocations",
	"connector_configs", "connector_inbound_dedups",
	"email_conversations", "email_intake_analyses", "email_outbound_messages",
	"inbound_email_messages", "feishu_ticket_syncs",
	"domain_configs", "provisioning_tasks",
	"process_instances", "process_tasks", "process_audit_logs", "process_variables",
	"process_timers", "process_version_changelogs", "process_approval_decisions",
	"process_execution_histories",
	"workflow_instances", "workflow_tasks", "workflow_templates", "workflow_versions",
	"workflows",
	"bpmn_permissions",
	"role_permissions", "permissions", "menus", "system_configs",
	"audit_logs", "endpoint_acls",
	"bot_runs", "bot_steps", "bot_events", "bot_artifacts",
	"bot_templates", "bot_tool_grants",
	"ai_analysis_results", "ai_feedbacks", "llm_user_preferences",
	"operational_commands",
	"configuration_items", "change_risk_assessments", "approval_workflows",
	"incident_events", "problems", "teams", "ticket_categories", "tags",
	"sla_policies", "process_definitions", "llm_provider_configs", "attachments",
	"bootstrap_tokens", "tenant_installations",
}

func main() {
	var (
		files    = flag.String("files", "", "要执行的迁移文件名列表（逗号分隔，位于 -dir）")
		rollback = flag.Bool("rollback", false, "按 002→001 顺序执行 *_rollback.sql")
		setPw    = flag.Bool("set-passwords", false, "按 .env 的 DB_APP_ROLE_PASSWORD / DB_ADMIN_ROLE_PASSWORD 设置角色密码")
		verify   = flag.Bool("verify", false, "校验角色/策略状态并运行 itsm_app 探针")
		query    = flag.String("query", "", "只读诊断：执行单条 SELECT 并输出结果（制表符分隔）")
		dir      = flag.String("dir", filepath.Join("database", "rls", "migrations"), "迁移脚本目录")
	)
	flag.Parse()

	if *files == "" && !*rollback && !*setPw && !*verify && *query == "" {
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
		for _, name := range []string{"005_ai_connector_tables_policies_rollback.sql", "004_lifecycle_tables_policies_rollback.sql", "003_business_tables_policies_rollback.sql", "002_pilot_policies_rollback.sql", "001_roles_rollback.sql"} {
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
	if *query != "" {
		runQuery(ctx, db, *query)
	}
}

// runQuery 执行单条只读 SELECT（诊断用途；拒绝非 SELECT 语句）。
func runQuery(ctx context.Context, db *sql.DB, q string) {
	trimmed := strings.TrimSpace(strings.ToUpper(q))
	if !strings.HasPrefix(trimmed, "SELECT") && !strings.HasPrefix(trimmed, "WITH") {
		fatalf("-query 仅允许 SELECT/WITH（只读诊断）")
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		fatalf("query: %v", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		fatalf("columns: %v", err)
	}
	fmt.Println(strings.Join(cols, "\t"))
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			fatalf("scan: %v", err)
		}
		out := make([]string, len(cols))
		for i, v := range vals {
			switch tv := v.(type) {
			case nil:
				out[i] = "NULL"
			case []byte:
				out[i] = string(tv)
			default:
				out[i] = fmt.Sprintf("%v", tv)
			}
		}
		fmt.Println(strings.Join(out, "\t"))
	}
	if err := rows.Err(); err != nil {
		fatalf("rows: %v", err)
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

	fmt.Println("== rls state (managed) ==")
	rows, err = db.QueryContext(ctx, `SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname='public' AND c.relkind='r'`)
	if err != nil {
		fatalf("query rls state: %v", err)
	}
	type rlsState struct{ enabled, forced bool }
	state := map[string]rlsState{}
	for rows.Next() {
		var tn string
		var rs, fr bool
		if err := rows.Scan(&tn, &rs, &fr); err != nil {
			fatalf("scan rls state: %v", err)
		}
		state[tn] = rlsState{enabled: rs, forced: fr}
	}
	rows.Close()

	rlsOn := map[string]bool{}
	found := map[string]bool{}
	for _, tn := range rlsManagedTables {
		st, exists := state[tn]
		if !exists {
			fmt.Printf("  %-26s （表不存在，跳过）\n", tn)
			continue
		}
		found[tn] = true
		rlsOn[tn] = st.enabled
		mark := "✓"
		if !st.enabled {
			mark = "✗ 未启用 RLS（执行 003/002 迁移）"
			ok = false
		}
		fmt.Printf("  %-26s rowsecurity=%-5v forcerowsecurity=%-5v %s\n", tn, st.enabled, st.forced, mark)
	}
	if !found["changes"] {
		fmt.Println("  changes 表不存在")
		ok = false
	}

	fmt.Println("== policies (managed) ==")
	rows, err = db.QueryContext(ctx, `SELECT tablename, policyname, cmd FROM pg_policies
		WHERE schemaname='public' ORDER BY tablename, policyname`)
	if err != nil {
		fatalf("query policies: %v", err)
	}
	managed := map[string]bool{}
	for _, tn := range rlsManagedTables {
		managed[tn] = true
	}
	polSeen := map[string]int{}
	polCount := 0
	for rows.Next() {
		var tn, pn, cmd string
		if err := rows.Scan(&tn, &pn, &cmd); err != nil {
			fatalf("scan policies: %v", err)
		}
		if !managed[tn] {
			continue
		}
		polCount++
		polSeen[tn]++
		fmt.Printf("  %-26s %-34s %s\n", tn, pn, cmd)
	}
	rows.Close()
	if polCount == 0 {
		fmt.Println("  （无策略）")
	}
	for _, tn := range rlsManagedTables {
		if found[tn] && rlsOn[tn] && polSeen[tn] == 0 {
			fmt.Printf("  ✗ %s 已启用 RLS 但无策略（默认拒绝，业务会 fail-closed）\n", tn)
			ok = false
		}
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
	scoped, err := probeTable(ctx, db, "changes", scopedTid)
	if err != nil {
		fmt.Printf("  scoped probe: %v\n", err)
		ok = false
	}
	none, err := probeTable(ctx, db, "changes", 0)
	if err != nil {
		fmt.Printf("  no-scope probe: %v\n", err)
		ok = false
	}
	other, err := probeTable(ctx, db, "changes", 999999)
	if err != nil {
		fmt.Printf("  other probe: %v\n", err)
		ok = false
	}
	fmt.Printf("  changes: total(top tenant %d)=%d | tenant=%d visible=%d | 无租户 visible=%d | tenant=999999 visible=%d\n",
		tenantID, total, scopedTid, scoped, none, other)
	if strict {
		second, err2 := probeTable(ctx, db, "changes", 990002)
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

	// ---- 003/004/005 批次 1+2+3：逐表低权探针（正例=数据最多租户；反例=无租户/不存在租户） ----
	fmt.Println("== batch-1/2/3 低权探针（逐表） ==")
	for _, tn := range rlsProbeTables {
		if !rlsOn[tn] {
			fmt.Printf("  %-26s RLS 未启用，跳过探针\n", tn)
			continue
		}
		var tid, total int
		err := db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT tenant_id, count(*) FROM %s WHERE tenant_id IS NOT NULL GROUP BY tenant_id ORDER BY count(*) DESC LIMIT 1`, tn)).
			Scan(&tid, &total)
		if err != nil {
			// 空表：无法构造正例，仍验证「无租户」fail-closed。
			none, nerr := probeTable(ctx, db, tn, 0)
			if nerr != nil {
				fmt.Printf("  %-26s no-scope probe 异常：%v\n", tn, nerr)
				ok = false
				continue
			}
			if none != 0 {
				fmt.Printf("  ✗ %-24s （空表）无租户仍可见 %d 行\n", tn, none)
				ok = false
			} else {
				fmt.Printf("  ✓ %-24s （空表）无租户 visible=0\n", tn)
			}
			continue
		}
		scoped, e1 := probeTable(ctx, db, tn, tid)
		none, e2 := probeTable(ctx, db, tn, 0)
		other, e3 := probeTable(ctx, db, tn, 999999)
		if e1 != nil || e2 != nil || e3 != nil {
			fmt.Printf("  ✗ %-24s 探针异常：scoped=%v none=%v other=%v\n", tn, e1, e2, e3)
			ok = false
			continue
		}
		bad := scoped == 0 || none != 0 || other != 0
		mark := "✓"
		if bad {
			mark = "✗"
			ok = false
		}
		fmt.Printf("  %s %-24s total(tenant %d)=%d | scoped=%d | none=%d | other=%d\n",
			mark, tn, tid, total, scoped, none, other)
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
			fmt.Printf("  ✗ 探针清理失败（请手工删除 RLS-PROBE-<tid> 行）：%v\n", err)
		} else {
			fmt.Println("  探针行已清理")
		}
	}, nil
}

// probeTable 在事务内切换到 itsm_app 角色并统计目标表可见行数。
// table 只允许来自本文件内的受管白名单（rlsProbeTables / changes），不接受外部输入。
// tenantID<=0 表示不设置 app.current_tenant（验证空值拒绝路径）。
func probeTable(ctx context.Context, db *sql.DB, table string, tenantID int) (int, error) {
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
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
