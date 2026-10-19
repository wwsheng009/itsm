package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O1-01 条件项：纯问答轨迹（conversation 维度）——链路 + 截断口径 + 租户隔离 + 未命中。

// convFixture 落一个会话（含消息）；withRun=true 时再落 run/工具/产物（任务式会话）。
func (e *traceEnv) convFixture(t *testing.T, withRun bool) (conversationID int, runID int) {
	t.Helper()
	ctx := e.ctx
	base := e.base

	conv := e.client.Conversation.Create().
		SetTenantID(e.tenantID).SetUserID(7).SetBotID(3).SetTitle("连不上网").
		SetCreatedAt(base).
		SaveX(ctx)

	e.client.Message.Create().
		SetTenantID(e.tenantID).SetConversationID(conv.ID).SetRole("user").
		SetContent("我连不上网了").SetCreatedAt(base).
		SaveX(ctx)
	e.client.Message.Create().
		SetTenantID(e.tenantID).SetConversationID(conv.ID).SetRole("assistant").
		SetContent("请先确认是否只有你无法访问").SetRequestID("req-1").
		SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)
	// 超长消息（> 2000 字符）：验证按跑数截断 + truncated 标记。
	long := strings.Repeat("知", conversationTraceMessageCapRune+50)
	e.client.Message.Create().
		SetTenantID(e.tenantID).SetConversationID(conv.ID).SetRole("assistant").
		SetContent(long).SetCreatedAt(base.Add(2 * time.Second)).
		SaveX(ctx)

	if !withRun {
		return conv.ID, 0
	}

	user := e.client.User.Create().
		SetUsername("conv-trace-user").SetEmail("conv-trace@example.com").SetName("会话轨迹用户").
		SetPasswordHash("hash").SetTenantID(e.tenantID).SaveX(ctx)

	run := e.client.BotRun.Create().
		SetTenantID(e.tenantID).SetConversationID(conv.ID).SetBotID(3).
		SetEntrypoint("chat").SetStatus("completed").SetModel("gpt-4o").
		SetStartedAt(base).SetFinishedAt(base.Add(3 * time.Second)).
		SaveX(ctx)

	e.client.ToolInvocation.Create().
		SetTenantID(e.tenantID).SetConversationID(conv.ID).SetRunID(run.ID).
		SetToolName("create_ticket").
		SetArguments(`{"raw":"secret-args"}`).
		SetArgsRedacted(`{"title":"***"}`).
		SetStatus("success").SetNeedsApproval(true).SetApprovalState("pending").
		SetRisk("act_low").SetCategory("ticket").SetUserID(user.ID).
		SetDurationMs(90).SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)

	e.client.BotArtifact.Create().
		SetTenantID(e.tenantID).SetOwnerUserID(user.ID).SetConversationID(conv.ID).SetRunID(run.ID).
		SetKind("draft").SetToolName("draft_reply").SetTitle("答复草稿").
		SetContentJSON(`{"body":"已恢复"}`).
		SaveX(ctx)

	return conv.ID, run.ID
}

func TestGetConversationTrace_PureQAAChain(t *testing.T) {
	env := newTraceEnv(t)
	convID, _ := env.convFixture(t, false)

	trace, err := env.svc.GetConversationTrace(env.ctx, env.tenantID, convID)
	require.NoError(t, err)
	require.NotNil(t, trace)

	// 会话档案。
	assert.Equal(t, convID, trace.Conversation.ID)
	assert.Equal(t, 7, trace.Conversation.UserID)
	assert.Equal(t, 3, trace.Conversation.BotID)
	assert.Equal(t, "连不上网", trace.Conversation.Title)

	// 消息回合（ID 升序 + 角色/内容）。
	require.Len(t, trace.Messages, 3)
	assert.Equal(t, 3, trace.MessageCount)
	assert.False(t, trace.MessagesTruncated)
	assert.Equal(t, "user", trace.Messages[0].Role)
	assert.Equal(t, "我连不上网了", trace.Messages[0].Content)
	assert.Equal(t, "assistant", trace.Messages[1].Role)
	assert.Equal(t, "请先确认是否只有你无法访问", trace.Messages[1].Content)

	// 截断口径：内容按跑数上限截断并显式标记。
	assert.True(t, trace.Messages[2].Truncated)
	assert.Len(t, []rune(trace.Messages[2].Content), conversationTraceMessageCapRune)

	// 纯问答：无运行、无工具、无产物。
	assert.Empty(t, trace.Runs)
	assert.Empty(t, trace.ToolInvocations)
	assert.Empty(t, trace.Artifacts)
}

