package bot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"itsm-backend/ent"
)

// B1-02：运行生命周期、预算护栏与「先落库后广播」的事件出口。
//
// 边界：
//   - Manager 只依赖 RunStore（持久化）与可选 Observer（广播）；SSE/HTTP 细节留给调用方；
//   - 预算超限一律以 error{code=budget_exceeded} 结束运行并落审计（BP8 参数）；
//   - 所有事件先写 bot_events（拿到 seq）再交给 Observer，保证 SSE 重放与审计同源。
const (
	// RunStatusRunning / RunStatusCompleted / RunStatusFailed 为已实现状态；
	// RunStatusCancelled 预留（B1-05 确认中止落地后启用）。
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"

	// ErrorCodeBudgetExceeded 是预算超限的稳定错误码（AB1-02）。
	ErrorCodeBudgetExceeded = "budget_exceeded"

	// EventTypeError 是错误事件类型（载荷含 code/reason；B1-03 注册表冻结后如有出入按注册表改）。
	EventTypeError = "error"

	stepTypeLLM = "llm"
)

// ErrBudgetExceeded 表示本次操作会突破 run 预算，调用方应停止推进并以错误码收口。
var ErrBudgetExceeded = errors.New("bot: 预算超限（budget_exceeded）")

// Budget 是每 run 预算与护栏参数（BP8 落配置，bootstrap 侧映射到本结构）。
//
// 非正值在 normalize 时回落默认值：配置笔误不得把护栏关成「无上限」。
type Budget struct {
	// MaxSteps: 每 run 步骤上限（含 llm/tool/confirm 步骤）。
	MaxSteps int
	// MaxTokens: 每 run token 上限（按 provider 回报累加；无回报则不参与判定）。
	MaxTokens int
	// MaxToolCalls: 每 run 工具调用次数上限。
	MaxToolCalls int
	// ToolTimeout: 单工具执行超时（默认 30s）。
	ToolTimeout time.Duration
	// MaxOutputBytes: 单次工具输出字节上限（超出部分截断并标记）。
	MaxOutputBytes int
}

// 预算默认值（与 config.BotBudgetConfig 的 BP8 默认一致）。
const (
	defaultMaxSteps       = 24
	defaultMaxTokens      = 100000
	defaultMaxToolCalls   = 12
	defaultToolTimeout    = 30 * time.Second
	defaultMaxOutputBytes = 65536
)

// DefaultBudget 返回 BP8 默认预算。
func DefaultBudget() Budget {
	return Budget{
		MaxSteps:       defaultMaxSteps,
		MaxTokens:      defaultMaxTokens,
		MaxToolCalls:   defaultMaxToolCalls,
		ToolTimeout:    defaultToolTimeout,
		MaxOutputBytes: defaultMaxOutputBytes,
	}
}

func (b Budget) normalize() Budget {
	if b.MaxSteps <= 0 {
		b.MaxSteps = defaultMaxSteps
	}
	if b.MaxTokens <= 0 {
		b.MaxTokens = defaultMaxTokens
	}
	if b.MaxToolCalls <= 0 {
		b.MaxToolCalls = defaultMaxToolCalls
	}
	if b.ToolTimeout <= 0 {
		b.ToolTimeout = defaultToolTimeout
	}
	if b.MaxOutputBytes <= 0 {
		b.MaxOutputBytes = defaultMaxOutputBytes
	}
	return b
}

// Observer 接收**已落库**的运行事件（广播侧实现由 SSE 层提供）。
//
// 实现方必须自行保证非阻塞（或自行入队），不得在回调内反向调用 Manager 造成环路。
type Observer interface {
	OnRunEvent(tenantID, runID, seq int, eventType string, payload map[string]any)
}

// ObserverFunc 让普通函数满足 Observer。
type ObserverFunc func(tenantID, runID, seq int, eventType string, payload map[string]any)

// OnRunEvent 实现 Observer。
func (f ObserverFunc) OnRunEvent(tenantID, runID, seq int, eventType string, payload map[string]any) {
	f(tenantID, runID, seq, eventType, payload)
}

// Manager 承载 run 的创建、预算判定与事件出口。
type Manager struct {
	store    *RunStore
	budget   Budget
	observer Observer
	clock    func() time.Time
}

