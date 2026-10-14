package msp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// 跨租户单据流转 P2-①：模板分发 handler 契约测试（替身服务，覆盖状态码/参数透传/fail-closed）。

type stubWorkflowDistribution struct {
	distResp       *dto.DistributeWorkflowTemplateResponse
	distErr        error
	listResp       *dto.WorkflowTemplateDistributionListResponse
	listErr        error
	lastCustomerID int
	lastKey        string
	lastVersion    string
}

func (s *stubWorkflowDistribution) Distribute(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID int, key, version string) (*dto.DistributeWorkflowTemplateResponse, error) {
	s.lastCustomerID, s.lastKey, s.lastVersion = customerTenantID, key, version
	return s.distResp, s.distErr
}

func (s *stubWorkflowDistribution) ListDistributions(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID int) (*dto.WorkflowTemplateDistributionListResponse, error) {
	s.lastCustomerID = customerTenantID
	return s.listResp, s.listErr
}

func newDistributionTestRouter(t *testing.T, stub *stubWorkflowDistribution, mspCtx *middleware.MSPContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(nil, nil, nil, nil, nil, stub, nil, nil, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-admin")
		c.Set("tenant_id", 3)
		if mspCtx != nil {
			c.Set(middleware.MSPContextKey, mspCtx)
		}
		c.Next()
	})
	r.POST("/api/v1/msp/customers/:customer_tenant_id/workflow-templates/:key/distribute", h.DistributeWorkflowTemplate)
	r.GET("/api/v1/msp/customers/:customer_tenant_id/workflow-templates", h.ListCustomerWorkflowTemplates)
	return r
}

func mspActorCtx() *middleware.MSPContext {
	return &middleware.MSPContext{IsMSP: true, MSPUserID: 7, Role: "provider_admin"}
}

type distributionEnvelope struct {
	Code int                                    `json:"code"`
	Data dto.DistributeWorkflowTemplateResponse `json:"data"`
}

type distributionListEnvelope struct {
	Code int                                          `json:"code"`
	Data dto.WorkflowTemplateDistributionListResponse `json:"data"`
}

func TestDistributeWorkflowTemplate_Success(t *testing.T) {
	stub := &stubWorkflowDistribution{distResp: &dto.DistributeWorkflowTemplateResponse{Distribution: &dto.WorkflowTemplateDistribution{
		ID: 9, CustomerTenantID: 6, SourceTemplateKey: "flow_a", SourceVersion: "1.0.0",
		CustomerTemplateVersion: "1.1.0", CustomerTemplateStatus: "draft",
	}}}
	r := newDistributionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/customers/6/workflow-templates/flow_a/distribute", strings.NewReader(`{"version":"1.0.0"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var env distributionEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, 0, env.Code)
	assert.Equal(t, 9, env.Data.Distribution.ID)
	assert.Equal(t, 6, stub.lastCustomerID)
	assert.Equal(t, "flow_a", stub.lastKey)
	assert.Equal(t, "1.0.0", stub.lastVersion)
}

func TestDistributeWorkflowTemplate_AccessDenied(t *testing.T) {
	stub := &stubWorkflowDistribution{distErr: service.NewCustomerAccessError(service.CodeMSPAllocationRequired, "目标客户未分配")}
	r := newDistributionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/customers/6/workflow-templates/flow_a/distribute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "MSP_ALLOCATION_REQUIRED")
}

func TestDistributeWorkflowTemplate_NotFoundAndConflict(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not_found", service.ErrWorkflowTemplateNotFound, http.StatusNotFound},
		{"conflict", service.ErrWorkflowTemplateConflict, http.StatusConflict},
		{"invalid", service.ErrWorkflowTemplateInvalid, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubWorkflowDistribution{distErr: tc.err}
			r := newDistributionTestRouter(t, stub, mspActorCtx())
			req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/customers/6/workflow-templates/flow_a/distribute", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
		})
	}
}

func TestDistributeWorkflowTemplate_NoMSPContextFailsClosed(t *testing.T) {
	stub := &stubWorkflowDistribution{}
	r := newDistributionTestRouter(t, stub, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/customers/6/workflow-templates/flow_a/distribute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, stub.lastCustomerID, "无 MSP 上下文不得触达服务层")
}

func TestDistributeWorkflowTemplate_BadCustomerID(t *testing.T) {
	stub := &stubWorkflowDistribution{}
	r := newDistributionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/customers/abc/workflow-templates/flow_a/distribute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListCustomerWorkflowTemplates_Success(t *testing.T) {
	stub := &stubWorkflowDistribution{listResp: &dto.WorkflowTemplateDistributionListResponse{Items: []*dto.WorkflowTemplateDistribution{
		{ID: 12, SourceTemplateKey: "flow_a", SourceVersion: "1.1.0", LatestProviderVersion: "1.2.0", UpgradeAvailable: true},
	}, Total: 1}}
	r := newDistributionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/msp/customers/6/workflow-templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var env distributionListEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, 1, env.Data.Total)
	require.Len(t, env.Data.Items, 1)
	assert.True(t, env.Data.Items[0].UpgradeAvailable)
	assert.Equal(t, 6, stub.lastCustomerID)
}
