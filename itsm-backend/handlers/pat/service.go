package pat

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"itsm-backend/ent"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// PATConfigView 服务层可见配置（来自 config.PATConfig）。
type PATConfigView struct {
	Enabled          bool
	DefaultTTLDays   int
	MaxTTLDays       int
	MaxActivePerUser int
}

// DefaultPATConfigView 缺省配置（env 未配置时的安全缺省）。
func DefaultPATConfigView() PATConfigView {
	return PATConfigView{
		Enabled:          true,
		DefaultTTLDays:   90,
		MaxTTLDays:       365,
		MaxActivePerUser: 20,
	}
}

// Service PAT 业务规则（事务边界在此层）。
type Service struct {
	repo     Repository
	cfg      PATConfigView
	logger   *zap.SugaredLogger
	authzctx func(ctx context.Context, userID int) (role string, ok bool) // 可注入的平台角色判定
}

// NewService 构造。
func NewService(repo Repository, cfg PATConfigView, logger *zap.SugaredLogger) *Service {
	if cfg.DefaultTTLDays <= 0 {
		cfg.DefaultTTLDays = 90
	}
	if cfg.MaxTTLDays <= 0 {
		cfg.MaxTTLDays = 365
	}
	if cfg.MaxActivePerUser <= 0 {
		cfg.MaxActivePerUser = 20
	}
	return &Service{repo: repo, cfg: cfg, logger: logger}
}

// ErrConfigDisabled PAT 功能未启用。
var ErrConfigDisabled = errors.New("PAT disabled")

// GeneratePlaintext 生成 PAT 明文：itsm_pat_ + base64url(32B crypto/rand)（D-PAT-2）。
func GeneratePlaintext() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext := middleware.PATAuthTokenPrefix + base64.URLEncoding.EncodeToString(raw)
	prefix := plaintext
	if len(prefix) > 17 {
		prefix = prefix[:17]
	}
	return plaintext, prefix, nil
}

// List 本人令牌列表。
func (s *Service) List(ctx context.Context, userID, tenantID int) ([]*Item, error) {
	rows, err := s.repo.ListOwn(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	items := make([]*Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, toItem(row, now))
	}
	return items, nil
}

// GetOwn 本人单个令牌。
func (s *Service) GetOwn(ctx context.Context, userID, tenantID, tokenID int) (*Item, error) {
	row, err := s.repo.GetOwn(ctx, userID, tenantID, tokenID)
	if err != nil {
		return nil, err
	}
	return toItem(row, time.Now()), nil
}