// NewManager 构造运行管理器；store 为 nil 时返回 nil（调用方据此走关闭态）。
func NewManager(store *RunStore, budget Budget) *Manager {
	if store == nil {
		return nil
	}
	return &Manager{store: store, budget: budget.normalize(), clock: time.Now}
}

// WithObserver 挂载事件广播实现（可选；nil 时只落库不广播）。
func (m *Manager) WithObserver(observer Observer) *Manager {
	if m == nil {
		return nil
	}
	m.observer = observer
	return m
}

// WithClock 注入时钟（测试用；nil 时保持 time.Now）。
func (m *Manager) WithClock(clock func() time.Time) *Manager {
	if m == nil {
		return nil
	}
	if clock != nil {
		m.clock = clock
	}
	return m
}

// Budget 返回生效预算（已归一化）。
func (m *Manager) Budget() Budget {
	if m == nil {
		return Budget{}
	}
	return m.budget
}

// Start 创建运行并返回句柄；不自动发事件（run_started 由调用方显式 Emit，便于包裹点控制）。
func (m *Manager) Start(ctx context.Context, in StartRunInput) (*Run, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("bot: run manager 未初始化")
	}
	row, err := m.store.StartRun(ctx, in)
	if err != nil {
		return nil, err
	}
	return &Run{manager: m, row: row}, nil
}

// Run 是一次运行的句柄：预算计数、步骤与事件的唯一写入口。
//
// 并发口径：字段更新在互斥锁内完成；持久化调用在锁外，避免 DB 抖动阻塞判定。
// 注意「计数先行、落库随后」——超限判定必须发生在写库之前，否则会先花掉预算。
type Run struct {
	manager *Manager
	row     *ent.BotRun

	mu        sync.Mutex
	stepIndex int
	tokens    int
	toolCalls int
	finished  bool
}

// ID 返回运行主键。
func (r *Run) ID() int {
	if r == nil || r.row == nil {
		return 0
	}
	return r.row.ID
}

// TenantID 返回租户 ID。
func (r *Run) TenantID() int {
	if r == nil || r.row == nil {
		return 0
	}
	return r.row.TenantID
}

// ConversationID 返回会话 ID（无会话为 0）。
func (r *Run) ConversationID() int {
	if r == nil || r.row == nil {
		return 0
	}
	return r.row.ConversationID
}

// Finished 报告运行是否已收口。
func (r *Run) Finished() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished
}

// Steps / ToolCalls / Tokens 返回当前累计用量（供指标与测试断言）。
func (r *Run) Steps() int     { return r.counter(func() int { return r.stepIndex }) }
func (r *Run) ToolCalls() int { return r.counter(func() int { return r.toolCalls }) }
func (r *Run) Tokens() int    { return r.counter(func() int { return r.tokens }) }

func (r *Run) counter(read func() int) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return read()
}

// Emit 先落库（分配 seq）再广播；返回落库后的事件行。
//
// Observer 回调可能panic（外部实现），这里不 recover——与既有 SSE 写入口径一致，
// 由调用方（HTTP 层）统一兜底；广播失败不影响已落库的审计事实。
func (r *Run) Emit(ctx context.Context, eventType string, payload map[string]any) (*ent.BotEvent, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("bot: run 未初始化")
	}
	event, err := r.manager.store.AppendEvent(ctx, r.row.TenantID, r.row.ID, eventType, payload)
	if err != nil {
		return nil, err
	}
	if observer := r.manager.observer; observer != nil {
		observer.OnRunEvent(r.row.TenantID, r.row.ID, event.Seq, eventType, payload)
	}
	return event, nil
}

// RecordStep 记录一个步骤；超预算返回 ErrBudgetExceeded（不写库）。
func (r *Run) RecordStep(ctx context.Context, stepType, payloadRef string, durationMs int) (int, error) {
	if r == nil || r.manager == nil {
		return 0, fmt.Errorf("bot: run 未初始化")
	}
	if stepType == "" {
		stepType = stepTypeLLM
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return 0, fmt.Errorf("bot: 运行已收口，拒绝写入步骤")
	}
	if r.stepIndex >= r.manager.budget.MaxSteps {
		r.mu.Unlock()
		return 0, fmt.Errorf("bot: step 预算超限（max_steps=%d）: %w", r.manager.budget.MaxSteps, ErrBudgetExceeded)
	}
	index := r.stepIndex
	r.stepIndex++
	r.mu.Unlock()

	if _, err := r.manager.store.AppendStep(ctx, r.row.TenantID, r.row.ID, index, stepType, payloadRef, durationMs); err != nil {
		// 落库失败回滚计数，避免「预算被吃掉但审计缺失」。
		r.mu.Lock()
		r.stepIndex--
		r.mu.Unlock()
		return 0, err
	}
	return index, nil
}

