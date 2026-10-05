package service

// tenant_user_admin.go：平台侧租户用户管理（TUM-1/TUM-2）。
//
// 权威方案：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md（v0.2 决策冻结）。
// 设计要点：
//  1. 平台面判定双层：路由层 RequirePermission("tenant","read|write") 粗筛 + 本服务 super_admin 硬校验（fail-closed）；
//  2. 跨租户 ctx 采用“按目标租户重绑定”（tenantctx.WithTenantID(target)，同 MSP 工作台 IP-P2-2 范式），
//     RLS enforce 下按目标租户收窄；所有用户查询均显式过滤 tenant_id（双保险）；
//  3. 读通道随路由上线；写通道受 TENANT_USER_ADMIN_ENABLED 灰度开关控制（默认关，可即时回滚）；
//  4. 会话吊销 = access（按用户 MinIssuedAt）+ refresh（按用户最低签发时间；TUM-D6）双吊销；
//  5. 审计行归属 actor 家租户、target_tenant_id=目标租户（source=platform_selected），动作名 user.admin_*。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
)

// 平台侧租户用户管理稳定错误码（字符串码；前端映射文案，对齐建号通道风格）。
const (
	TenantUserAdminCodeDisabled        = "TENANT_USER_ADMIN_DISABLED"
	TenantUserAdminCodePlatformScope   = "PLATFORM_SCOPE_REQUIRED"
	TenantUserAdminCodeTenantNotFound  = "TENANT_NOT_FOUND"
	TenantUserAdminCodeUserNotFound    = "TENANT_USER_NOT_FOUND"
	TenantUserAdminCodeLastAdmin       = "LAST_ADMIN_PROTECTED"
	TenantUserAdminCodeSelfOperation   = "SELF_OPERATION_FORBIDDEN"
	TenantUserAdminCodeConfirmRequired = "TENANT_CONFIRM_REQUIRED"
	TenantUserAdminCodePasswordPolicy  = "PASSWORD_POLICY_VIOLATION"
	TenantUserAdminCodeInvalidMode     = "INVALID_RESET_MODE"
	TenantUserAdminCodeInvalidParam    = "INVALID_PARAM"
)

const (
	tenantUserAdminPageSizeDefault = 20
	tenantUserAdminPageSizeMax     = 100
	tenantUserAdminPasswordLength  = 16
	// tenantUserAdminSpecialChars 生成口令的特殊字符集（保持 shell/SQL 转义安全）。
	tenantUserAdminSpecialChars = "!@#$%^&*()-_=+"
	// protectedDefaultTenantCode default 平台租户的二次确认码（TUM-D5）。
	protectedDefaultTenantCode = "default"
)

// TenantUserAdminError 平台侧租户用户管理稳定拒绝类型：handler 依据 Code/Status 映射响应。
type TenantUserAdminError struct {
	Code    string
	Message string
	Status  int
}

func (e *TenantUserAdminError) Error() string { return e.Code + ": " + e.Message }

func newTenantUserAdminError(code string, status int, format string, args ...interface{}) *TenantUserAdminError {
	return &TenantUserAdminError{Code: code, Message: fmt.Sprintf(format, args...), Status: status}
}

// AsTenantUserAdminError 提取稳定错误码；非本类错误返回 ok=false。
func AsTenantUserAdminError(err error) (*TenantUserAdminError, bool) {
	var target *TenantUserAdminError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// TenantAdminActor 是平台调用方身份快照（来自 JWT claims / gin context，不含敏感信息）。
type TenantAdminActor struct {
	UserID       int
	HomeTenantID int
	Role         string
	Username     string
}

// TenantUserAdminService 收口平台侧租户用户读通道与账号治理。零值不可用。
type TenantUserAdminService struct {
	client  *ent.Client
	users   *UserService
	logger  *zap.SugaredLogger
	enabled bool
}

// NewTenantUserAdminService 构造服务。
// 灰度开关：环境变量 TENANT_USER_ADMIN_ENABLED=true 时开启写通道（默认关）。
func NewTenantUserAdminService(client *ent.Client, users *UserService, logger *zap.SugaredLogger) *TenantUserAdminService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &TenantUserAdminService{
		client:  client,
		users:   users,
		logger:  logger,
		enabled: strings.EqualFold(os.Getenv("TENANT_USER_ADMIN_ENABLED"), "true"),
	}
}

