package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/ai"
	"itsm-backend/service/bot"
)

// I2-01 Autopilot 监督面端点契约：运行列表、暂停（取消）、回滚（冻结待审批 + 通知中心）、审批按 runId 联动。

type autopilotRunsResponse struct {
	Code int `json:"code"`
	Data struct {
		Items []struct {
			ID               int    `json:"id"`
			Status           string `json:"status"`
			Entrypoint       string `json:"entrypoint"`
			Steps            int    `json:"steps"`
			ToolCalls        int    `json:"toolCalls"`
			PendingApprovals int    `json:"pendingApprovals"`
			Artifacts        int    `json:"artifacts"`
		} `json:"items"`
		Days   int    `json:"days"`
		Status string `json:"status"`
	} `json:"data"`
}

type autopilotActionResponse struct {
	Code int `json:"code"`
	Data struct {
		RunID               int  `json:"runId"`
		Cancelled           bool `json:"cancelled"`
		RejectedInvocations int  `json:"rejectedInvocations"`
		Notifications       int  `json:"notifications"`
	} `json:"data"`
}

type autopilotEnv struct {
	client   *ent.Client
	tenantID int
	otherID  int
	actorID  int
	router   *gin.Engine
	runningA int
	runningB int
	doneID   int
	pendingA int
}

func setupAutopilotRouter(t *testing.T, withSupervision bool) *autopilotEnv {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:autopilot-%s-%v?mode=memory&cache=shared&_fk=1", t.Name(), withSupervision))
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetName("监督租户").SetCode("auto-1").SetDomain("auto.test").SaveX(ctx)
	other := client.Tenant.Create().SetName("其他租户").SetCode("auto-2").SetDomain("auto2.test").SaveX(ctx)
	actor := client.User.Create().
		SetUsername("auto-actor").SetEmail("actor@example.com").SetName("操作人").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetRole(user.RoleAdmin).SaveX(ctx)

	base := time.Now().Add(-time.Hour)
	// 运行 A：running（2 步骤 / 2 工具调用（1 待审批）/ 1 产物）；运行 B：running（回滚用例）；运行 C：completed。
	runningA := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("scheduler").SetStatus("running").SetStartedAt(base).SaveX(ctx).ID
	runningB := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("scheduler").SetStatus("running").SetStartedAt(base.Add(time.Minute)).SaveX(ctx).ID
	done := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus("completed").SetStartedAt(base.Add(-time.Hour)).SaveX(ctx).ID
	// 其他租户 running（隔离断言）。
	client.BotRun.Create().
		SetTenantID(other.ID).SetEntrypoint("chat").SetStatus("running").SetStartedAt(base).SaveX(ctx)

	client.BotStep.Create().SetTenantID(tenant.ID).SetRunID(runningA).SetStepIndex(0).SetType("llm").SaveX(ctx)
	client.BotStep.Create().SetTenantID(tenant.ID).SetRunID(runningA).SetStepIndex(1).SetType("tool").SaveX(ctx)
	pending := client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetRunID(runningA).SetToolName("create_ticket").
		SetStatus("pending").SetNeedsApproval(true).SetApprovalState("pending").SaveX(ctx)
	client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetRunID(runningA).SetToolName("list_tickets").
		SetStatus("success").SetApprovalState("auto").SaveX(ctx)
	client.BotArtifact.Create().
		SetTenantID(tenant.ID).SetOwnerUserID(actor.ID).SetRunID(runningA).
		SetKind("analysis").SetToolName("analyze_ci_impact_plan").SetTitle("影响分析").SaveX(ctx)
	// 运行 B 的待审批（回滚冻结对象）。
	client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetRunID(runningB).SetToolName("update_ticket").
		SetStatus("pending").SetNeedsApproval(true).SetApprovalState("pending").SaveX(ctx)

	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	if withSupervision {
		svc.SetTraceService(bot.NewTraceService(client))
		svc.SetBotRunner(bot.NewManager(bot.NewRunStore(client), bot.DefaultBudget()))
	}
	h := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/ai/runs", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", actor.ID)
		c.Set("role", "admin")
		h.GetAutopilotRuns(c)
	})
	r.POST("/api/v1/ai/runs/:id/cancel", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", actor.ID)
		c.Set("role", "admin")
		h.CancelAutopilotRun(c)
	})
	r.POST("/api/v1/ai/runs/:id/rollback", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", actor.ID)
		c.Set("role", "admin")
		h.RollbackAutopilotRun(c)
	})
	r.GET("/api/v1/agent/tools/invocations", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", actor.ID)
		c.Set("role", "admin")
		h.ListToolInvocations(c)
	})

	return &autopilotEnv{
		client: client, tenantID: tenant.ID, otherID: other.ID, actorID: actor.ID,
		router: r, runningA: runningA, runningB: runningB, doneID: done,
		pendingA: pending.ID,
	}
}

