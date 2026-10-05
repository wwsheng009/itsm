package router

import (
	"context"
	"testing"

	"itsm-backend/dto"
	"itsm-backend/ent"
	domainCommon "itsm-backend/handlers/common"
	tenantHandler "itsm-backend/handlers/tenant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// stubTenantServiceForRoutes 仅用于路由注册断言（TUM-1/TUM-2）。
type stubTenantServiceForRoutes struct{}

func (stubTenantServiceForRoutes) CreateTenant(context.Context, *dto.CreateTenantRequest) (*ent.Tenant, error) {
	return nil, nil
}
func (stubTenantServiceForRoutes) ListTenants(context.Context, *dto.ListTenantsRequest) ([]*ent.Tenant, int, error) {
	return nil, 0, nil
}
func (stubTenantServiceForRoutes) ListTenantsScoped(context.Context, *dto.ListTenantsRequest, int) ([]*ent.Tenant, int, error) {
	return nil, 0, nil
}
func (stubTenantServiceForRoutes) GetTenant(context.Context, int) (*ent.Tenant, error) {
	return nil, nil
}
func (stubTenantServiceForRoutes) UpdateTenant(context.Context, int, *dto.UpdateTenantRequest) (*ent.Tenant, error) {
	return nil, nil
}
func (stubTenantServiceForRoutes) UpdateTenantStatus(context.Context, int, string) error { return nil }
func (stubTenantServiceForRoutes) DeleteTenant(context.Context, int) error               { return nil }
func (stubTenantServiceForRoutes) QuotaUsage(context.Context, int) (*dto.TenantQuotaUsageResponse, error) {
	return nil, nil
}

// TestSetupRoutes_TenantUserAdminRoutes 路由契约（TUM-1/TUM-2）：
// 读列表/详情（tenant:read） + 重置密码/启停/强制下线（tenant:write），挂 /tenants 组。
func TestSetupRoutes_TenantUserAdminRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	client := enttest.Open(t, "sqlite3", "file:tenant-user-admin-routes?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	engine := gin.New()
	commonHandler := domainCommon.NewHandler(domainCommon.NewService(domainCommon.NewEntRepository(client), "tua-route-secret", logger, client))
	SetupRoutes(engine, &RouterConfig{
		JWTSecret:     "tua-route-secret",
		Logger:        logger,
		Client:        client,
		CommonHandler: commonHandler,
		TenantHandler: tenantHandler.NewHandler(stubTenantServiceForRoutes{}, logger),
	})

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/tenants/:id/users",
		"GET /api/v1/tenants/:id/users/:userId",
		"POST /api/v1/tenants/:id/users/:userId/reset-password",
		"PUT /api/v1/tenants/:id/users/:userId/status",
		"POST /api/v1/tenants/:id/users/:userId/force-logout",
	} {
		assert.True(t, registered[want], "缺少路由 %s", want)
	}
}
