package common

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	enttenant "itsm-backend/ent/tenant"
	entuser "itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ErrTenantAccessRevoked：refresh 续签时目标作用域已不可用
// （allocation/归属撤销、租户停用）——确定性拒绝，不回退 home（IP-P0-6）。
var ErrTenantAccessRevoked = errors.New("TENANT_ACCESS_REVOKED")

type Service struct {
	repo      Repository
	jwtSecret string
	logger    *zap.SugaredLogger
	client    *ent.Client // For legacy integrations if needed
	redis     *redis.Client
}

func NewService(repo Repository, jwtSecret string, logger *zap.SugaredLogger, client *ent.Client) *Service {
	return &Service{
		repo:      repo,
		jwtSecret: jwtSecret,
		logger:    logger,
		client:    client,
	}
}

// SetRedis 注入 Redis 客户端；启用 refresh token 黑名单（token rotation 后旧值失效）
func (s *Service) SetRedis(r *redis.Client) {
	s.redis = r
}

// refreshBlacklistKey Redis key for refresh token blacklist
func refreshBlacklistKey(token string) string { return "refresh:blacklist:" + token }

// blacklistRefreshToken 将 refresh token 加入黑名单，TTL = 剩余有效期
func (s *Service) blacklistRefreshToken(ctx context.Context, token string, expiresAt time.Time) error {
	if s.redis == nil {
		return nil // 未注入 redis：降级为无黑名单（记 warn 由调用方处理）
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	return s.redis.Set(ctx, refreshBlacklistKey(token), "1", ttl).Err()
}

// isRefreshBlacklisted 检查 refresh token 是否已拉黑
func (s *Service) isRefreshBlacklisted(ctx context.Context, token string) (bool, error) {
	if s.redis == nil {
		return false, nil
	}
	n, err := s.redis.Exists(ctx, refreshBlacklistKey(token)).Result()
	return n > 0, err
}

// Auth

// resolvePermissions IP-P1-2 权限单源：按 (user_id, tenant_id) 走
// middleware.ResolvePermissions（membership → role_id → role_permissions）。
// 无 membership 且未开启 AUTHZ_STATIC_FALLBACK 时解析器返回空集合（fail-closed），
// 仅基础设施错误会带 error，调用方记日志后仍使用返回值（空集合）。
// 说明：super_admin 直通 ["*"] 由解析器内部保持现状。
func (s *Service) resolvePermissions(ctx context.Context, userID, tenantID int) []string {
	permissions, source, err := middleware.ResolvePermissions(ctx, s.client, userID, tenantID)
	if err != nil && s.logger != nil {
		s.logger.Warnw("failed to resolve permissions",
			"user_id", userID, "tenant_id", tenantID, "source", source, "error", err)
	}
	return permissions
}

func (s *Service) Login(ctx context.Context, username, password string, tenantID int, tenantCode string) (*AuthResult, error) {
	// Resolve tenant
	if tenantID == 0 && tenantCode != "" {
		t, err := s.client.Tenant.Query().Where(enttenant.CodeEQ(tenantCode)).First(ctx)
		if err == nil {
			tenantID = t.ID
		}
	}
	// When no tenant is specified, find user by username alone (matches across tenants)
	var u *User
	var entUser *ent.User
	var err error
	if tenantID == 0 {
		// Look for user by username without tenant filter
		entUser, err = s.client.User.Query().Where(entuser.UsernameEQ(username)).Only(ctx)
		if err != nil {
			middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
				TenantID: tenantID, ActorAccount: username, Source: middleware.AuditSourceLogin,
				Action: "auth.login", Path: "/api/v1/auth/login", Method: "POST",
				StatusCode: 401, FailureReason: "用户不存在",
			})
			return nil, fmt.Errorf("invalid credentials")
		}
		u = toUserDomain(entUser)
	} else {
		entUser, err = s.client.User.Query().
			Where(entuser.UsernameEQ(username), entuser.TenantID(tenantID)).
			Only(ctx)
		if err != nil {
			middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
				TenantID: tenantID, ActorAccount: username, Source: middleware.AuditSourceLogin,
				Action: "auth.login", Path: "/api/v1/auth/login", Method: "POST",
				StatusCode: 401, FailureReason: "用户不存在",
			})
			return nil, fmt.Errorf("invalid credentials")
		}
		u = toUserDomain(entUser)
	}

	// Set msp_role from ent user
	mspRoleStr := string(entUser.MspRole)
	if mspRoleStr != "" {
		u.MSPRole = &mspRoleStr
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(entUser.PasswordHash), []byte(password)); err != nil {
		middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
			TenantID: entUser.TenantID, TargetTenantID: entUser.TenantID, ActorAccount: username,
			Source: middleware.AuditSourceLogin, Action: "auth.login",
			Path: "/api/v1/auth/login", Method: "POST", StatusCode: 401, FailureReason: "密码错误",
		})
		return nil, fmt.Errorf("invalid credentials")
	}

	if !u.Active {
		middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
			TenantID: entUser.TenantID, TargetTenantID: entUser.TenantID, ActorAccount: username,
			Source: middleware.AuditSourceLogin, Action: "auth.login",
			Path: "/api/v1/auth/login", Method: "POST", StatusCode: 401, FailureReason: "账户锁定",
		})
		return nil, fmt.Errorf("user account is inactive")
	}

	// 07:G3：JWT role 取「主角色」与「MSP 映射角色」中 rank 更高者（middleware.RoleRank 单一词表）。
	// 例：users.role=admin + msp_role=provider_admin → admin(4) > msp_manager(3)，保持 admin，
	// 同租户建号/管理能力不再被 msp_manager 遮蔽；MSP 路由鉴权仍按 msp_role 映射
	// （RequireMSPPermission 独立解析 msp_role，不受本处影响）。
	// 反例：users.role=end_user + msp_role=provider_admin → 映射 msp_manager(3) 更高，取 msp_manager。
	if mspRoleStr != "" {
		if mappedRole := middleware.GetMSPRBACRole(mspRoleStr); mappedRole != "" && middleware.RoleRank(mappedRole) > middleware.RoleRank(u.Role) {
			u.Role = mappedRole
		}
	}

	// 登录落 home（IP-P0-6）：token 作用域 = users.tenant_id；tenantCode 仅参与身份定位，
	// 不改变签发作用域（不因 last_active/tenantCode 直签客户）。
	loginTenant, tenantErr := s.client.Tenant.Get(ctx, u.TenantID)
	if tenantErr != nil {
		s.logger.Warnw("login tenant lookup failed", "user_id", u.ID, "tenant_id", u.TenantID, "error", tenantErr)
	}
	accessToken, err := middleware.GenerateAccessTokenWithSource(u.ID, u.Username, u.Role, u.TenantID, "home", s.jwtSecret, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	refreshToken, err := middleware.GenerateRefreshTokenWithSource(u.ID, u.Username, u.Role, u.TenantID, "home", s.jwtSecret, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}

	// 获取用户权限（IP-P1-2：按 home 租户经 membership → role_permissions 计算）
	u.Permissions = s.resolvePermissions(ctx, entUser.ID, u.TenantID)
	middleware.RecordAuthAudit(ctx, s.client, middleware.AuthAuditEntry{
		UserID: entUser.ID, TenantID: entUser.TenantID, TargetTenantID: u.TenantID, ActorAccount: username,
		Source: middleware.AuditSourceLogin, Action: "auth.login",
		Path: "/api/v1/auth/login", Method: "POST", StatusCode: 200,
	})

	return &AuthResult{
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		User:            u,
		Tenant:          toAuthTenantInfo(loginTenant),
		TenantSelection: &TenantSelection{Mode: tenantScopeMode(u.Role, loginTenant)},
	}, nil
}

