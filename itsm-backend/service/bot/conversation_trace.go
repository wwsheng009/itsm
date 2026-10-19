package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/conversation"
	"itsm-backend/ent/message"
	"itsm-backend/ent/toolinvocation"
)

// O1-01 条件项闭环：纯问答轨迹（conversation 维度）。
//
// 背景：O1-01 的 run 维度轨迹只覆盖产生 bot_runs 的任务式会话；纯问答（无 run）
// 此前只能复制会话 ID 报障。本文件提供会话维度回放：
//
//	会话档案 → 消息回合 → （若有）运行列表 → 工具调用与确认 → 产物
//
// 边界：
//   - 只读聚合，不触碰业务表；
//   - 租户前置过滤（跨租户与不存在同口径：ErrConversationNotFound）；
//   - 原始工具参数不回显（只回脱敏快照 args_redacted）；
//   - 消息内容按跑数截断（单条 2000 字符 / 单次 200 条），截断显式标记
//     （messagesTruncated / truncated），不伪装全量。

const (
	conversationTraceMaxMessages    = 200
	conversationTraceMessageCapRune = 2000
	conversationTraceMaxRuns        = 50
)

// ErrConversationNotFound 会话不存在或跨租户（对调用方同口径，避免探测）。
var ErrConversationNotFound = errors.New("bot: conversation not found")

