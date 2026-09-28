package ai

import (
	"context"
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

// B1-02 工具事件旁路（botToolObserver）的 UT：
//   - 事件原样透传（前端契约不变）；
//   - started 计入工具预算，超限记 error{budget_exceeded} 且不再计数；
//   - done/failed 落一条 tool 步骤 + tool_call 事件（先落库后广播由 RunManager 保证）。
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
	observer := svc.botToolObserver(run, func(event ToolStreamEvent) {
		forwarded = append(forwarded, event.Status)
	})

	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "started"})
	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "done", DurationMs: 12, ID: 42})
	observer(ToolStreamEvent{Tool: "ticket_read", Provider: "builtin", Phase: "read", Status: "failed", ErrorCode: "tool_execution_failed"})
	// 非工具事件状态（如 pending）不产生步骤/事件，但必须原样透传。
	observer(ToolStreamEvent{Tool: "ticket_update", Provider: "builtin", Phase: "write", Status: "pending"})

	assert.Equal(t, []string{"started", "done", "failed", "pending"}, forwarded, "事件必须原样透传（契约不变）")
	assert.Equal(t, 1, run.ToolCalls(), "只有 started 计数")

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

func TestBotToolObserver_BudgetExceededStopsCounting(t *testing.T) {
	ctx := context.Background()
	svc, run, client := newObserverService(t, bot.Budget{MaxToolCalls: 1})

	var forwarded int
	observer := svc.botToolObserver(run, func(ToolStreamEvent) { forwarded++ })

	observer(ToolStreamEvent{Tool: "a", Status: "started"})
	observer(ToolStreamEvent{Tool: "b", Status: "started"}) // 超限：记 error 事件，不再计数
	observer(ToolStreamEvent{Tool: "c", Status: "started"}) // 已标记超限：既不计数也不再重复记事件
	observer(ToolStreamEvent{Tool: "a", Status: "done", DurationMs: 1})

	assert.Equal(t, 4, forwarded, "超限也不得吞掉前端事件")
	assert.Equal(t, 1, run.ToolCalls(), "超限后不再计数")

	events, err := client.BotEvent.Query().Where(botevent.RunID(run.ID())).Order(ent.Asc(botevent.FieldSeq)).All(ctx)
	require.NoError(t, err)
	var budgetEvents int
	for _, event := range events {
		if event.Type == "error" {
			budgetEvents++
			assert.Contains(t, event.PayloadJSON, `"code":"budget_exceeded"`)
			assert.Contains(t, event.PayloadJSON, `"reason":"max_tool_calls"`)
		}
	}
	assert.Equal(t, 1, budgetEvents, "预算超限事件只记一次（不随每次 started 重复）")
}
