// cmd/backfill_ticket_snapshot：存量工单 MSP 快照幂等回填（IP-P0-3 的跨平台 CLI 版）。
//
// 与 scripts/msp/backfill-ticket-msp-snapshot.sql 判据一致；适用于无法直连 psql 的环境。
//
// 用法（在 itsm-backend 目录，自动读取 .env）：
//
//	backfill_ticket_snapshot                     # dry-run：只报告候选，不写库（默认）
//	backfill_ticket_snapshot -apply              # 全部租户实际执行
//	backfill_ticket_snapshot -tenant-id 11 -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/service"

	"go.uber.org/zap"
)

func main() {
	apply := flag.Bool("apply", false, "apply changes (default: dry-run report only)")
	tenantID := flag.Int("tenant-id", 0, "only process this tenant ID (0 = all)")
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
		"cmd:backfill_ticket_snapshot",
		"backfill legacy ticket MSP snapshots (IP-P0-3)",
	)

	res, err := service.BackfillTicketMSPSnapshots(ctx, client, sugar, *apply, *tenantID)
	if err != nil {
		fail(1, "backfill failed: %v", err)
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	fmt.Printf("mode=%s tenants=%d scanned=%d candidates=%d updated=%d skipped=%d\n",
		mode, res.Tenants, res.Scanned, res.Candidates, res.Updated, res.Skipped)
	if !*apply && res.Candidates > 0 {
		fmt.Println("pass -apply to write the snapshot fields")
	}
}

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
