package bot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// O1-01 运行轨迹：链路完整性 + 租户隔离 + 未命中口径。

type traceEnv struct {
	svc      *TraceService
	ctx      context.Context
	client   *ent.Client
	tenantID int
	otherID  int
	userID   int
	base     time.Time
}

func newTraceEnv(t *testing.T) *traceEnv {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:bot-trace-"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("tr-1").SetName("轨迹租户").SaveX(ctx)
	other := client.Tenant.Create().SetCode("tr-2").SetName("其他租户").SaveX(ctx)

	return &traceEnv{
		svc:      NewTraceService(client),
		ctx:      ctx,
		client:   client,
		tenantID: tenant.ID,
		otherID:  other.ID,
		base:     time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC),
	}
}

// fixture 落一条完整链路：run → step/event → tool（确认/verify）→ artifact → 业务对象。
func (e *traceEnv) fixture(t *testing.T) (runID int, stepID int, artifactID int) {
	t.Helper()
	ctx := e.ctx
	base := e.base
	finished := base.Add(3 * time.Second)

	// tool_invocations.user_id 有 FK（指向 users）；建一个真实用户避免约束失败。
	user := e.client.User.Create().
		SetUsername("trace-user").SetEmail("trace@example.com").SetName("轨迹用户").
		SetPasswordHash("hash").SetTenantID(e.tenantID).SaveX(ctx)
	e.userID = user.ID

	run := e.client.BotRun.Create().
		SetTenantID(e.tenantID).
		SetConversationID(9).
		SetBotID(7).
		SetEntrypoint("ticket_detail").
		SetStatus("completed").
		SetModel("gpt-4o").
		SetTargetType("ticket").
		SetTargetID(42).
		SetStartedAt(base).
		SetFinishedAt(finished).
		SaveX(ctx)

	e.client.BotStep.Create().
		SetTenantID(e.tenantID).SetRunID(run.ID).SetStepIndex(0).
		SetType("llm").SetDurationMs(800).SetCreatedAt(base).
		SaveX(ctx)
	toolStep := e.client.BotStep.Create().
		SetTenantID(e.tenantID).SetRunID(run.ID).SetStepIndex(1).
		SetType("tool").SetPayloadRef("tool_invocation:1").SetDurationMs(120).
		SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)

	e.client.BotEvent.Create().
		SetTenantID(e.tenantID).SetRunID(run.ID).SetSeq(0).
		SetType("run_started").SetPayloadJSON(`{"entrypoint":"ticket_detail"}`).
		SetCreatedAt(base).
		SaveX(ctx)
	e.client.BotEvent.Create().
		SetTenantID(e.tenantID).SetRunID(run.ID).SetSeq(1).
		SetType("tool_call_started").SetPayloadJSON(`{"tool":"create_ticket"}`).
		SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)
	e.client.BotEvent.Create().
		SetTenantID(e.tenantID).SetRunID(run.ID).SetSeq(2).
		SetType("run_finished").SetPayloadJSON(`{"status":"completed"}`).
		SetCreatedAt(finished).
		SaveX(ctx)

	e.client.ToolInvocation.Create().
		// 注：不设 conversation_id——该列有 FK，测试库不建会话行（会话归属由 run 承载）。
		SetTenantID(e.tenantID).SetRunID(run.ID).SetStepID(toolStep.ID).
		SetToolName("create_ticket").
		// 原始参数仅留存执行真源；轨迹回放只允许出现脱敏快照（脱敏断言见下）。
		SetArguments(`{"raw":"secret-args"}`).
		SetArgsRedacted(`{"title":"***"}`).
		SetStatus("success").SetNeedsApproval(true).SetApprovalState("approved").
		SetApprovedBy(user.ID).SetApprovedAt(base.Add(time.Minute)).
		SetRisk("act_low").SetCategory("ticket").
		SetTargetType("ticket").SetTargetID("42").
		SetVerifyState("verified").SetVerifyNote("回读工单存在").
		SetDurationMs(120).SetUserID(user.ID).
		SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)

	artifact := e.client.BotArtifact.Create().
		SetTenantID(e.tenantID).SetOwnerUserID(7).SetConversationID(9).SetRunID(run.ID).
		SetKind("analysis").SetToolName("analyze_ci_impact_plan").SetTitle("影响分析").
		SetContentJSON(`{"checklist":["备份"]}`).
		SaveX(ctx)

	return run.ID, toolStep.ID, artifact.ID
}

