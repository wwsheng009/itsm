package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/passwordresettoken"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/service"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	client         *ent.Client
	jwtSecret      string
	logger         *zap.SugaredLogger
	tokenBlacklist *service.TokenBlacklistService
	emailService   *service.EmailService
	baseURL        string
	// configService 提供租户生效的密码策略（system_configs 驱动）。
	configService *service.SystemConfigService
}

func NewService(client *ent.Client, jwtSecret string, logger *zap.SugaredLogger, tokenBlacklist *service.TokenBlacklistService) *Service {
	return &Service{client: client, jwtSecret: jwtSecret, logger: logger, tokenBlacklist: tokenBlacklist, baseURL: "http://localhost:3000"}
}

func (s *Service) SetEmailService(emailService *service.EmailService) { s.emailService = emailService }
func (s *Service) SetBaseURL(baseURL string)                          { s.baseURL = baseURL }

// SetSystemConfigService 注入系统配置服务，使注册/找回密码也遵循配置页的密码策略。
func (s *Service) SetSystemConfigService(configService *service.SystemConfigService) {
	s.configService = configService
}

// passwordPolicy 返回租户当前生效的密码策略；未注入配置服务时回退默认策略。
func (s *Service) passwordPolicy(ctx context.Context, tenantID int) service.PasswordPolicy {
	if s == nil || s.configService == nil {
		return service.DefaultPasswordPolicy()
	}
	return s.configService.GetPasswordPolicy(ctx, tenantID)
}

// PasswordPolicy 解析并返回租户当前生效的密码策略（公开端点用，无需登录）。
// 未指定 tenantCode 时与注册一致：仅当系统只有一个启用租户时才按其策略解析，
// 多租户场景返回默认策略，避免把任意租户的配置暴露给未指定租户的调用方。
func (s *Service) PasswordPolicy(ctx context.Context, tenantCode string) (*dto.PasswordPolicyResponse, error) {
	// R2B 阴影观察（2026-10-03）：公开端点（未登录）——租户解析为平台范围查询。
	ctx = tenantctx.SystemContext(ctx, "auth:password-policy", "pre-auth tenant resolution (public endpoint)")
	tenantID := 0
	if code := strings.TrimSpace(tenantCode); code != "" {
		tenantEntity, err := s.client.Tenant.Query().Where(tenant.CodeEQ(code)).First(ctx)
		if err != nil {
			return nil, fmt.Errorf("租户不存在")
		}
		tenantID = tenantEntity.ID
	} else {
		tenants, err := s.client.Tenant.Query().
			Where(tenant.StatusEQ("active")).
			Order(ent.Asc(tenant.FieldID)).
			Limit(2).
			All(ctx)
		if err == nil && len(tenants) == 1 {
			tenantID = tenants[0].ID
		}
	}

	policy := s.passwordPolicy(ctx, tenantID)
	return &dto.PasswordPolicyResponse{
		MinLength:           policy.MinLength,
		MaxLength:           policy.MaxLength,
		RequireUppercase:    policy.RequireUppercase,
		RequireLowercase:    policy.RequireLowercase,
		RequireNumbers:      policy.RequireNumbers,
		RequireSpecialChars: policy.RequireSpecialChars,
		Description:         policy.Description(),
	}, nil
}

// SwitchTenant 保持兼容签名：不撤销旧 refresh（测试/内部调用）。
func (s *Service) SwitchTenant(ctx context.Context, userID, tenantID int) (*dto.LoginResponse, error) {
	return s.SwitchTenantWithRevoke(ctx, userID, tenantID, "")
}

