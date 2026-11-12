package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
)

// TestRBACPrecheckUsesMembershipRoleDBPermissions 锁定 deep-switch 预检口径：
// JWT 主角色仍是 end_user，但 membership 合并出的 msp_tech 在 DB 持有 ticket:update，
// 预检必须与路由级 RequirePermission 同源放行，不能提前 403。
func TestRBACPrecheckUsesMembershipRoleDBPermissions(t *testing.T) {
	oldMode := PermissionConfig.Mode
	oldCache := PermissionConfig.EnableCache
	PermissionConfig.Mode = PermissionConfigModeDBOnly
	PermissionConfig.EnableCache = false
	InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		PermissionConfig.Mode = oldMode
		PermissionConfig.EnableCache = oldCache
		InvalidateAllPermissionCaches()
	})

	ctx := context.Background()
	client := membershipTestClient(t)
	tenantEntity := membershipTestTenant(t, ctx, client, "precheck-provider")
	userEntity := membershipTestUser(t, ctx, client, "precheck-provider-user", tenantEntity.ID, user.RoleEndUser)
	roleEntity := seedMembershipRole(t, ctx, client, tenantEntity.ID, "msp_tech", [][2]string{{"ticket", "update"}})
	_, err := client.UserTenantMembership.Create().
		SetUserID(userEntity.ID).
		SetTenantID(tenantEntity.ID).
		SetRoleID(roleEntity.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/tickets/workflow/cc", nil)
	c.Set("user_id", userEntity.ID)
	c.Set("tenant_id", tenantEntity.ID)
	c.Set("role", string(user.RoleEndUser))
	c.Set("roles", []string{"msp_tech"})
	c.Set("client", client)

	assert.True(t, hasPermission(client, string(user.RoleEndUser), http.MethodPost, c.Request.URL.Path, userEntity.ID, tenantEntity.ID, c))
}
