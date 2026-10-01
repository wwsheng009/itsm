package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/internal/initialization"
	"itsm-backend/pkg/bootstrap"
	"itsm-backend/pkg/seeder"

	"go.uber.org/zap"
)

func main() {
	action := flag.String("action", "status", "plan|apply|status|verify|retry|generate-bootstrap-token")
	releaseVersion := flag.String("release-version", os.Getenv("ITSM_RELEASE_VERSION"), "release version")
	requestedBy := flag.String("requested-by", "operator", "audited requester identity")
	bootstrapToken := flag.String("bootstrap-token", "", "bootstrap token for first admin creation (used with apply action)")
	tenantIDFlag := flag.Int("tenant-id", 1, "tenant ID for bootstrap token scope")
	tenantCodeFlag := flag.String("tenant-code", "", "tenant code (alternative to -tenant-id; takes precedence)")
	adminUsername := flag.String("admin-username", "", "override first admin username (default admin-<tenantCode>)")
	adminEmail := flag.String("admin-email", "", "override first admin email (default admin-<tenantCode>@bootstrap.local)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		exitf("load config: %v", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		exitf("initialize logger: %v", err)
	}
	defer logger.Sync()
	sugar := logger.Sugar()
	client, err := database.InitDatabaseWithRLS(&cfg.Database, &cfg.RLS, sugar)
	if err != nil {
		exitf("connect database: %v", err)
	}
	defer client.Close()

	store, err := initialization.NewSQLStore(database.GetRawDB())
	if err != nil {
		exitf("create initialization store: %v", err)
	}
	productSeeder := seeder.NewSeeder(client, sugar, cfg)
	components, err := seeder.ProductionInitializers(productSeeder)
	if err != nil {
		exitf("create initializer: %v", err)
	}
	engine, err := initialization.NewEngine(
		store, components, 30*time.Second,
	)
	if err != nil {
		exitf("create engine: %v", err)
	}
	scope := initialization.Scope{Type: "platform", ID: 0}
	ctx := tenantctx.SystemContext(
		context.Background(),
		"initialization:cli",
		"operator requested "+*action,
	)

	switch strings.ToLower(strings.TrimSpace(*action)) {
	case "generate-bootstrap-token":
		// Generate a new bootstrap token for first admin creation.
		targetTenant, err := resolveCLITenant(ctx, client, *tenantIDFlag, *tenantCodeFlag)
		if err != nil {
			exitf("resolve bootstrap tenant: %v", err)
		}
		tokenMgr := bootstrap.NewBootstrapTokenManager(client, sugar)
		rawToken, err := tokenMgr.GenerateToken(ctx, targetTenant.ID)
		if err != nil {
			exitf("generate bootstrap token: %v", err)
		}
		// Output only the raw token (shown once).
		fmt.Printf("=== BOOTSTRAP TOKEN (shown only once) ===\n%s\n=== USE THIS TOKEN TO CREATE FIRST ADMIN ===\n", rawToken)
		writeJSON(map[string]any{
			"token":     rawToken,
			"tenant_id": targetTenant.ID,
			"ttl_hours": "24",
			"usage":     "POST /api/v1/bootstrap/create-admin with {\"token\": \"<token>\", \"password\": \"<admin_password>\"}",
		})

	case "plan":
		plans, err := engine.Plan(ctx, scope)
		if err != nil {
			exitf("plan initialization: %v", err)
		}
		writeJSON(plans)
	case "apply", "retry":
		if strings.TrimSpace(*releaseVersion) == "" {
			exitf("-release-version or ITSM_RELEASE_VERSION is required")
		}

		// If bootstrap token is provided, consume it to create admin.
		if *bootstrapToken != "" {
			tokenMgr := bootstrap.NewBootstrapTokenManager(client, sugar)
			adminPassword := os.Getenv("ADMIN_PASSWORD")
			if adminPassword == "" {
				exitf("ADMIN_PASSWORD env var is required when using bootstrap token")
			}
			// 多租户：显式解析目标租户（-tenant-id / -tenant-code），不再写死 default。
			t, err := resolveCLITenant(ctx, client, *tenantIDFlag, *tenantCodeFlag)
			if err != nil {
				exitf("resolve bootstrap tenant: %v", err)
			}
			userID, err := tokenMgr.ConsumeToken(ctx, *bootstrapToken, t.ID, adminPassword, bootstrap.WithAdminIdentity(*adminUsername, *adminEmail))
			if err != nil {
				exitf("consume bootstrap token: %v", err)
			}
			writeJSON(map[string]any{"user_id": userID, "status": "admin_created_via_bootstrap_token"})
			return
		}

		executorID, _ := os.Hostname()
		executorID, err = initialization.NewExecutorID(executorID)
		if err != nil {
			exitf("create executor id: %v", err)
		}
		runID, err := engine.Apply(ctx, initialization.Request{
			Scope:          scope,
			TargetVersion:  seeder.CurrentTenantTemplateVersion,
			ReleaseVersion: *releaseVersion,
			RequestedBy:    *requestedBy,
			ExecutorID:     executorID,
		})
		if err != nil {
			exitf("initialization run %d failed: %v", runID, err)
		}
		writeJSON(map[string]any{"runId": runID, "status": "succeeded"})
	case "status":
		status, err := store.Status(ctx, scope)
		if err != nil {
			exitf("read initialization status: %v", err)
		}
		writeJSON(status)
	case "verify":
		plans, err := engine.Plan(ctx, scope)
		if err != nil {
			exitf("plan verification: %v", err)
		}
		componentByName := make(map[string]initialization.Initializer, len(components))
		for _, component := range components {
			componentByName[component.Name()] = component
		}
		for _, plan := range plans {
			if err := componentByName[plan.Component].Verify(ctx, scope, plan); err != nil {
				exitf("verify %s: %v", plan.Component, err)
			}
		}
		writeJSON(map[string]any{"status": "verified", "components": len(plans)})
	default:
		exitf("unsupported action %q", *action)
	}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		exitf("encode output: %v", err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// resolveCLITenant 解析 bootstrap 目标租户（IP-P1-5 多租户）：code 优先，其次 id，缺省 1。
func resolveCLITenant(ctx context.Context, client *ent.Client, tenantID int, tenantCode string) (*ent.Tenant, error) {
	if code := strings.TrimSpace(tenantCode); code != "" {
		return client.Tenant.Query().Where(tenant.CodeEQ(code)).Only(ctx)
	}
	if tenantID <= 0 {
		tenantID = 1
	}
	return client.Tenant.Get(ctx, tenantID)
}
