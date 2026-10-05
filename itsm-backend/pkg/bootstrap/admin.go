package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/role"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ErrAdminExists 该租户已存在 bootstrap 管理员（幂等保护；不覆盖既有账号）。
var ErrAdminExists = errors.New("bootstrap admin already exists for tenant")

// bootstrapAdminPolicy 首管身份策略（canon C1/D1 + IP-P1-2 权限单源）。
//
//   - 平台根租户（code=default）与 internal/standard 租户：保持 super_admin
//     （私有化单组织语义，私有部署下其即平台管理员）；
//   - msp_provider：租户内 msp_admin + users.msp_role=provider_admin，
//     禁止平台通配符（否则可读全租户目录，跨服务商信息泄露）；
//   - msp_customer / saas_customer：租户内 admin + users.msp_role=customer_user。
//
// Scoped=true 时调用方必须已执行模板供给（roles 表含目标角色），随后写入
// user_roles 边 + home membership（权限解析主路径，fail-closed）。
type bootstrapAdminPolicy struct {
	LegacyRole        string
	UserMSPRole       string
	RBACRoleCode      string
	MembershipMSPRole string
	AccountKind       usertenantmembership.AccountKind
	Scoped            bool
}

func resolveBootstrapAdminPolicy(tenantCode, tenantType string) bootstrapAdminPolicy {
	if tenantCode == "default" {
		return bootstrapAdminPolicy{LegacyRole: "super_admin"}
	}
	switch {
	case tenantmode.IsMSPProviderTenantType(tenantType):
		return bootstrapAdminPolicy{
			LegacyRole:        "admin",
			UserMSPRole:       "provider_admin",
			RBACRoleCode:      "msp_admin",
			MembershipMSPRole: "msp_admin",
			AccountKind:       usertenantmembership.AccountKindProvider,
			Scoped:            true,
		}
	case tenantmode.IsCustomerTenantType(tenantType):
		return bootstrapAdminPolicy{
			LegacyRole:   "admin",
			UserMSPRole:  "customer_user",
			RBACRoleCode: "admin",
			AccountKind:  usertenantmembership.AccountKindCustomer,
			Scoped:       true,
		}
	}
	return bootstrapAdminPolicy{LegacyRole: "super_admin"}
}

// attachScopedBootstrapIdentity 写入租户内 RBAC 角色边 + home membership。
// 角色缺失视为供给未完成（fail-fast，错误信息可直接指导操作者先跑模板供给）。
func attachScopedBootstrapIdentity(ctx context.Context, tx *ent.Tx, tenantID, adminID int, policy bootstrapAdminPolicy, sugar *zap.SugaredLogger) error {
	rbacRole, err := tx.Role.Query().
		Where(role.TenantIDEQ(tenantID), role.CodeEQ(policy.RBACRoleCode)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("resolve bootstrap RBAC role %q for tenant %d (run tenant template provisioning first): %w",
			policy.RBACRoleCode, tenantID, err)
	}
	if _, err := tx.User.UpdateOneID(adminID).AddRoleIDs(rbacRole.ID).Save(ctx); err != nil {
		return fmt.Errorf("attach bootstrap RBAC role: %w", err)
	}
	builder := tx.UserTenantMembership.Create().
		SetUserID(adminID).
		SetTenantID(tenantID).
		SetAccountKind(policy.AccountKind).
		SetSource(usertenantmembership.SourceHome).
		SetRoleID(rbacRole.ID).
		SetIsDefault(true).
		SetStatus(usertenantmembership.StatusActive)
	if policy.MembershipMSPRole != "" {
		builder = builder.SetMspRole(policy.MembershipMSPRole)
	}
	if _, err := builder.Save(ctx); err != nil {
		return fmt.Errorf("create bootstrap home membership: %w", err)
	}
	sugar.Infow("bootstrap admin scoped identity applied",
		"tenant_id", tenantID, "rbac_role", policy.RBACRoleCode, "msp_role", policy.UserMSPRole)
	return nil
}

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
	policy := resolveBootstrapAdminPolicy(tenantRecord.Code, string(tenantRecord.Type))
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
	createBuilder := tx.User.Create().
		SetUsername(username).
		SetRole(user.Role(policy.LegacyRole)).
		SetPasswordHash(string(passHash)).
		SetEmail(email).
		SetName("系统管理员").
		SetActive(true).
		SetTenantID(tenantID).
		SetIsBootstrapAdmin(true).
		SetMustChangePassword(options.mustChangePassword)
	if policy.UserMSPRole != "" {
		createBuilder = createBuilder.SetMspRole(user.MspRole(policy.UserMSPRole))
	}
	admin, err := createBuilder.Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("create first admin: %w", err)
	}
	if policy.Scoped {
		if err := attachScopedBootstrapIdentity(ctx, tx, tenantID, admin.ID, policy, sugar); err != nil {
			return 0, err
		}
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