// SetEnabled 显式设置写通道灰度开关（测试/按租户灰度使用）。
func (s *TenantUserAdminService) SetEnabled(enabled bool) { s.enabled = enabled }

// Enabled 报告写通道是否开启。
func (s *TenantUserAdminService) Enabled() bool { return s != nil && s.enabled }

func (s *TenantUserAdminService) ensureConfigured() error {
	if s == nil || s.client == nil || s.users == nil {
		return fmt.Errorf("tenant user admin service is not configured")
	}
	return nil
}

// requireGovernanceEnabled 写通道灰度校验（读通道不受限；TUM-D3）。
func (s *TenantUserAdminService) requireGovernanceEnabled() error {
	if !s.enabled {
		return newTenantUserAdminError(TenantUserAdminCodeDisabled, 403, "平台侧用户治理未启用")
	}
	return nil
}

// requirePlatformScope 平台面硬校验（TUM-D1）：super_admin 之外一律拒绝（fail-closed）。
func (s *TenantUserAdminService) requirePlatformScope(actor TenantAdminActor) error {
	if actor.Role != "super_admin" {
		return newTenantUserAdminError(TenantUserAdminCodePlatformScope, 403, "需要平台管理员权限")
	}
	return nil
}

// targetScopedContext 按目标租户重绑定查询作用域（RLS enforce 下 GUC 单值 = 查询语义）。
func targetScopedContext(ctx context.Context, tenantID int) context.Context {
	return tenantctx.WithTenantID(ctx, tenantID)
}

// loadTenant 在目标租户作用域内加载租户；不存在返回稳定错误码。
func (s *TenantUserAdminService) loadTenant(ctx context.Context, tenantID int) (*ent.Tenant, error) {
	t, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newTenantUserAdminError(TenantUserAdminCodeTenantNotFound, 404, "租户不存在")
		}
		return nil, fmt.Errorf("load tenant %d: %w", tenantID, err)
	}
	return t, nil
}

// loadTenantUser 在目标租户作用域内加载用户（双键），杜绝跨租户读写。
func (s *TenantUserAdminService) loadTenantUser(ctx context.Context, tenantID, userID int) (*ent.User, error) {
	u, err := s.client.User.Query().
		Where(user.IDEQ(userID), user.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newTenantUserAdminError(TenantUserAdminCodeUserNotFound, 404, "用户不存在")
		}
		return nil, fmt.Errorf("load tenant user %d/%d: %w", tenantID, userID, err)
	}
	return u, nil
}

// ListUsers 分页查询目标租户用户（读通道；GET /api/v1/tenants/:id/users）。
func (s *TenantUserAdminService) ListUsers(ctx context.Context, actor TenantAdminActor, tenantID int, q *dto.TenantUserListQuery) (*dto.TenantUserListResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := s.requirePlatformScope(actor); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的租户ID")
	}
	if q == nil {
		q = &dto.TenantUserListQuery{}
	}
	page, pageSize := normalizeTenantUserAdminPaging(q.Page, q.PageSize)
	qctx := targetScopedContext(ctx, tenantID)
	if _, err := s.loadTenant(qctx, tenantID); err != nil {
		return nil, err
	}

	query := s.client.User.Query().Where(user.TenantIDEQ(tenantID))
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		query = query.Where(user.Or(
			user.UsernameContainsFold(kw),
			user.NameContainsFold(kw),
			user.EmailContainsFold(kw),
		))
	}
	switch strings.ToLower(strings.TrimSpace(q.Status)) {
	case "", "all":
		// 全部
	case "active":
		query = query.Where(user.ActiveEQ(true))
	case "inactive":
		query = query.Where(user.ActiveEQ(false))
	default:
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的状态过滤值")
	}
	if role := strings.TrimSpace(q.Role); role != "" {
		query = query.Where(user.RoleEQ(user.Role(role)))
	}

	total, err := query.Clone().Count(qctx)
	if err != nil {
		return nil, fmt.Errorf("count tenant users: %w", err)
	}
	entities, err := query.Order(ent.Desc(user.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(qctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant users: %w", err)
	}

	items := make([]dto.TenantUserItem, 0, len(entities))
	for _, u := range entities {
		items = append(items, toTenantUserItem(u))
	}
	return &dto.TenantUserListResponse{Items: items, Total: int64(total), Page: page, PageSize: pageSize}, nil
}

// GetUser 查询目标租户单个用户（GET /api/v1/tenants/:id/users/:userId）。
func (s *TenantUserAdminService) GetUser(ctx context.Context, actor TenantAdminActor, tenantID, userID int) (*dto.TenantUserItem, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := s.requirePlatformScope(actor); err != nil {
		return nil, err
	}
	if tenantID <= 0 || userID <= 0 {
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的租户或用户ID")
	}
	qctx := targetScopedContext(ctx, tenantID)
	if _, err := s.loadTenant(qctx, tenantID); err != nil {
		return nil, err
	}
	u, err := s.loadTenantUser(qctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	item := toTenantUserItem(u)
	return &item, nil
}

func normalizeTenantUserAdminPaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = tenantUserAdminPageSizeDefault
	}
	if pageSize > tenantUserAdminPageSizeMax {
		pageSize = tenantUserAdminPageSizeMax
	}
	return page, pageSize
}

