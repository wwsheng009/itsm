package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type fakeTenantConfigInvalidator struct {
	calls []int
}

func (f *fakeTenantConfigInvalidator) PublishTenantConfigInvalidation(_ context.Context, tenantID int) error {
	f.calls = append(f.calls, tenantID)
	return nil
}

func createResolverTestConfig(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, key, value string) int {
	t.Helper()
	cfg, err := client.SystemConfig.Create().
		SetKey(key).
		SetValue(value).
		SetValueType("string").
		SetCategory("email").
		SetTenantID(tenantID).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
	return cfg.ID
}

func TestTenantConfigResolver_TenantIsolation(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	createResolverTestConfig(t, ctx, client, 1, "smtpHost", "smtp.tenant-a.example")
	createResolverTestConfig(t, ctx, client, 2, "smtpHost", "smtp.tenant-b.example")

	resolver := NewTenantConfigResolver(client, logger)

	valueA, ok := resolver.Get(ctx, 1, "smtpHost")
	require.True(t, ok)
	require.Equal(t, "smtp.tenant-a.example", valueA)

	valueB, ok := resolver.Get(ctx, 2, "smtpHost")
	require.True(t, ok)
	require.Equal(t, "smtp.tenant-b.example", valueB)

	// 不存在的键与租户：未配置语义（false），不跨租户泄漏。
	_, ok = resolver.Get(ctx, 1, "emailFrom")
	require.False(t, ok)
	_, ok = resolver.Get(ctx, 3, "smtpHost")
	require.False(t, ok)
	_, ok = resolver.Get(ctx, 0, "smtpHost")
	require.False(t, ok)
}

func TestTenantConfigResolver_CacheAndInvalidate(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	cfgID := createResolverTestConfig(t, ctx, client, 1, "smtpHost", "smtp.old.example")
	resolver := NewTenantConfigResolver(client, logger)

	value, ok := resolver.Get(ctx, 1, "smtpHost")
	require.True(t, ok)
	require.Equal(t, "smtp.old.example", value)

	// 直接改库：缓存未失效时仍读到旧值（缓存生效证据）。
	_, err := client.SystemConfig.UpdateOneID(cfgID).SetValue("smtp.new.example").Save(ctx)
	require.NoError(t, err)
	value, _ = resolver.Get(ctx, 1, "smtpHost")
	require.Equal(t, "smtp.old.example", value)

	// 本地失效后立即读到新值。
	resolver.InvalidateLocal(1)
	value, _ = resolver.Get(ctx, 1, "smtpHost")
	require.Equal(t, "smtp.new.example", value)
}

func TestTenantConfigResolver_FallbackAccessors(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	createResolverTestConfig(t, ctx, client, 1, "smtpPort", "2525")
	createResolverTestConfig(t, ctx, client, 1, "smtpEnableSSL", "false")
	createResolverTestConfig(t, ctx, client, 1, "smtpHost", "   ")
	createResolverTestConfig(t, ctx, client, 1, "dirtyPort", "not-a-number")

	resolver := NewTenantConfigResolver(client, logger)

	require.Equal(t, 2525, resolver.GetIntOr(ctx, 1, "smtpPort", 465))
	require.False(t, resolver.GetBoolOr(ctx, 1, "smtpEnableSSL", true))
	// 空串视为未配置 → 回退。
	require.Equal(t, "fallback-host", resolver.GetStringOr(ctx, 1, "smtpHost", "fallback-host"))
	// 脏值 → 回退。
	require.Equal(t, 465, resolver.GetIntOr(ctx, 1, "dirtyPort", 465))
	require.Equal(t, 465, resolver.GetIntOr(ctx, 1, "missingPort", 465))
	require.True(t, resolver.GetBoolOr(ctx, 1, "missingSSL", true))
}

func TestTenantConfigResolver_InvalidatePublishes(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	resolver := NewTenantConfigResolver(client, logger)
	publisher := &fakeTenantConfigInvalidator{}
	resolver.SetInvalidator(publisher)

	resolver.Invalidate(ctx, 7)
	require.Equal(t, []int{7}, publisher.calls)

	// 非法租户号不发布。
	resolver.Invalidate(ctx, 0)
	require.Equal(t, []int{7}, publisher.calls)
}
