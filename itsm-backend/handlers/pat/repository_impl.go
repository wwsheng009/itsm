package pat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/personalaccesstoken"
	"itsm-backend/ent/predicate"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// Repository ent 实现。
type entRepository struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewRepository 构造 ent 仓储。
func NewRepository(client *ent.Client, logger *zap.SugaredLogger) Repository {
	return &entRepository{client: client, logger: logger}
}

// resolver 认证期解析器（实现 middleware.PATAuthenticator / PATAuthenticatorV2）。
type resolver struct {
	client *ent.Client
	logger *zap.SugaredLogger
	repo   Repository // TouchUsage 复用仓储
}

// NewResolver 构造 PAT 认证解析器（装配到 middleware.ConfigurePersonalAccessTokens）。
func NewResolver(client *ent.Client, logger *zap.SugaredLogger) middleware.PATAuthenticator {
	return &resolver{client: client, logger: logger, repo: NewRepository(client, logger)}
}

// HashToken sha256 hex（D-PAT-3：高熵随机值 + 唯一索引 O(1) 点查）。
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Authenticate 认证一步到位（middleware.PATAuthenticatorV2）：
// 解析 → 单令牌限流判定（R-3c）→ best-effort 使用计数 touch（60s 节流，D-PAT-9）。
// 返回 (identity, allowed, err)；err 非 nil 时 identity 为 nil。
func (r *resolver) Authenticate(ctx context.Context, plaintext, clientIP string) (*middleware.PATIdentity, bool, error) {
	identity, err := r.Resolve(ctx, plaintext)
	if err != nil {
		return nil, false, err
	}
	if !middleware.AllowPATRequest(ctx, identity.TokenID) {
		return nil, false, nil
	}
	// 节流 touch：失败不影响请求（TouchUsage 内部 warn）。
	// 同步执行（已由 ShouldTouchLastUsed 节流至每令牌窗口期一次，开销可忽略；
	// 不用 goroutine 以避免 sqlite 内存库测试与后续请求的锁竞争）。
	if middleware.ShouldTouchLastUsed(ctx, identity.TokenID) {
		r.repo.TouchUsage(ctx, identity.TokenID, clientIP)
	}
	return identity, true, nil
}

// Resolve 认证期点查（middleware.PATAuthenticator）。
//
// 租户上下文尚未建立（认证先于租户解析，TenantMiddleware 在 AuthMiddleware 之后挂载）；
// RLS enforce 驱动对无租户上下文语句 fail-closed，system bypass 是唯一豁免通道，
// 因此使用**带审计的** tenantctx.SystemContext（每次调用留 zap 日志可追踪）。
// 查询按 token_hash 唯一索引点查，旁路范围仅这一条认证查询。
// 同类先例：bootstrap_tokens（RLS 010）同样在租户上下文前校验一次性令牌。
func (r *resolver) Resolve(ctx context.Context, plaintext string) (*middleware.PATIdentity, error) {
	hash := HashToken(plaintext)

	qctx := tenantctx.SystemContext(ctx, "pat_auth:resolve", "authenticate PAT before tenant resolution")
	row, err := r.client.PersonalAccessToken.Query().
		Where(personalaccesstoken.TokenHashEQ(hash)).
		Only(qctx)
	if err != nil {
		return nil, ErrPATNotFound
	}
	if row.RevokedAt != nil {
		return nil, ErrPATRevoked
	}
	if !row.ExpiresAt.After(time.Now()) {
		return nil, ErrPATExpired
	}

	u, err := r.client.User.Get(qctx, row.UserID)
	if err != nil {
		return nil, ErrPATUserInvalid
	}
	if !u.Active {
		// 停用 → 认证层 401（兜底，防 R3 批量吊销竞态；等价于 JWT 停用即 401）
		return nil, ErrPATUserInvalid
	}
	role := string(u.Role)
	if mapped := middleware.EffectiveTokenRole(string(u.Role), string(u.MspRole)); mapped != "" {
		role = mapped
	}
	return &middleware.PATIdentity{
		TokenID:     row.ID,
		TokenPrefix: row.TokenPrefix,
		UserID:      u.ID,
		Username:    u.Username,
		Role:        role,
		TenantID:    row.TenantID,
	}, nil
}

