package msp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	entChange "itsm-backend/ent/change"
	"itsm-backend/ent/enttest"
	entProblem "itsm-backend/ent/problem"
	entSR "itsm-backend/ent/servicerequest"
	entTenant "itsm-backend/ent/tenant"
	changeDomain "itsm-backend/handlers/change"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// 跨租户单据流转 P2-③ / IP-P2-12：读面 handler 契约测试（真实 sqlite 服务，覆盖状态码/参数/fail-closed）。

var recordsHandlerDBCounter int64

type recordsHandlerEnv struct {
	client          *ent.Client
	provider        *ent.Tenant
	custA           *ent.Tenant
	custC           *ent.Tenant
	problemC        *ent.Problem
	changeC         *ent.Change
	serviceRequestC *ent.ServiceRequest
}

func newRecordsHandlerEnv(t *testing.T) *recordsHandlerEnv {
	t.Helper()
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeMerge
	t.Cleanup(func() { middleware.PermissionConfig.Mode = prevMode })

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

	_, err = client.Problem.Create().
		SetProblemNumber("PRB-H1").SetTitle("handler 问题").SetStatus("investigating").
		SetPriority("high").SetCreatedBy(1).SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)

	cProblem, err := client.Problem.Create().
		SetProblemNumber("PRB-HC").SetTitle("handler 问题C").SetStatus("open").
		SetCreatedBy(1).SetTenantID(custC.ID).Save(ctx)
	require.NoError(t, err)

	cChange, err := client.Change.Create().
		SetChangeNumber("CHG-HC").SetTitle("handler 变更C").SetStatus("approved").
		SetPriority("low").SetDescription("C 描述").SetCreatedBy(1).SetTenantID(custC.ID).Save(ctx)
	require.NoError(t, err)

	cSR, err := client.ServiceRequest.Create().
		SetTenantID(custC.ID).SetCatalogID(1).SetRequesterID(1).
		SetTitle("handler 服务请求C").SetStatus("security_approved").SetReason("C 申请").Save(ctx)
	require.NoError(t, err)

	// 服务请求（客户 A；履约窗口内 security_approved，供写面契约用）。
	_, err = client.ServiceRequest.Create().
		SetTenantID(custA.ID).SetCatalogID(1).SetRequesterID(1).
		SetTitle("handler 服务请求").SetStatus("security_approved").SetReason("handler 申请").
		SetCurrentLevel(2).SetTotalLevels(2).SetProcessorID(7).Save(ctx)
	require.NoError(t, err)

	// 目标租户 RBAC（C2 同构最小子集；IP-P2-14/15：写门禁按目标租户客户侧业务码判定）。
	seedCustomerScopeRole(t, client, custA.ID, "msp_manager", "problem:write", "change:write", "service_request:write")
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(middleware.InvalidateAllPermissionCaches)
	return &recordsHandlerEnv{client: client, provider: provider, custA: custA, custC: custC, problemC: cProblem, changeC: cChange, serviceRequestC: cSR}
}

// seedCustomerScopeRole 在目标租户内建角色 + 权限 + 授权（handler 契约夹具；C2 基线同构最小子集）。
func seedCustomerScopeRole(t *testing.T, client *ent.Client, tenantID int, roleCode string, codes ...string) {
	t.Helper()
	ctx := context.Background()
	role, err := client.Role.Create().SetCode(roleCode).SetName(roleCode).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	for _, code := range codes {
		parts := strings.SplitN(code, ":", 2)
		require.Len(t, parts, 2, "permission code must be resource:action")
		perm, err := client.Permission.Create().
			SetCode(code).SetName(code).SetResource(parts[0]).SetAction(parts[1]).SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
		_, err = client.RolePermission.Create().
			SetRoleID(role.ID).SetPermissionID(perm.ID).SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
	}
}

func recordsActorCtx(allowed []int) *middleware.MSPContext {
	return &middleware.MSPContext{IsMSP: true, MSPUserID: 7, Role: "provider_admin", AllowedCustomers: allowed}
}

