package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// user_admin_test.go：平台侧租户用户管理 HTTP 契约（TUM-1/TUM-2）——
// 参数解析、身份透传、稳定错误码映射、未装配 503。

type fakeTenantUserAdmin struct {
	listResp *dto.TenantUserListResponse
	listErr  error
	getErr   error

	resetResp *dto.ResetTenantUserPasswordResponse
	resetErr  error

	statusErr error

	forceResp *dto.ForceLogoutTenantUserResponse
	forceErr  error

	gotActor  service.TenantAdminActor
	gotTenant int
	gotUser   int
	gotQuery  *dto.TenantUserListQuery
	gotReset  *dto.ResetTenantUserPasswordRequest
	gotStatus *dto.SetTenantUserStatusRequest
}

func (f *fakeTenantUserAdmin) ListUsers(_ context.Context, actor service.TenantAdminActor, tenantID int, q *dto.TenantUserListQuery) (*dto.TenantUserListResponse, error) {
	f.gotActor, f.gotTenant, f.gotQuery = actor, tenantID, q
	return f.listResp, f.listErr
}

func (f *fakeTenantUserAdmin) GetUser(_ context.Context, actor service.TenantAdminActor, tenantID, userID int) (*dto.TenantUserItem, error) {
	f.gotActor, f.gotTenant, f.gotUser = actor, tenantID, userID
	return nil, f.getErr
}

func (f *fakeTenantUserAdmin) ResetPassword(_ context.Context, actor service.TenantAdminActor, tenantID, userID int, req *dto.ResetTenantUserPasswordRequest) (*dto.ResetTenantUserPasswordResponse, error) {
	f.gotActor, f.gotTenant, f.gotUser, f.gotReset = actor, tenantID, userID, req
	return f.resetResp, f.resetErr
}

func (f *fakeTenantUserAdmin) SetActive(_ context.Context, actor service.TenantAdminActor, tenantID, userID int, req *dto.SetTenantUserStatusRequest) error {
	f.gotActor, f.gotTenant, f.gotUser, f.gotStatus = actor, tenantID, userID, req
	return f.statusErr
}

func (f *fakeTenantUserAdmin) ForceLogout(_ context.Context, actor service.TenantAdminActor, tenantID, userID int) (*dto.ForceLogoutTenantUserResponse, error) {
	f.gotActor, f.gotTenant, f.gotUser = actor, tenantID, userID
	return f.forceResp, f.forceErr
}

func newTUAHandler(svc TenantUserAdminService) *Handler {
	gin.SetMode(gin.TestMode)
	h := NewHandler(&mockTenantService{}, zap.NewNop().Sugar())
	if svc != nil {
		h.SetTenantUserAdminService(svc)
	}
	return h
}

func doTUARequest(h *Handler, method, target, body string, params gin.Params) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	c.Set("user_id", 42)
	c.Set("tenant_id", 3)
	c.Set("role", "super_admin")
	c.Set("username", "platform-admin")
	return w, c
}

func decodeTUABody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	return payload
}

func TestListTenantUsers_SuccessAndIdentity(t *testing.T) {
	fake := &fakeTenantUserAdmin{
		listResp: &dto.TenantUserListResponse{
			Items: []dto.TenantUserItem{{ID: 11, Username: "u1", Role: "admin", Active: true}},
			Total: 1, Page: 1, PageSize: 20,
		},
	}
	h := newTUAHandler(fake)

	w, c := doTUARequest(h, http.MethodGet, "/api/v1/tenants/7/users?keyword=u&status=active",
		"", gin.Params{{Key: "id", Value: "7"}})
	h.ListTenantUsers(c)

	assert.Equal(t, http.StatusOK, w.Code)
	body := decodeTUABody(t, w)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, 7, fake.gotTenant)
	assert.Equal(t, "super_admin", fake.gotActor.Role)
	assert.Equal(t, 42, fake.gotActor.UserID)
	assert.Equal(t, 3, fake.gotActor.HomeTenantID)
	require.NotNil(t, fake.gotQuery)
	assert.Equal(t, "u", fake.gotQuery.Keyword)
	assert.Equal(t, "active", fake.gotQuery.Status)
}

func TestResetTenantUserPassword_BindsRequest(t *testing.T) {
	fake := &fakeTenantUserAdmin{
		resetResp: &dto.ResetTenantUserPasswordResponse{UserID: 11, Mode: "generated", GeneratedPassword: "Abc12345xyz", MustChangePassword: true},
	}
	h := newTUAHandler(fake)

	w, c := doTUARequest(h, http.MethodPost, "/api/v1/tenants/7/users/11/reset-password",
		`{"mode":"generated"}`, gin.Params{{Key: "id", Value: "7"}, {Key: "userId", Value: "11"}})
	h.ResetTenantUserPassword(c)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, fake.gotReset)
	assert.Equal(t, "generated", fake.gotReset.Mode)
	assert.Equal(t, 7, fake.gotTenant)
	assert.Equal(t, 11, fake.gotUser)
	body := decodeTUABody(t, w)
	data, _ := body["data"].(map[string]any)
	assert.Equal(t, true, data["mustChangePassword"])
}

func TestSetTenantUserStatus_StableErrorMapping(t *testing.T) {
	fake := &fakeTenantUserAdmin{
		statusErr: &service.TenantUserAdminError{Code: service.TenantUserAdminCodeLastAdmin, Message: "该用户是租户最后一个可用管理员，不能停用", Status: http.StatusConflict},
	}
	h := newTUAHandler(fake)

	w, c := doTUARequest(h, http.MethodPut, "/api/v1/tenants/7/users/11/status",
		`{"active":false}`, gin.Params{{Key: "id", Value: "7"}, {Key: "userId", Value: "11"}})
	h.SetTenantUserStatus(c)

	assert.Equal(t, http.StatusConflict, w.Code)
	body := decodeTUABody(t, w)
	assert.Equal(t, service.TenantUserAdminCodeLastAdmin, body["code"])
	require.NotNil(t, fake.gotStatus)
	require.NotNil(t, fake.gotStatus.Active)
	assert.False(t, *fake.gotStatus.Active)
}

func TestSetTenantUserStatus_RequiresActiveParam(t *testing.T) {
	h := newTUAHandler(&fakeTenantUserAdmin{})

	w, c := doTUARequest(h, http.MethodPut, "/api/v1/tenants/7/users/11/status",
		`{}`, gin.Params{{Key: "id", Value: "7"}, {Key: "userId", Value: "11"}})
	h.SetTenantUserStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code, "缺少 active 必须 400")
}

func TestTenantUserAdmin_InvalidPathAndNotConfigured(t *testing.T) {
	h := newTUAHandler(&fakeTenantUserAdmin{})

	// 非法 userId → 400（不进入 service）。
	w, c := doTUARequest(h, http.MethodGet, "/api/v1/tenants/7/users/abc",
		"", gin.Params{{Key: "id", Value: "7"}, {Key: "userId", Value: "abc"}})
	h.GetTenantUser(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 未装配服务 → 503（服务不可用）。
	h2 := newTUAHandler(nil)
	w2, c2 := doTUARequest(h2, http.MethodGet, "/api/v1/tenants/7/users",
		"", gin.Params{{Key: "id", Value: "7"}})
	h2.ListTenantUsers(c2)
	assert.NotEqual(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "未启用")
}
