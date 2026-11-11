package common

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	entuser "itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
)

// TestGetMe_ReturnsTokenEffectiveRole 锁定 D9：/auth/me 的 role 必须与登录/续签
// 签发的 JWT effective role 一致，不能在刷新后掉回 users.role。
func TestGetMe_ReturnsTokenEffectiveRole(t *testing.T) {
	client, svc, home, _, ctx := newAuthScopeFixture(t)
	defer client.Close()

	u := client.User.Create().
		SetUsername("msp-manager-role").
		SetEmail("msp-manager-role@example.com").
		SetName("MSP Manager Role").
		SetPasswordHash("hash").
		SetRole(entuser.RoleEndUser).
		SetMspRole("provider_admin").
		SetActive(true).
		SetTenantID(home.ID).
		SaveX(ctx)

	h := NewHandler(svc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	c.Set("user_id", u.ID)
	c.Set("tenant_id", home.ID)
	c.Set("role", "msp_manager")

	h.GetMe(c)

	require.Equal(t, 200, w.Code)
	var body struct {
		Code int  `json:"code"`
		Data User `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.Equal(t, "msp_manager", body.Data.Role)
	assert.Equal(t, home.ID, body.Data.TenantID)
	assert.False(t, body.Data.IsElevatedMenuRole)
	assert.False(t, body.Data.IsPlatformRole)
}

// TestGetMe_ReturnsScopedMembership 锁定 C 方案：/auth/me 返回当前 JWT 作用域租户的
// membership 身份，而不是 users.tenant_id 的 home membership。
func TestGetMe_ReturnsScopedMembership(t *testing.T) {
	client, svc, home, user, ctx := newAuthScopeFixture(t)
	defer client.Close()

	customer := client.Tenant.Create().
		SetName("Customer").
		SetCode("customer-membership-me").
		SetStatus("active").
		SetType("msp_customer").
		SaveX(ctx)
	membership := client.UserTenantMembership.Create().
		SetUserID(user.ID).
		SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).
		SetIsDefault(false).
		SaveX(ctx)

	h := NewHandler(svc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	c.Set("user_id", user.ID)
	c.Set("tenant_id", customer.ID)
	c.Set("role", "end_user")

	h.GetMe(c)

	require.Equal(t, 200, w.Code)
	var body struct {
		Code int  `json:"code"`
		Data User `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, customer.ID, body.Data.TenantID)
	assert.Equal(t, home.ID, body.Data.HomeTenantID)
	assert.Equal(t, membership.ID, body.Data.MembershipID)
	assert.Equal(t, string(usertenantmembership.AccountKindProvider), body.Data.AccountKind)
	assert.Equal(t, string(usertenantmembership.SourceAllocation), body.Data.MembershipSource)
}