func newRecordsTestRouter(t *testing.T, env *recordsHandlerEnv, mspCtx *middleware.MSPContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(nil, nil, service.NewMSPWorkbenchService(env.client, nil, nil, logger), nil, nil, nil, nil, nil, logger)

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
	group.POST("/problems/:id/status", h.UpdateWorkbenchProblemStatus)
	group.POST("/changes/:id/status", h.UpdateWorkbenchChangeStatus)
	group.POST("/service-requests/:id/status", h.UpdateWorkbenchServiceRequestStatus)
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
	change, err := env.client.Change.Query().Where(entChange.TenantIDEQ(env.custA.ID)).Only(context.Background())
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
	require.Len(t, detailEnv.Data.AllowedActions, 2)
	assert.True(t, detailEnv.Data.AllowedActions[0].Allowed)
	assert.Equal(t, "status", detailEnv.Data.AllowedActions[1].Action)
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

// stubRecordProblemWriter 记录问题状态写面调用（handler 契约）。
type stubRecordProblemWriter struct {
	calls        int
	lastTenantID int
	lastID       int
	lastActorID  int
	lastStatus   string
	err          error
}

func (w *stubRecordProblemWriter) UpdateProblemStatus(_ context.Context, tenantID, id, actorID int, _, status, _ string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID, w.lastStatus = tenantID, id, actorID, status
	return w.err
}

func TestWorkbenchProblemStatus_Update(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	logger := zaptest.NewLogger(t).Sugar()
	workbench := service.NewMSPWorkbenchService(env.client, nil, nil, logger)
	writer := &stubRecordProblemWriter{}
	workbench.SetProblemStatusWriter(writer)

	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, workbench, nil, nil, nil, nil, nil, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-agent")
		c.Set("tenant_id", env.provider.ID)
		c.Set(middleware.MSPContextKey, recordsActorCtx([]int{env.custA.ID}))
		c.Next()
	})
	r.POST("/api/v1/msp/problems/:id/status", h.UpdateWorkbenchProblemStatus)

	problem, err := env.client.Problem.Query().Where(entProblem.TenantIDEQ(env.custA.ID)).Only(context.Background())
	require.NoError(t, err)

	body := fmt.Sprintf(`{"customerTenantId":%d,"status":"identified"}`, env.custA.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/problems/%d/status", problem.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, writer.calls)
	assert.Equal(t, env.custA.ID, writer.lastTenantID)
	assert.Equal(t, problem.ID, writer.lastID)
	assert.Equal(t, "identified", writer.lastStatus)

	// 缺 customerTenantId → 400。
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/problems/%d/status", problem.ID), strings.NewReader(`{"status":"identified"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 1, writer.calls)

	// 未分配客户（问题在 C 租户）→ 403 + reasonCode。
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"identified"}`, env.custC.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/problems/%d/status", env.problemC.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeMSPAllocationRequired)
	assert.Equal(t, 1, writer.calls)

	// 域状态机拒绝（409 语义）→ ConflictCode + reasonCode。
	writer.err = service.NewCustomerAccessError(service.CodeInvalidTransition, "当前问题状态不允许此操作")
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"identified"}`, env.custA.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/problems/%d/status", problem.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeInvalidTransition)
}

func TestWorkbenchProblemStatus_NoMSPContextFailsClosed(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	logger := zaptest.NewLogger(t).Sugar()
	workbench := service.NewMSPWorkbenchService(env.client, nil, nil, logger)
	writer := &stubRecordProblemWriter{}
	workbench.SetProblemStatusWriter(writer)

	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, workbench, nil, nil, nil, nil, nil, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/v1/msp/problems/:id/status", h.UpdateWorkbenchProblemStatus)

	problem, err := env.client.Problem.Query().Where(entProblem.TenantIDEQ(env.custA.ID)).Only(context.Background())
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/problems/%d/status", problem.ID), strings.NewReader(`{"customerTenantId":1,"status":"identified"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, writer.calls)
}

// stubRecordChangeWriter 记录变更写面调用（handler 契约）。
type stubRecordChangeWriter struct {
	calls        int
	lastTenantID int
	lastID       int
	lastActorID  int
	lastTarget   string
	lastComment  string
	err          error
}

func (w *stubRecordChangeWriter) TransitionChangeStatus(_ context.Context, tenantID, id, actorID int, targetStatus, comment, _ string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID = tenantID, id, actorID
	w.lastTarget, w.lastComment = targetStatus, comment
	return w.err
}

// stubRecordRequestWriter 记录服务请求写面调用（handler 契约）。
type stubRecordRequestWriter struct {
	calls        int
	lastTenantID int
	lastID       int
	lastActorID  int
	lastStatus   string
	err          error
}

func (w *stubRecordRequestWriter) UpdateRequestStatus(_ context.Context, tenantID, id, actorID int, _, status string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID, w.lastStatus = tenantID, id, actorID, status
	return w.err
}

