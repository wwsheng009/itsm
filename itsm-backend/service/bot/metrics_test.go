package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// B4-02 指标聚合测试：真实 ent/sqlite 库 + 固定时钟（过期判定可确定）。

type metricsEnv struct {
	svc      *MetricsService
	ctx      context.Context
	tenantID int
	otherID  int
	now      time.Time
}

func newMetricsEnv(t *testing.T) *metricsEnv {
	t.Helper()
	ctx := context.Background()
	dsn := "file:bot-metrics-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("m-1").SetName("指标租户").SaveX(ctx)
	other := client.Tenant.Create().SetCode("m-2").SetName("其他租户").SaveX(ctx)

	svc := NewMetricsService(client)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	return &metricsEnv{svc: svc, ctx: ctx, tenantID: tenant.ID, otherID: other.ID, now: now}
}

// fixture 落一批确定数据：
//   - runs：2 completed（时长 10s/30s）、1 failed（20s）、1 running、1 cancelled
//   - steps：每个 run 1 个 llm 步骤 + 1 个 tool 步骤（100ms/200ms…）
//   - invocations：工具成功/失败、verify verified/failed、确认 approved/rejected/expired/pending
func (e *metricsEnv) fixture(t *testing.T) map[string]int {
	t.Helper()
	ctx := e.ctx
	client := e.svc.client
	base := e.now.Add(-2 * time.Hour)

	mkRun := func(entrypoint string, botID int, status string, started time.Time, finished *time.Time) int {
		create := client.BotRun.Create().
			SetTenantID(e.tenantID).
			SetEntrypoint(entrypoint).
			SetStatus(status).
			SetStartedAt(started)
		if botID > 0 {
			create = create.SetBotID(botID)
		}
		if finished != nil {
			create = create.SetFinishedAt(*finished)
		}
		return create.SaveX(ctx).ID
	}
	at := func(d time.Duration) time.Time { return base.Add(d) }
	ptr := func(t time.Time) *time.Time { return &t }

	r1 := mkRun("chat", 7, "completed", at(0), ptr(at(10*time.Second)))
	r2 := mkRun("ticket_detail", 0, "completed", at(0), ptr(at(30*time.Second)))
	r3 := mkRun("chat", 7, "failed", at(0), ptr(at(20*time.Second)))
	r4 := mkRun("chat", 0, "running", at(0), nil)
	r5 := mkRun("ci_detail", 0, "cancelled", at(0), nil)

	mkStep := func(runID, index int, typ string, durMs int) {
		client.BotStep.Create().
			SetTenantID(e.tenantID).SetRunID(runID).SetStepIndex(index).
			SetType(typ).SetDurationMs(durMs).SetCreatedAt(base).
			SaveX(ctx)
	}
	for i, runID := range []int{r1, r2, r3, r4, r5} {
		mkStep(runID, 0, "llm", 100+i*10)
		mkStep(runID, 1, "tool", 200+i*10)
	}
	mkStep(r1, 2, "confirm", 50)

	mkInv := func(runID int, tool string, status string, durMs int, errCode string) int {
		return client.ToolInvocation.Create().
			SetTenantID(e.tenantID).SetRunID(runID).SetToolName(tool).
			SetStatus(status).SetDurationMs(durMs).SetErrorCode(errCode).
			SetCreatedAt(base).
			SaveX(ctx).ID
	}
	// 执行面：4 笔（list_tickets 2 笔用于 Top 榜确定性），verify 态落在同一行上（真实形态）。
	setVerify := func(id int, state string) {
		client.ToolInvocation.UpdateOneID(id).SetVerifyState(state).ExecX(ctx)
	}
	setVerify(mkInv(r1, "list_tickets", "success", 120, ""), "verified")
	setVerify(mkInv(r1, "list_tickets", "success", 80, ""), "skipped")
	setVerify(mkInv(r3, "create_ticket", "failed", 900, "provider_error"), "failed")
	setVerify(mkInv(r4, "update_ticket", "success", 100, ""), "pending")

	// 确认单四态（approval_state + expires_at）。
	//
	// 真实形态：一条 invocation 从 pending → approved → 执行（status/duration 落同一行）；
	// 故 approved 行按「已执行」计入工具指标，rejected/expired/pending 行不计入。
	mkConfirm := func(runID int, state string, expiresAt *time.Time, approvedAt *time.Time, executed bool) {
		create := client.ToolInvocation.Create().
			SetTenantID(e.tenantID).SetRunID(runID).SetToolName("update_ticket").
			SetStatus("pending").SetNeedsApproval(true).SetApprovalState(state).
			SetCreatedAt(base)
		if executed {
			create = create.SetStatus("success").SetDurationMs(60)
		}
		if expiresAt != nil {
			create = create.SetExpiresAt(*expiresAt)
		}
		if approvedAt != nil {
			create = create.SetApprovedAt(*approvedAt)
		}
		create.SaveX(ctx)
	}
	mkConfirm(r1, "approved", ptr(e.now.Add(time.Hour)), ptr(base.Add(2*time.Minute)), true)
	mkConfirm(r1, "rejected", ptr(e.now.Add(time.Hour)), ptr(base.Add(3*time.Minute)), false)
	mkConfirm(r2, "pending", ptr(e.now.Add(-time.Hour)), nil, false) // 已过期仍挂 pending
	mkConfirm(r2, "pending", ptr(e.now.Add(time.Hour)), nil, false)  // 真 pending

	// 其他租户的数据不得混入。
	otherRun := client.BotRun.Create().
		SetTenantID(e.otherID).SetEntrypoint("chat").SetStatus("failed").
		SetStartedAt(base).SaveX(ctx).ID
	client.ToolInvocation.Create().
		SetTenantID(e.otherID).SetRunID(otherRun).SetToolName("create_ticket").
		SetStatus("failed").SetErrorCode("boom").SetCreatedAt(base).SaveX(ctx)

	return map[string]int{"r1": r1, "r2": r2, "r3": r3, "r4": r4, "r5": r5}
}