// SwitchTenantWithRevoke 显式切换作用域（IP-P0-6）：
// 目标校验 → 重签 JWT（tenant_source=switch）→ 撤销旧 refresh → 审计 tenant.switch。
func (s *Service) SwitchTenantWithRevoke(ctx context.Context, userID, tenantID int, oldRefreshToken string) (*dto.LoginResponse, error) {
	userEntity, err := s.client.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("用户不存在")
		}
		s.logger.Errorw("Failed to load user for tenant switch", "user_id", userID, "error", err)
		return nil, fmt.Errorf("无权限访问该租户")
	}
	nativeSwitch := userEntity.TenantID == tenantID
	superAdmin := userEntity.Role == user.RoleSuperAdmin
	mspAllowed := false
	if !nativeSwitch && !superAdmin && string(userEntity.MspRole) != "" {
		origin, originErr := s.client.Tenant.Get(ctx, userEntity.TenantID)
		if originErr == nil && tenantmode.IsMSPProviderTenantType(string(origin.Type)) {
			target, targetErr := s.client.Tenant.Get(ctx, tenantID)
			if targetErr == nil && tenantmode.IsCustomerTenantType(string(target.Type)) {
				count, queryErr := s.client.MSPAllocation.Query().Where(mspallocation.MspUserIDEQ(userID), mspallocation.CustomerTenantIDEQ(tenantID), mspallocation.DeassignedAtIsNil()).Count(ctx)
				mspAllowed = queryErr == nil && count > 0
			}
		}
	}
	if !nativeSwitch && !superAdmin && !mspAllowed {
		s.logger.Warnw("Switch tenant denied", "user_id", userID, "tenant_id", tenantID, "native_switch", nativeSwitch, "super_admin", superAdmin, "msp_role", string(userEntity.MspRole))
		middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
			UserID: userID, TenantID: userEntity.TenantID, TargetTenantID: tenantID,
			ActorAccount: userEntity.Username, Source: middleware.AuditSourceSwitch,
			Action: "tenant.switch_denied", Path: "/api/v1/auth/switch-tenant", Method: "POST",
			StatusCode: 403, FailureReason: "无权限访问该租户",
		})
		return nil, fmt.Errorf("无权限访问该租户")
	}
	tenantEntity, err := s.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("租户不存在")
	}
	if tenantEntity.Status != "active" {
		return nil, fmt.Errorf("租户已被暂停")
	}
	if !tenantEntity.ExpiresAt.IsZero() && tenantEntity.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("租户已过期")
	}
	accessToken, err := middleware.GenerateAccessTokenWithSource(userEntity.ID, userEntity.Username, string(userEntity.Role), tenantID, "switch", s.jwtSecret, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("生成token失败")
	}
	refreshToken, err := middleware.GenerateRefreshTokenWithSource(userEntity.ID, userEntity.Username, string(userEntity.Role), tenantID, "switch", s.jwtSecret, 7*24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("生成刷新令牌失败")
	}

	// 撤销旧 refresh（若由 handler 提供）：切换即会话轮换，旧作用域不可再续签。
	if oldRefreshToken != "" && s.tokenBlacklist != nil {
		if oldClaims, cErr := middleware.ValidateRefreshToken(oldRefreshToken, s.jwtSecret); cErr == nil && oldClaims.ExpiresAt != nil {
			if bErr := s.tokenBlacklist.AddRefreshToBlacklist(oldRefreshToken, oldClaims.ExpiresAt.Time); bErr != nil {
				s.logger.Warnw("failed to revoke old refresh token on tenant switch", "user_id", userID, "error", bErr)
			}
		}
	}
	// 审计：tenant.switch（target_tenant 维度可查）。
	middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
		UserID: userID, TenantID: userEntity.TenantID, TargetTenantID: tenantID,
		ActorAccount: userEntity.Username, Source: middleware.AuditSourceSwitch,
		Action: "tenant.switch", Path: "/api/v1/auth/switch-tenant", Method: "POST", StatusCode: 200,
	})
	s.logger.Infow("tenant switched",
		"user_id", userID,
		"home_tenant_id", userEntity.TenantID,
		"target_tenant_id", tenantID,
		"tenant_source", "switch",
	)
	// IP-P1-2：权限单一真源——按「目标租户」经 membership → role_id → role_permissions 计算；
	// 无 membership 且未开启 AUTHZ_STATIC_FALLBACK 时为 fail-closed（空集合）。
	// A6 核心：同一账号 A/B 租户权限各自独立，切换响应不得沿用 home 权限。
	// 与 Login/RefreshToken/GetMe/菜单生成共用 middleware.ResolvePermissions。
	// 解析失败/无 membership：permissions 为空（fail-closed），仅记 warn，不影响切换本身（失败开放仅限"权限清单"这一展示面，
	// 鉴权仍由 RBACMiddleware 独立判定）。
	permissions, source, permErr := middleware.ResolvePermissions(ctx, s.client, userEntity.ID, tenantID)
	if permErr != nil {
		s.logger.Warnw("failed to resolve permissions for tenant switch",
			"user_id", userID, "target_tenant_id", tenantID, "source", source, "error", permErr)
	}
	mspRole := string(userEntity.MspRole)
	var mspRolePtr *string
	if mspRole != "" {
		mspRolePtr = &mspRole
	}
	return &dto.LoginResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: &dto.LoginUserResponse{ID: userEntity.ID, Username: userEntity.Username, Email: userEntity.Email, Name: userEntity.Name, Role: string(userEntity.Role), MSPRole: mspRolePtr, Department: userEntity.Department, DepartmentID: userEntity.DepartmentID, Phone: userEntity.Phone, Active: userEntity.Active, TenantID: tenantID, CreatedAt: userEntity.CreatedAt, UpdatedAt: userEntity.UpdatedAt, Permissions: permissions}, Tenant: tenantEntity, TenantSelection: &dto.TenantSelection{Mode: switchTenantMode(userEntity, tenantEntity)}}, nil
}

