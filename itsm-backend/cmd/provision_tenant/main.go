package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/bootstrap"
	"itsm-backend/pkg/seeder"

	"go.uber.org/zap"
)

func main() {
	tenantID := flag.Int("tenant-id", 0, "existing tenant ID to provision")
	tenantCode := flag.String("tenant-code", "", "existing tenant code (alternative to -tenant-id; takes precedence)")
	createAdmin := flag.Bool("create-admin", false, "create the tenant's first admin (admin-<tenantCode>, must change password on first login)")
	adminPassword := flag.String("admin-password", os.Getenv("ADMIN_PASSWORD"), "first admin password (or ADMIN_PASSWORD env; required with -create-admin)")
	adminUsername := flag.String("admin-username", "", "override first admin username (default admin-<tenantCode>)")
	adminEmail := flag.String("admin-email", "", "override first admin email (default admin-<tenantCode>@bootstrap.local)")
	templateVersion := flag.String(
		"template-version",
		seeder.CurrentTenantTemplateVersion,
		"product tenant template version",
	)
	flag.Parse()
	if *tenantID <= 0 && strings.TrimSpace(*tenantCode) == "" {
		fmt.Fprintln(os.Stderr, "either -tenant-id must be positive or -tenant-code must be provided")
		os.Exit(2)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()
	sugar := logger.Sugar()

	client, err := database.InitDatabaseWithRLS(&cfg.Database, &cfg.RLS, sugar)
	if err != nil {
		sugar.Fatalw("connect database", "error", err)
	}
	defer client.Close()

	ctx := tenantctx.SystemContext(
		context.Background(),
		"bootstrap:provision_tenant",
		fmt.Sprintf("install product template %s for tenant %d", *templateVersion, *tenantID),
	)
	targetID := *tenantID
	if code := strings.TrimSpace(*tenantCode); code != "" {
		t, err := client.Tenant.Query().Where(tenant.CodeEQ(code)).Only(ctx)
		if err != nil {
			sugar.Fatalw("resolve tenant by code failed", "tenant_code", code, "error", err)
		}
		targetID = t.ID
	}
	provisioner := seeder.NewSeeder(client, sugar, cfg)
	if err := provisioner.ProvisionTenant(ctx, targetID, *templateVersion); err != nil {
		sugar.Fatalw("tenant provisioning failed", "tenant_id", targetID, "error", err)
	}
	sugar.Infow("tenant provisioning completed", "tenant_id", targetID, "template_version", *templateVersion)

	if *createAdmin {
		if strings.TrimSpace(*adminPassword) == "" {
			sugar.Fatalw("first admin password required", "hint", "set -admin-password or ADMIN_PASSWORD")
		}
		adminID, err := bootstrap.CreateFirstAdmin(ctx, client, sugar, targetID, *adminPassword,
			bootstrap.WithAdminIdentity(*adminUsername, *adminEmail))
		if err != nil {
			sugar.Fatalw("create first admin failed", "tenant_id", targetID, "error", err)
		}
		sugar.Infow("first admin created", "tenant_id", targetID, "user_id", adminID)
	}
}