// ---------- Repository ----------

func (r *entRepository) Create(ctx context.Context, in CreateRow) (*ent.PersonalAccessToken, error) {
	return r.client.PersonalAccessToken.Create().
		SetUserID(in.UserID).
		SetTenantID(in.TenantID).
		SetName(in.Name).
		SetDescription(in.Description).
		SetTokenHash(in.TokenHash).
		SetTokenPrefix(in.TokenPrefix).
		SetExpiresAt(in.ExpiresAt).
		Save(ctx)
}

func (r *entRepository) ListOwn(ctx context.Context, userID, tenantID int) ([]*ent.PersonalAccessToken, error) {
	return r.client.PersonalAccessToken.Query().
		Where(
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.TenantIDEQ(tenantID),
		).
		Order(ent.Desc(personalaccesstoken.FieldCreatedAt)).
		All(ctx)
}

func (r *entRepository) GetOwn(ctx context.Context, userID, tenantID, tokenID int) (*ent.PersonalAccessToken, error) {
	row, err := r.client.PersonalAccessToken.Query().
		Where(
			personalaccesstoken.IDEQ(tokenID),
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.TenantIDEQ(tenantID),
		).
		Only(ctx)
	if err != nil {
		return nil, ErrPATNotFound
	}
	return row, nil
}

func (r *entRepository) CountActive(ctx context.Context, userID, tenantID int) (int, error) {
	return r.client.PersonalAccessToken.Query().
		Where(
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.TenantIDEQ(tenantID),
			personalaccesstoken.RevokedAtIsNil(),
			personalaccesstoken.ExpiresAtGT(time.Now()),
		).
		Count(ctx)
}

func (r *entRepository) Revoke(ctx context.Context, userID, tenantID, tokenID int, reason string) (*ent.PersonalAccessToken, error) {
	row, err := r.GetOwn(ctx, userID, tenantID, tokenID)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		return row, nil // 幂等
	}
	now := time.Now()
	return r.client.PersonalAccessToken.UpdateOneID(row.ID).
		SetRevokedAt(now).
		SetRevokedReason(reason).
		Save(ctx)
}

func (r *entRepository) RevokeAllOwn(ctx context.Context, userID, tenantID int, reason string) (int, error) {
	n, err := r.client.PersonalAccessToken.Update().
		Where(
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.TenantIDEQ(tenantID),
			personalaccesstoken.RevokedAtIsNil(),
		).
		SetRevokedAt(time.Now()).
		SetRevokedReason(reason).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// RevokeAllForUser 安全事件批量吊销：跨租户全覆盖（用户可能有多租户令牌），
// 绕过 RLS 租户作用域（治理动作，与 InvalidateUserAccessTokens 同级语义）。
func (r *entRepository) RevokeAllForUser(ctx context.Context, userID int, reason string) (int, error) {
	qctx := tenantctx.SystemContext(ctx, "pat_security_event", "revoke all PATs on account security event")
	n, err := r.client.PersonalAccessToken.Update().
		Where(
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.RevokedAtIsNil(),
		).
		SetRevokedAt(time.Now()).
		SetRevokedReason(reason).
		Save(qctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		r.logger.Infow("PAT revoked by security event", "user_id", userID, "count", n, "reason", reason)
	}
	return n, nil
}

func (r *entRepository) ClampExpiryForGrace(ctx context.Context, userID, tenantID, tokenID int, t time.Time, rotatedTo int) error {
	row, err := r.GetOwn(ctx, userID, tenantID, tokenID)
	if err != nil {
		return err
	}
	expires := row.ExpiresAt
	if t.Before(expires) {
		expires = t
	}
	upd := r.client.PersonalAccessToken.UpdateOneID(row.ID).SetExpiresAt(expires)
	if rotatedTo > 0 {
		upd = upd.SetRotatedToTokenID(rotatedTo)
	}
	return upd.Exec(ctx)
}

// TouchUsage 节流更新（调用方已过 ShouldTouchLastUsed 闸门；best-effort）。
func (r *entRepository) TouchUsage(ctx context.Context, tokenID int, ip string) {
	qctx := tenantctx.SystemContext(ctx, "pat_usage_touch", "update PAT last-used metadata")
	err := r.client.PersonalAccessToken.UpdateOneID(tokenID).
		AddUseCount(1).
		SetLastUsedAt(time.Now()).
		SetLastUsedIP(ip).
		Exec(qctx)
	if err != nil {
		r.logger.Warnw("PAT touch usage failed", "token_id", tokenID, "error", err)
	}
}

// ListUsages 令牌使用历史（audit_logs by tenant_id + token_id）。
func (r *entRepository) ListUsages(ctx context.Context, tenantID, tokenID, page, pageSize int) ([]UsageItem, int, error) {
	base := r.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(tenantID),
			auditlog.TokenIDEQ(tokenID),
		)
	total, err := base.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := base.
		Order(ent.Desc(auditlog.FieldCreatedAt)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]UsageItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, UsageItem{
			ID:         row.ID,
			At:         row.CreatedAt,
			Action:     row.Action,
			Method:     row.Method,
			Path:       row.Path,
			StatusCode: row.StatusCode,
			IP:         row.IP,
			Source:     row.Source,
		})
	}
	return items, total, nil
}

