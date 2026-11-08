// cmd/backfill_sla_snapshots：把存量合同绑定/SLA grant materialize 到
// customer 租户的 sla_definition_snapshots（Q-09 裁决 B）。
//
// 用法（在 itsm-backend 目录，自动读取 .env）：
//
//	backfill_sla_snapshots              # dry-run：只报告待回填数量
//	backfill_sla_snapshots -apply       # 实际写入快照
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
	apply := flag.Bool("apply", false, "apply changes (default: dry-run)")
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
		"cmd:backfill_sla_snapshots",
		"backfill SLA definition snapshots for Q-09 B",
	)

	contracts, err := client.MSPContract.Query().Count(ctx)
	if err != nil {
		fail(1, "count contracts: %v", err)
	}
	grants, err := client.SLADefinitionGrant.Query().Count(ctx)
	if err != nil {
		fail(1, "count grants: %v", err)
	}

	if !*apply {
		fmt.Printf("dry-run contracts=%d grants=%d; pass -apply to write snapshots\n", contracts, grants)
		return
	}
	if err := service.BackfillAllSLASnapshots(ctx, client); err != nil {
		fail(1, "backfill failed: %v", err)
	}
	fmt.Printf("applied contracts=%d grants=%d\n", contracts, grants)
}

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