// Create 创建令牌（D-PAT-8/13/14 规则收敛点）。
func (s *Service) Create(ctx context.Context, userID, tenantID int, in CreateInput, callerRole string) (*CreateResult, error) {
	if !s.cfg.Enabled {
		return nil, ErrConfigDisabled
	}
	// D-PAT-14：平台角色禁止创建（365 天全权限旁路凭据）
	if middleware.IsPlatformRole(callerRole) {
		return nil, ErrPATForbidden
	}
	if in.Name == "" || len(in.Name) > 64 {
		return nil, errors.New("令牌名称必填且不超过 64 字符")
	}
	if len(in.Description) > 255 {
		return nil, errors.New("备注不超过 255 字符")
	}

	expiresAt, err := s.resolveExpiry(in)
	if err != nil {
		return nil, err
	}

	// MaxActive 上限（check-then-insert 竞态已接受并文档化，见方案 §3.3）
	count, err := s.repo.CountActive(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if count >= s.cfg.MaxActivePerUser {
		return nil, ErrPATLimit
	}

	plaintext, prefix, err := GeneratePlaintext()
	if err != nil {
		return nil, err
	}
	row, err := s.repo.Create(ctx, CreateRow{
		UserID:      userID,
		TenantID:    tenantID,
		Name:        in.Name,
		Description: in.Description,
		TokenHash:   HashToken(plaintext),
		TokenPrefix: prefix,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &CreateResult{Token: toItem(row, time.Now()), PlaintextToken: plaintext}, nil
}

// resolveExpiry 有效期解析：ExpiresAt 与 ExpiresInDays 互斥，[1, MaxTTLDays]。
func (s *Service) resolveExpiry(in CreateInput) (time.Time, error) {
	now := time.Now()
	switch {
	case in.ExpiresAt != nil:
		t := *in.ExpiresAt
		if !t.After(now) {
			return time.Time{}, errors.New("过期时间必须晚于当前时间")
		}
		if days := int(t.Sub(now).Hours() / 24); days > s.cfg.MaxTTLDays {
			return time.Time{}, errors.New("有效期超过上限")
		}
		return t, nil
	case in.ExpiresInDays > 0:
		if in.ExpiresInDays > s.cfg.MaxTTLDays {
			return time.Time{}, errors.New("有效期超过上限")
		}
		return now.AddDate(0, 0, in.ExpiresInDays), nil
	default:
		return now.AddDate(0, 0, s.cfg.DefaultTTLDays), nil
	}
}

// Rotate 轮换（D-PAT-7）：新令牌继承名称/描述/剩余有效期；旧令牌立即吊销或宽限收紧。
func (s *Service) Rotate(ctx context.Context, userID, tenantID, tokenID, graceHours int) (*CreateResult, error) {
	if !s.cfg.Enabled {
		return nil, ErrConfigDisabled
	}
	if graceHours != 0 && graceHours != 24 {
		return nil, errors.New("gracePeriodHours 仅支持 0 或 24")
	}
	old, err := s.repo.GetOwn(ctx, userID, tenantID, tokenID)
	if err != nil {
		return nil, err
	}
	if old.RevokedAt != nil {
		return nil, ErrPATRevoked
	}
	if !old.ExpiresAt.After(time.Now()) {
		return nil, ErrPATExpired
	}

	plaintext, prefix, err := GeneratePlaintext()
	if err != nil {
		return nil, err
	}
	newRow, err := s.repo.Create(ctx, CreateRow{
		UserID:      userID,
		TenantID:    tenantID,
		Name:        old.Name,
		Description: old.Description,
		TokenHash:   HashToken(plaintext),
		TokenPrefix: prefix,
		ExpiresAt:   old.ExpiresAt, // 继承剩余有效期
	})
	if err != nil {
		return nil, err
	}

	if graceHours == 0 {
		if _, err := s.repo.Revoke(ctx, userID, tenantID, tokenID, RevokeReasonRotated); err != nil {
			return nil, err
		}
	} else {
		if err := s.repo.ClampExpiryForGrace(ctx, userID, tenantID, tokenID, time.Now().Add(time.Duration(graceHours)*time.Hour), newRow.ID); err != nil {
			return nil, err
		}
	}
	return &CreateResult{Token: toItem(newRow, time.Now()), PlaintextToken: plaintext}, nil
}

// Revoke 单令牌吊销（幂等）。
func (s *Service) Revoke(ctx context.Context, userID, tenantID, tokenID int) (*Item, error) {
	row, err := s.repo.Revoke(ctx, userID, tenantID, tokenID, RevokeReasonSelf)
	if err != nil {
		return nil, err
	}
	return toItem(row, time.Now()), nil
}

// RevokeAll 吊销本人全部 active 令牌。
func (s *Service) RevokeAll(ctx context.Context, userID, tenantID int) (int, error) {
	return s.repo.RevokeAllOwn(ctx, userID, tenantID, RevokeReasonRevokeAll)
}

// RevokeAllForUser 安全事件批量吊销（T8 接线：停用/删除/改密/离职）。
func (s *Service) RevokeAllForUser(ctx context.Context, userID int) (int, error) {
	return s.repo.RevokeAllForUser(ctx, userID, RevokeReasonSecurityEvent)
}

// Usages 令牌使用历史（分页 + 汇总）。
func (s *Service) Usages(ctx context.Context, userID, tenantID, tokenID, page, pageSize int) (*UsagePage, error) {
	// 归属校验（非本人 → 404，防枚举）
	row, err := s.repo.GetOwn(ctx, userID, tenantID, tokenID)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := s.repo.ListUsages(ctx, tenantID, tokenID, page, pageSize)
	if err != nil {
		return nil, err
	}
	return &UsagePage{
		Items: items, Total: total, Page: page, PageSize: pageSize,
		Summary: UsageSummary{
			UseCount:   row.UseCount,
			LastUsedAt: row.LastUsedAt,
			LastUsedIP: row.LastUsedIP,
		},
	}, nil
}

// SecurityEvents 本人安全事件（分页；pageSize 上限 100，与 usages 同口径）。
func (s *Service) SecurityEvents(ctx context.Context, userID, tenantID, page, pageSize int, kind string) (*EventPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if kind != "login" && kind != "token" {
		kind = "all"
	}
	items, total, err := s.repo.ListSecurityEvents(ctx, tenantID, userID, page, pageSize, kind)
	if err != nil {
		return nil, err
	}
	return &EventPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// toItem ent 行 → 展示项（永不包含明文与哈希）。
func toItem(row *ent.PersonalAccessToken, now time.Time) *Item {
	return &Item{
		ID:            row.ID,
		Name:          row.Name,
		Description:   row.Description,
		TokenPrefix:   row.TokenPrefix,
		MaskedToken:   MaskedToken(row.TokenPrefix),
		Status:        deriveStatus(row.RevokedAt, row.ExpiresAt, now),
		ExpiresAt:     row.ExpiresAt,
		LastUsedAt:    row.LastUsedAt,
		LastUsedIP:    row.LastUsedIP,
		UseCount:      row.UseCount,
		RevokedAt:     row.RevokedAt,
		RevokedReason: row.RevokedReason,
		CreatedAt:     row.CreatedAt,
	}
}