func TestWorkbenchChangeStatus_Update(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	logger := zaptest.NewLogger(t).Sugar()
	workbench := service.NewMSPWorkbenchService(env.client, nil, nil, logger)
	writer := &stubRecordChangeWriter{}
	workbench.SetChangeStatusWriter(writer)

	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, workbench, nil, nil, nil, nil, nil, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-agent")
		c.Set("tenant_id", env.provider.ID)
		c.Set(middleware.MSPContextKey, recordsActorCtx([]int{env.custA.ID}))
		c.Next()
	})
	r.POST("/api/v1/msp/changes/:id/status", h.UpdateWorkbenchChangeStatus)

	change, err := env.client.Change.Query().Where(entChange.TenantIDEQ(env.custA.ID)).Only(context.Background())
	require.NoError(t, err)

	body := fmt.Sprintf(`{"customerTenantId":%d,"status":"scheduled","comment":"排期"}`, env.custA.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/changes/%d/status", change.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, writer.calls)
	assert.Equal(t, env.custA.ID, writer.lastTenantID)
	assert.Equal(t, change.ID, writer.lastID)
	assert.Equal(t, "scheduled", writer.lastTarget)
	assert.Equal(t, "排期", writer.lastComment)

	// 缺 customerTenantId → 400。
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/changes/%d/status", change.ID), strings.NewReader(`{"status":"scheduled"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 1, writer.calls)

	// 审批类状态（不在 MSP 面）→ 403 + reasonCode。
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"approved"}`, env.custA.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/changes/%d/status", change.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeActionNotAllowed)
	assert.Equal(t, 1, writer.calls)

	// 未分配客户（变更在 C 租户）→ 403 + reasonCode。
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"scheduled"}`, env.custC.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/changes/%d/status", env.changeC.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeMSPAllocationRequired)
	assert.Equal(t, 1, writer.calls)

	// 域状态机拒绝（409 语义）→ ConflictCode + reasonCode。
	writer.err = service.NewCustomerAccessError(service.CodeInvalidTransition, "从 'submitted' 到 'scheduled'")
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"scheduled"}`, env.custA.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/changes/%d/status", change.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeInvalidTransition)
}

func TestWorkbenchServiceRequestStatus_Update(t *testing.T) {
	env := newRecordsHandlerEnv(t)
	logger := zaptest.NewLogger(t).Sugar()
	workbench := service.NewMSPWorkbenchService(env.client, nil, nil, logger)
	writer := &stubRecordRequestWriter{}
	workbench.SetRequestStatusWriter(writer)

	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, workbench, nil, nil, nil, nil, nil, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-agent")
		c.Set("tenant_id", env.provider.ID)
		c.Set(middleware.MSPContextKey, recordsActorCtx([]int{env.custA.ID}))
		c.Next()
	})
	r.POST("/api/v1/msp/service-requests/:id/status", h.UpdateWorkbenchServiceRequestStatus)

	sr, err := env.client.ServiceRequest.Query().Where(entSR.TenantIDEQ(env.custA.ID)).Only(context.Background())
	require.NoError(t, err)

	body := fmt.Sprintf(`{"customerTenantId":%d,"status":"provisioning"}`, env.custA.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/service-requests/%d/status", sr.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, writer.calls)
	assert.Equal(t, env.custA.ID, writer.lastTenantID)
	assert.Equal(t, sr.ID, writer.lastID)
	assert.Equal(t, "provisioning", writer.lastStatus)

	// 审批类状态（不在 MSP 面）→ 403 + reasonCode。
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"approved"}`, env.custA.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/service-requests/%d/status", sr.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeActionNotAllowed)
	assert.Equal(t, 1, writer.calls)

	// 未分配客户（SR 在 C 租户）→ 403 + reasonCode。
	body = fmt.Sprintf(`{"customerTenantId":%d,"status":"provisioning"}`, env.custC.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/service-requests/%d/status", env.serviceRequestC.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), service.CodeMSPAllocationRequired)
	assert.Equal(t, 1, writer.calls)

	// 缺 customerTenantId → 400。
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/msp/service-requests/%d/status", sr.ID), strings.NewReader(`{"status":"provisioning"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 1, writer.calls)
}

// TestTranslateChangeDomainError 锁定变更域哨兵错误的分流口径（与客户侧 handler 一致）：
// 状态机/并发 → 409 语义（BusinessError Conflict）；非审批人 → 403；不存在 → 404；未知错误原样上抛。
func TestTranslateChangeDomainError(t *testing.T) {
	conflict := translateChangeDomainError(changeDomain.ErrInvalidTransition)
	biz, ok := conflict.(*common.BusinessError)
	require.True(t, ok)
	assert.Equal(t, common.ConflictCode, biz.Code)

	conflict = translateChangeDomainError(fmt.Errorf("wrap: %w", changeDomain.ErrConcurrentModification))
	biz, ok = conflict.(*common.BusinessError)
	require.True(t, ok)
	assert.Equal(t, common.ConflictCode, biz.Code)

	forbidden := translateChangeDomainError(changeDomain.ErrNotApprover)
	biz, ok = forbidden.(*common.BusinessError)
	require.True(t, ok)
	assert.Equal(t, common.ForbiddenCode, biz.Code)

	notFound := translateChangeDomainError(changeDomain.ErrChangeNotFound)
	biz, ok = notFound.(*common.BusinessError)
	require.True(t, ok)
	assert.Equal(t, common.NotFoundErrorCode, biz.Code)

	raw := errors.New("boom")
	assert.Equal(t, raw, translateChangeDomainError(raw))
	assert.NoError(t, translateChangeDomainError(nil))
}
