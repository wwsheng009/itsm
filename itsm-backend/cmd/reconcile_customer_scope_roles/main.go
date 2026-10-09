// cmd/reconcile_customer_scope_roles：对存量客户租户重跑 C2（客户侧）msp_* 角色基线对账（幂等）。
//
// 场景（IP-P2-14 写门禁口径修复）：工作台跨租户写门禁改为按**目标租户的客户侧业务码**判定
// （工单 reply=ticket:create / status=ticket:update / assign=ticket:assign；问题状态=problem:write）。
// C2 基线新增了 ticket:assign / problem:write，存量客户租户需重跑对账后才能通过写门禁
// （与供给期 seeder 同一实现，幂等；新开通租户自动生效）。
//
// 用法（在 itsm-backend 目录，自动读取 .env）：
//
//	reconcile_customer_scope_roles              # dry-run：仅列出客户类型租户
//	reconcile_customer_scope_roles -apply       # 实际执行（幂等）
//	reconcile_customer_scope_roles -tenant-id 11 -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/seeder"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
)

func main() {
	apply := flag.Bool("apply", false, "apply changes (default: dry-run report only)")
	tenantID := flag.Int("tenant-id", 0, "only process this tenant ID (0 = all customer-type tenants)")
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
		"cmd:reconcile_customer_scope_roles",
		"reconcile C2 customer-scope MSP role baseline",
	)

	targets, err := loadCustomerTenants(ctx, client, *tenantID)
	if err != nil {
		fail(1, "load customer tenants: %v", err)
	}
	if len(targets) == 0 {
		fmt.Println("no customer-type tenants matched")
		return
	}

	processed, failed := 0, 0
	for _, t := range targets {
		if !*apply {
			fmt.Printf("[dry-run] tenant id=%d code=%s name=%s type=%s\n", t.ID, t.Code, t.Name, t.Type)
			continue
		}
		if err := seeder.ReconcileCustomerScopeMSPRoles(ctx, client, t.ID); err != nil {
			failed++
			fmt.Printf("[fail] tenant id=%d code=%s: %v\n", t.ID, t.Code, err)
			continue
		}
		processed++
		fmt.Printf("[ok] tenant id=%d code=%s reconciled\n", t.ID, t.Code)
	}
	if !*apply {
		fmt.Printf("dry-run: %d customer tenant(s); rerun with -apply to reconcile\n", len(targets))
		return
	}
	fmt.Printf("done: %d reconciled, %d failed\n", processed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func loadCustomerTenants(ctx context.Context, client *ent.Client, tenantID int) ([]*ent.Tenant, error) {
	q := client.Tenant.Query()
	if tenantID > 0 {
		q = q.Where(tenant.IDEQ(tenantID))
	}
	rows, err := q.Order(ent.Asc(tenant.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ent.Tenant, 0, len(rows))
	for _, t := range rows {
		if tenantmode.IsCustomerTenantType(string(t.Type)) {
			out = append(out, t)
		}
	}
	return out, nil
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