func TestGetConversationTrace_RunsAndToolChain(t *testing.T) {
	env := newTraceEnv(t)
	convID, runID := env.convFixture(t, true)

	trace, err := env.svc.GetConversationTrace(env.ctx, env.tenantID, convID)
	require.NoError(t, err)

	// 会话下的运行摘要（倒序，跳 run 轨迹入口）。
	require.Len(t, trace.Runs, 1)
	assert.Equal(t, runID, trace.Runs[0].ID)
	assert.Equal(t, "completed", trace.Runs[0].Status)
	assert.Equal(t, "chat", trace.Runs[0].Entrypoint)
	require.NotNil(t, trace.Runs[0].FinishedAt)

	// 工具调用（确认状态规范化 + 脱敏快照）。
	require.Len(t, trace.ToolInvocations, 1)
	tool := trace.ToolInvocations[0]
	assert.Equal(t, "create_ticket", tool.ToolName)
	assert.Equal(t, string(ConfirmationPending), tool.ConfirmationState)
	assert.Equal(t, "act_low", tool.Risk)

	// 产物。
	require.Len(t, trace.Artifacts, 1)
	assert.Equal(t, "draft", trace.Artifacts[0].Kind)
	assert.Equal(t, "答复草稿", trace.Artifacts[0].Title)

	// 脱敏断言：原始参数绝不进入会话轨迹响应。
	raw, err := json.Marshal(trace)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "secret-args")
	assert.Contains(t, string(raw), "***")
}

func TestGetConversationTrace_TenantIsolationAndNotFound(t *testing.T) {
	env := newTraceEnv(t)
	convID, _ := env.convFixture(t, false)

	// 跨租户：与不存在同口径（ErrConversationNotFound），不泄漏存在性。
	_, err := env.svc.GetConversationTrace(env.ctx, env.otherID, convID)
	assert.ErrorIs(t, err, ErrConversationNotFound)

	// 未命中与非法入参。
	_, err = env.svc.GetConversationTrace(env.ctx, env.tenantID, 999999)
	assert.ErrorIs(t, err, ErrConversationNotFound)
	_, err = env.svc.GetConversationTrace(env.ctx, env.tenantID, 0)
	assert.ErrorIs(t, err, ErrConversationNotFound)

	// 未装配（nil）→ 非 NotFound（调用方据此返回 503）。
	var nilSvc *TraceService
	_, err = nilSvc.GetConversationTrace(env.ctx, env.tenantID, convID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrConversationNotFound)
}

func TestGetConversationTrace_MessageWindowTruncated(t *testing.T) {
	env := newTraceEnv(t)
	ctx := env.ctx
	conv := env.client.Conversation.Create().
		SetTenantID(env.tenantID).SetUserID(7).SetTitle("窗口截断").
		SaveX(ctx)

	// 消息数超过单次窗口上限：只回前 200 条 + messagesTruncated=true（总量仍如实上报）。
	total := conversationTraceMaxMessages + 5
	for i := 0; i < total; i++ {
		env.client.Message.Create().
			SetTenantID(env.tenantID).SetConversationID(conv.ID).SetRole("user").
			SetContent(fmt.Sprintf("m-%d", i)).
			SaveX(ctx)
	}

	trace, err := env.svc.GetConversationTrace(ctx, env.tenantID, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, total, trace.MessageCount)
	assert.Len(t, trace.Messages, conversationTraceMaxMessages)
	assert.True(t, trace.MessagesTruncated)
	assert.Equal(t, "m-0", trace.Messages[0].Content)
}
