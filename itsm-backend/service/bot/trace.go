package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
	"itsm-backend/ent/toolinvocation"
)

// O1-01 单任务全链路轨迹（run_id 回放）。
//
// 链路口径（方案 §13.4）：会话 → run → step/event → tool_invocation（确认/审批）
// → artifact（产物）→ 业务对象（run/tool 的 target_type#target_id）。
//
// 边界：
//   - 只读聚合，不触碰业务表；
//   - 租户前置过滤（跨租户与不存在同口径：ErrRunNotFound）；
//   - `arguments`（原始参数）绝不回显，只回脱敏快照 args_redacted。
type TraceService struct {
	client *ent.Client
}

// ErrRunNotFound 运行不存在或跨租户（对调用方同口径，避免探测）。
var ErrRunNotFound = errors.New("bot: run not found")

// NewTraceService 构造轨迹服务；client 为 nil 时返回 nil（调用方据此降级为 503）。
func NewTraceService(client *ent.Client) *TraceService {
	if client == nil {
		return nil
	}
	return &TraceService{client: client}
}

// RunTraceRun 运行档案（会话/入口/模型/状态/业务对象）。
type RunTraceRun struct {
	ID             int        `json:"id"`
	ConversationID int        `json:"conversationId"`
	BotID          int        `json:"botId"`
	Entrypoint     string     `json:"entrypoint"`
	Status         string     `json:"status"`
	Model          string     `json:"model,omitempty"`
	TargetType     string     `json:"targetType,omitempty"`
	TargetID       int        `json:"targetId,omitempty"`
	ErrorCode      string     `json:"errorCode,omitempty"`
	BudgetJSON     string     `json:"budgetJson,omitempty"`
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

// RunTraceStep 运行步骤（llm/tool/confirm…）。
type RunTraceStep struct {
	ID         int       `json:"id"`
	StepIndex  int       `json:"stepIndex"`
	Type       string    `json:"type"`
	PayloadRef string    `json:"payloadRef,omitempty"`
	DurationMs int       `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

// RunTraceEvent 运行事件（seq 升序，SSE 重放同源）。
type RunTraceEvent struct {
	ID        int            `json:"id"`
	Seq       int            `json:"seq"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// RunTraceTool 工具调用（含确认/审批与 verify 字段；不含原始参数）。
type RunTraceTool struct {
	ID                int        `json:"id"`
	ToolName          string     `json:"toolName"`
	Status            string     `json:"status"`
	ApprovalState     string     `json:"approvalState"`
	ConfirmationState string     `json:"confirmationState"`
	NeedsApproval     bool       `json:"needsApproval"`
	Risk              string     `json:"risk,omitempty"`
	Category          string     `json:"category,omitempty"`
	Provider          string     `json:"provider,omitempty"`
	ServerName        string     `json:"serverName,omitempty"`
	ArgsRedacted      string     `json:"argsRedacted,omitempty"`
	Result            *string    `json:"result,omitempty"`
	OutputSummary     string     `json:"outputSummary,omitempty"`
	Error             *string    `json:"error,omitempty"`
	ErrorCode         string     `json:"errorCode,omitempty"`
	VerifyState       string     `json:"verifyState,omitempty"`
	VerifyNote        string     `json:"verifyNote,omitempty"`
	DurationMs        int64      `json:"durationMs"`
	TargetType        string     `json:"targetType,omitempty"`
	TargetID          string     `json:"targetId,omitempty"`
	StepID            int        `json:"stepId"`
	UserID            int        `json:"userId"`
	ApprovedBy        int        `json:"approvedBy,omitempty"`
	ApprovedAt        *time.Time `json:"approvedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// RunTraceArtifact 产物（bot_artifacts；content/evidence 为 JSON 字符串）。
type RunTraceArtifact struct {
	ID           int       `json:"id"`
	Kind         string    `json:"kind"`
	ToolName     string    `json:"toolName,omitempty"`
	Title        string    `json:"title,omitempty"`
	ContentJSON  string    `json:"contentJson,omitempty"`
	EvidenceJSON string    `json:"evidenceJson,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// RunTrace 是单次运行的全链路轨迹（一次查询返回，前端可直接回放）。
type RunTrace struct {
	Run             RunTraceRun        `json:"run"`
	Steps           []RunTraceStep     `json:"steps"`
	Events          []RunTraceEvent    `json:"events"`
	ToolInvocations []RunTraceTool     `json:"toolInvocations"`
	Artifacts       []RunTraceArtifact `json:"artifacts"`
}

// GetRunTrace 读取一次运行的全链路（租户前置；未命中 → ErrRunNotFound）。
func (s *TraceService) GetRunTrace(ctx context.Context, tenantID, runID int) (*RunTrace, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: trace service 未初始化")
	}
	if tenantID <= 0 || runID <= 0 {
		return nil, ErrRunNotFound
	}
	// RLS：按运行租户重绑定 ctx（与 RunStore 同口径）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)

	run, err := s.client.BotRun.Query().
		Where(botrun.ID(runID), botrun.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("bot: 读取运行失败: %w", err)
	}

	steps, err := s.client.BotStep.Query().
		Where(botstep.TenantID(tenantID), botstep.RunID(runID)).
		Order(ent.Asc(botstep.FieldStepIndex)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取运行步骤失败: %w", err)
	}

	events, err := s.client.BotEvent.Query().
		Where(botevent.TenantID(tenantID), botevent.RunID(runID)).
		Order(ent.Asc(botevent.FieldSeq)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取运行事件失败: %w", err)
	}

	tools, err := s.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(tenantID), toolinvocation.RunID(runID)).
		Order(ent.Asc(toolinvocation.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取工具调用失败: %w", err)
	}

	artifacts, err := s.client.BotArtifact.Query().
		Where(botartifact.TenantID(tenantID), botartifact.RunID(runID)).
		Order(ent.Asc(botartifact.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取产物失败: %w", err)
	}

	trace := &RunTrace{
		Run:             RunTraceRun{},
		Steps:           make([]RunTraceStep, 0, len(steps)),
		Events:          make([]RunTraceEvent, 0, len(events)),
		ToolInvocations: make([]RunTraceTool, 0, len(tools)),
		Artifacts:       make([]RunTraceArtifact, 0, len(artifacts)),
	}
	trace.Run = RunTraceRun{
		ID:             run.ID,
		ConversationID: run.ConversationID,
		BotID:          run.BotID,
		Entrypoint:     run.Entrypoint,
		Status:         run.Status,
		Model:          run.Model,
		TargetType:     run.TargetType,
		TargetID:       run.TargetID,
		ErrorCode:      run.ErrorCode,
		BudgetJSON:     run.BudgetJSON,
		StartedAt:      run.StartedAt,
		FinishedAt:     run.FinishedAt,
	}

	for _, step := range steps {
		trace.Steps = append(trace.Steps, RunTraceStep{
			ID:         step.ID,
			StepIndex:  step.StepIndex,
			Type:       step.Type,
			PayloadRef: step.PayloadRef,
			DurationMs: step.DurationMs,
			CreatedAt:  step.CreatedAt,
		})
	}

	for _, event := range events {
		item := RunTraceEvent{
			ID:        event.ID,
			Seq:       event.Seq,
			Type:      event.Type,
			CreatedAt: event.CreatedAt,
		}
		if event.PayloadJSON != "" && event.PayloadJSON != "{}" {
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err == nil {
				item.Payload = payload
			}
		}
		trace.Events = append(trace.Events, item)
	}

	for _, inv := range tools {
		trace.ToolInvocations = append(trace.ToolInvocations, mapTraceTool(inv))
	}

	for _, artifact := range artifacts {
		trace.Artifacts = append(trace.Artifacts, mapTraceArtifact(artifact))
	}

	return trace, nil
}

// —— I2-01 运行列表（Autopilot 监督面） ——

const (
	runListDefaultLimit = 50
	runListMaxLimit     = 100
	runListMaxDays      = 90
)

// RunListFilter 运行列表过滤（监督面）。
type RunListFilter struct {
	Days   int
	Status string
	Limit  int
}

// RunTraceSummary 运行列表行：状态 + 步骤/工具/待审批/产物计数（挑运行、挑要处理的审批）。
type RunTraceSummary struct {
	ID               int        `json:"id"`
	ConversationID   int        `json:"conversationId"`
	BotID            int        `json:"botId"`
	Entrypoint       string     `json:"entrypoint"`
	Status           string     `json:"status"`
	Model            string     `json:"model,omitempty"`
	ErrorCode        string     `json:"errorCode,omitempty"`
	TargetType       string     `json:"targetType,omitempty"`
	TargetID         int        `json:"targetId,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	Steps            int        `json:"steps"`
	ToolCalls        int        `json:"toolCalls"`
	PendingApprovals int        `json:"pendingApprovals"`
	Artifacts        int        `json:"artifacts"`
}

// ListRuns 列出窗口内的运行（倒序）+ 每行的步骤/工具/待审批/产物计数。
//
// 计数按 RunIDIn 批量读取，避免逐行 N+1；租户前置（跨租户不可见）。
func (s *TraceService) ListRuns(ctx context.Context, tenantID int, filter RunListFilter) ([]RunTraceSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: trace service 未初始化")
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("bot: 运行列表需要租户")
	}
	days := filter.Days
	if days <= 0 {
		days = 7
	}
	if days > runListMaxDays {
		days = runListMaxDays
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = runListDefaultLimit
	}
	if limit > runListMaxLimit {
		limit = runListMaxLimit
	}

	ctx = tenantctx.WithTenantID(ctx, tenantID)
	since := time.Now().AddDate(0, 0, -days)
	query := s.client.BotRun.Query().
		Where(botrun.TenantID(tenantID), botrun.StartedAtGTE(since))
	if filter.Status != "" {
		query = query.Where(botrun.StatusEQ(filter.Status))
	}
	rows, err := query.Order(ent.Desc(botrun.FieldStartedAt)).Limit(limit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取运行列表失败: %w", err)
	}
	summaries := make([]RunTraceSummary, 0, len(rows))
	if len(rows) == 0 {
		return summaries, nil
	}

	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	stepCounts := map[int]int{}
	steps, err := s.client.BotStep.Query().
		Where(botstep.TenantID(tenantID), botstep.RunIDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取运行步骤计数失败: %w", err)
	}
	for _, step := range steps {
		stepCounts[step.RunID]++
	}

	toolCounts := map[int]int{}
	pendingCounts := map[int]int{}
	tools, err := s.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(tenantID), toolinvocation.RunIDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取工具调用计数失败: %w", err)
	}
	for _, inv := range tools {
		toolCounts[inv.RunID]++
		if NormalizeConfirmationState(inv.ApprovalState, inv.Status) == ConfirmationPending {
			pendingCounts[inv.RunID]++
		}
	}

	artifactCounts := map[int]int{}
	artifacts, err := s.client.BotArtifact.Query().
		Where(botartifact.TenantID(tenantID), botartifact.RunIDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取产物计数失败: %w", err)
	}
	for _, artifact := range artifacts {
		artifactCounts[artifact.RunID]++
	}

	for _, row := range rows {
		summaries = append(summaries, RunTraceSummary{
			ID:               row.ID,
			ConversationID:   row.ConversationID,
			BotID:            row.BotID,
			Entrypoint:       row.Entrypoint,
			Status:           row.Status,
			Model:            row.Model,
			ErrorCode:        row.ErrorCode,
			TargetType:       row.TargetType,
			TargetID:         row.TargetID,
			StartedAt:        row.StartedAt,
			FinishedAt:       row.FinishedAt,
			Steps:            stepCounts[row.ID],
			ToolCalls:        toolCounts[row.ID],
			PendingApprovals: pendingCounts[row.ID],
			Artifacts:        artifactCounts[row.ID],
		})
	}
	return summaries, nil
}
