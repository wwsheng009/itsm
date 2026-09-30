package bot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botevent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

func newTestRun(t *testing.T, budget Budget) (*Manager, *Run) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "run-manager.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	manager := NewManager(NewRunStore(client), budget)
	require.NotNil(t, manager)
	run, err := manager.Start(context.Background(), StartRunInput{TenantID: 1, ConversationID: 7, Entrypoint: "chat"})
	require.NoError(t, err)
	require.NotNil(t, run)
	return manager, run
}

// TestRunManager_StateTransitions 覆盖状态推进 running → completed/failed 与收口幂等。
func TestRunManager_StateTransitions(t *testing.T) {
	ctx := context.Background()
	_, run := newTestRun(t, Budget{MaxSteps: 5, MaxToolCalls: 2})

	assert.False(t, run.Finished())
	require.NoError(t, run.Finish(ctx, RunStatusCompleted, ""))
	assert.True(t, run.Finished())

	// 二次 Finish（不同终态）不得改写首个终态。
	require.NoError(t, run.Finish(ctx, RunStatusFailed, "late_error"))
	row := run.manager.store.client.BotRun.GetX(ctx, run.ID())
	assert.Equal(t, RunStatusCompleted, row.Status)
	assert.Empty(t, row.ErrorCode)

	// 收口后拒绝写入步骤与工具调用（fail-closed，不产生半截审计）。
	_, err := run.RecordStep(ctx, "llm", "", 1)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrBudgetExceeded), "收口后拒绝写入不等于预算超限")
	assert.Error(t, run.ReserveToolCall())

	// failed 路径：错误码落库。
	_, second := newTestRun(t, Budget{})
	require.NoError(t, second.Finish(ctx, RunStatusFailed, "chat_error"))
	failedRow := second.manager.store.client.BotRun.GetX(ctx, second.ID())
	assert.Equal(t, RunStatusFailed, failedRow.Status)
	assert.Equal(t, "chat_error", failedRow.ErrorCode)
	require.NotNil(t, failedRow.FinishedAt)
}

// TestRunManager_BudgetGuards 覆盖 ab1-02 的预算判定：step / tool_call / token 三类超限，
// 以及超限不写库（步骤预算吃到上限即停）。
func TestRunManager_BudgetGuards(t *testing.T) {
	ctx := context.Background()
	_, run := newTestRun(t, Budget{MaxSteps: 3, MaxToolCalls: 2, MaxTokens: 100})

	// step 预算：3 条可写，第 4 条被拒。
	for index := 0; index < 3; index++ {
		got, err := run.RecordStep(ctx, "llm", "", 1)
		require.NoError(t, err)
		assert.Equal(t, index, got)
	}
	_, err := run.RecordStep(ctx, "llm", "", 1)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	assert.Equal(t, 3, run.Steps(), "超限不得写库、也不得增加计数")

	// 工具调用预算：2 次可留，第 3 次被拒。
	require.NoError(t, run.ReserveToolCall())
	require.NoError(t, run.ReserveToolCall())
	require.ErrorIs(t, run.ReserveToolCall(), ErrBudgetExceeded)
	assert.Equal(t, 2, run.ToolCalls())

	// token 预算：累加后超限返回错误（计数保留，便于审计对照）。
	require.NoError(t, run.AddTokens(60))
	require.NoError(t, run.AddTokens(40))
	require.ErrorIs(t, run.AddTokens(1), ErrBudgetExceeded)
	assert.Equal(t, 101, run.Tokens())
	require.NoError(t, run.AddTokens(0), "0 值不参与判定")

	// 单工具超时：ctx 带 deadline 且约为配置值。
	toolCtx, cancel, beginErr := run.BeginToolCall(ctx)
	require.ErrorIs(t, beginErr, ErrBudgetExceeded, "工具预算已耗尽")

	_, fresh := newTestRun(t, Budget{MaxToolCalls: 5, ToolTimeout: 1234 * time.Millisecond})
	toolCtx, cancel, beginErr = fresh.BeginToolCall(ctx)
	require.NoError(t, beginErr)
	deadline, ok := toolCtx.Deadline()
	require.True(t, ok, "单工具 ctx 必须带 deadline")
	assert.WithinDuration(t, time.Now().Add(1234*time.Millisecond), deadline, 500*time.Millisecond)
	cancel()
}

