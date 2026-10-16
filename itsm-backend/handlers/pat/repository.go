package pat

import (
	"context"
	"time"

	"itsm-backend/ent"
)

// Repository PAT 持久化接口。
type Repository interface {
	// Create 落库一条令牌（只存哈希与前缀）。
	Create(ctx context.Context, in CreateRow) (*ent.PersonalAccessToken, error)
	// ListOwn 本人全部令牌（created_at DESC）。
	ListOwn(ctx context.Context, userID, tenantID int) ([]*ent.PersonalAccessToken, error)
	// GetOwn 按 id 取本人令牌；非本人/不存在返回 ErrPATNotFound。
	GetOwn(ctx context.Context, userID, tenantID, tokenID int) (*ent.PersonalAccessToken, error)
	// CountActive 本人 active 令牌数。
	CountActive(ctx context.Context, userID, tenantID int) (int, error)
	// Revoke 吊销单条（幂等：已吊销返回当前行）。
	Revoke(ctx context.Context, userID, tenantID, tokenID int, reason string) (*ent.PersonalAccessToken, error)
	// RevokeAllForUser 批量吊销该用户全部 active 令牌（含跨租户——安全事件必须全覆盖），
	// 返回吊销数量。供 T8 安全事件接线调用。
	RevokeAllForUser(ctx context.Context, userID int, reason string) (int, error)
	// RevokeAllOwn 本人全部 active 令牌（限当前租户），返回吊销数量。
	RevokeAllOwn(ctx context.Context, userID, tenantID int, reason string) (int, error)
	// ClampExpiryForGrace 轮换宽限：把旧令牌 expires_at 收紧为 min(旧, t)。
	ClampExpiryForGrace(ctx context.Context, userID, tenantID, tokenID int, t time.Time, rotatedTo int) error
	// TouchUsage 节流更新使用计数（best-effort，失败不影响请求）。
	TouchUsage(ctx context.Context, tokenID int, ip string)
	// ListUsages 令牌使用历史（audit_logs by token_id）。
	ListUsages(ctx context.Context, tenantID, tokenID, page, pageSize int) ([]UsageItem, int, error)
	// ListSecurityEvents 本人安全事件（audit_logs resource=auth by user_id）。
	ListSecurityEvents(ctx context.Context, tenantID, userID, page, pageSize int, kind string) ([]SecurityEvent, int, error)
}

// CreateRow 创建行。
type CreateRow struct {
	UserID      int
	TenantID    int
	Name        string
	Description string
	TokenHash   string
	TokenPrefix string
	ExpiresAt   time.Time
}

// 领域错误（handler 映射 HTTP 状态）。
var (
	ErrPATNotFound    = &domainError{msg: "访问令牌不存在"}
	ErrPATRevoked     = &domainError{msg: "访问令牌已吊销"}
	ErrPATExpired     = &domainError{msg: "访问令牌已过期"}
	ErrPATUserInvalid = &domainError{msg: "访问令牌不可用"}
	ErrPATLimit       = &domainError{msg: "访问令牌数量已达上限，请先吊销不再使用的令牌"}
	ErrPATForbidden   = &domainError{msg: "该角色不允许创建访问令牌"}
)

// domainError 领域错误。
type domainError struct{ msg string }

func (e *domainError) Error() string { return e.msg }