func TestTraceService_GetRunTraceChain(t *testing.T) {
	env := newTraceEnv(t)
	runID, stepID, artifactID := env.fixture(t)

	trace, err := env.svc.GetRunTrace(env.ctx, env.tenantID, runID)
	require.NoError(t, err)
	require.NotNil(t, trace)

	// 会话 → run。
	assert.Equal(t, runID, trace.Run.ID)
	assert.Equal(t, 9, trace.Run.ConversationID)
	assert.Equal(t, "ticket_detail", trace.Run.Entrypoint)
	assert.Equal(t, "completed", trace.Run.Status)
	assert.Equal(t, "gpt-4o", trace.Run.Model)
	assert.Equal(t, "ticket", trace.Run.TargetType)
	assert.Equal(t, 42, trace.Run.TargetID)
	require.NotNil(t, trace.Run.FinishedAt)

	// 步骤（step_index 升序）。
	require.Len(t, trace.Steps, 2)
	assert.Equal(t, []int{0, 1}, []int{trace.Steps[0].StepIndex, trace.Steps[1].StepIndex})
	assert.Equal(t, "tool", trace.Steps[1].Type)
	assert.Equal(t, "tool_invocation:1", trace.Steps[1].PayloadRef)
	assert.Equal(t, 120, trace.Steps[1].DurationMs)

	// 事件（seq 升序 + payload 解码）。
	require.Len(t, trace.Events, 3)
	assert.Equal(t, []int{0, 1, 2}, []int{trace.Events[0].Seq, trace.Events[1].Seq, trace.Events[2].Seq})
	assert.Equal(t, "ticket_detail", trace.Events[0].Payload["entrypoint"])

	// 工具调用（确认/审批 + verify + 业务对象）。
	require.Len(t, trace.ToolInvocations, 1)
	tool := trace.ToolInvocations[0]
	assert.Equal(t, "create_ticket", tool.ToolName)
	assert.Equal(t, string(ConfirmationConfirmed), tool.ConfirmationState)
	assert.True(t, tool.NeedsApproval)
	assert.Equal(t, stepID, tool.StepID)
	assert.Equal(t, "verified", tool.VerifyState)
	assert.Equal(t, "ticket", tool.TargetType)
	assert.Equal(t, "42", tool.TargetID)
	assert.Equal(t, "act_low", tool.Risk)
	assert.Equal(t, env.userID, tool.ApprovedBy)
	require.NotNil(t, tool.ApprovedAt)

	// 产物。
	require.Len(t, trace.Artifacts, 1)
	assert.Equal(t, artifactID, trace.Artifacts[0].ID)
	assert.Equal(t, "analysis", trace.Artifacts[0].Kind)
	assert.Equal(t, "影响分析", trace.Artifacts[0].Title)
	assert.Equal(t, "analyze_ci_impact_plan", trace.Artifacts[0].ToolName)

	// 脱敏断言：原始参数绝不进入轨迹响应。
	raw, err := json.Marshal(trace)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "secret-args")
	assert.Contains(t, string(raw), "***")
}

func TestTraceService_TenantIsolationAndNotFound(t *testing.T) {
	env := newTraceEnv(t)
	runID, _, _ := env.fixture(t)

	// 跨租户：与不存在同口径（ErrRunNotFound），不泄漏存在性。
	_, err := env.svc.GetRunTrace(env.ctx, env.otherID, runID)
	assert.ErrorIs(t, err, ErrRunNotFound)

	// 未命中与非法入参。
	_, err = env.svc.GetRunTrace(env.ctx, env.tenantID, 999999)
	assert.ErrorIs(t, err, ErrRunNotFound)
	_, err = env.svc.GetRunTrace(env.ctx, env.tenantID, 0)
	assert.ErrorIs(t, err, ErrRunNotFound)

	// 未装配（nil）→ 非 NotFound（调用方据此返回 503）。
	var nilSvc *TraceService
	_, err = nilSvc.GetRunTrace(env.ctx, env.tenantID, runID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRunNotFound)
}