// switchTenantMode 派生切换后的作用域模式（平台控制台/provider 家/单一租户）。
func switchTenantMode(userEntity *ent.User, tenantEntity *ent.Tenant) string {
	if string(userEntity.Role) == "super_admin" || string(userEntity.Role) == "sysadmin" {
		return "platform"
	}
	if tenantmode.IsMSPProviderTenantType(string(tenantEntity.Type)) {
		return "home"
	}
	return "single"
}

func (s *Service) Register(ctx context.Context, req *dto.RegisterRequest) (*dto.RegisterResponse, error) {
	// IP-P0-5 / F3：自助注册角色白名单——仅 end_user（user/空 归一）；
	// 平台/管理角色（super_admin/sysadmin/admin/manager/agent 等）一律拒绝，防注册提权。
	role := normalizeSelfRegisterRole(req.Role)
	if role != user.RoleEndUser {
		return nil, fmt.Errorf("不允许的角色: %s（自助注册仅支持 end_user）", req.Role)
	}
	// R2B 阴影观察（2026-10-03）：注册为预认证路径，用户名/邮箱唯一性检查跨租户（平台范围）。
	regCtx := tenantctx.SystemContext(ctx, "auth:register", "pre-auth uniqueness check (cross-tenant)")
	if exists, err := s.client.User.Query().Where(user.UsernameEQ(req.Username)).Exist(regCtx); err != nil {
		return nil, fmt.Errorf("检查用户名失败")
	} else if exists {
		return nil, fmt.Errorf("用户名已被注册")
	}
	if exists, err := s.client.User.Query().Where(user.EmailEQ(req.Email)).Exist(regCtx); err != nil {
		return nil, fmt.Errorf("检查邮箱失败")
	} else if exists {
		return nil, fmt.Errorf("邮箱已被注册")
	}
	var tenantID int
	if req.TenantCode != "" {
		tenantEntity, err := s.client.Tenant.Query().Where(tenant.CodeEQ(req.TenantCode)).First(regCtx)
		if err != nil {
			return nil, fmt.Errorf("租户不存在")
		}
		tenantID = tenantEntity.ID
	} else {
		tenants, err := s.client.Tenant.Query().Where(tenant.StatusEQ("active")).Order(ent.Asc(tenant.FieldID)).Limit(2).All(regCtx)
		if err != nil {
			return nil, fmt.Errorf("查询租户失败")
		}
		if len(tenants) != 1 {
			return nil, fmt.Errorf("请指定要加入的租户(tenantCode)")
		}
		tenantID = tenants[0].ID
	}
	// 注册同样受配置页的密码策略约束（历史上这里只做 min=8 的绑定校验，会绕过策略）。
	if err := s.passwordPolicy(ctx, tenantID).Validate(req.Password); err != nil {
		return nil, err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败")
	}
	// 角色已在入口处归一为 end_user（IP-P0-5 白名单）。
	userEntity, err := s.client.User.Create().SetUsername(req.Username).SetEmail(req.Email).SetName(req.ResolvedDisplayName()).SetPasswordHash(string(hashedPassword)).SetPhone(req.Phone).SetDepartment(req.Company).SetRole(role).SetTenantID(tenantID).SetActive(true).
		Save(tenantctx.WithTenantID(ctx, tenantID))
	if err != nil {
		return nil, fmt.Errorf("创建用户失败")
	}
	return &dto.RegisterResponse{ID: userEntity.ID, Username: userEntity.Username, Email: userEntity.Email, Message: "注册成功"}, nil
}

// normalizeSelfRegisterRole 归一自助注册角色：""/"user"/"end_user" → end_user；
// 其余原样返回（由调用方拒绝）。
func normalizeSelfRegisterRole(role string) user.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "user", "end_user":
		return user.RoleEndUser
	default:
		return user.Role(strings.ToLower(strings.TrimSpace(role)))
	}
}