func toTenantUserItem(u *ent.User) dto.TenantUserItem {
	item := dto.TenantUserItem{
		ID:                 u.ID,
		Username:           u.Username,
		Name:               u.Name,
		Email:              u.Email,
		Role:               string(u.Role),
		Department:         u.Department,
		Active:             u.Active,
		MustChangePassword: u.MustChangePassword,
		IsBootstrapAdmin:   u.IsBootstrapAdmin,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
	}
	if string(u.MspRole) != "" {
		role := string(u.MspRole)
		item.MSPRole = &role
	}
	return item
}

// ResetPassword 重置目标租户用户密码（TUM-D4）：
//   - mode=generated：服务端按目标租户密码策略生成一次性口令，must_change_password=true，明文仅本次回传；
//   - mode=specified：管理员指定新密码，需通过目标租户密码策略，must_change_password=false；
//   - 两种模式均触发会话双吊销（access+refresh）。
func (s *TenantUserAdminService) ResetPassword(ctx context.Context, actor TenantAdminActor, tenantID, userID int, req *dto.ResetTenantUserPasswordRequest) (*dto.ResetTenantUserPasswordResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := s.requireGovernanceEnabled(); err != nil {
		return nil, err
	}
	if err := s.requirePlatformScope(actor); err != nil {
		return nil, err
	}
	if tenantID <= 0 || userID <= 0 {
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的租户或用户ID")
	}
	if req == nil {
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "请求参数错误")
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = dto.TenantUserResetModeGenerated // 默认一次性口令（TUM-D4）
	}

	qctx := targetScopedContext(ctx, tenantID)
	if _, err := s.loadTenant(qctx, tenantID); err != nil {
		return nil, err
	}
	if _, err := s.loadTenantUser(qctx, tenantID, userID); err != nil {
		return nil, err
	}

	policy := s.users.PasswordPolicy(qctx, tenantID)
	plain := ""
	mustChange := false
	switch mode {
	case dto.TenantUserResetModeGenerated:
		generated, err := generatePolicyCompliantPassword(policy)
		if err != nil {
			return nil, fmt.Errorf("generate one-time password: %w", err)
		}
		plain = generated
		mustChange = true
	case dto.TenantUserResetModeSpecified:
		if strings.TrimSpace(req.NewPassword) == "" {
			return nil, newTenantUserAdminError(TenantUserAdminCodePasswordPolicy, 400, "密码不能为空")
		}
		if err := policy.Validate(req.NewPassword); err != nil {
			return nil, newTenantUserAdminError(TenantUserAdminCodePasswordPolicy, 400, "%s", err.Error())
		}
		plain = req.NewPassword
	default:
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidMode, 400, "不支持的重置模式")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	if err := s.client.User.UpdateOneID(userID).
		Where(user.TenantIDEQ(tenantID)).
		SetPasswordHash(string(hashed)).
		SetMustChangePassword(mustChange).
		Exec(qctx); err != nil {
		return nil, fmt.Errorf("reset tenant user password: %w", err)
	}

	revocationFailures := s.revokeUserSessions(qctx, userID)
	s.recordAudit(ctx, actor, "user.admin_password_reset", tenantID, userID, withRevocationFailures(map[string]any{
		"mode":                 mode,
		"must_change_password": mustChange,
	}, revocationFailures), 200)

	resp := &dto.ResetTenantUserPasswordResponse{
		UserID:             userID,
		Mode:               mode,
		MustChangePassword: mustChange,
	}
	if mode == dto.TenantUserResetModeGenerated {
		resp.GeneratedPassword = plain
	}
	return resp, nil
}

