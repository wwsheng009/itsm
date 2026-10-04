package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeProvisioningService 实现本包 ProvisioningService，仅测试用。
type fakeProvisioningService struct {
	readinessResp *dto.TenantReadinessResponse
	readinessErr  error

	provisionResp    *dto.TenantReadinessResponse
	provisionErr     error
	provisionVersion string
	provisionCalls   int

	createResp *dto.BootstrapAdminResponse
	createErr  error
}

func (f *fakeProvisioningService) Readiness(_ context.Context, _ int) (*dto.TenantReadinessResponse, error) {
	return f.readinessResp, f.readinessErr
}

func (f *fakeProvisioningService) Provision(_ context.Context, _ int, templateVersion string) (*dto.TenantReadinessResponse, error) {
	f.provisionCalls++
	f.provisionVersion = templateVersion
	return f.provisionResp, f.provisionErr
}

func (f *fakeProvisioningService) CreateBootstrapAdmin(_ context.Context, _ int, _ *dto.BootstrapAdminRequest) (*dto.BootstrapAdminResponse, error) {
	return f.createResp, f.createErr
}

func newProvisioningHandler(svc ProvisioningService) *Handler {
	gin.SetMode(gin.TestMode)
	h := NewHandler(&mockTenantService{}, zap.NewNop().Sugar())
	if svc != nil {
		h.SetProvisioningService(svc)
	}
	return h
}

func doProvisioningRequest(h *Handler, method, target, body string, params gin.Params) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	c.Set("user_id", 1)
	c.Set("role", "super_admin")
	return w, c
}

func decodeProvisioningResponse(t *testing.T, w *httptest.ResponseRecorder) common.Response {
	t.Helper()
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestGetTenantReadiness_Success(t *testing.T) {
	fake := &fakeProvisioningService{
		readinessResp: &dto.TenantReadinessResponse{
			TenantID: 7, TemplateVersion: "1.0.0", Ready: true, BootstrapAdmins: 1,
			Items: []dto.TenantReadinessItem{{Key: "roles", Label: "角色", Count: 3, Required: true}},
		},
	}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodGet, "/api/v1/tenants/7/readiness", "", gin.Params{{Key: "id", Value: "7"}})
	h.GetTenantReadiness(c)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeProvisioningResponse(t, w)
	assert.Equal(t, common.SuccessCode, resp.Code)
	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(7), data["tenantId"])
	assert.Equal(t, true, data["ready"])
	assert.Equal(t, "1.0.0", data["templateVersion"])
	assert.Equal(t, float64(1), data["bootstrapAdmins"])
	items, ok := data["items"].([]interface{})
	require.True(t, ok)
	require.Len(t, items, 1)
}

func TestGetTenantReadiness_NotFound(t *testing.T) {
	fake := &fakeProvisioningService{readinessErr: service.ErrTenantNotFound}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodGet, "/api/v1/tenants/5/readiness", "", gin.Params{{Key: "id", Value: "5"}})
	h.GetTenantReadiness(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, common.NotFoundCode, decodeProvisioningResponse(t, w).Code)
}

func TestProvisionTenant_PassesTemplateVersionThrough(t *testing.T) {
	fake := &fakeProvisioningService{
		provisionResp: &dto.TenantReadinessResponse{TenantID: 1, TemplateVersion: "2.0.0", Ready: true},
	}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/provision", `{"templateVersion":"2.0.0"}`, gin.Params{{Key: "id", Value: "1"}})
	h.ProvisionTenant(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, fake.provisionCalls)
	assert.Equal(t, "2.0.0", fake.provisionVersion, "handler 必须原样透传 templateVersion 给 service")
	resp := decodeProvisioningResponse(t, w)
	assert.Equal(t, common.SuccessCode, resp.Code)
	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "2.0.0", data["templateVersion"])
}

func TestProvisionTenant_EmptyBodyDefaultsTemplateVersion(t *testing.T) {
	fake := &fakeProvisioningService{
		provisionResp: &dto.TenantReadinessResponse{TenantID: 1, TemplateVersion: "1.0.0", Ready: true},
	}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/provision", "", gin.Params{{Key: "id", Value: "1"}})
	h.ProvisionTenant(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, fake.provisionCalls)
	assert.Empty(t, fake.provisionVersion, "空 body 时透传空串，由 service 落默认版本")
}