func TestMetricsService_SummaryAggregates(t *testing.T) {
	env := newMetricsEnv(t)
	env.fixture(t)

	got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{})
	require.NoError(t, err)

	assert.Equal(t, MetricsDefaultDays, got.WindowDays)
	assert.False(t, got.TokensRecorded)
	assert.NotEmpty(t, got.Notes, "token 未接线须在 Notes 中明示")

	// 运行：2 completed / 1 failed / 1 running / 1 cancelled。
	assert.Equal(t, 5, got.Runs.Total)
	assert.Equal(t, 2, got.Runs.Completed)
	assert.Equal(t, 1, got.Runs.Failed)
	assert.Equal(t, 1, got.Runs.Running)
	assert.Equal(t, 1, got.Runs.Cancelled)
	assert.InDelta(t, 0.667, got.Runs.SuccessRate, 0.001)
	// 完成时长：10s、30s、20s → 平均 20s。
	assert.InDelta(t, 20000, got.Runs.AvgDurationMs, 1)

	// 步骤：11 步（5 run × 2 + 1 confirm）。
	assert.Equal(t, 11, got.Steps.Total)
	assert.Equal(t, 5, got.Steps.LLMSteps)
	assert.Equal(t, 5, got.Steps.ToolSteps)
	assert.Equal(t, 1, got.Steps.ConfirmSteps)
	assert.InDelta(t, 11.0/5.0, got.Steps.AvgPerRun, 0.001)
	assert.InDelta(t, (100+110+120+130+140+200+210+220+230+240+50)/11.0, got.Steps.AvgDurationMs, 0.5)

	// 工具：执行面 5 笔（4 直接执行 + 1 审批通过后执行）；未执行的 pending/rejected 不计入。
	assert.Equal(t, 5, got.Tools.Total)
	assert.Equal(t, 1, got.Tools.Errors)
	assert.InDelta(t, 0.2, got.Tools.ErrorRate, 0.001)
	assert.InDelta(t, (120+80+900+100+60)/5.0, got.Tools.AvgDurationMs, 0.5)
	require.NotEmpty(t, got.Tools.TopTools)
	assert.Equal(t, "list_tickets", got.Tools.TopTools[0].Key)
	assert.Equal(t, 2, got.Tools.TopTools[0].Count)

	// verify：verified 1 / failed 1 / skipped 1 / pending 1。
	assert.Equal(t, 1, got.Verify.Verified)
	assert.Equal(t, 1, got.Verify.Failed)
	assert.Equal(t, 1, got.Verify.Skipped)
	assert.Equal(t, 1, got.Verify.Pending)
	assert.InDelta(t, 0.5, got.Verify.FailRate, 0.001)

	// 确认：approved 1 / rejected 1 / expired 1 / pending 1。
	assert.Equal(t, 1, got.Confirmations.Approved)
	assert.Equal(t, 1, got.Confirmations.Rejected)
	assert.Equal(t, 1, got.Confirmations.Expired)
	assert.Equal(t, 1, got.Confirmations.Pending)
	assert.InDelta(t, 0.333, got.Confirmations.ApprovalRate, 0.001)
	assert.InDelta(t, 0.333, got.Confirmations.RejectRate, 0.001)
	assert.InDelta(t, 0.333, got.Confirmations.ExpireRate, 0.001)

	// 成本代理：LLM 步 5、工具调用 5、步数 11。
	assert.Equal(t, 5, got.Cost.LLMCalls)
	assert.Equal(t, 5, got.Cost.ToolCalls)
	assert.Equal(t, 11, got.Cost.Steps)
	assert.InDelta(t, 11.0/5.0, got.Cost.AvgStepsPerRun, 0.001)
	assert.InDelta(t, 5.0/5.0, got.Cost.AvgToolCallsPerRun, 0.001)

	// 分解：入口 chat=3 run，bot#7=2 run，ticket_detail=1，ci_detail=1，兼容默认=2。
	byEntry := map[string]Breakdown{}
	for _, b := range got.ByEntrypoint {
		byEntry[b.Key] = b
	}
	assert.Equal(t, 3, byEntry["chat"].Runs)
	assert.Equal(t, 1, byEntry["ticket_detail"].Runs)
	assert.Equal(t, 1, byEntry["ci_detail"].Runs)
	assert.Equal(t, 1, byEntry["chat"].Failed)

	byBot := map[string]Breakdown{}
	for _, b := range got.ByBot {
		byBot[b.Key] = b
	}
	assert.Equal(t, 2, byBot["bot#7"].Runs)
	assert.Equal(t, 3, byBot["compat-default"].Runs)

	// 其他租户数据不得混入。
	assert.Equal(t, 5, got.Runs.Total)
	assert.Equal(t, 5, got.Tools.Total)
}

