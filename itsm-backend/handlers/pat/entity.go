package pat

import (
	"time"
)

// 个人访问令牌（PAT）领域实体与 DTO。
// HTTP/JSON 字段一律 camelCase（仓库数据结构约定）；数据库字段由 ent schema 管理（snake_case）。

// Item 令牌列表/详情项（永不包含明文与哈希）。
type Item struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	TokenPrefix   string     `json:"tokenPrefix"`
	MaskedToken   string     `json:"maskedToken"`
	Status        string     `json:"status"` // active | revoked | expired
	ExpiresAt     time.Time  `json:"expiresAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedIP    string     `json:"lastUsedIp,omitempty"`
	UseCount      int        `json:"useCount"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	RevokedReason string     `json:"revokedReason,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// CreateInput 创建令牌输入。
type CreateInput struct {
	Name          string
	Description   string
	ExpiresInDays int        // 0 = 未传
	ExpiresAt     *time.Time // 与 ExpiresInDays 互斥
}

// CreateResult 创建/轮换结果：明文仅此一次返回。
type CreateResult struct {
	Token          *Item  `json:"token"`
	PlaintextToken string `json:"plaintextToken"`
}

// UsageItem 令牌使用历史行（来自 audit_logs）。
type UsageItem struct {
	ID         int       `json:"id"`
	At         time.Time `json:"at"`
	Action     string    `json:"action"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	StatusCode int       `json:"statusCode"`
	IP         string    `json:"ip"`
	Source     string    `json:"source"`
}

// UsagePage 使用历史分页 + 汇总。
type UsagePage struct {
	Items    []UsageItem  `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Summary  UsageSummary `json:"summary"`
}

// UsageSummary 使用汇总。
type UsageSummary struct {
	UseCount    int        `json:"useCount"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedIP  string     `json:"lastUsedIp,omitempty"`
	FirstUsedAt *time.Time `json:"firstUsedAt,omitempty"`
}

// SecurityEvent 本人安全事件行（登录/切换/令牌事件，来自 audit_logs resource=auth）。
type SecurityEvent struct {
	ID          int       `json:"id"`
	At          time.Time `json:"at"`
	Action      string    `json:"action"`
	Source      string    `json:"source"`
	Path        string    `json:"path"`
	Method      string    `json:"method"`
	StatusCode  int       `json:"statusCode"`
	IP          string    `json:"ip"`
	TokenID     int       `json:"tokenId,omitempty"`
	TokenPrefix string    `json:"tokenPrefix,omitempty"`
}

// EventPage 安全事件分页。
type EventPage struct {
	Items    []SecurityEvent `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// 令牌状态派生。
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
	StatusExpired = "expired"
)

// 吊销原因。
const (
	RevokeReasonSelf          = "self"
	RevokeReasonRotated       = "rotated"
	RevokeReasonRevokeAll     = "revoke_all"
	RevokeReasonSecurityEvent = "security_event"
)

// deriveStatus 由 revoked_at / expires_at 派生展示状态（不落库）。
func deriveStatus(revokedAt *time.Time, expiresAt time.Time, now time.Time) string {
	if revokedAt != nil {
		return StatusRevoked
	}
	if !expiresAt.After(now) {
		return StatusExpired
	}
	return StatusActive
}

// MaskedToken 前缀 + 固定掩码（不含任何真实后缀字符）。
func MaskedToken(prefix string) string {
	return prefix + "****"
}
