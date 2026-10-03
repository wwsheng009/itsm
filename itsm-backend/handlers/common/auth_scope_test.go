package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
)

const authScopeTestSecret = "auth-scope-test-secret"

// newAuthScopeFixture 构造 handlers/common Service（含 repo/client）与 home 用户。
func newAuthScopeFixture(t *testing.T) (*ent.Client, *Service, *ent.Tenant, *ent.User, context.Context) {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:common_scope_%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_"))
	client := enttest.Open(t, "sqlite3", dsn)
	logger := zaptest.NewLogger(t).Sugar()
	svc := NewService(NewEntRepository(client), authScopeTestSecret, logger, client)

	home := client.Tenant.Create().
		SetName("Home").SetCode("home-1").SetStatus("active").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").SetEmail("alice@example.com").SetName("Alice").
		SetPasswordHash("hash").SetRole("end_user").SetActive(true).SetTenantID(home.ID).
		SaveX(ctx)
	return client, svc, home, u, ctx
}

// TestRefreshToken_PreservesScope 锁定 IP-P0-6：refresh 按 claims.TenantID 重签（作用域保持）。
func TestRefreshToken_PreservesScope(t *testing.T) {
	client, svc, home, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	tok, err := middleware.GenerateRefreshTokenWithSource(u.ID, u.Username, string(u.Role), home.ID, "home", authScopeTestSecret, time.Hour)
	require.NoError(t, err)

	res, err := svc.RefreshToken(ctx, tok)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Tenant)
	assert.Equal(t, home.ID, res.Tenant.ID)
	require.NotNil(t, res.TenantSelection)
	assert.Equal(t, "home", res.TenantSelection.Mode)

	claims, err := middleware.ValidateAccessToken(res.AccessToken, authScopeTestSecret)
	require.NoError(t, err)
	assert.Equal(t, home.ID, claims.TenantID)
	assert.Equal(t, "home", claims.TenantSource)
}

// TestRefreshToken_ProviderAllocationScopeKept 锁定：切换后的客户作用域在 refresh 后保持。
func TestRefreshToken_ProviderAllocationScopeKept(t *testing.T) {
	client, svc, _, _, ctx := newAuthScopeFixture(t)
	defer client.Close()

	provider := client.Tenant.Create().SetName("Provider").SetCode("prov-1").SetStatus("active").SetType("msp_provider").SaveX(ctx)
	customer := client.Tenant.Create().SetName("Customer").SetCode("cust-1").SetStatus("active").SetType("msp_customer").SetMspProviderID(provider.ID).SaveX(ctx)
	mspUser := client.User.Create().
		SetUsername("mspadmin").SetEmail("mspadmin@example.com").SetName("MSP").
		SetPasswordHash("hash").SetRole("admin").SetMspRole("provider_admin").SetActive(true).SetTenantID(provider.ID).
		SaveX(ctx)
	client.MSPAllocation.Create().SetMspUserID(mspUser.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).SetRole("provider_agent").SaveX(ctx)

	tok, err := middleware.GenerateRefreshTokenWithSource(mspUser.ID, mspUser.Username, string(mspUser.Role), customer.ID, "switch", authScopeTestSecret, time.Hour)
	require.NoError(t, err)
	res, err := svc.RefreshToken(ctx, tok)
	require.NoError(t, err)
	require.NotNil(t, res.Tenant)
	assert.Equal(t, customer.ID, res.Tenant.ID)
	require.NotNil(t, res.TenantSelection)
	assert.Equal(t, "switch", res.TenantSelection.Mode)

	claims, err := middleware.ValidateAccessToken(res.AccessToken, authScopeTestSecret)
	require.NoError(t, err)
	assert.Equal(t, customer.ID, claims.TenantID)
	assert.Equal(t, "switch", claims.TenantSource)
}

// TestRefreshToken_RevokedScopeRejected 锁定：目标作用域不可用 → TENANT_ACCESS_REVOKED，不回退 home。
func TestRefreshToken_RevokedScopeRejected(t *testing.T) {
	client, svc, _, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	other := client.Tenant.Create().SetName("Other").SetCode("other-1").SetStatus("active").SaveX(ctx)
	tok, err := middleware.GenerateRefreshTokenWithSource(u.ID, u.Username, string(u.Role), other.ID, "switch", authScopeTestSecret, time.Hour)
	require.NoError(t, err)

	_, err = svc.RefreshToken(ctx, tok)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantAccessRevoked), "err=%v", err)

	// 已停用租户同样拒绝（即使 claims 指向 home）。
	suspended := client.Tenant.Create().SetName("Susp").SetCode("susp-1").SetStatus("suspended").SaveX(ctx)
	tok2, err := middleware.GenerateRefreshTokenWithSource(u.ID, u.Username, string(u.Role), suspended.ID, "switch", authScopeTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = svc.RefreshToken(ctx, tok2)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantAccessRevoked), "err=%v", err)
}

