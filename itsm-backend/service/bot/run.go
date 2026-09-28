package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
)

// RunStore 是 bot_runs / bot_steps / bot_events 三表的写入端口（B1-01）。
//
// 归属与边界（与 B1-02 RunManager 的分工）：
//   - 本组件只负责**持久化**：起运行、记步骤、记事件、收运行；
//   - 事件序号由本组件在事务内分配（max(seq)+1），冲突重试一次；
//   - 状态机、预算护栏、SSE 广播归 B1-02 RunManager，本组件不感知。
//
// 关闭态：未注入（nil）时调用方不得写入，行为与现状零差异。
type RunStore struct {
	client *ent.Client
}

// NewRunStore 构造运行存储；client 为 nil 时返回 nil（调用方据此关闭记录）。
func NewRunStore(client *ent.Client) *RunStore {
	if client == nil {
		return nil
	}
	return &RunStore{client: client}
}

// StartRunInput 描述一次运行的起始信息。
type StartRunInput struct {
	TenantID       int
	ConversationID int
	BotID          int
	Entrypoint     string
	Model          string
	BudgetJSON     string
}

// StartRun 创建一条 status=running 的运行记录。
func (s *RunStore) StartRun(ctx context.Context, in StartRunInput) (*ent.BotRun, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: run store 未初始化")
	}
	entrypoint := in.Entrypoint
	if entrypoint == "" {
		entrypoint = "chat"
	}
	create := s.client.BotRun.Create().
		SetTenantID(in.TenantID).
		SetEntrypoint(entrypoint).
		SetStatus("running")
	if in.ConversationID > 0 {
		create.SetConversationID(in.ConversationID)
	}
	if in.BotID > 0 {
		create.SetBotID(in.BotID)
	}
	if in.Model != "" {
		create.SetModel(in.Model)
	}
	if in.BudgetJSON != "" {
		create.SetBudgetJSON(in.BudgetJSON)
	}
	return create.Save(ctx)
}

// AppendStep 记录一个步骤；step_index 在运行内唯一，重复序号由 DB 唯一约束拒绝。
func (s *RunStore) AppendStep(ctx context.Context, tenantID, runID, stepIndex int, stepType, payloadRef string, durationMs int) (*ent.BotStep, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: run store 未初始化")
	}
	create := s.client.BotStep.Create().
		SetTenantID(tenantID).
		SetRunID(runID).
		SetStepIndex(stepIndex).
		SetType(stepType).
		SetDurationMs(durationMs)
	if payloadRef != "" {
		create.SetPayloadRef(payloadRef)
	}
	return create.Save(ctx)
}

// AppendEvent 记录一个事件，seq 由本组件在事务内分配（max+1），唯一冲突重试一次。
//
// 「先落库后广播」（B1-02）依赖本方法返回后再下发 SSE；payload 为 nil 时落空对象。
func (s *RunStore) AppendEvent(ctx context.Context, tenantID, runID int, eventType string, payload map[string]any) (*ent.BotEvent, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: run store 未初始化")
	}
	encoded := "{}"
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("bot: 事件载荷序列化失败: %w", err)
		}
		encoded = string(raw)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		seq, err := s.nextSeq(ctx, tenantID, runID)
		if err != nil {
			return nil, err
		}
		event, err := s.client.BotEvent.Create().
			SetTenantID(tenantID).
			SetRunID(runID).
			SetSeq(seq).
			SetType(eventType).
			SetPayloadJSON(encoded).
			Save(ctx)
		if err == nil {
			return event, nil
		}
		lastErr = err
		if !IsUniqueViolation(err) {
			return nil, err
		}
		// 并发写入同序号 → 重新取号再试一次。
	}
	return nil, fmt.Errorf("bot: 事件序号分配冲突（已重试）: %w", lastErr)
}

// nextSeq 取该运行下一个事件序号（max(seq)+1；无事件时为 0）。
func (s *RunStore) nextSeq(ctx context.Context, tenantID, runID int) (int, error) {
	// Aggregate 扫描进 *int；NULL（无行）与错误分别处理。
	value, err := s.client.BotEvent.Query().
		Where(botevent.TenantID(tenantID), botevent.RunID(runID)).
		Aggregate(ent.Max(botevent.FieldSeq)).
		Int(ctx)
	if err != nil {
		return 0, nil // 无事件（NULL → 扫描错误）与错误同口径：从 0 起编
	}
	if value < 0 {
		return 0, nil
	}
	return value + 1, nil
}

// FinishRun 收口运行：置 status 与 error_code，写入 finished_at。
func (s *RunStore) FinishRun(ctx context.Context, tenantID, runID int, status, errorCode string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("bot: run store 未初始化")
	}
	update := s.client.BotRun.Update().
		Where(botrun.ID(runID), botrun.TenantID(tenantID)).
		SetStatus(status).
		SetFinishedAt(time.Now()).
		SetErrorCode(errorCode)
	return update.Exec(ctx)
}

// ListRunSteps / ListRunEvents 供审计与测试按运行回溯（租户维度前置）。
func (s *RunStore) ListRunSteps(ctx context.Context, tenantID, runID int) ([]*ent.BotStep, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: run store 未初始化")
	}
	return s.client.BotStep.Query().
		Where(botstep.TenantID(tenantID), botstep.RunID(runID)).
		Order(ent.Asc(botstep.FieldStepIndex)).
		All(ctx)
}

// ListRunEvents 按 seq 升序返回运行事件（SSE 重放口径）。
func (s *RunStore) ListRunEvents(ctx context.Context, tenantID, runID int) ([]*ent.BotEvent, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: run store 未初始化")
	}
	return s.client.BotEvent.Query().
		Where(botevent.TenantID(tenantID), botevent.RunID(runID)).
		Order(ent.Asc(botevent.FieldSeq)).
		All(ctx)
}
