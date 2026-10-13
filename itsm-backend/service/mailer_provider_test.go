package service

import (
	"context"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestMailerProvider_TenantOverridesFallback(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("cd", 32))
	t.Setenv("SERVER_ENV", "development")

	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	secrets := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	encrypted, err := secrets.Encrypt("tenant-smtp-secret")
	require.NoError(t, err)

	createResolverTestConfig(t, ctx, client, 1, "smtpHost", "127.0.0.1")
	createResolverTestConfig(t, ctx, client, 1, "smtpPort", "2525")
	createResolverTestConfig(t, ctx, client, 1, "smtpUsername", "mailer@tenant-a.example")
	createResolverTestConfig(t, ctx, client, 1, "smtpPassword", encrypted)
	createResolverTestConfig(t, ctx, client, 1, "smtpEnableSSL", "false")
	createResolverTestConfig(t, ctx, client, 1, "emailFrom", "noreply@tenant-a.example")

	resolver := NewTenantConfigResolver(client, logger)
	provider := NewMailerProvider(resolver, secrets, EmailConfig{
		Host: "fallback.example", Port: 25, From: "fallback@example.com", FromName: "Fallback",
	}, logger)

	mailer, err := provider.ForTenant(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", mailer.config.Host)
	require.Equal(t, 2525, mailer.config.Port)
	require.Equal(t, "mailer@tenant-a.example", mailer.config.Username)
	require.Equal(t, "tenant-smtp-secret", mailer.config.Password)
	require.False(t, mailer.config.SSL)
	require.Equal(t, "noreply@tenant-a.example", mailer.config.From)
}

func TestMailerProvider_FallbackAndUnconfigured(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("ef", 32))
	t.Setenv("SERVER_ENV", "development")

	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	// 租户 2：完全未配置 → 回退部署级。
	// 租户 3：初始化占位值（example.com）→ 同样回退部署级。
	createResolverTestConfig(t, ctx, client, 3, "smtpHost", "smtp.example.com")
	createResolverTestConfig(t, ctx, client, 3, "emailFrom", "noreply@example.com")

	resolver := NewTenantConfigResolver(client, logger)
	secrets := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	provider := NewMailerProvider(resolver, secrets, EmailConfig{
		Host: "fallback.example", Port: 587, From: "fallback@example.com", SSL: false,
	}, logger)

	for _, tenantID := range []int{2, 3} {
		mailer, err := provider.ForTenant(ctx, tenantID)
		require.NoError(t, err, "tenant %d should fall back", tenantID)
		require.Equal(t, "fallback.example", mailer.config.Host)
		require.Equal(t, 587, mailer.config.Port)
	}

	// 无部署级回退且租户未配置 → 统一错误（调用方告警跳过）。
	noFallback := NewMailerProvider(resolver, secrets, EmailConfig{}, logger)
	_, err := noFallback.ForTenant(ctx, 2)
	require.ErrorIs(t, err, ErrMailNotConfigured)

	// 占位发件人 + 空用户名 → 仍缺发件人时给出未配置错误。
	createResolverTestConfig(t, ctx, client, 4, "smtpHost", "smtp.tenant-d.example")
	_, err = noFallback.ForTenant(ctx, 4)
	require.ErrorIs(t, err, ErrMailNotConfigured)
}

func TestMailerProvider_InvalidateTenantRebuilds(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("12", 32))
	t.Setenv("SERVER_ENV", "development")

	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	cfgID := createResolverTestConfig(t, ctx, client, 5, "smtpHost", "smtp.old.example")
	createResolverTestConfig(t, ctx, client, 5, "emailFrom", "ops@tenant-e.example")

	resolver := NewTenantConfigResolver(client, logger)
	secrets := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	provider := NewMailerProvider(resolver, secrets, EmailConfig{}, logger)

	mailer, err := provider.ForTenant(ctx, 5)
	require.NoError(t, err)
	require.Equal(t, "smtp.old.example", mailer.config.Host)

	// 改库 + 失效链路（解析器本地失效 + Mailer 缓存失效，与 bootstrap 回调一致）。
	_, err = client.SystemConfig.UpdateOneID(cfgID).SetValue("smtp.new.example").Save(ctx)
	require.NoError(t, err)
	resolver.InvalidateLocal(5)
	provider.InvalidateTenant(5)

	mailer, err = provider.ForTenant(ctx, 5)
	require.NoError(t, err)
	require.Equal(t, "smtp.new.example", mailer.config.Host)
}

func TestMailerProvider_FailClosedOnUndecryptableSecret(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("34", 32))
	t.Setenv("SERVER_ENV", "development")

	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	// 用另一个密钥产生的密文：当前密钥无法解密 → 拒绝构建（fail-closed）。
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("56", 32))
	other := NewConfigSecretBoxFromEnv(logger, "jwt")
	foreignCipher, err := other.Encrypt("secret")
	require.NoError(t, err)
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("34", 32))

	createResolverTestConfig(t, ctx, client, 6, "smtpHost", "smtp.tenant-f.example")
	createResolverTestConfig(t, ctx, client, 6, "emailFrom", "ops@tenant-f.example")
	createResolverTestConfig(t, ctx, client, 6, "smtpPassword", foreignCipher)

	resolver := NewTenantConfigResolver(client, logger)
	secrets := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	provider := NewMailerProvider(resolver, secrets, EmailConfig{}, logger)

	_, err = provider.ForTenant(ctx, 6)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SMTP 凭据不可用")
}

// 部署级占位主机（config.yaml 默认 smtp.example.com）必须视为未配置：
// 否则未配置租户的邮件会被投递到假域名（238 演示环境实测回归）。
func TestMailerProvider_PlaceholderFallbackDisabled(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("78", 32))
	t.Setenv("SERVER_ENV", "development")

	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	resolver := NewTenantConfigResolver(client, logger)
	secrets := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	provider := NewMailerProvider(resolver, secrets, EmailConfig{
		Host: "smtp.example.com", Port: 587, From: "noreply@example.com",
	}, logger)

	_, err := provider.ForTenant(ctx, 7)
	require.ErrorIs(t, err, ErrMailNotConfigured)
}
