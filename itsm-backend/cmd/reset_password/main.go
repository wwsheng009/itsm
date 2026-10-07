// cmd/reset_password：租户用户口令恢复 CLI（TUM-D3 修订的服务器侧兜底通道）。
//
// 与 HTTP 窄通道（平台管理面重置首管）互为兜底：
//   - 适用：无法登录平台控制台 / 无 super_admin 会话时的运维恢复；
//   - 默认目标：目标租户的 bootstrap 管理员（admin-<tenantCode>）；-username 可显式指定其他用户；
//   - 动作：按目标租户密码策略校验 → bcrypt 更新 → 会话双吊销（尽力）→ 审计
//     user.admin_password_reset（path=cmd:reset_password，source=system）；
//   - 口令只来自 -password，不回显；-must-change 默认 true（首登强制改密）。
//
// 用法：
//
//	reset_password -tenant-code B2CCUST -password 'NewPassw0rd!'
//	reset_password -tenant-id 8 -username b2ccust_admin -password 'NewPassw0rd!' -must-change=false
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
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	tenantID := flag.Int("tenant-id", 0, "tenant ID")
	tenantCode := flag.String("tenant-code", "", "tenant code (alternative to -tenant-id; takes precedence)")
	username := flag.String("username", "", "target username (default: the tenant's bootstrap admin)")
	password := flag.String("password", "", "new password (validated against the tenant password policy)")
	mustChange := flag.Bool("must-change", true, "force password change on next login")
	revoke := flag.Bool("revoke", true, "revoke existing access+refresh sessions (best effort; shared Redis required for multi-instance)")
	flag.Parse()

	if *tenantID <= 0 && strings.TrimSpace(*tenantCode) == "" {
		fail(2, "either -tenant-id must be positive or -tenant-code must be provided")
	}
	if strings.TrimSpace(*password) == "" {
		fail(2, "-password is required")
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fail(1, "load config: %v", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		fail(1, "initialize logger: %v", err)
	}
	defer logger.Sync()
	sugar := logger.Sugar()

	client, err := database.InitDatabaseWithRLS(&cfg.Database, &cfg.RLS, sugar)
	if err != nil {
		fail(1, "connect database: %v", err)
	}
	defer client.Close()

	ctx := tenantctx.SystemContext(
		context.Background(),
		"bootstrap:reset_password",
		"reset tenant user password via CLI",
	)

	targetTenant, err := resolveTenant(ctx, client, *tenantID, *tenantCode)
	if err != nil {
		sugar.Fatalw("resolve tenant failed", "tenant_id", *tenantID, "tenant_code", *tenantCode, "error", err)
	}
	targetUser, err := resolveUser(ctx, client, targetTenant.ID, *username)
	if err != nil {
		sugar.Fatalw("resolve user failed", "tenant_id", targetTenant.ID, "username", *username, "error", err)
	}

	userSvc := service.NewUserService(client, sugar)
	userSvc.SetSystemConfigService(service.NewSystemConfigService(client, sugar))
	if err := userSvc.PasswordPolicy(ctx, targetTenant.ID).Validate(*password); err != nil {
		sugar.Fatalw("password policy violation", "tenant_id", targetTenant.ID, "error", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		sugar.Fatalw("hash password failed", "error", err)
	}
	if err := client.User.UpdateOneID(targetUser.ID).
		Where(user.TenantIDEQ(targetTenant.ID)).
		SetPasswordHash(string(hash)).
		SetMustChangePassword(*mustChange).
		Exec(ctx); err != nil {
		sugar.Fatalw("update password failed", "user_id", targetUser.ID, "error", err)
	}

	revoked, revokeErrs := revokeSessions(ctx, cfg, targetUser.ID, *revoke, sugar)

	payload, _ := json.Marshal(map[string]any{
		"mode":                 "cli",
		"must_change_password": *mustChange,
		"revoked":              revoked,
		"revocation_errors":    revokeErrs,
	})
	if _, err := client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(targetTenant.ID).
		SetTargetTenantID(targetTenant.ID).
		SetTargetUserID(targetUser.ID).
		SetSource(middleware.AuditSourceSystem).
		SetResource("user").
		SetAction("user.admin_password_reset").
		SetPath("cmd:reset_password").
		SetMethod("CLI").
		SetStatusCode(200).
		SetActorAccount("cli:reset_password").
		SetRequestBody(string(payload)).
		Save(ctx); err != nil {
		sugar.Warnw("audit write failed", "error", err)
	}

	sugar.Infow("tenant user password reset via cli",
		"tenant_id", targetTenant.ID,
		"user_id", targetUser.ID,
		"username", targetUser.Username,
		"must_change_password", *mustChange,
		"revoked", revoked,
		"revocation_errors", revokeErrs,
	)
}

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}

func resolveTenant(ctx context.Context, client *ent.Client, tenantID int, tenantCode string) (*ent.Tenant, error) {
	if code := strings.TrimSpace(tenantCode); code != "" {
		return client.Tenant.Query().Where(tenant.CodeEQ(code)).Only(ctx)
	}
	return client.Tenant.Get(ctx, tenantID)
}

// resolveUser 双键解析：用户名（显式）或租户 bootstrap 管理员（默认）。
func resolveUser(ctx context.Context, client *ent.Client, tenantID int, username string) (*ent.User, error) {
	q := client.User.Query().Where(user.TenantIDEQ(tenantID))
	if name := strings.TrimSpace(username); name != "" {
		return q.Where(user.UsernameEQ(name)).Only(ctx)
	}
	return q.Where(user.IsBootstrapAdminEQ(true)).Only(ctx)
}

// revokeSessions 尽力双吊销：优先接入共享 Redis（运行中实例可见）；
// 不可用时仅记录进程内并告警（单实例内存存储无法跨进程生效，旧会话可能存活到过期）。
func revokeSessions(ctx context.Context, cfg *config.Config, userID int, enabled bool, sugar *zap.SugaredLogger) (bool, []string) {
	if !enabled {
		return false, nil
	}
	var errs []string
	if cfg.Redis.Host != "" {
		rc := redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		defer rc.Close()
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := rc.Ping(pingCtx).Err(); err != nil {
			sugar.Warnw("redis unavailable; revocation will not propagate to the running server", "error", err)
			errs = append(errs, "redis: "+err.Error())
		} else {
			middleware.ConfigureAccessTokenRevocationRedis(rc)
		}
	} else {
		sugar.Warnw("redis not configured; revocation only recorded in this cli process (server sessions may remain valid until expiry)")
		errs = append(errs, "redis: not configured")
	}
	if err := middleware.InvalidateUserAccessTokens(ctx, userID, time.Now()); err != nil {
		errs = append(errs, "access: "+err.Error())
	}
	if err := middleware.InvalidateUserRefreshTokens(ctx, userID, time.Now()); err != nil {
		errs = append(errs, "refresh: "+err.Error())
	}
	return true, errs
}
