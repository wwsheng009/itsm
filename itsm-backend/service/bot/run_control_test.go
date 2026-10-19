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

// I2-01 监督面数据面：运行取消（条件更新/幂等/租户隔离）与运行列表（计数/过滤）。

func TestRunStore_CancelRun(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:run-cancel?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("rc-1").SetName("取消租户").SaveX(ctx)
	other := client.Tenant.Create().SetCode("rc-2").SetName("其他租户").SaveX(ctx)

	running := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus("running").
		SetStartedAt(time.Now()).SaveX(ctx)
	finished := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus("completed").
		SetStartedAt(time.Now()).SaveX(ctx)
	otherRunning := client.BotRun.Create().
		SetTenantID(other.ID).SetEntrypoint("chat").SetStatus("running").
		SetStartedAt(time.Now()).SaveX(ctx)

	store := NewRunStore(client)
	cancelled, err := store.CancelRun(ctx, tenant.ID, running.ID, "pause_by_admin")
	require.NoError(t, err)
	assert.True(t, cancelled)

	row := client.BotRun.GetX(ctx, running.ID)
	assert.Equal(t, "cancelled", row.Status)
	assert.Equal(t, "pause_by_admin", row.ErrorCode)
	require.NotNil(t, row.FinishedAt, "取消需写入 finished_at")

	// 幂等：再次取消 → false（已非 running）。
	cancelled, err = store.CancelRun(ctx, tenant.ID, running.ID, "")
	require.NoError(t, err)
	assert.False(t, cancelled)

	// 已收口 → false；跨租户 → false 且不改变他租户状态。
	cancelled, _ = store.CancelRun(ctx, tenant.ID, finished.ID, "")
	assert.False(t, cancelled)
	cancelled, _ = store.CancelRun(ctx, tenant.ID, otherRunning.ID, "")
	assert.False(t, cancelled)
	assert.Equal(t, "running", client.BotRun.GetX(ctx, otherRunning.ID).Status)

	// 未装配 → 明确错误。
	var nilStore *RunStore
	_, err = nilStore.CancelRun(ctx, tenant.ID, running.ID, "")
	require.Error(t, err)
}

func TestTraceService_ListRuns(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:run-list?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("rl-1").SetName("列表租户").SaveX(ctx)
	other := client.Tenant.Create().SetCode("rl-2").SetName("其他租户").SaveX(ctx)
	base := time.Now().Add(-time.Hour)

	running := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("scheduler").SetStatus("running").
		SetStartedAt(base).SaveX(ctx)
	completed := client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus("completed").
		SetStartedAt(base.Add(-time.Hour)).SaveX(ctx)
	client.BotRun.Create().
		SetTenantID(other.ID).SetEntrypoint("chat").SetStatus("running").
		SetStartedAt(base).SaveX(ctx)

	// running：2 步骤 / 2 工具调用（1 待审批 + 1 已批准）/ 1 产物。
	client.BotStep.Create().SetTenantID(tenant.ID).SetRunID(running.ID).SetStepIndex(0).SetType("llm").SaveX(ctx)
	client.BotStep.Create().SetTenantID(tenant.ID).SetRunID(running.ID).SetStepIndex(1).SetType("tool").SaveX(ctx)
	client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetRunID(running.ID).SetToolName("create_ticket").
		SetStatus("pending").SetNeedsApproval(true).SetApprovalState("pending").SaveX(ctx)
	client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetRunID(running.ID).SetToolName("list_tickets").
		SetStatus("success").SetApprovalState("auto").SaveX(ctx)
	client.BotArtifact.Create().
		SetTenantID(tenant.ID).SetOwnerUserID(1).SetRunID(running.ID).
		SetKind("analysis").SetToolName("analyze").SetTitle("影响分析").SaveX(ctx)

	svc := NewTraceService(client)
	rows, err := svc.ListRuns(ctx, tenant.ID, RunListFilter{Days: 7})
	require.NoError(t, err)
	require.Len(t, rows, 2, "仅本租户运行（跨租户不可见）")
	assert.Equal(t, running.ID, rows[0].ID, "按 started_at 倒序")
	assert.Equal(t, 2, rows[0].Steps)
	assert.Equal(t, 2, rows[0].ToolCalls)
	assert.Equal(t, 1, rows[0].PendingApprovals, "仅 pending 计入待审批")
	assert.Equal(t, 1, rows[0].Artifacts)
	assert.Equal(t, "scheduler", rows[0].Entrypoint)
	assert.Equal(t, 0, rows[1].Steps)

	filtered, err := svc.ListRuns(ctx, tenant.ID, RunListFilter{Days: 7, Status: "completed"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, completed.ID, filtered[0].ID)

	// 窗口过滤：更早的运行（8 天前）在 days=7 之外。
	client.BotRun.Create().
		SetTenantID(tenant.ID).SetEntrypoint("chat").SetStatus("completed").
		SetStartedAt(time.Now().AddDate(0, 0, -8)).SaveX(ctx)
	windowed, err := svc.ListRuns(ctx, tenant.ID, RunListFilter{Days: 7})
	require.NoError(t, err)
	assert.Len(t, windowed, 2)

	// 空租户 → 空列表（非 nil）。
	empty, err := svc.ListRuns(ctx, other.ID+1000, RunListFilter{})
	require.NoError(t, err)
	assert.Empty(t, empty)
}