// tenantScopeMode 派生登录作用域模式：平台控制台 / provider 家 / 单一租户。
func tenantScopeMode(role string, t *ent.Tenant) string {
	if role == "super_admin" || role == "sysadmin" {
		return "platform"
	}
	if t != nil && tenantmode.IsMSPProviderTenantType(string(t.Type)) {
		return "home"
	}
	return "single"
}

func toAuthTenantInfo(t *ent.Tenant) *TenantInfo {
	if t == nil {
		return nil
	}
	return &TenantInfo{
		ID:        t.ID,
		Name:      t.Name,
		Code:      t.Code,
		Type:      string(t.Type),
		Status:    t.Status,
		ExpiresAt: t.ExpiresAt,
	}
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*AuthResult, error) {
	claims, err := middleware.ValidateRefreshToken(refreshToken, s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// 黑名单检查：token rotation 后旧值不允许再使用
	if blacklisted, chkErr := s.isRefreshBlacklisted(ctx, refreshToken); chkErr != nil {
		s.logger.Warnw("refresh blacklist check failed, deny by default", "error", chkErr)
		return nil, fmt.Errorf("refresh token validation failed")
	} else if blacklisted {
		s.logger.Warnw("refresh token replay detected", "user_id", claims.UserID)
		return nil, fmt.Errorf("refresh token has been revoked")
	}

	user, err := s.repo.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	// IP-P0-6：作用域保持——按 claims.TenantID 重签并复核；失效 → TENANT_ACCESS_REVOKED
	// （不回退 home，避免"切换后被静默拉回家租户"）。
	scopeTenantID := claims.TenantID
	if scopeTenantID <= 0 {
		scopeTenantID = user.TenantID
	}
	targetTenant, allowed := s.tenantAccessForRefresh(ctx, user, scopeTenantID)
	if !allowed {
		s.logger.Warnw("refresh tenant access revoked",
			"user_id", user.ID,
			"home_tenant_id", user.TenantID,
			"scope_tenant_id", scopeTenantID,
		)
		return nil, ErrTenantAccessRevoked
	}
	source := claims.TenantSource
	if source == "" {
		source = "home" // 兼容旧 token
	}

	// regenerate tokens（作用域与来源保持）
	accessToken, err := middleware.GenerateAccessTokenWithSource(user.ID, user.Username, user.Role, scopeTenantID, source, s.jwtSecret, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	newRefresh, err := middleware.GenerateRefreshTokenWithSource(user.ID, user.Username, user.Role, scopeTenantID, source, s.jwtSecret, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}

	// 拉黑旧 refresh token，TTL = 剩余有效期
	if claims.ExpiresAt != nil {
		if bErr := s.blacklistRefreshToken(ctx, refreshToken, claims.ExpiresAt.Time); bErr != nil {
			s.logger.Warnw("failed to blacklist old refresh token", "user_id", user.ID, "error", bErr)
		}
	}

	// IP-P1-2：刷新响应权限按当前 token 作用域（目标租户）重算，
	// 保证切换租户后续签不会把权限解析回 home 租户。
	user.Permissions = s.resolvePermissions(ctx, user.ID, scopeTenantID)

	return &AuthResult{
		AccessToken:     accessToken,
		RefreshToken:    newRefresh,
		User:            user,
		Tenant:          toAuthTenantInfo(targetTenant),
		TenantSelection: &TenantSelection{Mode: source},
	}, nil
}

// tenantAccessForRefresh 复核 refresh 目标作用域：home / 平台 / 有效 allocation 客户；
// 租户不存在/停用/过期一律不允许（fail-closed）。
func (s *Service) tenantAccessForRefresh(ctx context.Context, u *User, tenantID int) (*ent.Tenant, bool) {
	target, err := s.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return nil, false
	}
	if target.Status != "active" {
		return target, false
	}
	if !target.ExpiresAt.IsZero() && target.ExpiresAt.Before(time.Now()) {
		return target, false
	}
	if u.TenantID == tenantID {
		return target, true
	}
	if u.Role == "super_admin" || u.Role == "sysadmin" {
		return target, true
	}
	if u.MSPRole != nil && *u.MSPRole != "" && tenantmode.IsCustomerTenantType(string(target.Type)) {
		origin, oErr := s.client.Tenant.Get(ctx, u.TenantID)
		if oErr == nil && tenantmode.IsMSPProviderTenantType(string(origin.Type)) {
			n, cErr := s.client.MSPAllocation.Query().
				Where(
					mspallocation.MspUserIDEQ(u.ID),
					mspallocation.CustomerTenantIDEQ(tenantID),
					mspallocation.DeassignedAtIsNil(),
				).
				Count(ctx)
			if cErr == nil && n > 0 {
				return target, true
			}
		}
	}
	return target, false
}

// User Management

// GetUser 获取用户信息（/auth/me 数据源）。
// 前端刷新页面后由 AuthGuard 重建 user，若此处不带 permissions，
// hasPermission 会全部返回 false，导致 Sidebar 管理功能区等权限驱动 UI 消失。
// IP-P1-2：与 Login 相同走权限单源解析器（按 home 租户；super_admin → ["*"]）。
func (s *Service) GetUser(ctx context.Context, id int) (*User, error) {
	u, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// IP-P1-2：权限统一经解析器按 home 租户计算（GetMe 的 token 作用域优先走
	// GetUserScoped；保留本方法作为未传租户的兼容入口）。
	u.Permissions = s.resolvePermissions(ctx, u.ID, u.TenantID)
	return u, nil
}

// GetUserScoped 获取用户信息，并按指定租户（token 作用域）计算 permissions。
// /auth/me 在切换租户后必须返回目标租户的权限，故由 handler 传入 context 中的 tenant_id。
func (s *Service) GetUserScoped(ctx context.Context, id, tenantID int) (*User, error) {
	u, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		tenantID = u.TenantID
	}
	u.Permissions = s.resolvePermissions(ctx, u.ID, tenantID)
	return u, nil
}