// ConversationTraceConversation 会话档案。
type ConversationTraceConversation struct {
	ID        int       `json:"id"`
	UserID    int       `json:"userId,omitempty"`
	BotID     int       `json:"botId,omitempty"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConversationTraceMessage 对话消息（content 超过上限截断，truncated 显式标记）。
type ConversationTraceMessage struct {
	ID        int       `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Truncated bool      `json:"truncated,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConversationTraceRun 会话下的运行摘要（跳 run 维度轨迹用）。
type ConversationTraceRun struct {
	ID         int        `json:"id"`
	Status     string     `json:"status"`
	Entrypoint string     `json:"entrypoint,omitempty"`
	Model      string     `json:"model,omitempty"`
	ErrorCode  string     `json:"errorCode,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// ConversationTrace 是会话维度的全链路轨迹（纯问答无 run 同样可回放）。
type ConversationTrace struct {
	Conversation      ConversationTraceConversation `json:"conversation"`
	Messages          []ConversationTraceMessage    `json:"messages"`
	MessageCount      int                           `json:"messageCount"`
	MessagesTruncated bool                          `json:"messagesTruncated"`
	Runs              []ConversationTraceRun        `json:"runs"`
	ToolInvocations   []RunTraceTool                `json:"toolInvocations"`
	Artifacts         []RunTraceArtifact            `json:"artifacts"`
}

// GetConversationTrace 读取一个会话的轨迹（租户前置；未命中 → ErrConversationNotFound）。
func (s *TraceService) GetConversationTrace(ctx context.Context, tenantID, conversationID int) (*ConversationTrace, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: trace service 未初始化")
	}
	if tenantID <= 0 || conversationID <= 0 {
		return nil, ErrConversationNotFound
	}
	// RLS：按会话租户重绑定 ctx（与 RunStore 同口径）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)

	conv, err := s.client.Conversation.Query().
		Where(conversation.ID(conversationID), conversation.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrConversationNotFound
		}
		return nil, fmt.Errorf("bot: 读取会话失败: %w", err)
	}

	// 总量与实际返回分离：truncated 只在真的截断时为 true。
	messageCount, err := s.client.Message.Query().
		Where(message.TenantID(tenantID), message.ConversationID(conversationID)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取会话消息计数失败: %w", err)
	}
	messages, err := s.client.Message.Query().
		Where(message.TenantID(tenantID), message.ConversationID(conversationID)).
		Order(ent.Asc(message.FieldID)).
		Limit(conversationTraceMaxMessages).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取会话消息失败: %w", err)
	}

	runs, err := s.client.BotRun.Query().
		Where(botrun.TenantID(tenantID), botrun.ConversationID(conversationID)).
		Order(ent.Desc(botrun.FieldStartedAt)).
		Limit(conversationTraceMaxRuns).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取会话运行失败: %w", err)
	}

	tools, err := s.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(tenantID), toolinvocation.ConversationID(conversationID)).
		Order(ent.Asc(toolinvocation.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取会话工具调用失败: %w", err)
	}

	artifacts, err := s.client.BotArtifact.Query().
		Where(botartifact.TenantID(tenantID), botartifact.ConversationID(conversationID)).
		Order(ent.Asc(botartifact.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: 读取会话产物失败: %w", err)
	}

	trace := &ConversationTrace{
		Conversation: ConversationTraceConversation{
			ID:        conv.ID,
			UserID:    conv.UserID,
			BotID:     conv.BotID,
			Title:     conv.Title,
			CreatedAt: conv.CreatedAt,
		},
		Messages:        make([]ConversationTraceMessage, 0, len(messages)),
		MessageCount:    messageCount,
		Runs:            make([]ConversationTraceRun, 0, len(runs)),
		ToolInvocations: make([]RunTraceTool, 0, len(tools)),
		Artifacts:       make([]RunTraceArtifact, 0, len(artifacts)),
	}

	for _, msg := range messages {
		content, truncated := truncateTraceContent(msg.Content)
		trace.Messages = append(trace.Messages, ConversationTraceMessage{
			ID:        msg.ID,
			Role:      msg.Role,
			Content:   content,
			Truncated: truncated,
			CreatedAt: msg.CreatedAt,
		})
	}
	trace.MessagesTruncated = messageCount > len(messages)

	for _, run := range runs {
		trace.Runs = append(trace.Runs, ConversationTraceRun{
			ID:         run.ID,
			Status:     run.Status,
			Entrypoint: run.Entrypoint,
			Model:      run.Model,
			ErrorCode:  run.ErrorCode,
			StartedAt:  run.StartedAt,
			FinishedAt: run.FinishedAt,
		})
	}

	for _, inv := range tools {
		trace.ToolInvocations = append(trace.ToolInvocations, mapTraceTool(inv))
	}
	for _, artifact := range artifacts {
		trace.Artifacts = append(trace.Artifacts, mapTraceArtifact(artifact))
	}

	return trace, nil
}

// truncateTraceContent 按跑数上限截断消息内容（超长显式标记）。
func truncateTraceContent(content string) (string, bool) {
	runes := []rune(content)
	if len(runes) <= conversationTraceMessageCapRune {
		return content, false
	}
	return string(runes[:conversationTraceMessageCapRune]), true
}

// mapTraceTool 工具调用 → 轨迹条目（run/conversation 两种轨迹共用口径）。
func mapTraceTool(inv *ent.ToolInvocation) RunTraceTool {
	var approvedAt *time.Time
	if !inv.ApprovedAt.IsZero() {
		value := inv.ApprovedAt
		approvedAt = &value
	}
	return RunTraceTool{
		ID:                inv.ID,
		ToolName:          inv.ToolName,
		Status:            inv.Status,
		ApprovalState:     inv.ApprovalState,
		ConfirmationState: string(NormalizeConfirmationState(inv.ApprovalState, inv.Status)),
		NeedsApproval:     inv.NeedsApproval,
		Risk:              inv.Risk,
		Category:          inv.Category,
		Provider:          inv.Provider,
		ServerName:        inv.McpServerName,
		ArgsRedacted:      inv.ArgsRedacted,
		Result:            inv.Result,
		OutputSummary:     inv.OutputSummary,
		Error:             inv.Error,
		ErrorCode:         inv.ErrorCode,
		VerifyState:       inv.VerifyState,
		VerifyNote:        inv.VerifyNote,
		DurationMs:        int64(inv.DurationMs),
		TargetType:        inv.TargetType,
		TargetID:          inv.TargetID,
		StepID:            inv.StepID,
		UserID:            inv.UserID,
		ApprovedBy:        inv.ApprovedBy,
		ApprovedAt:        approvedAt,
		CreatedAt:         inv.CreatedAt,
	}
}

// mapTraceArtifact 产物 → 轨迹条目（run/conversation 两种轨迹共用口径）。
func mapTraceArtifact(artifact *ent.BotArtifact) RunTraceArtifact {
	return RunTraceArtifact{
		ID:           artifact.ID,
		Kind:         artifact.Kind,
		ToolName:     artifact.ToolName,
		Title:        artifact.Title,
		ContentJSON:  artifact.ContentJSON,
		EvidenceJSON: artifact.EvidenceJSON,
		CreatedAt:    artifact.CreatedAt,
	}
}