// TestRunManager_BudgetExceededClosesRun 覆盖 ab1-02：超限以 error{code=budget_exceeded}
// 结束运行、错误事件落库、运行收口为 failed。
func TestRunManager_BudgetExceededClosesRun(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "budget.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := NewRunStore(client)
	manager := NewManager(store, Budget{MaxSteps: 1})
	run, err := manager.Start(ctx, StartRunInput{TenantID: 1, Entrypoint: "chat"})
	require.NoError(t, err)

	_, err = run.RecordStep(ctx, "llm", "", 1)
	require.NoError(t, err)
	_, err = run.RecordStep(ctx, "llm", "", 1)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.NoError(t, run.BudgetExceeded(ctx, "max_steps"))

	assert.True(t, run.Finished())
	row := store.client.BotRun.GetX(ctx, run.ID())
	assert.Equal(t, RunStatusFailed, row.Status)
	assert.Equal(t, ErrorCodeBudgetExceeded, row.ErrorCode)

	events, err := store.ListRunEvents(ctx, 1, run.ID())
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeError, events[0].Type)
	assert.Contains(t, events[0].PayloadJSON, `"code":"budget_exceeded"`)
	assert.Contains(t, events[0].PayloadJSON, `"reason":"max_steps"`)
}

// TestRunManager_EmitPersistsBeforeBroadcast 覆盖「先落库后广播」：Observer 回调触发时，
// 该事件必须已可被反查到（顺序不可颠倒），并且广播顺序与 seq 一致。
func TestRunManager_EmitPersistsBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "emit.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := NewRunStore(client)

	var (
		mu       sync.Mutex
		observed []string
		missing  []string
	)
	manager := NewManager(store, Budget{}).WithObserver(ObserverFunc(func(tenantID, runID, seq int, eventType string, payload map[string]any) {
		// 回调内立即反查：事件必须已经落库（先落库后广播）。
		found, queryErr := client.BotEvent.Query().Where(
			botevent.RunID(runID), botevent.Seq(seq),
		).Exist(ctx)
		mu.Lock()
		defer mu.Unlock()
		if queryErr != nil || !found {
			missing = append(missing, eventType)
		}
		observed = append(observed, eventType)
	}))

	run, err := manager.Start(ctx, StartRunInput{TenantID: 1, Entrypoint: "chat"})
	require.NoError(t, err)
	require.NoError(t, emitEvent(ctx, run, "run_started"))
	require.NoError(t, emitEvent(ctx, run, "run_finished"))

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, missing, "Observer 回调时事件必须已落库（先落库后广播）")
	assert.Equal(t, []string{"run_started", "run_finished"}, observed, "广播顺序必须与 seq 一致")

	events, err := store.ListRunEvents(ctx, 1, run.ID())
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, 0, events[0].Seq)
	assert.Equal(t, 1, events[1].Seq)
}

// TestRunManager_TruncateOutput 覆盖输出字节上限（含边界与禁用语义）。
func TestRunManager_TruncateOutput(t *testing.T) {
	payload := []byte(strings.Repeat("a", 100))

	out, truncated := TruncateOutput(payload, 100)
	assert.False(t, truncated)
	assert.Len(t, out, 100)

	out, truncated = TruncateOutput(payload, 10)
	assert.True(t, truncated)
	assert.Len(t, out, 10)

	out, truncated = TruncateOutput(payload, 0)
	assert.False(t, truncated, "<=0 视为不限制（由 normalize 保证不会出现在生效预算中）")
	assert.Len(t, out, 100)
}

// TestRunManager_BudgetNormalize 覆盖配置笔误 fail-safe：非正预算回落默认值。
func TestRunManager_BudgetNormalize(t *testing.T) {
	manager := NewManager(NewRunStore(nil), Budget{})
	if manager != nil {
		t.Fatal("RunStore 为 nil 时 Manager 必须为 nil（关闭态）")
	}

	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "norm.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := NewRunStore(client)
	normalized := NewManager(store, Budget{MaxSteps: 0, MaxToolCalls: -1, ToolTimeout: 0, MaxOutputBytes: 0}).Budget()
	assert.Equal(t, defaultMaxSteps, normalized.MaxSteps)
	assert.Equal(t, defaultMaxTokens, normalized.MaxTokens)
	assert.Equal(t, defaultMaxToolCalls, normalized.MaxToolCalls)
	assert.Equal(t, defaultToolTimeout, normalized.ToolTimeout)
	assert.Equal(t, defaultMaxOutputBytes, normalized.MaxOutputBytes)
}

// emitEvent 帮助函数：调用 Run.Emit 并返回错误（保持断言行简短）。
func emitEvent(ctx context.Context, run *Run, eventType string) error {
	_, err := run.Emit(ctx, eventType, map[string]any{"type": eventType})
	return err
}
