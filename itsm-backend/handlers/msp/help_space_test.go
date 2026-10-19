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

// P2 MSP 帮助空间与分发授权 handler 契约测试（替身服务：状态码/参数透传/错误映射/fail-closed）。

type stubHelpSpace struct {
	docsResp     *dto.MSPHelpDocListResponse
	docResp      *dto.MSPHelpDoc
	distResp     *dto.MSPHelpDistribution
	listDistResp *dto.MSPHelpDistributionListResponse
	err          error
	deleteErr    error

	lastCreate     dto.MSPHelpDocRequest
	lastDocID      int
	lastCustomerID int
	lastDistID     int
}

func (s *stubHelpSpace) ListDocs(_ context.Context, _ service.MSPWorkbenchActor) (*dto.MSPHelpDocListResponse, error) {
	return s.docsResp, s.err
}

func (s *stubHelpSpace) CreateDoc(_ context.Context, _ service.MSPWorkbenchActor, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error) {
	s.lastCreate = req
	return s.docResp, s.err
}

func (s *stubHelpSpace) UpdateDoc(_ context.Context, _ service.MSPWorkbenchActor, id int, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error) {
	s.lastDocID, s.lastCreate = id, req
	return s.docResp, s.err
}

func (s *stubHelpSpace) PublishDoc(_ context.Context, _ service.MSPWorkbenchActor, id int) (*dto.MSPHelpDoc, error) {
	s.lastDocID = id
	return s.docResp, s.err
}

func (s *stubHelpSpace) DeleteDoc(_ context.Context, _ service.MSPWorkbenchActor, id int) error {
	s.lastDocID = id
	return s.deleteErr
}

func (s *stubHelpSpace) ListDistributions(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID int) (*dto.MSPHelpDistributionListResponse, error) {
	s.lastCustomerID = customerTenantID
	return s.listDistResp, s.err
}

func (s *stubHelpSpace) Distribute(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID, docID int) (*dto.MSPHelpDistribution, error) {
	s.lastCustomerID, s.lastDocID = customerTenantID, docID
	return s.distResp, s.err
}

func (s *stubHelpSpace) Upgrade(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error) {
	s.lastCustomerID, s.lastDistID = customerTenantID, distributionID
	return s.distResp, s.err
}

func (s *stubHelpSpace) Unbind(_ context.Context, _ service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error) {
	s.lastCustomerID, s.lastDistID = customerTenantID, distributionID
	return s.distResp, s.err
}

func newHelpSpaceTestRouter(t *testing.T, stub *stubHelpSpace, mspCtx *middleware.MSPContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, logger)
	if stub != nil {
		h.SetHelpSpace(stub)
	}

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
	r.GET("/api/v1/msp/help/docs", h.ListHelpDocs)
	r.POST("/api/v1/msp/help/docs", h.CreateHelpDoc)
	r.PUT("/api/v1/msp/help/docs/:id", h.UpdateHelpDoc)
	r.POST("/api/v1/msp/help/docs/:id/publish", h.PublishHelpDoc)
	r.DELETE("/api/v1/msp/help/docs/:id", h.DeleteHelpDoc)
	r.GET("/api/v1/msp/customers/:customer_tenant_id/help/distributions", h.ListHelpDistributions)
	r.POST("/api/v1/msp/customers/:customer_tenant_id/help/docs/:id/distribute", h.DistributeHelpDoc)
	r.POST("/api/v1/msp/customers/:customer_tenant_id/help/distributions/:distId/upgrade", h.UpgradeHelpDistribution)
	r.POST("/api/v1/msp/customers/:customer_tenant_id/help/distributions/:distId/unbind", h.UnbindHelpDistribution)
	return r
}

func mspHelpActorCtx() *middleware.MSPContext {
	return &middleware.MSPContext{IsMSP: true, MSPUserID: 7, Role: "provider_admin"}
}

