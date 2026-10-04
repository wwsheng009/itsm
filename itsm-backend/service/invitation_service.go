package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/invitation"
	"itsm-backend/ent/role"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	"itsm-backend/pkg/mspguard"
	"itsm-backend/pkg/tenantmode"
)

// =============================================================================
// IP-P1-4：邀请生命周期（实施方案 §4.0-C 契约冻结）
//
// 生命周期：创建（角色白名单 + msp_role 通道校验）→ 投递（SMTP 或 inviteUrl）→
// 接受（一次性：pending ∧ now()<expires_at，事务内置 accepted + 建号/绑定 membership）→
// 撤销（仅 pending）→ 过期按到期判定；重发 = 新 token 且旧 token 失效。
//
// 安全：token 128-bit 随机 + sha256 存储（原始 token 不落库）、日志脱敏、
// super_admin/sysadmin 不可被邀请；接受路径使用显式建号通道（channel=invite）。
// =============================================================================

// 邀请错误码（稳定契约；handler 依 Code/Status 映射响应）。
const (
	InvitationCodeForbidden         = "INVITATION_FORBIDDEN"
	InvitationCodeRoleNotGrantable  = "INVITATION_ROLE_NOT_GRANTABLE"
	InvitationCodeMSPRoleNotAllowed = "INVITATION_MSP_ROLE_NOT_ALLOWED"
	InvitationCodeTenantNotFound    = "INVITATION_TENANT_NOT_FOUND"
	InvitationCodeTenantSuspended   = "INVITATION_TENANT_SUSPENDED"
	InvitationCodeNotFound          = "INVITATION_NOT_FOUND"
	InvitationCodeExpired           = "INVITATION_EXPIRED"
	InvitationCodeRevoked           = "INVITATION_REVOKED"
	InvitationCodeAlreadyAccepted   = "INVITATION_ALREADY_ACCEPTED"
	InvitationCodeEmailExists       = "INVITATION_EMAIL_EXISTS"
	InvitationCodePasswordWeak      = "INVITATION_PASSWORD_WEAK"
	InvitationCodeUserNotFound      = "INVITATION_USER_NOT_FOUND"
	InvitationCodeEmailMismatch     = "INVITATION_EMAIL_MISMATCH"
)

// InvitationError 邀请域的稳定拒绝类型。
type InvitationError struct {
	Code    string
	Message string
	Status  int
}

func (e *InvitationError) Error() string { return e.Code + ": " + e.Message }

func newInvitationError(code string, status int, format string, args ...interface{}) *InvitationError {
	return &InvitationError{Code: code, Message: fmt.Sprintf(format, args...), Status: status}
}