func TestProvisionTenant_UnsupportedVersionIsParamError(t *testing.T) {
	fake := &fakeProvisioningService{provisionErr: service.ErrUnsupportedTenantTemplateVersion}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/provision", `{"templateVersion":"9.9.9"}`, gin.Params{{Key: "id", Value: "1"}})
	h.ProvisionTenant(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, common.ParamErrorCode, decodeProvisioningResponse(t, w).Code)
}

func TestCreateBootstrapAdmin_ConflictWhenAdminExists(t *testing.T) {
	fake := &fakeProvisioningService{createErr: service.ErrBootstrapAdminExists}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/bootstrap-admin", `{}`, gin.Params{{Key: "id", Value: "1"}})
	h.CreateBootstrapAdmin(c)

	assert.Equal(t, http.StatusConflict, w.Code)
	resp := decodeProvisioningResponse(t, w)
	assert.Equal(t, common.ConflictCode, resp.Code)
	assert.Equal(t, "该租户已存在首个管理员", resp.Message)
}

func TestCreateBootstrapAdmin_GeneratedPasswordReturned(t *testing.T) {
	fake := &fakeProvisioningService{
		createResp: &dto.BootstrapAdminResponse{
			UserID: 9, Username: "admin-acme", Email: "admin-acme@bootstrap.local",
			Password: "Abcdef1234567890", Generated: true, MustChangePassword: true,
		},
	}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/bootstrap-admin", `{}`, gin.Params{{Key: "id", Value: "1"}})
	h.CreateBootstrapAdmin(c)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeProvisioningResponse(t, w)
	assert.Equal(t, common.SuccessCode, resp.Code)
	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Abcdef1234567890", data["password"])
	assert.Equal(t, true, data["generated"])
	assert.Equal(t, true, data["mustChangePassword"])
	assert.Equal(t, "admin-acme", data["username"])
}

func TestCreateBootstrapAdmin_RejectsShortPassword(t *testing.T) {
	fake := &fakeProvisioningService{}
	h := newProvisioningHandler(fake)

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/bootstrap-admin", `{"password":"short"}`, gin.Params{{Key: "id", Value: "1"}})
	h.CreateBootstrapAdmin(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, common.ParamErrorCode, decodeProvisioningResponse(t, w).Code)
}

func TestProvisioningEndpoints_InvalidTenantID(t *testing.T) {
	fake := &fakeProvisioningService{}
	h := newProvisioningHandler(fake)

	for _, id := range []string{"abc", "0", "-1"} {
		w, c := doProvisioningRequest(h, http.MethodGet, "/api/v1/tenants/"+id+"/readiness", "", gin.Params{{Key: "id", Value: id}})
		h.GetTenantReadiness(c)
		assert.Equal(t, http.StatusBadRequest, w.Code, "id=%s", id)
		assert.Equal(t, common.ParamErrorCode, decodeProvisioningResponse(t, w).Code)
	}

	w, c := doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/abc/provision", "", gin.Params{{Key: "id", Value: "abc"}})
	h.ProvisionTenant(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.provisionCalls)
}

func TestProvisioningEndpoints_NilServiceUnavailable(t *testing.T) {
	h := newProvisioningHandler(nil)

	w, c := doProvisioningRequest(h, http.MethodGet, "/api/v1/tenants/1/readiness", "", gin.Params{{Key: "id", Value: "1"}})
	h.GetTenantReadiness(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, common.ServiceUnavailableCode, decodeProvisioningResponse(t, w).Code)

	w, c = doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/provision", "", gin.Params{{Key: "id", Value: "1"}})
	h.ProvisionTenant(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	w, c = doProvisioningRequest(h, http.MethodPost, "/api/v1/tenants/1/bootstrap-admin", `{}`, gin.Params{{Key: "id", Value: "1"}})
	h.CreateBootstrapAdmin(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