// SetActive 启用/停用目标租户用户（TUM-D5 护栏）：
//   - 禁止停用调用者自身；
//   - 目标租户最后一个可用管理员不可停用（409 LAST_ADMIN_PROTECTED）；
//   - default 平台租户停用需 confirmCode=default（防误伤全局兜底）；
//   - 停用触发会话双吊销。
func (s *TenantUserAdminService) SetActive(ctx context.Context, actor TenantAdminActor, tenantID, userID int, req *dto.SetTenantUserStatusRequest) error {
	if err := s.ensureConfigured(); err != nil {
		return err
	}
	if err := s.requireGovernanceEnabled(); err != nil {
		return err
	}
	if err := s.requirePlatformScope(actor); err != nil {
		return err
	}
	if tenantID <= 0 || userID <= 0 {
		return newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的租户或用户ID")
	}
	if req == nil || req.Active == nil {
		return newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "缺少 active 参数")
	}
	active := *req.Active

	qctx := targetScopedContext(ctx, tenantID)
	tenantEntity, err := s.loadTenant(qctx, tenantID)
	if err != nil {
		return err
	}
	target, err := s.loadTenantUser(qctx, tenantID, userID)
	if err != nil {
		return err
	}

	if !active {
		if actor.UserID == userID {
			return newTenantUserAdminError(TenantUserAdminCodeSelfOperation, 409, "不能停用当前登录用户")
		}
		if tenantEntity.Code == protectedDefaultTenantCode && strings.TrimSpace(req.ConfirmCode) != protectedDefaultTenantCode {
			return newTenantUserAdminError(TenantUserAdminCodeConfirmRequired, 409, "平台默认租户停用需二次确认（confirmCode=default）")
		}
		// 最后管理员护栏：仅当目标是当前激活的 admin，且同租户无其他可用 admin。
		if target.Active && target.Role == user.RoleAdmin {
			others, cErr := s.client.User.Query().
				Where(
					user.TenantIDEQ(tenantID),
					user.RoleEQ(user.RoleAdmin),
					user.ActiveEQ(true),
					user.IDNEQ(userID),
				).
				Count(qctx)
			if cErr != nil {
				return fmt.Errorf("count tenant admins: %w", cErr)
			}
			if others == 0 {
				return newTenantUserAdminError(TenantUserAdminCodeLastAdmin, 409, "该用户是租户最后一个可用管理员，不能停用")
			}
		}
	}

	// 复用既有用户状态语义（防自停用 + 停用时吊销 access token）。
	if err := s.users.ChangeUserStatus(qctx, userID, active, actor.UserID, tenantID); err != nil {
		return err
	}
	revocationFailures := []string(nil)
	if !active {
		// ChangeUserStatus 已吊销 access；此处补齐 refresh 双吊销（TUM-D6）。
		revocationFailures = s.revokeUserRefresh(qctx, userID)
	}
	s.recordAudit(ctx, actor, "user.admin_status", tenantID, userID, withRevocationFailures(map[string]any{
		"active": active,
	}, revocationFailures), 200)
	return nil
}

// ForceLogout 强制下线目标用户：吊销其全部存量 access + refresh（不改密码、不改状态）。
func (s *TenantUserAdminService) ForceLogout(ctx context.Context, actor TenantAdminActor, tenantID, userID int) (*dto.ForceLogoutTenantUserResponse, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := s.requireGovernanceEnabled(); err != nil {
		return nil, err
	}
	if err := s.requirePlatformScope(actor); err != nil {
		return nil, err
	}
	if tenantID <= 0 || userID <= 0 {
		return nil, newTenantUserAdminError(TenantUserAdminCodeInvalidParam, 400, "无效的租户或用户ID")
	}
	qctx := targetScopedContext(ctx, tenantID)
	if _, err := s.loadTenant(qctx, tenantID); err != nil {
		return nil, err
	}
	if _, err := s.loadTenantUser(qctx, tenantID, userID); err != nil {
		return nil, err
	}
	now := time.Now()
	revocationFailures := s.revokeUserSessions(qctx, userID)
	s.recordAudit(ctx, actor, "user.admin_force_logout", tenantID, userID, withRevocationFailures(map[string]any{
		"revoked_at": now.UTC().Format(time.RFC3339),
	}, revocationFailures), 200)
	return &dto.ForceLogoutTenantUserResponse{UserID: userID, RevokedAt: now}, nil
}

