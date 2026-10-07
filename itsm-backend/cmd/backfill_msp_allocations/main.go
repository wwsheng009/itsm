// cmd/backfill_msp_allocations：为存量 provider 首管补齐 primary allocation（首体验修复）。
//
// 场景：provider 首管在"默认分配"落地前创建、或客户在落地前开通 → 工作台空白。
// 本 CLI 幂等遍历 provider（或单个 -provider-id），调用与运行时钩子相同的
// service.EnsureBootstrapAllocationsForProvider（dry-run 默认，只报告待创建数）。
//
// 用法（在 itsm-backend 目录，自动读取 .env）：
//
//	backfill_msp_allocations                # dry-run：全部 provider
//	backfill_msp_allocations -apply         # 实际执行
//	backfill_msp_allocations -provider-id 10 -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/service"

	"go.uber.org/zap"
)

func main() {
	apply := flag.Bool("apply", false, "apply changes (default: dry-run report only)")
	providerID := flag.Int("provider-id", 0, "only process this provider tenant ID (0 = all)")
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
		"cmd:backfill_msp_allocations",
		"backfill provider bootstrap admin allocations",
	)

	providerIDs := []int{*providerID}
	if *providerID <= 0 {
		rows, qErr := client.Tenant.Query().Where(
			tenant.TypeIn(
				tenant.Type(tenantmode.TenantTypeMSPProvider),
				tenant.Type(tenantmode.TenantTypeLegacyMSP),
			),
		).Select(tenant.FieldID).All(ctx)
		if qErr != nil {
			fail(1, "list provider tenants: %v", qErr)
		}
		providerIDs = providerIDs[:0]
		for _, r := range rows {
			providerIDs = append(providerIDs, r.ID)
		}
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	total := 0
	for _, pid := range providerIDs {
		n, runErr := service.EnsureBootstrapAllocationsForProvider(ctx, client, sugar, *apply, pid)
		if runErr != nil {
			fail(1, "provider %d: %v", pid, runErr)
		}
		if n > 0 {
			fmt.Printf("provider=%d would_create=%d\n", pid, n)
		}
		total += n
	}
	fmt.Printf("mode=%s providers=%d allocations=%d\n", mode, len(providerIDs), total)
	if !*apply && total > 0 {
		fmt.Println("pass -apply to create the allocations")
	}
}

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