func (s *Service) ListUsers(ctx context.Context, tenantID int) ([]*User, error) {
	return s.repo.ListUsers(ctx, tenantID)
}

// Organization Management

func (s *Service) GetDepartment(ctx context.Context, id int, tenantID int) (*Department, error) {
	return s.repo.GetDepartment(ctx, id, tenantID)
}

func (s *Service) GetDepartmentTree(ctx context.Context, tenantID int) ([]*Department, error) {
	return s.repo.GetDepartmentTree(ctx, tenantID)
}

func (s *Service) ListDepartments(ctx context.Context, tenantID int) ([]*Department, error) {
	return s.repo.ListDepartments(ctx, tenantID)
}

func (s *Service) CreateDepartment(ctx context.Context, d *Department) (*Department, error) {
	return s.repo.CreateDepartment(ctx, d)
}

func (s *Service) UpdateDepartment(ctx context.Context, d *Department) (*Department, error) {
	return s.repo.UpdateDepartment(ctx, d)
}

func (s *Service) DeleteDepartment(ctx context.Context, id int, tenantID int) error {
	return s.repo.DeleteDepartment(ctx, id, tenantID)
}

func (s *Service) ListTeams(ctx context.Context, tenantID int) ([]*Team, error) {
	return s.repo.ListTeams(ctx, tenantID)
}