// ReserveToolCall 工具调用预算判定与计数（不派生超时 ctx），超限返回 ErrBudgetExceeded。
func (r *Run) ReserveToolCall() error {
	if r == nil || r.manager == nil {
		return fmt.Errorf("bot: run 未初始化")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return fmt.Errorf("bot: 运行已收口，拒绝发起工具调用")
	}
	if r.toolCalls >= r.manager.budget.MaxToolCalls {
		return fmt.Errorf("bot: 工具调用预算超限（max_tool_calls=%d）: %w", r.manager.budget.MaxToolCalls, ErrBudgetExceeded)
	}
	r.toolCalls++
	return nil
}

// BeginToolCall 工具调用前置检查：超预算返回 ErrBudgetExceeded；成功则返回带单工具超时的 ctx。
//
// 返回的 cancel 必须被调用（defer），否则超时定时器泄漏。
func (r *Run) BeginToolCall(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if err := r.ReserveToolCall(); err != nil {
		return ctx, func() {}, err
	}
	toolCtx, cancel := context.WithTimeout(ctx, r.manager.budget.ToolTimeout)
	return toolCtx, func() {
		cancel()
	}, nil
}

// AddTokens 累加 token 用量；超预算返回 ErrBudgetExceeded（计数保留，便于审计与指标对照）。
func (r *Run) AddTokens(count int) error {
	if r == nil || r.manager == nil {
		return fmt.Errorf("bot: run 未初始化")
	}
	if count <= 0 {
		return nil
	}
	r.mu.Lock()
	r.tokens += count
	over := r.tokens > r.manager.budget.MaxTokens
	r.mu.Unlock()
	if over {
		return fmt.Errorf("bot: token 预算超限（max_tokens=%d，已用 %d）: %w",
			r.manager.budget.MaxTokens, r.Tokens(), ErrBudgetExceeded)
	}
	return nil
}

// Finish 收口运行：状态推进 running → completed/failed(/cancelled)，幂等（二次调用不改写）。
func (r *Run) Finish(ctx context.Context, status, errorCode string) error {
	if r == nil || r.manager == nil {
		return fmt.Errorf("bot: run 未初始化")
	}
	if status == "" {
		status = RunStatusCompleted
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return nil // 幂等：首个终态胜出
	}
	r.finished = true
	r.mu.Unlock()
	return r.manager.store.FinishRun(ctx, r.row.TenantID, r.row.ID, status, errorCode)
}

// BudgetExceeded 以 `error{code=budget_exceeded}` 结束运行：
// 先落库错误事件（含原因），再收口为 failed + error_code=budget_exceeded。
//
// 返回首个错误（事件写失败或收口失败），不改变「运行已按超限收口」的语义。
func (r *Run) BudgetExceeded(ctx context.Context, reason string) error {
	if r == nil || r.manager == nil {
		return fmt.Errorf("bot: run 未初始化")
	}
	var firstErr error
	if _, err := r.Emit(ctx, EventTypeError, map[string]any{
		"code":   ErrorCodeBudgetExceeded,
		"reason": reason,
	}); err != nil {
		firstErr = err
	}
	if finishErr := r.Finish(ctx, RunStatusFailed, ErrorCodeBudgetExceeded); finishErr != nil && firstErr == nil {
		firstErr = finishErr
	}
	return firstErr
}

// TruncateOutput 按字节上限截断工具输出；maxBytes<=0 视为不限制。
//
// 返回截断后的字节、是否发生截断。字节口径可能切在多字节字符中间（调用方按 JSON
// 字符串编码时由 encoding/json 负责替换为 U+FFFD，不产生非法 UTF-8 落库）。
func TruncateOutput(payload []byte, maxBytes int) ([]byte, bool) {
	if maxBytes <= 0 || len(payload) <= maxBytes {
		return payload, false
	}
	return payload[:maxBytes], true
}