func doHelpSpaceRequest(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHelpSpace_AuthoringEndpoints(t *testing.T) {
	stub := &stubHelpSpace{docResp: &dto.MSPHelpDoc{ID: 11, Slug: "sla-guide", Title: "SLA 指南", Status: "draft", Version: 1}}
	r := newHelpSpaceTestRouter(t, stub, mspHelpActorCtx())

	// 创建：请求体透传。
	w := doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/help/docs",
		`{"slug":"sla-guide","title":"SLA 指南","pageKey":"sla.monitor","bodyMd":"# SLA"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "sla-guide", stub.lastCreate.Slug)
	assert.Equal(t, "sla.monitor", stub.lastCreate.PageKey)

	// 非法 JSON → 400（不触服务）。
	w = doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/help/docs", `{`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 更新：路径 id + 请求体。
	w = doHelpSpaceRequest(t, r, http.MethodPut, "/api/v1/msp/help/docs/11",
		`{"title":"SLA 指南 v2","pageKey":"sla.monitor","bodyMd":"# v2"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 11, stub.lastDocID)

	// 发布：路径 id。
	w = doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/help/docs/11/publish", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 11, stub.lastDocID)

	// 删除草稿：返回 deleted=true。
	w = doHelpSpaceRequest(t, r, http.MethodDelete, "/api/v1/msp/help/docs/11", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var deleteResp struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &deleteResp))
	assert.Equal(t, true, deleteResp.Data["deleted"])

	// 列表：总数透传。
	stub.docsResp = &dto.MSPHelpDocListResponse{Items: []*dto.MSPHelpDoc{{ID: 11}}, Total: 1}
	w = doHelpSpaceRequest(t, r, http.MethodGet, "/api/v1/msp/help/docs", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"total":1`)
}

func TestHelpSpace_DistributionEndpoints(t *testing.T) {
	stub := &stubHelpSpace{distResp: &dto.MSPHelpDistribution{ID: 21, CustomerTenantID: 6, SourceSlug: "sla-guide", SourceVersion: 3, Status: "active"}}
	r := newHelpSpaceTestRouter(t, stub, mspHelpActorCtx())

	// 分发：客户 + 源文档 ID 透传。
	w := doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/customers/6/help/docs/11/distribute", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 6, stub.lastCustomerID)
	assert.Equal(t, 11, stub.lastDocID)
	assert.Contains(t, w.Body.String(), `"distribution"`)

	// 升级/解绑：客户 + 分发行 ID 透传。
	w = doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/customers/6/help/distributions/21/upgrade", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 21, stub.lastDistID)

	w = doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/customers/6/help/distributions/21/unbind", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 21, stub.lastDistID)

	// 列表：升级提示字段透传。
	stub.listDistResp = &dto.MSPHelpDistributionListResponse{Items: []*dto.MSPHelpDistribution{
		{ID: 21, UpgradeAvailable: true, LatestProviderVersion: 4},
	}, Total: 1}
	w = doHelpSpaceRequest(t, r, http.MethodGet, "/api/v1/msp/customers/6/help/distributions", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"upgradeAvailable":true`)
	assert.Contains(t, w.Body.String(), `"latestProviderVersion":4`)
}

func TestHelpSpace_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"未找到 → 404", service.ErrMSPHelpDocNotFound, http.StatusNotFound},
		{"状态冲突 → 409", service.ErrMSPHelpDocConflict, http.StatusConflict},
		{"未发布不可分发 → 409", service.ErrMSPHelpDocNotPublished, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubHelpSpace{err: tc.err}
			r := newHelpSpaceTestRouter(t, stub, mspHelpActorCtx())
			w := doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/customers/6/help/docs/11/distribute", "")
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}

	// 参数错误（服务层校验）→ 400。
	stub := &stubHelpSpace{err: service.ErrMSPHelpDocInvalid}
	r := newHelpSpaceTestRouter(t, stub, mspHelpActorCtx())
	w := doHelpSpaceRequest(t, r, http.MethodPost, "/api/v1/msp/help/docs", `{"slug":"x","title":"y"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// 非 MSP 上下文 → 403（不触服务）。
	w = doHelpSpaceRequest(t, newHelpSpaceTestRouter(t, stub, nil), http.MethodGet, "/api/v1/msp/help/docs", "")
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 服务未装配 → 503。
	w = doHelpSpaceRequest(t, newHelpSpaceTestRouter(t, nil, mspHelpActorCtx()), http.MethodGet, "/api/v1/msp/help/docs", "")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// 非法路径参数 → 400。
	w = doHelpSpaceRequest(t, newHelpSpaceTestRouter(t, stub, mspHelpActorCtx()), http.MethodPut, "/api/v1/msp/help/docs/abc", `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
