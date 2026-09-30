package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botstep"
	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// B1-02/B1-03 工具事件旁路（botToolObserver）的 UT：
//   - 事件原样透传（前端契约不变）；
//   - done/failed 落一条 tool 步骤 + tool_call 事件，并广播 v2 step 事件；
//   - pending（写路径待审批）不产生步骤/运行事件，但必须透传；
//   - 运行收口后不再接受步骤写入（审计一致性优先）。
//
// 预算判定不在此（B1-03 上移到执行点闸门 admitTool）：准入判定与收口由
// `run_manager_test.go`（ReserveToolCall/BudgetExceeded）与 botintegration 端到端用例覆盖。
func newObserverService(t *testing.T, budget bot.Budget) (*Service, *bot.Run, *ent.Client) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "observer.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	manager := bot.NewManager(bot.NewRunStore(client), budget)
	run, err := manager.Start(context.Background(), bot.StartRunInput{TenantID: 1, Entrypoint: "chat"})
	require.NoError(t, err)

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return svc, run, client
}

func TestBotToolObserver_ForwardsAndRecords(t *testing.T) {
	ctx := context.Background()
	svc, run, client := newObserverService(t, bot.Budget{MaxToolCalls: 5})

	var forwarded []string
	var runEvents []string
	observer := svc.botToolObserver(run, func(eventType string, payload map[string]any) {
		runEvents = append(runEvents, eventType)
		require.Equal(t, run.ID(), payload["runId"], "v2 事件必须带 runId")
	}, func(event ToolStreamEvent) {
		forwarded = append(forwarded, event.Status)
	})

	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "started"})
	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "done", DurationMs: 12, ID: 42})
	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "failed", ErrorCode: "tool_execution_failed"})
	// 非工具事件状态（如 pending）不产生步骤/事件，但必须原样透传。
	observer(ToolStreamEvent{Tool: "ticket_update", Provider: "builtin", Phase: "write", Status: "pending"})

	assert.Equal(t, []string{"started", "done", "failed", "pending"}, forwarded, "事件必须原样透传（契约不变）")
	assert.Equal(t, []string{"step", "step"}, runEvents, "done/failed 各广播一条 v2 step 事件")
	assert.Zero(t, run.ToolCalls(), "B1-03：观察者不做预算计数（计数在执行点闸门）")

	steps, err := client.BotStep.Query().Where(botstep.RunID(run.ID())).All(ctx)
	require.NoError(t, err)
	require.Len(t, steps, 2, "done/failed 各落一条 tool 步骤")
	assert.Equal(t, "tool", steps[0].Type)
	assert.Equal(t, "tool_invocation:42", steps[0].PayloadRef, "有 invocation id 时以 id 为引用")
	assert.Equal(t, 12, steps[0].DurationMs)
	assert.Equal(t, "ticket_read", steps[1].PayloadRef, "无 id 时退回工具名")

	events, err := client.BotEvent.Query().Where(botevent.RunID(run.ID())).All(ctx)
	require.NoError(t, err)
	require.Len(t, events, 2, "done/failed 各落一条运行事件")
	assert.Equal(t, "tool_call", events[0].Type)
	assert.Contains(t, events[0].PayloadJSON, `"status":"done"`)
	assert.Contains(t, events[1].PayloadJSON, `"status":"failed"`)
	// seq 连续（先落库后广播的审计同源基础）。
	assert.Equal(t, 0, events[0].Seq)
	assert.Equal(t, 1, events[1].Seq)
}

// TestBotToolObserver_StepRecordRejectedAfterFinish 固定「运行收口后拒绝新步骤」的边界：
// 观察者仍透传前端事件，但不得写入已收口运行的档案。
func TestBotToolObserver_StepRecordRejectedAfterFinish(t *testing.T) {
	ctx := context.Background()
	svc, run, client := newObserverService(t, bot.Budget{})

	require.NoError(t, run.Finish(ctx, bot.RunStatusCompleted, ""))

	var forwarded int
	observer := svc.botToolObserver(run, nil, func(ToolStreamEvent) { forwarded++ })
	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "done", DurationMs: 3})

	assert.Equal(t, 1, forwarded, "前端事件仍透传（收口不影响流内可见性）")
	steps, err := client.BotStep.Query().Where(botstep.RunID(run.ID())).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, steps, "已收口运行不再接受步骤写入")
}

// TestToolEventErrorCode_BudgetGate 锁定执行点闸门错误的错误码映射（B1-03）：
// 前端据此把「预算拒绝」与「执行失败」区分开。
func TestToolEventErrorCode_BudgetGate(t *testing.T) {
	assert.Equal(t, bot.ErrorCodeBudgetExceeded, toolEventErrorCode(
		fmt.Errorf("本次对话已超出工具调用预算: %w", bot.ErrBudgetExceeded)))
}