func TestAutopilotRuns_ListWithCountsAndIsolation(t *testing.T) {
	env := setupAutopilotRouter(t, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/runs?days=7", nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body autopilotRunsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Len(t, body.Data.Items, 3, "仅本租户运行（其他租户 running 不出现）")

	byID := map[int]int{}
	for index, item := range body.Data.Items {
		byID[item.ID] = index
	}
	first := body.Data.Items[byID[env.runningB]]
	assert.Equal(t, "running", first.Status)
	aRow := body.Data.Items[byID[env.runningA]]
	assert.Equal(t, 2, aRow.Steps)
	assert.Equal(t, 2, aRow.ToolCalls)
	assert.Equal(t, 1, aRow.PendingApprovals)
	assert.Equal(t, 1, aRow.Artifacts)

	// 状态过滤 + 非法状态 400。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/runs?status=completed", nil)
	env.router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 1)
	assert.Equal(t, env.doneID, body.Data.Items[0].ID)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/runs?status=bogus", nil)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAutopilotRun_CancelAndRollback(t *testing.T) {
	env := setupAutopilotRouter(t, true)

	// 暂停（取消）运行 B：running → cancelled。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/runs/%d/cancel", env.runningB), bytes.NewBufferString(`{"reason":"pause_by_admin"}`))
	req.Header.Set("Content-Type", "application/json")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var cancelBody autopilotActionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cancelBody))
	assert.True(t, cancelBody.Data.Cancelled)
	row := env.client.BotRun.GetX(context.Background(), env.runningB)
	assert.Equal(t, "cancelled", row.Status)
	assert.Equal(t, "pause_by_admin", row.ErrorCode)

	// 幂等重放 → cancelled=false；已收口运行 → false。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/runs/%d/cancel", env.runningB), nil)
	env.router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cancelBody))
	assert.False(t, cancelBody.Data.Cancelled)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/runs/%d/cancel", env.doneID), nil)
	env.router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cancelBody))
	assert.False(t, cancelBody.Data.Cancelled)

	// 回滚运行 A：终止 running + 冻结 1 项待审批 + 通知操作人（无管理员时的回退）。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/runs/%d/rollback", env.runningA), bytes.NewBufferString(`{"reason":"wrong_plan"}`))
	req.Header.Set("Content-Type", "application/json")
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var rollbackBody autopilotActionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rollbackBody))
	assert.True(t, rollbackBody.Data.Cancelled)
	assert.Equal(t, 1, rollbackBody.Data.RejectedInvocations)
	assert.Equal(t, 1, rollbackBody.Data.Notifications)

	cancelledInv := env.client.ToolInvocation.GetX(context.Background(), env.pendingA)
	assert.Equal(t, "cancelled", cancelledInv.ApprovalState, "待审批被冻结（永不执行）")
	assert.Equal(t, "cancelled", cancelledInv.Status)
	assert.Equal(t, "wrong_plan", cancelledInv.ApprovalReason)
	assert.Equal(t, env.actorID, cancelledInv.ApprovedBy)

	rows := env.client.Notification.Query().
		Where(notification.TenantID(env.tenantID), notification.TypeEQ("ai_autopilot")).AllX(context.Background())
	require.Len(t, rows, 1)
	assert.Equal(t, env.actorID, rows[0].UserID)
	assert.Equal(t, "/ai/approval", rows[0].ActionURL)
	assert.Contains(t, rows[0].Title, fmt.Sprintf("运行 #%d", env.runningA))
}

func TestAutopilotRuns_ApprovalFilterByRunID(t *testing.T) {
	env := setupAutopilotRouter(t, true)

	// 审批联动：?runId= 只看该运行的待审批。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/agent/tools/invocations?state=pending&runId=%d", env.runningA), nil)
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				ID    int `json:"id"`
				RunID int `json:"runId"`
			} `json:"items"`
			RunID int `json:"runId"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 1)
	assert.Equal(t, env.pendingA, body.Data.Items[0].ID)
	assert.Equal(t, env.runningA, body.Data.RunID)

	// 非法 runId → 400。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/tools/invocations?runId=abc", nil)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAutopilotRuns_UnavailableWithoutWiring(t *testing.T) {
	env := setupAutopilotRouter(t, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/runs", nil)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/runs/%d/cancel", env.runningA), nil)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