// ListSecurityEvents 本人安全事件（audit_logs resource=auth by user_id）。
// kind: all | login | token
func (r *entRepository) ListSecurityEvents(ctx context.Context, tenantID, userID, page, pageSize int, kind string) ([]SecurityEvent, int, error) {
	preds := []predicate.AuditLog{
		auditlog.TenantIDEQ(tenantID),
		auditlog.UserIDEQ(userID),
		auditlog.ResourceEQ("auth"),
	}
	switch kind {
	case "login":
		preds = append(preds, auditlog.SourceEQ("login"))
	case "token":
		preds = append(preds, auditlog.SourceEQ(middleware.PATAuthSourcePAT))
	}
	base := r.client.AuditLog.Query().Where(preds...)
	total, err := base.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := base.
		Order(ent.Desc(auditlog.FieldCreatedAt)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]SecurityEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, SecurityEvent{
			ID:         row.ID,
			At:         row.CreatedAt,
			Action:     row.Action,
			Source:     row.Source,
			Path:       row.Path,
			Method:     row.Method,
			StatusCode: row.StatusCode,
			IP:         row.IP,
			TokenID:    row.TokenID,
		})
	}
	return items, total, nil
}

// RevokeAllForUserByClient 安全事件批量吊销的独立入口（T8 接线）：
// 供 service/user_service（停用/删除/改密）、service/tenant_user_admin、service/offboarding
// 在既有 InvalidateUserAccessTokens 调用点**并列**调用；不要求持有 pat.Service。
// 系统旁路 + 跨租户全覆盖（治理动作，与 InvalidateUserAccessTokens 同级语义）。
func RevokeAllForUserByClient(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, userID int) {
	if client == nil || userID <= 0 {
		return
	}
	qctx := tenantctx.SystemContext(ctx, "pat_security_event", "revoke all PATs on account security event")
	n, err := client.PersonalAccessToken.Update().
		Where(
			personalaccesstoken.UserIDEQ(userID),
			personalaccesstoken.RevokedAtIsNil(),
		).
		SetRevokedAt(time.Now()).
		SetRevokedReason(RevokeReasonSecurityEvent).
		Save(qctx)
	if err != nil {
		if logger != nil {
			logger.Warnw("PAT security-event revocation failed", "user_id", userID, "error", err)
		}
		return
	}
	if n > 0 && logger != nil {
		logger.Infow("PAT revoked by security event", "user_id", userID, "count", n)
	}
}

// RevokeAllForUserReason 安全事件原因常量转发（供 service/user_service 等调用点使用）。
func SecurityEventReason() string { return RevokeReasonSecurityEvent }
