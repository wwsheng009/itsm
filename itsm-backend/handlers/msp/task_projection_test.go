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

// 跨租户单据流转 P2-②：任务投影 handler 契约测试（替身服务，覆盖状态码/参数透传/fail-closed）。

type stubTaskProjection struct {
	listResp     *dto.MSPTaskListResponse
	listErr      error
	claimItem    *dto.MSPTaskItem
	claimErr     error
	completeErr  error
	lastActor    service.MSPWorkbenchActor
	lastTaskID   string
	lastTenantID int
	lastVars     map[string]interface{}
}

func (s *stubTaskProjection) ListTasks(_ context.Context, actor service.MSPWorkbenchActor, _ dto.MSPTaskQuery) (*dto.MSPTaskListResponse, error) {
	s.lastActor = actor
	return s.listResp, s.listErr
}

func (s *stubTaskProjection) ClaimTask(_ context.Context, actor service.MSPWorkbenchActor, taskID string, declaredTenantID int) (*dto.MSPTaskItem, error) {
	s.lastActor, s.lastTaskID, s.lastTenantID = actor, taskID, declaredTenantID
	return s.claimItem, s.claimErr
}

func (s *stubTaskProjection) CompleteTask(_ context.Context, actor service.MSPWorkbenchActor, taskID string, declaredTenantID int, variables map[string]interface{}) error {
	s.lastActor, s.lastTaskID, s.lastTenantID, s.lastVars = actor, taskID, declaredTenantID, variables
	return s.completeErr
}

func newTaskProjectionTestRouter(t *testing.T, stub *stubTaskProjection, mspCtx *middleware.MSPContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil, stub, logger)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set("username", "msp-agent")
		c.Set("tenant_id", 3)
		if mspCtx != nil {
			c.Set(middleware.MSPContextKey, mspCtx)
		}
		c.Next()
	})
	r.GET("/api/v1/msp/workbench/tasks", h.ListWorkbenchTasks)
	r.POST("/api/v1/msp/workbench/tasks/:taskId/claim", h.ClaimWorkbenchTask)
	r.POST("/api/v1/msp/workbench/tasks/:taskId/complete", h.CompleteWorkbenchTask)
	return r
}

func TestListWorkbenchTasks_Success(t *testing.T) {
	stub := &stubTaskProjection{listResp: &dto.MSPTaskListResponse{
		Items: []dto.MSPTaskItem{{TaskID: "TASK-1", TaskName: "服务商处理", CustomerTenantID: 11, Source: "queue"}},
		Total: 1,
	}}
	r := newTaskProjectionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/msp/workbench/tasks?customerTenantIds=11,12&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code int                     `json:"code"`
		Data dto.MSPTaskListResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, 1, env.Data.Total)
	require.Len(t, env.Data.Items, 1)
	assert.Equal(t, "TASK-1", env.Data.Items[0].TaskID)
	assert.Equal(t, 7, stub.lastActor.UserID)
	assert.Equal(t, "provider_admin", stub.lastActor.MSPRole)
}

func TestClaimWorkbenchTask_Success(t *testing.T) {
	stub := &stubTaskProjection{claimItem: &dto.MSPTaskItem{TaskID: "TASK-1", Assignee: "7", Status: "assigned"}}
	r := newTaskProjectionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/workbench/tasks/TASK-1/claim", strings.NewReader(`{"customerTenantId":11}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "TASK-1", stub.lastTaskID)
	assert.Equal(t, 11, stub.lastTenantID)
}

func TestCompleteWorkbenchTask_Success(t *testing.T) {
	stub := &stubTaskProjection{}
	r := newTaskProjectionTestRouter(t, stub, mspActorCtx())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/msp/workbench/tasks/TASK-1/complete", strings.NewReader(`{"customerTenantId":11,"variables":{"approved":true}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "TASK-1", stub.lastTaskID)
	assert.Equal(t, 11, stub.lastTenantID)
	assert.Equal(t, true, stub.lastVars["approved"])
}

func TestWorkbenchTasks_NotFoundAndDenied(t *testing.T) {
	cases := []struct {
		name   string
		stub   *stubTaskProjection
		method string
		path   string
		status int
		reason string
	}{
		{"claim_not_found", &stubTaskProjection{claimErr: service.ErrMSPTaskNotFound}, http.MethodPost, "/api/v1/msp/workbench/tasks/TASK-x/claim", http.StatusNotFound, ""},
		{"complete_not_found", &stubTaskProjection{completeErr: service.ErrMSPTaskNotFound}, http.MethodPost, "/api/v1/msp/workbench/tasks/TASK-x/complete", http.StatusNotFound, ""},
		{"claim_denied", &stubTaskProjection{claimErr: service.NewCustomerAccessError(service.CodeActionNotAllowed, "任务已被他人认领")}, http.MethodPost, "/api/v1/msp/workbench/tasks/TASK-x/claim", http.StatusForbidden, "ACTION_NOT_ALLOWED"},
		{"list_unallocated", &stubTaskProjection{listErr: service.NewCustomerAccessError(service.CodeMSPAllocationRequired, "未分配到客户租户")}, http.MethodGet, "/api/v1/msp/workbench/tasks?customerTenantIds=99", http.StatusForbidden, "MSP_ALLOCATION_REQUIRED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTaskProjectionTestRouter(t, tc.stub, mspActorCtx())
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			if tc.reason != "" {
				assert.Contains(t, w.Body.String(), tc.reason)
			}
		})
	}
}

func TestWorkbenchTasks_NoMSPContextFailsClosed(t *testing.T) {
	stub := &stubTaskProjection{}
	r := newTaskProjectionTestRouter(t, stub, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/msp/workbench/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, stub.lastActor.UserID, "无 MSP 上下文不得触达服务层")
}

func TestListWorkbenchTasks_BadParams(t *testing.T) {
	stub := &stubTaskProjection{}
	r := newTaskProjectionTestRouter(t, stub, mspActorCtx())

	for _, path := range []string{
		"/api/v1/msp/workbench/tasks?customerTenantIds=abc",
		"/api/v1/msp/workbench/tasks?limit=-1",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
	}
}
