// Command backfill_membership 存量 membership 回填修复器（MSW-FU-1）。
//
// 重放迁移 20260504 回填 1/2 的语义（创建优先、不覆盖异源行），口径与
// 建号/分配写通道完全一致（MembershipSyncService）；修复巡检 missing_membership
// FAIL（M1 之前创建的账号/分配没有 membership 写入路径）。
//
// 用法：
//
//	backfill_membership                     # dry-run（默认，只报告计划，不写库）
//	backfill_membership -apply              # 实际执行（幂等；写 membership.sync 审计）
//	backfill_membership -apply -reconcile   # 同时对既有同源行做幂等校正（修 role_id/kind/status 漂移）
//	backfill_membership -json               # JSON 报告（供 CI/巡检平台消费）
//
// 闭环：执行后运行 `go run ./cmd/membership-drift-check`，期望 FAIL=0（WARN 为观察项）。
// 退出码：Errors>0 → 1；dry-run 存在待创建项不视为失败。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

func main() {
	apply := flag.Bool("apply", false, "实际写库（默认 dry-run 只报告计划）")
	reconcile := flag.Bool("reconcile", false, "对已存在同源行做幂等校正（修 role_id/account_kind/status 漂移）")
	asJSON := flag.Bool("json", false, "输出 JSON 报告")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		fail(1, "load config: %v", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		fail(1, "initialize logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()
	sugar := logger.Sugar()

	client, err := database.InitDatabaseWithRLS(&cfg.Database, &cfg.RLS, sugar)
	if err != nil {
		fail(1, "connect database: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := tenantctx.SystemContext(
		context.Background(),
		"cmd:backfill_membership",
		"backfill legacy memberships (MSW-FU-1)",
	)

	svc := service.NewMembershipSyncService(client, sugar)
	report, err := svc.BackfillMemberships(ctx, service.MembershipAuditMeta{
		ActorAccount: "system",
		Source:       middleware.AuditSourceSystem,
		Trigger:      "membership.backfill",
	}, service.MembershipBackfillOptions{Apply: *apply, Reconcile: *reconcile})
	if err != nil {
		fail(1, "backfill failed: %v", err)
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{
			"generatedAt": time.Now().Format(time.RFC3339),
			"mode":        mode,
			"report":      report,
		}); err != nil {
			fail(1, "encode report: %v", err)
		}
	} else {
		fmt.Printf("membership backfill (MSW-FU-1) — mode=%s reconcile=%v\n", mode, *reconcile)
		fmt.Printf("scope: users=%d active_allocations=%d\n", report.UsersScanned, report.AllocationsScanned)
		fmt.Printf("home:       created=%d skipped=%d reconciled=%d conflicts=%d\n",
			report.HomeCreated, report.HomeSkipped, report.HomeReconciled, report.HomeConflicts)
		fmt.Printf("allocation: created=%d skipped=%d reconciled=%d conflicts=%d\n",
			report.AllocCreated, report.AllocSkipped, report.AllocReconciled, report.AllocConflicts)
		for _, c := range report.Conflicts {
			fmt.Printf("[conflict] %s\n", c)
		}
		for _, e := range report.Errors {
			fmt.Printf("[error] %s\n", e)
		}
		if !*apply {
			fmt.Println("summary: dry-run（未写库）；确认计划后加 -apply 执行")
		} else {
			fmt.Println("summary: 已执行；请运行 go run ./cmd/membership-drift-check 复核 FAIL=0")
		}
	}

	if len(report.Errors) > 0 {
		fail(1, "backfill finished with %d error(s)", len(report.Errors))
	}
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