// revokeUserSessions 双吊销（access + refresh）并返回失败明细；
// 吊销失败不阻断治理动作本身，但必须记录（沿用既有降权延迟窗口的处置口径）。
func (s *TenantUserAdminService) revokeUserSessions(ctx context.Context, userID int) []string {
	failures := s.revokeUserRefresh(ctx, userID)
	if err := middleware.InvalidateUserAccessTokens(ctx, userID, time.Now()); err != nil {
		failures = append(failures, "access: "+err.Error())
		s.logger.Errorw("吊销用户 access token 失败", "user_id", userID, "error", err)
	}
	return failures
}

// revokeUserRefresh 仅吊销 refresh（停用路径：access 已由 ChangeUserStatus 处理）。
func (s *TenantUserAdminService) revokeUserRefresh(ctx context.Context, userID int) []string {
	if err := middleware.InvalidateUserRefreshTokens(ctx, userID, time.Now()); err != nil {
		s.logger.Errorw("吊销用户 refresh token 失败", "user_id", userID, "error", err)
		return []string{"refresh: " + err.Error()}
	}
	return nil
}

// recordAudit 平台治理动作显式审计：
// 行归属 actor 家租户（tenant_id），target_tenant_id=目标租户，source=platform_selected；
// 一次性口令绝不入审计（payload 不含明文）。
func (s *TenantUserAdminService) recordAudit(ctx context.Context, actor TenantAdminActor, action string, tenantID, userID int, payload map[string]any, statusCode int) {
	if s.client == nil {
		return
	}
	body, _ := json.Marshal(payload)
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = tenantID
	}
	auditCtx, cancel := context.WithTimeout(targetScopedContext(ctx, rowTenant), 2*time.Second)
	defer cancel()
	create := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetTargetTenantID(tenantID).
		SetSource(middleware.AuditSourcePlatformSelected).
		SetResource("tenant_user").
		SetAction(action).
		SetPath(fmt.Sprintf("/api/v1/tenants/%d/users/%d", tenantID, userID)).
		SetMethod("POST").
		SetStatusCode(statusCode).
		SetRequestBody(string(body))
	if actor.UserID > 0 {
		create = create.SetUserID(actor.UserID)
	}
	if actor.Username != "" {
		create = create.SetActorAccount(actor.Username)
	}
	if _, err := create.Save(auditCtx); err != nil {
		s.logger.Warnw("tenant user admin audit write failed", "error", err, "action", action, "user_id", userID)
	}
}

func withRevocationFailures(payload map[string]any, failures []string) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["outcome"] = "success"
	if len(failures) > 0 {
		payload["revocation_errors"] = failures
	}
	return payload
}

// generatePolicyCompliantPassword 生成满足目标租户策略的一次性口令（TUM-D4）：
// 覆盖大写/小写/数字/特殊字符四类，长度 16（不小于策略最小值、不超过策略最大值）。
func generatePolicyCompliantPassword(policy PasswordPolicy) (string, error) {
	sets := []string{
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"abcdefghijklmnopqrstuvwxyz",
		"0123456789",
		tenantUserAdminSpecialChars,
	}
	length := tenantUserAdminPasswordLength
	if policy.MinLength > length {
		length = policy.MinLength
	}
	if policy.MaxLength > 0 && length > policy.MaxLength {
		length = policy.MaxLength
	}
	if length < policy.MinLength {
		return "", fmt.Errorf("租户密码策略长度区间非法（min=%d max=%d）", policy.MinLength, policy.MaxLength)
	}

	combined := strings.Join(sets, "")
	out := make([]byte, 0, length)
	for _, set := range sets {
		c, err := randomCharFrom(set)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < length {
		c, err := randomCharFrom(combined)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	// Fisher-Yates 洗牌（crypto/rand），避免类别位置固定。
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		k := int(j.Int64())
		out[i], out[k] = out[k], out[i]
	}
	password := string(out)
	if err := policy.Validate(password); err != nil {
		return "", fmt.Errorf("生成口令不满足租户策略: %w", err)
	}
	return password, nil
}

func randomCharFrom(charset string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	if err != nil {
		return 0, err
	}
	return charset[n.Int64()], nil
}
