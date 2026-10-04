package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/user"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ErrAdminExists 该租户已存在 bootstrap 管理员（幂等保护；不覆盖既有账号）。
var ErrAdminExists = errors.New("bootstrap admin already exists for tenant")

// CreateFirstAdmin 创建租户首个管理员（无 token 通道；供 `provision_tenant` 引导，IP-P1-5）。
//
// 与 ConsumeToken 同口径：账号策略 `admin-<tenantCode>`（07:G2）、默认首登强制改密、
// 审计 BOOTSTRAP_ADMIN_CREATED；幂等：租户已有 bootstrap 管理员时返回 ErrAdminExists。
func CreateFirstAdmin(ctx context.Context, client *ent.Client, sugar *zap.SugaredLogger, tenantID int, adminPassword string, opts ...ConsumeOption) (int, error) {
	if client == nil {
		return 0, errors.New("ent client is nil")
	}
	if sugar == nil {
		sugar = zap.NewNop().Sugar()
	}
	if len(adminPassword) < 12 || len(adminPassword) > 128 {
		return 0, errors.New("admin password must be between 12 and 128 characters")
	}
	options := consumeOptions{mustChangePassword: defaultMustChangePassword()}
	for _, opt := range opts {
		opt(&options)
	}
	// 审计来源路径默认保持既有 CLI 口径；HTTP 通道用 WithAuditPath 覆盖。
	auditPath := "cmd:provision_tenant"
	if options.auditPath != "" {
		auditPath = options.auditPath
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	tenantRecord, err := tx.Tenant.Get(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("resolve tenant: %w", err)
	}
	exists, err := tx.User.Query().
		Where(user.IsBootstrapAdminEQ(true), user.TenantIDEQ(tenantID)).
		Exist(ctx)
	if err != nil {
		return 0, fmt.Errorf("check existing bootstrap admin: %w", err)
	}
	if exists {
		return 0, ErrAdminExists
	}

	username, email := bootstrapAdminIdentity(tenantRecord.Code, tenantID)
	if options.username != "" {
		username = options.username
	}
	if options.email != "" {
		email = options.email
	}
	passHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash admin password: %w", err)
	}
	admin, err := tx.User.Create().
		SetUsername(username).
		SetRole("super_admin").
		SetPasswordHash(string(passHash)).
		SetEmail(email).
		SetName("系统管理员").
		SetActive(true).
		SetTenantID(tenantID).
		SetIsBootstrapAdmin(true).
		SetMustChangePassword(options.mustChangePassword).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("create first admin: %w", err)
	}
	if _, err := tx.AuditLog.Create().
		SetTenantID(tenantID).
		SetUserID(admin.ID).
		SetResource("bootstrap_admin").
		SetAction("BOOTSTRAP_ADMIN_CREATED").
		SetPath(auditPath).
		SetMethod("CLI").
		SetStatusCode(200).
		Save(ctx); err != nil {
		return 0, fmt.Errorf("audit first admin creation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	sugar.Infow("first admin created via provisioning channel",
		"user_id", admin.ID, "tenant_id", tenantID, "username", username,
		"must_change_password", options.mustChangePassword)
	return admin.ID, nil
}
