// Package tenantquota 定义租户硬配额（limits）模型与超限错误（IP-P2-6）。
//
// 语义（权威：实施方案 §3.0-A 错误码注册表 / §6.4 平台租户管理批次）：
//   - 三个键：maxUsers / maxTicketsPerMonth / maxStorageMB（单位 MB）；
//   - 值 <= 0 或键缺省 = 不限（fail-open，保持既有行为不变）；
//   - 写入按显式模型校验：仅接受上述键、非负整数、单键不超过 maxLimitValue。
//
// 存储：tenants.quota（jsonb，NULL/空 = 不限）；读写两端共用本包，避免各处
// 自行拼装 map 造成键名漂移。
package tenantquota

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const (
	// CodeTenantQuotaExceeded 稳定错误码（实施方案 §3.0-A 注册表）。
	CodeTenantQuotaExceeded = "TENANT_QUOTA_EXCEEDED"

	// 键名（与 DTO quota 字段、审计载荷一致）。
	QuotaMaxUsers           = "maxUsers"
	QuotaMaxTicketsPerMonth = "maxTicketsPerMonth"
	QuotaMaxStorageMB       = "maxStorageMB"

	// maxLimitValue 单键上限（防御 int64 溢出与明显误配：1<<40 ≈ 1.1e12）。
	maxLimitValue = int64(1 << 40)
)

// Limits 租户硬配额（零值 = 全部不限）。
type Limits struct {
	MaxUsers           int64 `json:"maxUsers,omitempty"`
	MaxTicketsPerMonth int64 `json:"maxTicketsPerMonth,omitempty"`
	MaxStorageMB       int64 `json:"maxStorageMB,omitempty"`
}

// IsZero 报告是否全部不限。
func (l Limits) IsZero() bool { return l == Limits{} }

// ToMap 输出显式键（仅 >0 的键），用于 API 响应与审计载荷。
func (l Limits) ToMap() map[string]interface{} {
	out := map[string]interface{}{}
	if l.MaxUsers > 0 {
		out[QuotaMaxUsers] = l.MaxUsers
	}
	if l.MaxTicketsPerMonth > 0 {
		out[QuotaMaxTicketsPerMonth] = l.MaxTicketsPerMonth
	}
	if l.MaxStorageMB > 0 {
		out[QuotaMaxStorageMB] = l.MaxStorageMB
	}
	return out
}

// StorageLimitBytes 返回存储上限字节数（不限时为 0）。
func (l Limits) StorageLimitBytes() int64 {
	if l.MaxStorageMB <= 0 {
		return 0
	}
	return l.MaxStorageMB * 1024 * 1024
}

// InvalidError 配额写入非法（未知键/负值/非整数值/超上限）→ HTTP 400。
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "tenant quota invalid: " + e.Reason }

// Parse 将 API 层的 quota map 解析为 Limits。
//
//   - nil / 空 map → 零值（不限）；
//   - 未知键、负值、非整数数值、超出上限 → *InvalidError；
//   - 0 视为不限（与缺省同义），不会“冻结”租户。
func Parse(raw map[string]interface{}) (Limits, error) {
	var limits Limits
	if len(raw) == 0 {
		return limits, nil
	}
	for key, value := range raw {
		n, err := toInt64(key, value)
		if err != nil {
			return Limits{}, err
		}
		if n < 0 {
			return Limits{}, &InvalidError{Reason: fmt.Sprintf("%s 不能为负值（%d）", key, n)}
		}
		if n > maxLimitValue {
			return Limits{}, &InvalidError{Reason: fmt.Sprintf("%s 超出上限 %d（%d）", key, maxLimitValue, n)}
		}
		switch key {
		case QuotaMaxUsers:
			limits.MaxUsers = n
		case QuotaMaxTicketsPerMonth:
			limits.MaxTicketsPerMonth = n
		case QuotaMaxStorageMB:
			limits.MaxStorageMB = n
		default:
			return Limits{}, &InvalidError{Reason: fmt.Sprintf("未知配额键 %q（允许：%s/%s/%s）",
				key, QuotaMaxUsers, QuotaMaxTicketsPerMonth, QuotaMaxStorageMB)}
		}
	}
	return limits, nil
}

// toInt64 严格数值转换：拒绝字符串/布尔/对象等非数值输入，拒绝非整数浮点。
func toInt64(key string, v interface{}) (int64, error) {
	switch n := v.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case float64:
		if n != math.Trunc(n) {
			return 0, &InvalidError{Reason: fmt.Sprintf("%s 必须为整数（%v）", key, n)}
		}
		return int64(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, &InvalidError{Reason: fmt.Sprintf("%s 必须为整数（%v）", key, n)}
		}
		return i, nil
	default:
		return 0, &InvalidError{Reason: fmt.Sprintf("%s 必须为整数（%T）", key, v)}
	}
}

// ExceededError 配额超限（HTTP 422 + reasonCode=TENANT_QUOTA_EXCEEDED）。
type ExceededError struct {
	Quota string // 触发的配额键
	Limit int64  // 上限（存储为字节数）
	Used  int64  // 当前用量（含本次新增前的量）
}

func (e *ExceededError) Error() string {
	return fmt.Sprintf("tenant quota exceeded: %s (limit=%d, used=%d)", e.Quota, e.Limit, e.Used)
}

// AsExceeded 提取超限错误；非超限错误返回 ok=false。
func AsExceeded(err error) (*ExceededError, bool) {
	var target *ExceededError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// CheckUsers 校验“再建一个用户”是否超限（含本次新增）。
func (l Limits) CheckUsers(used int64) *ExceededError {
	if l.MaxUsers > 0 && used+1 > l.MaxUsers {
		return &ExceededError{Quota: QuotaMaxUsers, Limit: l.MaxUsers, Used: used}
	}
	return nil
}

// CheckTicketsThisMonth 校验“再建一张当月工单”是否超限（含本次新增）。
func (l Limits) CheckTicketsThisMonth(used int64) *ExceededError {
	if l.MaxTicketsPerMonth > 0 && used+1 > l.MaxTicketsPerMonth {
		return &ExceededError{Quota: QuotaMaxTicketsPerMonth, Limit: l.MaxTicketsPerMonth, Used: used}
	}
	return nil
}

// CheckStorage 校验“再写入 addBytes 字节”是否超出存储上限（含本次新增）。
func (l Limits) CheckStorage(usedBytes, addBytes int64) *ExceededError {
	limit := l.StorageLimitBytes()
	if limit > 0 && usedBytes+addBytes > limit {
		return &ExceededError{Quota: QuotaMaxStorageMB, Limit: limit, Used: usedBytes}
	}
	return nil
}