// AsInvitationError 提取邀请域错误码；非邀请类错误 ok=false。
func AsInvitationError(err error) (*InvitationError, bool) {
	var target *InvitationError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// InvitationActor 邀请人身份快照（来自 JWT claims）。
type InvitationActor struct {
	UserID       int
	HomeTenantID int
	Role         string
	MSPRole      string
	Username     string
}

// CreateInvitationRequest 创建邀请请求。
type CreateInvitationRequest struct {
	TenantID     int
	Email        string
	RoleID       int
	MSPRole      string
	TargetUserID int
}

// CreateInvitationResult 创建邀请结果（SMTP 未配置时 inviteUrl 为唯一投递通道）。
type CreateInvitationResult struct {
	Invitation *ent.Invitation
	Token      string
	InviteURL  string
	EmailSent  bool
	ExpiresAt  time.Time
}

// AcceptInvitationRequest 接受邀请（落地页设置密码；已存在账号绑定时密码可空）。
type AcceptInvitationRequest struct {
	Password string
	Name     string
}

// AcceptInvitationResult 接受邀请结果。
type AcceptInvitationResult struct {
	User       *ent.User
	Membership *ent.UserTenantMembership
}

// InvitationInfo 邀请最小回显（GET 落地页；邮箱脱敏，不泄露 token 归属细节）。
type InvitationInfo struct {
	Status        string    `json:"status"`
	EmailMasked   string    `json:"emailMasked"`
	TenantName    string    `json:"tenantName"`
	RoleCode      string    `json:"roleCode"`
	MSPRole       string    `json:"mspRole,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
	HasTargetUser bool      `json:"hasTargetUser"`
}

// InvitationMailer 邀请投递接口；平台 SMTP 未配置时保持 nil，返回 emailSent=false + inviteUrl。
type InvitationMailer interface {
	SendInvitation(ctx context.Context, inv *ent.Invitation, token, inviteURL string) (bool, error)
}

// InvitationService 邀请生命周期服务。
type InvitationService struct {
	client     *ent.Client
	users      *UserService
	logger     *zap.SugaredLogger
	checker    *mspguard.Checker
	mailer     InvitationMailer
	appBaseURL string
	ttl        time.Duration
}

// NewInvitationService 构造邀请服务。
// 配置：INVITATION_TTL_HOURS（默认 72）、APP_BASE_URL（默认 http://localhost:5173）。
func NewInvitationService(client *ent.Client, users *UserService, logger *zap.SugaredLogger) *InvitationService {
	ttl := 72 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("INVITATION_TTL_HOURS")); raw != "" {
		if hours, err := strconv.Atoi(raw); err == nil && hours > 0 {
			ttl = time.Duration(hours) * time.Hour
		}
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
	if base == "" {
		base = "http://localhost:5173"
	}
	return &InvitationService{
		client:     client,
		users:      users,
		logger:     logger,
		checker:    mspguard.New(client),
		appBaseURL: base,
		ttl:        ttl,
	}
}

// SetMailer 注入邮件投递实现（可选；SMTP 未配置时保持 nil）。
func (s *InvitationService) SetMailer(m InvitationMailer) { s.mailer = m }

// Create 创建邀请：inviter 授权（platform/tenant/msp 三通道 + rank 上限）→
// 角色白名单 → 重发旧邀请失效 → 落库（token 仅哈希）→ 审计 user.invite。
func (s *InvitationService) Create(ctx context.Context, actor InvitationActor, req *CreateInvitationRequest) (*CreateInvitationResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("invitation service unavailable")
	}
	if req == nil {
		return nil, newInvitationError("BAD_REQUEST", http.StatusBadRequest, "请求体不能为空")
	}

	target, err := s.client.Tenant.Query().Where(tenant.IDEQ(req.TenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeTenantNotFound, http.StatusNotFound, "目标租户不存在: %d", req.TenantID)
		}
		return nil, fmt.Errorf("查询目标租户失败: %w", err)
	}
	if target.Status != "active" {
		return nil, newInvitationError(InvitationCodeTenantSuspended, http.StatusForbidden,
			"目标租户不可用（status=%s）", target.Status)
	}

	channel, inviterRank, err := s.authorizeInviter(ctx, actor, target)
	if err != nil {
		return nil, err
	}
	// 写路径租户收窄：先 WithTenantID(target)，非本租户通道再叠加显式建号 bypass（RLS/写守卫）。
	writeCtx := tenantctx.WithTenantID(ctx, target.ID)
	if channel != ProvisionChannelTenant {
		actorLabel := strings.TrimSpace(actor.Username)
		if actorLabel == "" {
			actorLabel = fmt.Sprintf("user:%d", actor.UserID)
		}
		writeCtx = tenantctx.WithProvisioningBypass(writeCtx, actorLabel, channel, target.ID)
	}

	roleEntity, err := s.client.Role.Query().
		Where(role.IDEQ(req.RoleID), role.TenantIDEQ(target.ID)).
		Only(writeCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeRoleNotGrantable, http.StatusUnprocessableEntity,
				"角色不存在或不属于目标租户: %d", req.RoleID)
		}
		return nil, fmt.Errorf("查询角色失败: %w", err)
	}
	if err := s.validateInvitationRole(roleEntity, inviterRank); err != nil {
		return nil, err
	}

	mspRole, err := s.validateInvitationMSPRole(actor, channel, target, req.MSPRole)
	if err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validInvitationEmail(email) {
		return nil, newInvitationError("BAD_REQUEST", http.StatusBadRequest, "邮箱格式无效")
	}

	var targetUserID int
	if req.TargetUserID > 0 {
		targetUser, err := s.client.User.Query().Where(user.IDEQ(req.TargetUserID)).Only(writeCtx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, newInvitationError(InvitationCodeUserNotFound, http.StatusNotFound,
					"绑定账号不存在: %d", req.TargetUserID)
			}
			return nil, fmt.Errorf("查询绑定账号失败: %w", err)
		}
		if strings.ToLower(targetUser.Email) != email {
			return nil, newInvitationError(InvitationCodeEmailMismatch, http.StatusUnprocessableEntity,
				"绑定账号邮箱与邀请邮箱不一致")
		}
		targetUserID = targetUser.ID
	}

	// 重发语义：同租户同邮箱的 pending 邀请先置 revoked（再插入新行）。
	if _, err := s.client.Invitation.Update().
		Where(
			invitation.TenantIDEQ(target.ID),
			invitation.EmailEQ(email),
			invitation.StatusEQ(invitation.StatusPending),
		).
		SetStatus(invitation.StatusRevoked).
		SetRevokedAt(time.Now()).
		Save(writeCtx); err != nil {
		return nil, fmt.Errorf("撤销旧邀请失败: %w", err)
	}

	rawToken, tokenHash, err := newInvitationToken()
	if err != nil {
		return nil, fmt.Errorf("生成邀请 token 失败: %w", err)
	}
	expiresAt := time.Now().Add(s.ttl)

	builder := s.client.Invitation.Create().
		SetTenantID(target.ID).
		SetTokenHash(tokenHash).
		SetEmail(email).
		SetRoleID(roleEntity.ID).
		SetInvitedBy(actor.UserID).
		SetStatus(invitation.StatusPending).
		SetExpiresAt(expiresAt)
	if mspRole != "" {
		builder = builder.SetMspRole(mspRole)
	}
	if targetUserID > 0 {
		builder = builder.SetTargetUserID(targetUserID)
	}
	created, err := builder.Save(writeCtx)
	if err != nil {
		return nil, fmt.Errorf("创建邀请失败: %w", err)
	}

	inviteURL := s.appBaseURL + "/invite/" + rawToken
	emailSent := false
	if s.mailer != nil {
		sent, sendErr := s.mailer.SendInvitation(ctx, created, rawToken, inviteURL)
		if sendErr != nil {
			if s.logger != nil {
				s.logger.Warnw("invitation email send failed", "error", sendErr, "invitation_id", created.ID)
			}
		} else {
			emailSent = sent
		}
	}

	s.recordInvitationAudit(ctx, actor, channel, target.ID, "user.invite", http.StatusCreated, map[string]any{
		"invitation_id": created.ID,
		"channel":       channel,
		"role_id":       roleEntity.ID,
		"msp_role":      mspRole,
		"email_sent":    emailSent,
	})

	if s.logger != nil {
		s.logger.Infow("invitation created",
			"invitation_id", created.ID, "tenant_id", target.ID, "channel", channel,
			"invited_by", actor.UserID, "role_id", roleEntity.ID, "email_sent", emailSent)
	}
	return &CreateInvitationResult{
		Invitation: created,
		Token:      rawToken,
		InviteURL:  inviteURL,
		EmailSent:  emailSent,
		ExpiresAt:  expiresAt,
	}, nil
}

// newInvitationToken 生成 128-bit 随机 token（hex 32 chars）并返回 sha256 hex 哈希。
func newInvitationToken() (string, string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

// validInvitationEmail 轻量邮箱校验（handler binding 已有 email 校验；服务层兜底）。
func validInvitationEmail(email string) bool {
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1 && !strings.ContainsAny(email, " \t\r\n")
}

// recordInvitationAudit 审计 user.invite / user.invite_accept（行归属 actor 家租户，target 指向目标租户）。
// ctx 必须携带与行 tenant_id 一致的租户作用域（enforce 下 fail-closed；accept 走带租户的 acceptCtx）。
func (s *InvitationService) recordInvitationAudit(ctx context.Context, actor InvitationActor, channel string, targetTenantID int, action string, status int, payload map[string]any) {
	if s == nil || s.client == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["channel"] = channel
	payload["actor_user_id"] = actor.UserID
	payload["actor_home_tenant_id"] = actor.HomeTenantID
	payload["target_tenant_id"] = targetTenantID
	body, _ := json.Marshal(payload)

	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = targetTenantID
	}
	source := middleware.AuditSourceHeader
	if channel == ProvisionChannelPlatform {
		source = middleware.AuditSourcePlatformSelected
	}
	if channel == "invite" {
		source = middleware.AuditSourceSystem
	}

	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	path := "/api/v1/users/invitations"
	method := http.MethodPost
	if action == "user.invite_accept" {
		path = "/api/v1/auth/invitations/:token/accept"
	}
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(targetTenantID).
		SetSource(source).
		SetResource("user").
		SetAction(action).
		SetPath(path).
		SetMethod(method).
		SetStatusCode(status).
		SetRequestBody(string(body)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("invitation audit write failed", "error", err, "action", action, "target_tenant_id", targetTenantID)
	}
}

// authorizeInviter 邀请人授权 + rank：platform=5；tenant=调用方有效 rank；
// msp=provider 管理员 + 目标客户有有效 allocation（rank 4）。
func (s *InvitationService) authorizeInviter(ctx context.Context, actor InvitationActor, target *ent.Tenant) (string, int, error) {
	if isPlatformActorRole(actor.Role) {
		return ProvisionChannelPlatform, 5, nil
	}
	if actor.HomeTenantID > 0 && actor.HomeTenantID == target.ID {
		rank := serviceRoleRank(actor.Role)
		if mapped := serviceRoleRank(mappedMSPRole(actor.MSPRole)); mapped > rank {
			rank = mapped
		}
		return ProvisionChannelTenant, rank, nil
	}
	if isProviderAdminInvitationActor(actor) && tenantmode.IsCustomerTenantType(string(target.Type)) {
		if err := s.checker.CanAccessCustomer(ctx, actor.UserID, target.ID); err != nil {
			if ae, ok := mspguard.AsAccessError(err); ok {
				status := http.StatusForbidden
				if ae.Code == mspguard.CodeCustomerTenantNotFound {
					status = http.StatusNotFound
				}
				return "", 0, newInvitationError(ae.Code, status, "%s", ae.Message)
			}
			return "", 0, err
		}
		return ProvisionChannelMSP, 4, nil
	}
	return "", 0, newInvitationError(InvitationCodeForbidden, http.StatusForbidden,
		"无权限在该租户发起邀请（需要平台角色、该租户内管理员身份或目标客户的有效 MSP 分配）")
}

// isProviderAdminInvitationActor provider 管理员判定（与建号通道同口径，适配 InvitationActor）。
func isProviderAdminInvitationActor(actor InvitationActor) bool {
	switch strings.ToLower(strings.TrimSpace(actor.MSPRole)) {
	case "provider_admin":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(actor.Role)) {
	case "msp_admin", "msp_manager":
		return true
	}
	return false
}

// validateInvitationRole 角色白名单：平台角色不可被邀请；rank 不得高于邀请人上限。
func (s *InvitationService) validateInvitationRole(roleEntity *ent.Role, inviterRank int) error {
	code := strings.ToLower(strings.TrimSpace(roleEntity.Code))
	if code == "" {
		return newInvitationError(InvitationCodeRoleNotGrantable, http.StatusUnprocessableEntity, "角色 code 为空")
	}
	switch code {
	case "super_admin", "sysadmin":
		return newInvitationError(InvitationCodeRoleNotGrantable, http.StatusUnprocessableEntity,
			"平台角色不可被邀请: %s", code)
	}
	if rank := serviceRoleRank(code); rank > inviterRank {
		return newInvitationError(InvitationCodeRoleNotGrantable, http.StatusUnprocessableEntity,
			"无权限邀请高于当前通道上限的角色: %s", code)
	}
	return nil
}

// validateInvitationMSPRole msp_role 白名单与通道校验（同建号通道语义）。
func (s *InvitationService) validateInvitationMSPRole(actor InvitationActor, channel string, target *ent.Tenant, raw string) (string, error) {
	mspRole := strings.ToLower(strings.TrimSpace(raw))
	if mspRole == "" {
		return "", nil
	}
	if mspRole != "provider_admin" && mspRole != "provider_agent" {
		return "", newInvitationError(InvitationCodeMSPRoleNotAllowed, http.StatusUnprocessableEntity,
			"mspRole=%s 不允许写入（白名单 provider_admin/provider_agent）", raw)
	}
	allowed := channel == ProvisionChannelPlatform || channel == ProvisionChannelMSP ||
		(channel == ProvisionChannelTenant &&
			tenantmode.IsMSPProviderTenantType(string(target.Type)) &&
			actor.HomeTenantID == target.ID)
	if !allowed {
		return "", newInvitationError(InvitationCodeMSPRoleNotAllowed, http.StatusUnprocessableEntity,
			"当前通道不允许设置 mspRole（仅平台通道、MSP 通道或 provider 本租户）")
	}
	return mspRole, nil
}

// Accept 接受邀请：一次性消费（pending ∧ 未过期）→ 事务内建号（或绑定已有账号）+
// 建 membership（source=invite）+ 置 accepted；审计 user.invite_accept。
//
// 接受路径使用 channel=invite 的显式建号通道（ADR-004:A2：邀请属 P1 通道）。
func (s *InvitationService) Accept(ctx context.Context, token string, req *AcceptInvitationRequest) (*AcceptInvitationResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("invitation service unavailable")
	}
	// 预认证面（token 即凭证，跨租户查询）：RLS enforce 下以平台作用域执行，
	// 否则 driver fail-closed（"enforce mode requires tenant_id"）→ 邀请接受 500（C2b 实锤）。
	ctx = tenantctx.WithSystemBypass(ctx)
	if req == nil {
		req = &AcceptInvitationRequest{}
	}

	inv, err := s.findInvitationByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	switch inv.Status {
	case invitation.StatusAccepted:
		return nil, newInvitationError(InvitationCodeAlreadyAccepted, http.StatusConflict, "邀请已被接受")
	case invitation.StatusRevoked:
		return nil, newInvitationError(InvitationCodeRevoked, http.StatusGone, "邀请已被撤销")
	case invitation.StatusExpired:
		return nil, newInvitationError(InvitationCodeExpired, http.StatusGone, "邀请已过期")
	}
	now := time.Now()
	if now.After(inv.ExpiresAt) {
		_, _ = s.client.Invitation.UpdateOneID(inv.ID).
			SetStatus(invitation.StatusExpired).
			Save(tenantctx.WithTenantID(ctx, inv.TenantID))
		return nil, newInvitationError(InvitationCodeExpired, http.StatusGone, "邀请已过期")
	}

	readCtx := tenantctx.WithTenantID(ctx, inv.TenantID)

	target, err := s.client.Tenant.Query().Where(tenant.IDEQ(inv.TenantID)).Only(readCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeTenantNotFound, http.StatusNotFound, "目标租户不存在")
		}
		return nil, fmt.Errorf("查询目标租户失败: %w", err)
	}
	if target.Status != "active" {
		return nil, newInvitationError(InvitationCodeTenantSuspended, http.StatusForbidden,
			"目标租户不可用（status=%s）", target.Status)
	}

	roleEntity, err := s.client.Role.Query().
		Where(role.IDEQ(inv.RoleID), role.TenantIDEQ(inv.TenantID)).
		Only(readCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeRoleNotGrantable, http.StatusUnprocessableEntity, "邀请角色已失效")
		}
		return nil, fmt.Errorf("查询邀请角色失败: %w", err)
	}

	var existingUser *ent.User
	if inv.TargetUserID != nil && *inv.TargetUserID > 0 {
		existingUser, err = s.client.User.Query().Where(user.IDEQ(*inv.TargetUserID)).Only(readCtx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, newInvitationError(InvitationCodeUserNotFound, http.StatusNotFound, "绑定账号不存在")
			}
			return nil, fmt.Errorf("查询绑定账号失败: %w", err)
		}
		if strings.ToLower(existingUser.Email) != strings.ToLower(inv.Email) {
			return nil, newInvitationError(InvitationCodeEmailMismatch, http.StatusUnprocessableEntity,
				"绑定账号邮箱与邀请邮箱不一致")
		}
	} else {
		exists, err := s.client.User.Query().Where(user.EmailEQ(inv.Email)).Exist(readCtx)
		if err != nil {
			return nil, fmt.Errorf("检查邮箱失败: %w", err)
		}
		if exists {
			return nil, newInvitationError(InvitationCodeEmailExists, http.StatusConflict,
				"该邮箱已存在账号；请由管理员绑定后重发邀请")
		}
	}

	newUser := existingUser == nil
	if newUser {
		if err := s.users.PasswordPolicy(readCtx, inv.TenantID).Validate(req.Password); err != nil {
			return nil, newInvitationError(InvitationCodePasswordWeak, http.StatusUnprocessableEntity, "%s", err.Error())
		}
	} else if strings.TrimSpace(req.Password) != "" {
		if err := s.users.PasswordPolicy(readCtx, inv.TenantID).Validate(req.Password); err != nil {
			return nil, newInvitationError(InvitationCodePasswordWeak, http.StatusUnprocessableEntity, "%s", err.Error())
		}
	}

	// 显式建号通道：先 WithTenantID(target) 再 WithProvisioningBypass(channel=invite)。
	acceptCtx := tenantctx.WithTenantID(ctx, inv.TenantID)
	acceptCtx = tenantctx.WithProvisioningBypass(acceptCtx, fmt.Sprintf("invite:%d", inv.ID), "invite", inv.TenantID)

	tx, err := s.client.Tx(acceptCtx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	createdOrBound := existingUser
	userID := 0
	if newUser {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("密码加密失败: %w", err)
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = usernameFromEmail(inv.Email)
		}
		username, err := s.uniqueUsername(acceptCtx, usernameFromEmail(inv.Email))
		if err != nil {
			return nil, err
		}
		builder := tx.User.Create().
			SetUsername(username).
			SetEmail(inv.Email).
			SetName(name).
			SetPasswordHash(string(passwordHash)).
			SetActive(true).
			SetTenantID(inv.TenantID).
			SetRole(user.Role(userRoleForCode(roleEntity.Code)))
		if inv.MspRole != nil && strings.TrimSpace(*inv.MspRole) != "" {
			mspRole := user.MspRole(strings.ToLower(strings.TrimSpace(*inv.MspRole)))
			builder = builder.SetNillableMspRole(&mspRole)
		}
		createdOrBound, err = builder.Save(acceptCtx)
		if err != nil {
			return nil, fmt.Errorf("创建邀请用户失败: %w", err)
		}
		userID = createdOrBound.ID
		if _, err := tx.User.Update().
			Where(user.IDEQ(userID)).
			AddRoleIDs(roleEntity.ID).
			Save(acceptCtx); err != nil {
			return nil, fmt.Errorf("写入用户角色边失败: %w", err)
		}
	} else {
		userID = createdOrBound.ID
		hasRole, err := createdOrBound.QueryRoles().Where(role.IDEQ(roleEntity.ID)).Exist(acceptCtx)
		if err != nil {
			return nil, fmt.Errorf("查询用户角色边失败: %w", err)
		}
		if !hasRole {
			if _, err := tx.User.Update().
				Where(user.IDEQ(userID)).
				AddRoleIDs(roleEntity.ID).
				Save(acceptCtx); err != nil {
				return nil, fmt.Errorf("写入用户角色边失败: %w", err)
			}
		}
	}

	liveMemberships, err := tx.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserIDEQ(userID),
			usertenantmembership.DeletedAtIsNil(),
		).
		Count(acceptCtx)
	if err != nil {
		return nil, fmt.Errorf("查询成员身份失败: %w", err)
	}
	accountKind := usertenantmembership.AccountKindCustomer
	if tenantmode.IsMSPProviderTenantType(string(target.Type)) {
		accountKind = usertenantmembership.AccountKindProvider
	}
	membershipBuilder := tx.UserTenantMembership.Create().
		SetUserID(userID).
		SetTenantID(inv.TenantID).
		SetAccountKind(accountKind).
		SetSource(usertenantmembership.SourceInvite).
		SetRoleID(roleEntity.ID).
		SetInvitedBy(inv.InvitedBy).
		SetJoinedAt(now).
		SetStatus(usertenantmembership.StatusActive).
		SetIsDefault(liveMemberships == 0)
	if inv.MspRole != nil && strings.TrimSpace(*inv.MspRole) != "" {
		membershipBuilder = membershipBuilder.SetMspRole(strings.TrimSpace(*inv.MspRole))
	}
	membership, err := membershipBuilder.Save(acceptCtx)
	if err != nil {
		return nil, fmt.Errorf("创建成员身份失败: %w", err)
	}

	if _, err := tx.Invitation.UpdateOneID(inv.ID).
		SetStatus(invitation.StatusAccepted).
		SetAcceptedAt(now).
		Save(acceptCtx); err != nil {
		return nil, fmt.Errorf("更新邀请状态失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	auditActor := InvitationActor{UserID: userID, HomeTenantID: inv.TenantID, Username: createdOrBound.Username}
	s.recordInvitationAudit(acceptCtx, auditActor, "invite", inv.TenantID, "user.invite_accept", http.StatusCreated, map[string]any{
		"invitation_id": inv.ID,
		"user_id":       userID,
		"bound":         !newUser,
	})

	if s.logger != nil {
		s.logger.Infow("invitation accepted",
			"invitation_id", inv.ID, "tenant_id", inv.TenantID, "user_id", userID, "bound", !newUser)
	}
	return &AcceptInvitationResult{User: createdOrBound, Membership: membership}, nil
}

// Revoke 撤销邀请：仅 pending；行级授权与创建同口径（platform/tenant/msp）。
func (s *InvitationService) Revoke(ctx context.Context, actor InvitationActor, invitationID int) (*ent.Invitation, error) {
	inv, err := s.client.Invitation.Query().Where(invitation.IDEQ(invitationID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeNotFound, http.StatusNotFound, "邀请不存在")
		}
		return nil, fmt.Errorf("查询邀请失败: %w", err)
	}
	target, err := s.client.Tenant.Query().Where(tenant.IDEQ(inv.TenantID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询目标租户失败: %w", err)
	}
	channel, _, err := s.authorizeInviter(ctx, actor, target)
	if err != nil {
		return nil, err
	}
	revokeCtx := tenantctx.WithTenantID(ctx, inv.TenantID)
	if channel != ProvisionChannelTenant {
		actorLabel := strings.TrimSpace(actor.Username)
		if actorLabel == "" {
			actorLabel = fmt.Sprintf("user:%d", actor.UserID)
		}
		revokeCtx = tenantctx.WithProvisioningBypass(revokeCtx, actorLabel, channel, inv.TenantID)
	}
	switch inv.Status {
	case invitation.StatusAccepted:
		return nil, newInvitationError(InvitationCodeAlreadyAccepted, http.StatusConflict, "邀请已被接受，无法撤销")
	case invitation.StatusRevoked:
		return nil, newInvitationError(InvitationCodeRevoked, http.StatusGone, "邀请已被撤销")
	case invitation.StatusExpired:
		return nil, newInvitationError(InvitationCodeExpired, http.StatusGone, "邀请已过期")
	}
	now := time.Now()
	updated, err := s.client.Invitation.UpdateOneID(inv.ID).
		SetStatus(invitation.StatusRevoked).
		SetRevokedAt(now).
		Save(revokeCtx)
	if err != nil {
		return nil, fmt.Errorf("撤销邀请失败: %w", err)
	}
	if s.logger != nil {
		s.logger.Infow("invitation revoked", "invitation_id", inv.ID, "tenant_id", inv.TenantID, "actor_user_id", actor.UserID)
	}
	return updated, nil
}

// Inspect 邀请最小回显（GET 落地页）：邮箱脱敏；过期即置 expired。
func (s *InvitationService) Inspect(ctx context.Context, token string) (*InvitationInfo, error) {
	// 预认证面：与 Accept 同口径（system 作用域）；否则 enforce 下 500（C2b 实锤）。
	ctx = tenantctx.WithSystemBypass(ctx)
	inv, err := s.findInvitationByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if inv.Status == invitation.StatusPending && time.Now().After(inv.ExpiresAt) {
		if updated, uerr := s.client.Invitation.UpdateOneID(inv.ID).
			SetStatus(invitation.StatusExpired).
			Save(ctx); uerr == nil {
			inv = updated
		}
	}
	readCtx := tenantctx.WithTenantID(ctx, inv.TenantID)
	tenantName := ""
	if t, err := s.client.Tenant.Query().Where(tenant.IDEQ(inv.TenantID)).Only(readCtx); err == nil {
		tenantName = t.Name
	}
	roleCode := ""
	if r, err := s.client.Role.Query().Where(role.IDEQ(inv.RoleID)).Only(readCtx); err == nil {
		roleCode = r.Code
	}
	info := &InvitationInfo{
		Status:        string(inv.Status),
		EmailMasked:   maskEmail(inv.Email),
		TenantName:    tenantName,
		RoleCode:      roleCode,
		ExpiresAt:     inv.ExpiresAt,
		HasTargetUser: inv.TargetUserID != nil && *inv.TargetUserID > 0,
	}
	if inv.MspRole != nil {
		info.MSPRole = *inv.MspRole
	}
	return info, nil
}

// findInvitationByToken 按 sha256(token) 查找邀请（未命中 → INVITATION_NOT_FOUND）。
func (s *InvitationService) findInvitationByToken(ctx context.Context, token string) (*ent.Invitation, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, newInvitationError(InvitationCodeNotFound, http.StatusNotFound, "邀请不存在或 token 无效")
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	inv, err := s.client.Invitation.Query().Where(invitation.TokenHashEQ(hash)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newInvitationError(InvitationCodeNotFound, http.StatusNotFound, "邀请不存在或 token 无效")
		}
		return nil, fmt.Errorf("查询邀请失败: %w", err)
	}
	return inv, nil
}

// uniqueUsername 由邮箱前缀派生用户名；冲突时追加随机后缀（用户名全局唯一）。
func (s *InvitationService) uniqueUsername(ctx context.Context, base string) (string, error) {
	if len(base) < 3 {
		base = base + strings.Repeat("0", 3-len(base))
	}
	if len(base) > 40 {
		base = base[:40]
	}
	candidate := base
	for i := 0; i < 3; i++ {
		exists, err := s.client.User.Query().Where(user.UsernameEQ(candidate)).Exist(ctx)
		if err != nil {
			return "", fmt.Errorf("检查用户名失败: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := make([]byte, 2)
		if _, err := rand.Read(suffix); err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%s", base, hex.EncodeToString(suffix))
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano()%100000), nil
}

// usernameFromEmail 邮箱前缀归一（保留字母数字与 . _ -）。
func usernameFromEmail(email string) string {
	local := email
	if at := strings.Index(email, "@"); at > 0 {
		local = email[:at]
	}
	var b strings.Builder
	for _, r := range local {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	out := b.String()
	if out == "" {
		out = "user"
	}
	return out
}

// userRoleForCode 将角色 code 映射到 users.role 枚举（roles 表词表 ⊃ users.role 枚举；
// 非枚举值回退 end_user，权限事实源仍是 membership.role_id → role_permissions）。
func userRoleForCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "super_admin", "admin", "manager", "it_admin", "security_admin", "sysadmin",
		"agent", "technician", "security":
		return strings.ToLower(strings.TrimSpace(code))
	default:
		return "end_user"
	}
}

// maskEmail 邮箱脱敏：a***@example.com。
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
