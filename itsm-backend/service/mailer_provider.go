package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ErrMailNotConfigured 租户未配置 SMTP 且部署级无回退配置时的统一错误。
// 发信点应记录告警并跳过（不静默），由运维在 /admin/system-config 邮件设置中补齐。
var ErrMailNotConfigured = errors.New("邮件服务未配置：租户未设置 SMTP，且部署级未配置回退邮箱")

type mailerCacheEntry struct {
	mailer    *EmailService
	expiresAt time.Time
}

// MailerProvider 按租户解析生效的邮件服务（方案 P0-2 / SCD2 / D5）。
//
// 回退链：租户 system_configs（smtpHost 等） > 部署级 SMTP（env/config.yaml，作为 fallback）
// > 未配置（返回 ErrMailNotConfigured，调用方告警跳过）。
//
// 凭据：smtpPassword 存量密文（v1: 前缀）经 ConfigSecretBox 解密；历史明文兼容读取。
// 缓存：按租户缓存已构建的 EmailService（含限流窗口），保存配置后由失效链路清除。
type MailerProvider struct {
	resolver *TenantConfigResolver
	secrets  *ConfigSecretBox
	logger   *zap.SugaredLogger
	fallback EmailConfig

	mu    sync.RWMutex
	cache map[int]mailerCacheEntry
}

// NewMailerProvider 创建按租户邮件服务工厂；fallback 为部署级 SMTP（Host=="" 表示无回退）。
func NewMailerProvider(resolver *TenantConfigResolver, secrets *ConfigSecretBox, fallback EmailConfig, logger *zap.SugaredLogger) *MailerProvider {
	// 防御：部署级占位主机（example.com 保留域名）视为未配置，避免未配置租户的邮件
	// 被投递到假域名（历史上 config.yaml 默认 smtp.host=smtp.example.com 会触发该问题）。
	if isPlaceholderSMTPHost(fallback.Host) {
		logger.Warnw("deployment smtp host is a placeholder; fallback mailer disabled", "host", fallback.Host)
		fallback.Host = ""
	}
	return &MailerProvider{
		resolver: resolver,
		secrets:  secrets,
		logger:   logger,
		fallback: fallback,
		cache:    make(map[int]mailerCacheEntry),
	}
}

// ForTenant 返回该租户当前生效的邮件服务实例。
func (p *MailerProvider) ForTenant(ctx context.Context, tenantID int) (*EmailService, error) {
	if p == nil {
		return nil, ErrMailNotConfigured
	}
	if tenantID <= 0 {
		return p.fallbackMailer()
	}

	p.mu.RLock()
	if entry, ok := p.cache[tenantID]; ok && time.Now().Before(entry.expiresAt) {
		p.mu.RUnlock()
		return entry.mailer, nil
	}
	p.mu.RUnlock()

	mailer, err := p.build(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if p.cache == nil {
		p.cache = make(map[int]mailerCacheEntry)
	}
	p.cache[tenantID] = mailerCacheEntry{mailer: mailer, expiresAt: time.Now().Add(tenantConfigCacheTTL)}
	p.mu.Unlock()
	return mailer, nil
}

// InvalidateTenant 清除租户邮件服务缓存（配置保存/Redis 广播失效时调用）。
func (p *MailerProvider) InvalidateTenant(tenantID int) {
	if p == nil || tenantID <= 0 {
		return
	}
	p.mu.Lock()
	delete(p.cache, tenantID)
	p.mu.Unlock()
}

func (p *MailerProvider) build(ctx context.Context, tenantID int) (*EmailService, error) {
	host := strings.TrimSpace(p.resolver.GetStringOr(ctx, tenantID, "smtpHost", ""))
	if host == "" || isPlaceholderSMTPHost(host) {
		// 租户未配置（或仍是初始化占位值）→ 回退部署级 SMTP。
		return p.fallbackMailer()
	}

	port := p.resolver.GetIntOr(ctx, tenantID, "smtpPort", 465)
	username := strings.TrimSpace(p.resolver.GetStringOr(ctx, tenantID, "smtpUsername", ""))
	if isPlaceholderSMTPAddress(username) {
		username = ""
	}

	password := ""
	if stored := p.resolver.GetStringOr(ctx, tenantID, "smtpPassword", ""); stored != "" {
		decrypted, err := p.secrets.Decrypt(stored)
		if err != nil {
			// 凭据不可解密（密钥轮换/缺失）→ fail-closed：拒绝构建，避免以错误凭据反复重试。
			p.logger.Errorw("decrypt tenant smtp password failed; mailer disabled for tenant",
				"error", err, "tenant_id", tenantID)
			return nil, fmt.Errorf("SMTP 凭据不可用（解密失败），请重新保存邮件设置: %w", err)
		}
		password = decrypted
	}

	from := strings.TrimSpace(p.resolver.GetStringOr(ctx, tenantID, "emailFrom", ""))
	if from == "" || isPlaceholderSMTPAddress(from) {
		from = username
	}
	if from == "" {
		from = p.fallback.From
	}
	if from == "" {
		return nil, fmt.Errorf("%w（租户 %d 缺少发件人地址 emailFrom）", ErrMailNotConfigured, tenantID)
	}

	fromName := p.fallback.FromName
	if fromName == "" {
		fromName = "ITSM 系统通知"
	}

	ssl := p.resolver.GetBoolOr(ctx, tenantID, "smtpEnableSSL", port == 465)
	config := EmailConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		From:     from,
		FromName: fromName,
		SSL:      ssl,
	}
	p.logger.Infow("tenant mailer resolved", "tenant_id", tenantID, "host", host, "port", port, "ssl", ssl)
	return NewEmailService(config, p.logger), nil
}

func (p *MailerProvider) fallbackMailer() (*EmailService, error) {
	if p.fallback.Host == "" || isPlaceholderSMTPHost(p.fallback.Host) {
		return nil, ErrMailNotConfigured
	}
	return NewEmailService(p.fallback, p.logger), nil
}

// isPlaceholderSMTPHost 识别初始化默认配置的占位 SMTP 主机（example.com 为保留域名）。
func isPlaceholderSMTPHost(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	return lower == "smtp.example.com" || strings.HasSuffix(lower, ".example.com") || lower == "example.com"
}

func isPlaceholderSMTPAddress(addr string) bool {
	lower := strings.ToLower(strings.TrimSpace(addr))
	return lower == "noreply@example.com" || strings.HasSuffix(lower, "@example.com")
}
