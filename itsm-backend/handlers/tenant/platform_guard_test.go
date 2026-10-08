package tenant

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// platform_guard_test.go：租户治理写面的平台管理员硬校验回归（IP-P2-7 收口）。
//
// 背景：路由权限 tenant:write 仅粗筛——权限目录克隆使客户/服务商角色也持有该权限码；
// 若无 HTTP 层硬校验，客户侧即可跨租户创建/改删租户、执行供给与首管。

func TestCreateTenant_NonPlatformRoleForbidden(t *testing.T) {
	m := &mockTenantService{}
	h := newTenantHandler(m)

	w, c := doJSON(h, http.MethodPost, "/api/v1/tenants", `{"name":"Acme","code":"acme","type":"internal"}`)
	c.Set("role", "admin") // 客户/服务商侧角色（agent/admin 等非平台角色）
	h.CreateTenant(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	m.AssertNotCalled(t, "CreateTenant", mock.Anything, mock.Anything)
}

func TestUpdateTenant_NonPlatformRoleForbidden(t *testing.T) {
	m := &mockTenantService{}
	h := newTenantHandler(m)

	w, c := doJSON(h, http.MethodPut, "/api/v1/tenants/1", `{"name":"Renamed"}`)
	c.Set("role", "manager")
	h.UpdateTenant(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	m.AssertNotCalled(t, "UpdateTenant", mock.Anything, mock.Anything, mock.Anything)
}

func TestDeleteTenant_NonPlatformRoleForbidden(t *testing.T) {
	m := &mockTenantService{}
	h := newTenantHandler(m)

	w, c := doJSON(h, http.MethodDelete, "/api/v1/tenants/2", "")
	c.Set("role", "admin")
	h.DeleteTenant(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	m.AssertNotCalled(t, "DeleteTenant", mock.Anything, mock.Anything)
}

func TestUpdateTenantStatus_NonPlatformRoleForbidden(t *testing.T) {
	m := &mockTenantService{}
	h := newTenantHandler(m)

	w, c := doJSON(h, http.MethodPut, "/api/v1/tenants/2/status", `{"status":"suspended"}`)
	c.Set("role", "admin")
	h.UpdateTenantStatus(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	m.AssertNotCalled(t, "UpdateTenantStatus", mock.Anything, mock.Anything, mock.Anything)
}

func TestProvisionTenant_NonPlatformRoleForbidden(t *testing.T) {
	fake := &fakeProvisioningService{}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/provision", "", gin.Params{{Key: "id", Value: "1"}})
	c.Set("role", "admin")
	h.ProvisionTenant(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, fake.provisionCalls, "非平台角色不得触发租户供给")
}

func TestCreateBootstrapAdmin_NonPlatformRoleForbidden(t *testing.T) {
	fake := &fakeProvisioningService{}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/bootstrap-admin", `{}`, gin.Params{{Key: "id", Value: "1"}})
	c.Set("role", "admin")
	h.CreateBootstrapAdmin(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
