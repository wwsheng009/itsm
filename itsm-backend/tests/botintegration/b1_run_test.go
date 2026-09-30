// Package botintegration 的 B1-01 集成验收：
// 三表生命周期（run/step/event 序号分配与唯一冲突）、租户隔离、
// 以及 run_id 经上下文贯通到 tool_invocations（读审计与写待审批两条路径）。
package botintegration

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/service"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// TestB1RunStoreLifecycleAndIsolation 覆盖 RunStore 的持久化语义与租户隔离。
func TestB1RunStoreLifecycleAndIsolation(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "b1-run.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := bot.NewRunStore(client)
	require.NotNil(t, store)

	// 1) 起运行 → 记步骤 → 记事件（seq 自动从 0 递增）→ 收口。
	run, err := store.StartRun(ctx, bot.StartRunInput{TenantID: 1, ConversationID: 11, Entrypoint: "chat"})
	require.NoError(t, err)
	assert.Equal(t, "running", run.Status)
	assert.Equal(t, 11, run.ConversationID)

	_, err = store.AppendStep(ctx, 1, run.ID, 0, "llm", "", 120)
	require.NoError(t, err)
	_, err = store.AppendStep(ctx, 1, run.ID, 1, "tool", "invocation:42", 7)
	require.NoError(t, err)
	_, err = store.AppendStep(ctx, 1, run.ID, 1, "tool", "invocation:43", 7)
	require.Error(t, err, "同运行重复 step_index 必须被唯一约束拒绝")
	require.True(t, bot.IsUniqueViolation(err))

	first, err := store.AppendEvent(ctx, 1, run.ID, "run_started", map[string]any{"entrypoint": "chat"})
	require.NoError(t, err)
	assert.Equal(t, 0, first.Seq)
	second, err := store.AppendEvent(ctx, 1, run.ID, "run_finished", map[string]any{"status": "completed"})
	require.NoError(t, err)
	assert.Equal(t, 1, second.Seq, "seq 必须单调递增")
	assert.Contains(t, first.PayloadJSON, "chat")

	require.NoError(t, store.FinishRun(ctx, 1, run.ID, "completed", ""))
	finished, err := client.BotRun.Get(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", finished.Status)
	require.NotNil(t, finished.FinishedAt)

	steps, err := store.ListRunSteps(ctx, 1, run.ID)
	require.NoError(t, err)
	assert.Len(t, steps, 2)
	events, err := store.ListRunEvents(ctx, 1, run.ID)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, []int{0, 1}, []int{events[0].Seq, events[1].Seq})

	// 2) 租户隔离：另一租户既看不到运行，也不能收口该运行。
	other, err := store.StartRun(ctx, bot.StartRunInput{TenantID: 2, Entrypoint: "chat"})
	require.NoError(t, err)
	require.NoError(t, store.FinishRun(ctx, 2, other.ID, "failed", "chat_error"))
	tenantOneRuns, err := client.BotRun.Query().Where(botrun.TenantID(1)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, tenantOneRuns, 1)
	// 跨租户收口：Where(tenant_id=2) 命中不到 → 影响 0 行，但不得改动租户 1 的运行。
	require.NoError(t, store.FinishRun(ctx, 2, run.ID, "failed", "cross_tenant"))
	unchanged, err := client.BotRun.Get(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", unchanged.Status, "跨租户收口不得改写既有运行")

	// 3) 三表计数与谓词口径。
	stepsAll, err := client.BotStep.Query().Where(botstep.TenantID(1)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, stepsAll, 2)
	eventsAll, err := client.BotEvent.Query().Where(botevent.TenantID(1)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, eventsAll, 2)

	// 4) 事件唯一约束直插冲突（绕过 AppendEvent 的取号）必须被拒绝。
	_, err = client.BotEvent.Create().SetTenantID(1).SetRunID(run.ID).SetSeq(0).SetType("dup").Save(ctx)
	require.Error(t, err, "(run_id, seq) 唯一约束必须生效")
}

// b1Harness 在 B0 夹具基础上注入 RunStore，用于验证 run_id 贯通。
type b1Harness struct {
	*b0Harness
	store *bot.RunStore
}

func newB1Harness(t *testing.T) *b1Harness {
	t.Helper()
	base := newB0Harness(t)
	store := bot.NewRunStore(base.client)
	require.NotNil(t, store)
	return &b1Harness{b0Harness: base, store: store}
}

// TestB1RunIDPropagationToToolInvocations 覆盖 AB1-01 的「run_id 贯通」：
// 运行上下文内的读工具审计与写工具待审批记录都必须带 run_id；无运行上下文时为 0。
func TestB1RunIDPropagationToToolInvocations(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	run, err := h.store.StartRun(ctx, bot.StartRunInput{TenantID: h.tenantID, ConversationID: h.convID, Entrypoint: "chat"})
	require.NoError(t, err)
	runCtx := bot.WithRunID(ctx, run.ID)
	require.Equal(t, run.ID, bot.RunIDFromContext(runCtx))

	// 1) 读工具（provider 只读）：审计行必须带 run_id。
	readDef := service.ToolDefinition{
		Name: "stub__read_note", Description: "读取备注（B1-01 桩）", ReadOnly: true,
		Resource: "ticket", Action: "read", Provider: "stub", ServerName: "stub", RawToolName: "read_note",
		Risk: service.ToolRiskRead, Category: "ticket", RedactionProfile: service.ToolRedactionDefault,
		ArgsSchema: map[string]interface{}{"type": "object"},
	}
	h.provider.defs = append(h.provider.defs, readDef)
	_, _, err = h.svc.ExecuteToolWithConversation(runCtx, h.userID, h.tenantID, "super_admin", readDef.Name,
		map[string]interface{}{"id": 1}, h.convID)
	require.NoError(t, err)

	// 2) 写工具（provider 写）：待审批行必须带 run_id。
	_, pendingID, err := h.svc.ExecuteToolWithConversation(runCtx, h.userID, h.tenantID, "super_admin", "stub__create_note",
		map[string]interface{}{"title": "运行内写工具"}, h.convID)
	require.NoError(t, err)
	require.Positive(t, pendingID)

	linked, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID), toolinvocation.RunID(run.ID)).
		All(ctx)
	require.NoError(t, err)
	assert.Len(t, linked, 2, "运行内产生的调用记录必须全部带 run_id")
	for _, inv := range linked {
		assert.Equal(t, run.ID, inv.RunID)
		assert.Equal(t, h.convID, inv.ConversationID, "会话归属不变（B0-03 口径保持）")
	}

	// 3) 无运行上下文：run_id 为 0（不入列），不污染其他调用。
	_, plainID, err := h.svc.ExecuteToolWithConversation(ctx, h.userID, h.tenantID, "super_admin", "stub__create_note",
		map[string]interface{}{"title": "运行外写工具"}, h.convID)
	require.NoError(t, err)
	plain, err := h.client.ToolInvocation.Get(ctx, plainID)
	require.NoError(t, err)
	assert.Zero(t, plain.RunID, "无运行上下文不得写入 run_id")

	// 4) 事件与步骤（运行态档案）可回溯，且与调用记录同运行。
	_, err = h.store.AppendStep(ctx, h.tenantID, run.ID, 1, "tool", "invocation:"+strconv.Itoa(pendingID), 3)
	require.NoError(t, err)
	_, err = h.store.AppendEvent(ctx, h.tenantID, run.ID, "tool_call", map[string]any{"tool": "stub__create_note"})
	require.NoError(t, err)
	events, err := h.store.ListRunEvents(ctx, h.tenantID, run.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, 0, events[0].Seq)
}
