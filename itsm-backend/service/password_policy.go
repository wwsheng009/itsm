package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/ent/systemconfig"
)

// passwordPolicyCacheTTL 密码策略缓存的 TTL，仅作兜底；
// 配置写入路径会主动失效缓存，使改动立即可见。
const passwordPolicyCacheTTL = 5 * time.Minute

// PasswordPolicy 描述租户当前生效的密码强度策略。
// 由 system_configs 表驱动（admin/system-config 保存的 passwordMinLength 等键），
// 未注入配置服务或读取失败时回退 DefaultPasswordPolicy。
type PasswordPolicy struct {
	MinLength           int
	MaxLength           int
	RequireUppercase    bool
	RequireLowercase    bool
	RequireNumbers      bool
	RequireSpecialChars bool
}

// DefaultPasswordPolicy 返回系统默认的密码策略，
// 与 InitDefaultConfigs 中声明的默认值保持一致。
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:           8,
		MaxLength:           128,
		RequireUppercase:    true,
		RequireLowercase:    true,
		RequireNumbers:      true,
		RequireSpecialChars: false,
	}
}

// Validate 检查密码是否满足当前策略。
func (p PasswordPolicy) Validate(password string) error {
	if len(password) < p.MinLength {
		return fmt.Errorf("密码长度不少于 %d 位", p.MinLength)
	}
	if len(password) > p.MaxLength {
		return fmt.Errorf("密码长度不超过 %d 位", p.MaxLength)
	}
	if p.RequireUppercase && !hasUpper(password) {
		return fmt.Errorf("密码必须包含大写字母")
	}
	if p.RequireLowercase && !hasLower(password) {
		return fmt.Errorf("密码必须包含小写字母")
	}
	if p.RequireNumbers && !hasNumber(password) {
		return fmt.Errorf("密码必须包含数字")
	}
	if p.RequireSpecialChars && !hasSpecial(password) {
		return fmt.Errorf("密码必须包含特殊字符")
	}
	return nil
}

// Description 返回人类可读的策略描述，供前端渲染提示。
func (p PasswordPolicy) Description() string {
	parts := []string{fmt.Sprintf("至少 %d 位", p.MinLength)}
	if p.RequireUppercase {
		parts = append(parts, "大写字母")
	}
	if p.RequireLowercase {
		parts = append(parts, "小写字母")
	}
	if p.RequireNumbers {
		parts = append(parts, "数字")
	}
	if p.RequireSpecialChars {
		parts = append(parts, "特殊字符")
	}
	return strings.Join(parts, "，")
}

// passwordPolicyCacheEntry 缓存项：搭载一次性读取的完整策略 + 过期时间。
type passwordPolicyCacheEntry struct {
	policy    PasswordPolicy
	expiresAt time.Time
}

// GetPasswordPolicy 返回租户当前生效的密码策略。
// 优先读缓存；缓存未命中或过期时从 system_configs 读取并回填。
// 读取失败（含数据库错误）时回退为默认策略，保证密码校验不因配置异常而放行。
func (s *SystemConfigService) GetPasswordPolicy(ctx context.Context, tenantID int) PasswordPolicy {
	// 读缓存
	s.policyMu.RLock()
	if entry, ok := s.policyCache[tenantID]; ok && time.Now().Before(entry.expiresAt) {
		s.policyMu.RUnlock()
		return entry.policy
	}
	s.policyMu.RUnlock()

	policy := DefaultPasswordPolicy()

	// 从 system_configs 批量读取密码策略相关键
	keys := []string{
		"passwordMinLength",
		"passwordRequireUppercase",
		"passwordRequireLowercase",
		"passwordRequireNumbers",
		"passwordRequireSpecialChars",
	}
	configs, err := s.client.SystemConfig.Query().
		Where(systemconfig.KeyIn(keys...), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		All(ctx)
	if err != nil {
		s.logger.Warnf("读取密码策略配置失败，回退默认策略: %v", err)
	} else {
		for _, cfg := range configs {
			switch cfg.Key {
			case "passwordMinLength":
				if n, e := strconv.Atoi(cfg.Value); e == nil && n > 0 {
					policy.MinLength = n
				}
			case "passwordRequireUppercase":
				if b, e := strconv.ParseBool(cfg.Value); e == nil {
					policy.RequireUppercase = b
				}
			case "passwordRequireLowercase":
				if b, e := strconv.ParseBool(cfg.Value); e == nil {
					policy.RequireLowercase = b
				}
			case "passwordRequireNumbers":
				if b, e := strconv.ParseBool(cfg.Value); e == nil {
					policy.RequireNumbers = b
				}
			case "passwordRequireSpecialChars":
				if b, e := strconv.ParseBool(cfg.Value); e == nil {
					policy.RequireSpecialChars = b
				}
			}
		}
	}

	// 回填缓存
	s.policyMu.Lock()
	if s.policyCache == nil {
		s.policyCache = make(map[int]passwordPolicyCacheEntry)
	}
	s.policyCache[tenantID] = passwordPolicyCacheEntry{
		policy:    policy,
		expiresAt: time.Now().Add(passwordPolicyCacheTTL),
	}
	s.policyMu.Unlock()

	return policy
}

// InvalidatePasswordPolicy 清除指定租户的密码策略缓存，
// 使系统配置保存（含 InitDefaultConfigs）后立即可见。
func (s *SystemConfigService) InvalidatePasswordPolicy(tenantID int) {
	s.policyMu.Lock()
	delete(s.policyCache, tenantID)
	s.policyMu.Unlock()
}

// --- 字符集检查辅助 ---

func hasUpper(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func hasLower(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

func hasNumber(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func hasSpecial(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return true
		}
	}
	return false
}