// TestGetUserTenants_Union 锁定 IP-P0-6：home ∪ 有效 allocation ∪ 平台全量。
func TestGetUserTenants_Union(t *testing.T) {
	client, svc, home, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	t.Run("home only", func(t *testing.T) {
		list, err := svc.GetUserTenants(ctx, u.ID)
		require.NoError(t, err)
		assert.Len(t, list, 1)
	})

	provider := client.Tenant.Create().SetName("Provider").SetCode("prov-2").SetStatus("active").SetType("msp_provider").SaveX(ctx)
	customerA := client.Tenant.Create().SetName("CustA").SetCode("cust-a").SetStatus("active").SetType("msp_customer").SetMspProviderID(provider.ID).SaveX(ctx)
	customerB := client.Tenant.Create().SetName("CustB").SetCode("cust-b").SetStatus("active").SetType("msp_customer").SetMspProviderID(provider.ID).SaveX(ctx)

	mspUser := client.User.Create().
		SetUsername("agent").SetEmail("agent@example.com").SetName("Agent").
		SetPasswordHash("hash").SetRole("agent").SetMspRole("provider_agent").SetActive(true).SetTenantID(provider.ID).
		SaveX(ctx)
	client.MSPAllocation.Create().SetMspUserID(mspUser.ID).SetCustomerTenantID(customerA.ID).SetProviderTenantID(provider.ID).SetRole("provider_agent").SaveX(ctx)
	client.MSPAllocation.Create().SetMspUserID(mspUser.ID).SetCustomerTenantID(customerB.ID).SetProviderTenantID(provider.ID).SetRole("provider_agent").SaveX(ctx)
	// 已解除的分配不出现
	allocC := client.MSPAllocation.Create().SetMspUserID(mspUser.ID).SetCustomerTenantID(home.ID).SetProviderTenantID(provider.ID).SetRole("provider_agent").SaveX(ctx)
	client.MSPAllocation.UpdateOneID(allocC.ID).SetDeassignedAt(time.Now()).SaveX(ctx)

	t.Run("provider home plus active allocations", func(t *testing.T) {
		list, err := svc.GetUserTenants(ctx, mspUser.ID)
		require.NoError(t, err)
		assert.Len(t, list, 3) // home + A + B（C 已解除且重复 home）
	})

	superUser := client.User.Create().
		SetUsername("root").SetEmail("root@example.com").SetName("Root").
		SetPasswordHash("hash").SetRole("super_admin").SetActive(true).SetTenantID(home.ID).
		SaveX(ctx)
	t.Run("platform sees all active", func(t *testing.T) {
		list, err := svc.GetUserTenants(ctx, superUser.ID)
		require.NoError(t, err)
		assert.Len(t, list, 4) // home + provider + cust-a + cust-b
	})
}

// TestLogin_JWTRoleRankSelection 锁定 07:G3：JWT role = max(主角色, MSP 映射角色)。
func TestLogin_JWTRoleRankSelection(t *testing.T) {
	client, svc, _, _, ctx := newAuthScopeFixture(t)
	defer client.Close()

	provider := client.Tenant.Create().SetName("Provider3").SetCode("prov-3").SetStatus("active").SetType("msp_provider").SaveX(ctx)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	require.NoError(t, err)

	t.Run("admin primary beats msp_manager mapping", func(t *testing.T) {
		client.User.Create().
			SetUsername("mspadmin3").SetEmail("mspadmin3@example.com").SetName("MSP Admin 3").
			SetPasswordHash(string(hash)).SetRole("admin").SetMspRole("provider_admin").
			SetActive(true).SetTenantID(provider.ID).
			SaveX(ctx)

		res, err := svc.Login(ctx, "mspadmin3", "password123", 0, "")
		require.NoError(t, err)
		claims, err := middleware.ValidateAccessToken(res.AccessToken, authScopeTestSecret)
		require.NoError(t, err)
		assert.Equal(t, "admin", claims.Role, "主角色 rank(4) > msp_manager(3)，应保留 admin")
		assert.Equal(t, provider.ID, claims.TenantID, "登录落 provider 家")
		assert.Equal(t, "home", claims.TenantSource)
	})

	t.Run("weak primary falls back to mapped role", func(t *testing.T) {
		client.User.Create().
			SetUsername("weakadmin").SetEmail("weakadmin@example.com").SetName("Weak").
			SetPasswordHash(string(hash)).SetRole("end_user").SetMspRole("provider_admin").
			SetActive(true).SetTenantID(provider.ID).
			SaveX(ctx)

		res, err := svc.Login(ctx, "weakadmin", "password123", 0, "")
		require.NoError(t, err)
		claims, err := middleware.ValidateAccessToken(res.AccessToken, authScopeTestSecret)
		require.NoError(t, err)
		assert.Equal(t, "msp_manager", claims.Role, "主角色 rank(1) < 映射(3)，应取 msp_manager")
	})
}
