package msp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// 跨租户单据流转 P2-③ / IP-P2-12：读面 handler 契约测试（真实 sqlite 服务，覆盖状态码/参数/fail-closed）。

var recordsHandlerDBCounter int64

type recordsHandlerEnv struct {
	client   *ent.Client
	provider *ent.Tenant
	custA    *ent.Tenant
	custC    *ent.Tenant
}

func newRecordsHandlerEnv(t *testing.T) *recordsHandlerEnv {
	t.Helper()
	dsn := fmt.Sprintf("file:records_handler_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&recordsHandlerDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetCode("rh-prov").SetName("服务商").SetType(entTenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	custA, err := client.Tenant.Create().
		SetCode("rh-a").SetName("客户A").SetType(entTenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	custC, err := client.Tenant.Create().
		SetCode("rh-c").SetName("客户C").SetType(entTenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)

	_, err = client.Change.Create().
		SetChangeNumber("CHG-H1").SetTitle("handler 变更").SetStatus("submitted").
		SetPriority("medium").SetDescription("handler 描述").
		SetCreatedBy(1).SetTenantID(custA.ID).
		SetCreatedAt(time.Now()).SetUpdatedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	return &recordsHandlerEnv{client: client, provider: provider, custA: custA, custC: custC}
}

func recordsActorCtx(allowed []int) *middleware.MSPContext {
	return &middleware.MSPContext{IsMSP: true, MSPUserID: 7, Role: "provider_admin", AllowedCustomers: allowed}
}

func newRecordsTestRouter(t *testing.T, env *recordsHandlerEnv, mspCtx *middleware.MSPContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(nil, nil, service.NewMSPWorkbenchService(env.client, nil, nil, logger), nil, nil, nil, nil, logger)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-agent")
		c.Set("tenant_id", env.provider.ID)
		if mspCtx != nil {
			c.Set(middleware.MSPContextKey, mspCtx)
		}
		c.Next()
	})
	// 路径与方法与 router/msp_routes.go 一致（Auth/RBAC/MSP 中间件链由 router 包覆盖）。
	group := r.Group("/api/v1/msp")
	group.GET("/workbench/changes", h.ListWorkbenchChanges)
	group.GET("/workbench/problems", h.ListWorkbenchProblems)
	group.GET("/workbench/service-requests", h.ListWorkbenchServiceRequests)
	group.GET("/changes/:id", h.GetWorkbenchChange)
	group.GET("/problems/:id", h.GetWorkbenchProblem)
	group.GET("/service-requests/:id", h.GetWorkbenchServiceRequest)
	return r
}

func TestWorkbenchRecords_ListAndDetail(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	r := newRecordsTestRouter(t, env, recordsActorCtx([]int{env.custA.ID}))

	// 列表。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/msp/workbench/changes?customerTenantIds=all&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listEnv struct {
		Code int                       `json:"code"`
		Data dto.MSPRecordListResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listEnv))
	require.Equal(t, 1, listEnv.Data.Total)
	require.Len(t, listEnv.Data.Items, 1)
	assert.Equal(t, "CHG-H1", listEnv.Data.Items[0].Number)
	assert.Equal(t, "客户A", listEnv.Data.Items[0].CustomerName)
	assert.Equal(t, env.custA.ID, listEnv.Data.Items[0].CustomerTenantID)

	// 详情。
	change, err := env.client.Change.Query().Only(context.Background())
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/msp/changes/%d?customerTenantId=%d", change.ID, env.custA.ID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detailEnv struct {
		Code int                         `json:"code"`
		Data dto.MSPRecordDetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detailEnv))
	assert.Equal(t, "handler 描述", detailEnv.Data.Description)
	assert.Equal(t, "CHG-H1", detailEnv.Data.Number)
	require.Len(t, detailEnv.Data.AllowedActions, 1)
	assert.True(t, detailEnv.Data.AllowedActions[0].Allowed)
}

func TestWorkbenchRecords_BadParams(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	r := newRecordsTestRouter(t, env, recordsActorCtx([]int{env.custA.ID}))

	for _, path := range []string{
		"/api/v1/msp/workbench/changes?customerTenantIds=abc",
		"/api/v1/msp/workbench/problems?limit=-1",
		"/api/v1/msp/changes/abc?customerTenantId=1",
		"/api/v1/msp/problems/1",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
	}
}

func TestWorkbenchRecords_NoMSPContextFailsClosed(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	r := newRecordsTestRouter(t, env, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/msp/workbench/changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestWorkbenchRecords_NotFoundAndDenied(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	r := newRecordsTestRouter(t, env, recordsActorCtx([]int{env.custA.ID}))

	// 未知 id → 404 + reasonCode。
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/msp/changes/999999?customerTenantId=%d", env.custA.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeCustomerTenantNotFound)

	// 未分配客户 → 403 + reasonCode。
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/msp/workbench/changes?customerTenantIds=%d", env.custC.ID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeMSPAllocationRequired)
}