func (s *Service) GetTeam(ctx context.Context, id int, tenantID int) (*Team, error) {
	return s.repo.GetTeam(ctx, id, tenantID)
}

func (s *Service) CreateTeam(ctx context.Context, t *Team) (*Team, error) {
	return s.repo.CreateTeam(ctx, t)
}

func (s *Service) UpdateTeam(ctx context.Context, t *Team) (*Team, error) {
	return s.repo.UpdateTeam(ctx, t)
}

func (s *Service) DeleteTeam(ctx context.Context, id int, tenantID int) error {
	return s.repo.DeleteTeam(ctx, id, tenantID)
}

func (s *Service) AddTeamMember(ctx context.Context, teamID int, userID int) error {
	return s.repo.AddTeamMember(ctx, teamID, userID)
}

// Tags

func (s *Service) ListTags(ctx context.Context, tenantID int) ([]*Tag, error) {
	return s.repo.ListTags(ctx, tenantID)
}

func (s *Service) CreateTag(ctx context.Context, t *Tag) (*Tag, error) {
	return s.repo.CreateTag(ctx, t)
}

// Auditing

func (s *Service) LogActivity(ctx context.Context, log *AuditLog) error {
	return s.repo.CreateAuditLog(ctx, log)
}

func (s *Service) GetAuditLogs(ctx context.Context, tenantID int, userID int) ([]*AuditLog, error) {
	return s.repo.ListAuditLogs(ctx, tenantID, userID, 100)
}

// GetUserTenants 获取用户可访问的租户集合（IP-P0-6 语义修正）：
// home ∪ 有效 allocation 客户 ∪ 平台全量（super_admin/sysadmin）；去重且 home 优先。
func (s *Service) GetUserTenants(ctx context.Context, userID int) ([]interface{}, error) {
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	seen := make(map[int]bool)
	result := make([]interface{}, 0, 4)
	add := func(t *ent.Tenant) {
		if t == nil || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		result = append(result, map[string]interface{}{
			"id":        t.ID,
			"name":      t.Name,
			"code":      t.Code,
			"type":      t.Type,
			"status":    t.Status,
			"expiresAt": t.ExpiresAt,
		})
	}

	// 1) home（家租户始终第一位）
	if home, hErr := s.client.Tenant.Get(ctx, user.TenantID); hErr == nil {
		add(home)
	}

	if string(user.Role) == "super_admin" || string(user.Role) == "sysadmin" {
		// 2a) 平台角色：全部 active 租户（治理面）
		all, aErr := s.client.Tenant.Query().
			Where(enttenant.StatusEQ("active")).
			Order(ent.Asc(enttenant.FieldID)).
			All(ctx)
		if aErr != nil {
			return nil, fmt.Errorf("failed to list tenants: %w", aErr)
		}
		for _, t := range all {
			add(t)
		}
		return result, nil
	}

	// 2b) provider 员工：home ∪ 有效 allocation 的客户租户（R9/R10 同源）。
	if string(user.MspRole) != "" {
		allocs, aErr := s.client.MSPAllocation.Query().
			Where(mspallocation.MspUserIDEQ(userID), mspallocation.DeassignedAtIsNil()).
			Order(ent.Asc(mspallocation.FieldID)).
			All(ctx)
		if aErr != nil {
			return nil, fmt.Errorf("failed to list allocations: %w", aErr)
		}
		for _, a := range allocs {
			if t, tErr := s.client.Tenant.Get(ctx, a.CustomerTenantID); tErr == nil {
				add(t)
			}
		}
	}

	return result, nil
}
