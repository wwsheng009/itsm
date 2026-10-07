// cmd/sync_role_permissions：把租户内置角色权限对齐到 internal/authz 词表（P1 workflow:* 缺口修复配套）。
//
// 场景：DBOnly 模式下 role_permissions 是授权唯一真源；启动 seed 仅同步 default 租户，
// 存量租户在词表新增权限码（如 workflow:read/write）后需要显式同步。幂等可重跑。
//
// 用法（在 itsm-backend 目录，自动读取 .env）：
//
//	sync_role_permissions              # dry-run：报告各租户待增/待删数
//	sync_role_permissions -apply       # 实际执行
//	sync_role_permissions -tenant-id 11 -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/pkg/seeder"

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
		"cmd:sync_role_permissions",
		"sync builtin role permissions for tenants",
	)

	dryRun := !*apply
	if *tenantID > 0 {
		res, err := seeder.SyncBuiltinRolePermissionsForTenant(ctx, client, *tenantID, dryRun)
		if err != nil {
			fail(1, "sync tenant %d: %v", *tenantID, err)
		}
		printResult(res)
	} else {
		results, err := seeder.SyncBuiltinRolePermissionsAllTenants(ctx, client, dryRun)
		if err != nil {
			fail(1, "sync tenants: %v", err)
		}
		totalCreated, totalDeleted := 0, 0
		for _, res := range results {
			printResult(res)
			totalCreated += res.Created
			totalDeleted += res.Deleted
		}
		mode := "apply"
		if dryRun {
			mode = "dry-run"
		}
		fmt.Printf("mode=%s tenants=%d would_change=%d/%d\n", mode, len(results), totalCreated, totalDeleted)
		if dryRun && (totalCreated > 0 || totalDeleted > 0) {
			fmt.Println("pass -apply to write the changes")
		}
	}
}

func printResult(res seeder.RolePermissionSyncResult) {
	fmt.Printf("tenant=%d code=%s roles=%d created=%d deleted=%d\n",
		res.TenantID, res.TenantCode, res.Roles, res.Created, res.Deleted)
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
