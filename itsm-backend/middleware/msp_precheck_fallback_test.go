package middleware

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OB-14 回归（2026-10-12，238 UI 写面验证发现）：
// MSP 路由族（/api/v1/msp/*）的通用预检在 token 角色判定失败时，必须按用户 DB `msp_role`
// 的映射角色（provider_admin→msp_manager）再判一次——否则主角色 admin(rank 4) 遮蔽
// msp_manager(rank 3) 会导致「读面 allowedActions 允许、提交 403」的口径不一致。
//
// 同时锁定：① customer_user 映射回 end_user 仍拒绝（不得越权）；② 非 MSP 路径不受兜底影响。
func TestHasPermissionMSPRouteFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:msp_precheck_fallback?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	prov, err := client.Tenant.Create().
		SetName("ProviderFallback").SetCode("prov-fb").
		SetType(tenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)

	seedRole := func(code string, permCodes ...string) {
		roleEntity, rerr := client.Role.Create().SetName(code).SetCode(code).SetTenantID(prov.ID).Save(ctx)
		require.NoError(t, rerr)
		for _, permCode := range permCodes {
			parts := strings.SplitN(permCode, ":", 2)
			permEntity, perr := client.Permission.Create().
				SetCode(permCode).SetName(permCode).
				SetResource(parts[0]).SetAction(parts[1]).
				SetTenantID(prov.ID).Save(ctx)
			require.NoError(t, perr)
			_, lerr := client.RolePermission.Create().
				SetRoleID(roleEntity.ID).SetPermissionID(permEntity.ID).SetTenantID(prov.ID).Save(ctx)
			require.NoError(t, lerr)
		}
	}
	// msp_manager 具备 msp_ticket:write（生产 seeding 同构）；admin 仅 msp:read（显式配置、无写权限）；
	// end_user 配置为空集（customer_user 映射后被显式拒绝，而非走硬编码兜底）。
	seedRole("msp_manager", "msp_ticket:write")
	seedRole("admin", "msp:read")
	seedRole("end_user")

	mkUser := func(name, role, mspRole string) *ent.User {
		u, uerr := client.User.Create().
			SetUsername(name).SetName(name).SetEmail(name + "@example.com").SetPasswordHash("x").
			SetRole(user.Role(role)).SetMspRole(user.MspRole(mspRole)).SetActive(true).SetTenantID(prov.ID).Save(ctx)
		require.NoError(t, uerr)
		return u
	}
	adminProvider := mkUser("fb-admin", "admin", "provider_admin")
	adminCustomer := mkUser("fb-cust", "admin", "customer_user")

	prevMode := PermissionConfig.Mode
	PermissionConfig.Mode = PermissionConfigModeDBOnly
	defer func() { PermissionConfig.Mode = prevMode }()

	newCtx := func(role string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/v1/msp/problems/1/status", nil)
		c.Set("role", role)
		return c
	}

	const mspPath = "/api/v1/msp/problems/1/status"

	t.Run("admin+provider_admin 预检兜底放行（OB-14）", func(t *testing.T) {
		assert.True(t, hasPermission(client, "admin", "POST", mspPath, adminProvider.ID, prov.ID, newCtx("admin")),
			"token 角色 admin 遮蔽 msp_manager 时，应按 msp_role 映射兜底放行")
	})
	t.Run("映射角色解析正确", func(t *testing.T) {
		assert.Equal(t, "msp_manager", mspMappedRoleForUser(newCtx("admin"), client, adminProvider.ID))
	})
	t.Run("customer_user 映射回 end_user 仍拒绝", func(t *testing.T) {
		assert.False(t, hasPermission(client, "admin", "POST", mspPath, adminCustomer.ID, prov.ID, newCtx("admin")),
			"customer_user 不得借兜底获得 MSP 写权限")
	})
	t.Run("token 角色本身具备权限时不受影响", func(t *testing.T) {
		assert.True(t, hasPermission(client, "msp_manager", "POST", mspPath, adminProvider.ID, prov.ID, newCtx("msp_manager")))
	})
	t.Run("非 MSP 路径不受兜底影响", func(t *testing.T) {
		assert.False(t, hasPermission(client, "admin", "POST", "/api/v1/tenants", adminProvider.ID, prov.ID, newCtx("admin")),
			"非 /msp/* 路径不得应用 MSP 兜底")
	})
}