func (s *Service) ForgotPassword(ctx context.Context, req *dto.ForgotPasswordRequest) (*dto.ForgotPasswordResponse, error) {
	genericOK := &dto.ForgotPasswordResponse{Message: "如果该邮箱已注册，我们将发送密码重置链接"}
	// R2B 阴影观察（2026-10-03）：找回密码为预认证路径，邮箱查找跨租户（平台范围）。
	lookupCtx := tenantctx.SystemContext(ctx, "auth:forgot-password", "pre-auth email lookup (cross-tenant)")
	query := s.client.User.Query().Where(user.EmailEQ(req.Email))
	if req.TenantCode != "" {
		tenantEntity, err := s.client.Tenant.Query().Where(tenant.CodeEQ(req.TenantCode)).First(lookupCtx)
		if err != nil {
			return genericOK, nil
		}
		query = query.Where(user.TenantIDEQ(tenantEntity.ID))
	}
	userEntity, err := query.First(lookupCtx)
	if err != nil {
		return genericOK, nil
	}
	token, err := generateResetToken()
	if err != nil {
		return nil, fmt.Errorf("生成重置令牌失败: %w", err)
	}
	if _, err = s.client.PasswordResetToken.Create().SetUserID(userEntity.ID).SetEmail(req.Email).SetToken(token).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx); err != nil {
		return nil, fmt.Errorf("生成重置令牌失败")
	}
	if s.emailService != nil {
		if err := s.emailService.SendPasswordResetEmail(ctx, []string{req.Email}, token, s.baseURL); err != nil {
			s.logger.Errorw("Failed to send password reset email", "user_id", userEntity.ID, "error", err)
		}
	}
	return genericOK, nil
}

func (s *Service) ResetPassword(ctx context.Context, req *dto.PasswordResetRequest) (*dto.PasswordResetResponse, error) {
	if req.Password != req.PasswordConfirm {
		return nil, fmt.Errorf("两次输入的密码不一致")
	}
	// 先按令牌所属租户的策略校验新密码：避免用不合格密码消耗一次性重置令牌。
	tokenOwner, err := s.client.PasswordResetToken.Query().
		Where(passwordresettoken.TokenEQ(req.Token), passwordresettoken.EmailEQ(req.Email), passwordresettoken.Used(false), passwordresettoken.ExpiresAtGT(time.Now())).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("令牌无效或已使用")
	}
	tokenUser, err := s.client.User.Get(ctx, tokenOwner.UserID)
	if err != nil {
		return nil, fmt.Errorf("令牌无效或已使用")
	}
	if err := s.passwordPolicy(ctx, tokenUser.TenantID).Validate(req.Password); err != nil {
		return nil, err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("启动密码重置事务失败")
	}
	defer func() { _ = tx.Rollback() }()
	affected, err := tx.PasswordResetToken.Update().Where(passwordresettoken.TokenEQ(req.Token), passwordresettoken.EmailEQ(req.Email), passwordresettoken.Used(false), passwordresettoken.ExpiresAtGT(time.Now())).SetUsed(true).Save(ctx)
	if err != nil || affected != 1 {
		return nil, fmt.Errorf("令牌无效或已使用")
	}
	tokenEntity, err := tx.PasswordResetToken.Query().Where(passwordresettoken.TokenEQ(req.Token), passwordresettoken.EmailEQ(req.Email)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("令牌无效或已使用")
	}
	if _, err = tx.User.UpdateOneID(tokenEntity.UserID).SetPasswordHash(string(hashedPassword)).Save(ctx); err != nil {
		return nil, fmt.Errorf("更新密码失败")
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交密码重置失败")
	}
	if s.tokenBlacklist != nil {
		_ = s.tokenBlacklist.RevokeUserTokens(ctx, tokenEntity.UserID)
	}
	return &dto.PasswordResetResponse{Message: "密码重置成功，请使用新密码登录"}, nil
}

func (s *Service) ValidateResetToken(ctx context.Context, req *dto.ValidateResetTokenRequest) (*dto.ValidateResetTokenResponse, error) {
	tokenEntity, err := s.client.PasswordResetToken.Query().Where(passwordresettoken.TokenEQ(req.Token), passwordresettoken.EmailEQ(req.Email), passwordresettoken.Used(false)).First(ctx)
	valid := err == nil && !time.Now().After(tokenEntity.ExpiresAt)
	return &dto.ValidateResetTokenResponse{Valid: valid, Email: req.Email}, nil
}

func generateResetToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read cryptographic random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
