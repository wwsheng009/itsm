package service_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"itsm-backend/config"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/seeder"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/service"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// 外部测试包（service_test）：pkg/seeder 反向 import service，只有外部测试包
// 才允许 import 一个又 import 被测包的依赖（in-package 会报 import cycle not allowed in test）。

var tenantProvisioningDBCounter int64

func tenantProvisioningTestDSN() string {
	return fmt.Sprintf("file:tenant_provisioning_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&tenantProvisioningDBCounter, 1))
}

// newTenantProvisioningFixture 建默认模板（SeedProduction）+ 目标 saas_customer 租户，
// 返回已装配的 TenantProvisioningService（真实 seeder，经结构化接口注入）。
func newTenantProvisioningFixture(t *testing.T) (*service.TenantProvisioningService, *ent.Client, int) {
	t.Helper()
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("SEED_USER1_PASSWORD", "user123")
	t.Setenv("SEED_SECURITY1_PASSWORD", "sec123")
	t.Setenv("BOOTSTRAP_ADMIN_MUST_CHANGE_PASSWORD", "true")

	client := enttest.Open(t, "sqlite3", tenantProvisioningTestDSN())
	t.Cleanup(func() {
		client.Close()
	})

	ctx := context.Background()
	cfg := &config.Config{
		Deployment: config.DeploymentConfig{Mode: tenantmode.DeploymentModePrivate},
	}
	templateSeeder := seeder.NewSeeder(client, zap.NewNop().Sugar(), cfg)
	require.NoError(t, templateSeeder.SeedProduction(ctx))

	target, err := client.Tenant.Create().
		SetName("Acme").
		SetCode("acme").
		SetType(tenant.TypeSaasCustomer).
		Save(ctx)
	require.NoError(t, err)

	svc := service.NewTenantProvisioningService(client, templateSeeder, zap.NewNop().Sugar())
	return svc, client, target.ID
}

func readinessCounts(resp *dto.TenantReadinessResponse) map[string]int {
	counts := make(map[string]int, len(resp.Items))
	for _, item := range resp.Items {
		counts[item.Key] = item.Count
	}
	return counts
}

func TestTenantProvisioningProvisionReadinessAndIdempotency(t *testing.T) {
	svc, _, tenantID := newTenantProvisioningFixture(t)
	ctx := context.Background()

	resp, err := svc.Provision(ctx, tenantID, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, tenantID, resp.TenantID)
	assert.True(t, resp.Ready, "provision 完成后 ready 必须为 true")
	assert.Equal(t, seeder.CurrentTenantTemplateVersion, resp.TemplateVersion)
	assert.Len(t, resp.Items, 7)
	for _, item := range resp.Items {
		assert.True(t, item.Required)
	}
	counts := readinessCounts(resp)
	assert.Positive(t, counts["roles"])
	assert.Positive(t, counts["menus"])
	assert.Equal(t, 0, resp.BootstrapAdmins)

	// 幂等：重复供给不报错、不产生重复数据（逐项计数不变）。
	repeated, err := svc.Provision(ctx, tenantID, seeder.CurrentTenantTemplateVersion)
	require.NoError(t, err)
	assert.True(t, repeated.Ready)
	assert.Equal(t, counts, readinessCounts(repeated))

	// readiness 只读端点与 provision 返回同口径。
	readiness, err := svc.Readiness(ctx, tenantID)
	require.NoError(t, err)
	assert.True(t, readiness.Ready)
	assert.Equal(t, seeder.CurrentTenantTemplateVersion, readiness.TemplateVersion)
	assert.Equal(t, counts, readinessCounts(readiness))
}

func TestTenantProvisioningCreateBootstrapAdminGeneratedPassword(t *testing.T) {
	svc, _, tenantID := newTenantProvisioningFixture(t)
	ctx := context.Background()
	_, err := svc.Provision(ctx, tenantID, "")
	require.NoError(t, err)

	// 未传 password（nil 请求体）→ 服务端生成 16 位强密码并仅本次回传。
	resp, err := svc.CreateBootstrapAdmin(ctx, tenantID, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Generated)
	assert.True(t, resp.MustChangePassword)
	assert.Len(t, resp.Password, 16)
	assert.GreaterOrEqual(t, len(resp.Password), 12)
	assert.True(t, strings.ContainsAny(resp.Password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"), "missing uppercase")
	assert.True(t, strings.ContainsAny(resp.Password, "abcdefghijklmnopqrstuvwxyz"), "missing lowercase")
	assert.True(t, strings.ContainsAny(resp.Password, "0123456789"), "missing digit")
	assert.Equal(t, "admin-acme", resp.Username)
	assert.Equal(t, "admin-acme@bootstrap.local", resp.Email)
	assert.Positive(t, resp.UserID)

	// 重复创建 → sentinel ErrBootstrapAdminExists（handler 据此映射 409）。
	_, err = svc.CreateBootstrapAdmin(ctx, tenantID, &dto.BootstrapAdminRequest{Password: "another-strong-password"})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrBootstrapAdminExists)

	readiness, err := svc.Readiness(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, 1, readiness.BootstrapAdmins)
}

func TestTenantProvisioningCreateBootstrapAdminWithProvidedPassword(t *testing.T) {
	svc, _, tenantID := newTenantProvisioningFixture(t)
	ctx := context.Background()
	_, err := svc.Provision(ctx, tenantID, "")
	require.NoError(t, err)

	resp, err := svc.CreateBootstrapAdmin(ctx, tenantID, &dto.BootstrapAdminRequest{
		Password: "provided-password-123",
		Username: "custom-admin",
		Email:    "custom-admin@example.com",
	})
	require.NoError(t, err)
	assert.False(t, resp.Generated)
	assert.Empty(t, resp.Password, "调用方传入的密码不得回显")
	assert.Equal(t, "custom-admin", resp.Username)
	assert.Equal(t, "custom-admin@example.com", resp.Email)
	assert.True(t, resp.MustChangePassword)
}

func TestTenantProvisioningRejectsUnknownTenantAndUnsupportedVersion(t *testing.T) {
	svc, _, tenantID := newTenantProvisioningFixture(t)
	ctx := context.Background()

	_, err := svc.Provision(ctx, 999999, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrTenantNotFound)

	_, err = svc.Provision(ctx, tenantID, "9.9.9")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrUnsupportedTenantTemplateVersion)

	_, err = svc.Readiness(ctx, 999999)
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrTenantNotFound)

	_, err = svc.CreateBootstrapAdmin(ctx, 999999, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrTenantNotFound)
}