func TestMetricsService_Filters(t *testing.T) {
	env := newMetricsEnv(t)
	env.fixture(t)

	t.Run("按 Bot 过滤", func(t *testing.T) {
		got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{BotID: 7})
		require.NoError(t, err)
		assert.Equal(t, 2, got.Runs.Total)
		assert.Equal(t, 1, got.Runs.Completed)
		assert.Equal(t, 1, got.Runs.Failed)
		// 归属 bot#7 的工具调用 4 笔（r1 的 2 笔 + 审批通过后执行 1 笔 + r3 的 1 笔失败）；
		// 未执行的 rejected/pending/expired 不计入执行面。
		assert.Equal(t, 4, got.Tools.Total)
		// 步骤按 run 归属收敛：2 run × 2 步 + 1 confirm = 5。
		assert.Equal(t, 5, got.Steps.Total)
	})

	t.Run("按入口过滤", func(t *testing.T) {
		got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{Entrypoint: "ticket_detail"})
		require.NoError(t, err)
		assert.Equal(t, 1, got.Runs.Total)
		assert.Equal(t, 1, got.Runs.Completed)
		assert.Equal(t, 0, got.Runs.Failed)
		assert.Equal(t, 1.0, got.Runs.SuccessRate)
		assert.Equal(t, 0, got.Tools.Total)
		assert.Equal(t, 2, got.Steps.Total)
	})
}

func TestMetricsService_ZeroSafeAndValidation(t *testing.T) {
	env := newMetricsEnv(t)

	got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{})
	require.NoError(t, err)
	assert.Zero(t, got.Runs.Total)
	assert.Zero(t, got.Runs.SuccessRate, "空数据不得产出 NaN")
	assert.Zero(t, got.Tools.ErrorRate)
	assert.Zero(t, got.Verify.FailRate)
	assert.Zero(t, got.Confirmations.ApprovalRate)
	assert.Empty(t, got.ByEntrypoint)

	t.Run("参数校验", func(t *testing.T) {
		_, err := NewMetricsService(nil).Summary(env.ctx, env.tenantID, MetricsQuery{})
		require.Error(t, err)
		_, err = env.svc.Summary(env.ctx, 0, MetricsQuery{})
		require.Error(t, err)
	})

	t.Run("窗口收敛", func(t *testing.T) {
		got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{Days: MetricsMaxDays + 100})
		require.NoError(t, err)
		assert.Equal(t, MetricsMaxDays, got.WindowDays)
		got, err = env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{Days: -3})
		require.NoError(t, err)
		assert.Equal(t, MetricsDefaultDays, got.WindowDays)
	})

	t.Run("窗口外数据不计入", func(t *testing.T) {
		client := env.svc.client
		old := env.now.AddDate(0, 0, -(MetricsMaxDays + 5))
		client.BotRun.Create().
			SetTenantID(env.tenantID).SetEntrypoint("chat").SetStatus("failed").
			SetStartedAt(old).SaveX(env.ctx)
		got, err := env.svc.Summary(env.ctx, env.tenantID, MetricsQuery{})
		require.NoError(t, err)
		assert.Zero(t, got.Runs.Total, "超出窗口的运行不得计入")
	})
}
