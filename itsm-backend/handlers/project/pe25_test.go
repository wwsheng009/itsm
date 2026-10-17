package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent/auditlog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProjectAIPort 项目 AI 端口假实现（PE-25 测试；记录调用入参）。
type fakeProjectAIPort struct {
	summary   string
	warnings  []AIRiskWarning
	proposals []AITaskProposal
	err       error
	model     string

	summaryCalls int
	riskCalls    int
	minutesCalls int
	lastFacts    *AIProjectFacts
	lastCadence  string
	lastContent  string
	lastLimit    int
	lastTID      int
	lastUID      int
}

func (f *fakeProjectAIPort) Summarize(_ context.Context, tenantID, userID, _ int, cadence string, facts *AIProjectFacts) (string, error) {
	f.summaryCalls++
	f.lastTID, f.lastUID, f.lastCadence, f.lastFacts = tenantID, userID, cadence, facts
	if f.err != nil {
		return "", f.err
	}
	return f.summary, nil
}

func (f *fakeProjectAIPort) AssessRisks(_ context.Context, tenantID, userID, _ int, facts *AIProjectFacts) ([]AIRiskWarning, error) {
	f.riskCalls++
	f.lastTID, f.lastUID, f.lastFacts = tenantID, userID, facts
	if f.err != nil {
		return nil, f.err
	}
	return f.warnings, nil
}

func (f *fakeProjectAIPort) ExtractTasks(_ context.Context, tenantID, userID, _ int, content string, limit int) ([]AITaskProposal, error) {
	f.minutesCalls++
	f.lastTID, f.lastUID, f.lastContent, f.lastLimit = tenantID, userID, content, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.proposals, nil
}

func (f *fakeProjectAIPort) ModelName() string { return f.model }

// TestAIDegradedWithoutPort 未装配 AI 端口（降级模式）：三端点返回确定性内容、不报错（PE-25/D-44）。
func TestAIDegradedWithoutPort(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	admin := seedUserWithMembership(t, client, tid, "ai-deg", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "AI 降级项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, admin.ID, &MemberAddRequest{UserID: admin.ID})
	require.NoError(t, err)

	past := time.Now().Add(-96 * time.Hour)
	// 项目整体逾期（直接置结束日期为过去）。
	_, err = client.Project.UpdateOneID(p.ID).SetEndDate(past).Save(ctx)
	require.NoError(t, err)
	// 逾期任务 + 阻塞任务。
	overdueTask, err := svc.CreateTask(ctx, tid, p.ID, admin.ID, &TaskCreateRequest{Name: "逾期任务", PlanEnd: &past})
	require.NoError(t, err)
	blockedTask, err := svc.CreateTask(ctx, tid, p.ID, admin.ID, &TaskCreateRequest{Name: "阻塞任务"})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, blockedTask.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, blockedTask.ID, &TaskStatusRequest{Action: TaskActionBlock})
	require.NoError(t, err)
	// 逾期未达成里程碑。
	_, err = svc.CreateMilestone(ctx, tid, p.ID, admin.ID, &MilestoneCreateRequest{Name: "逾期里程碑", DueDate: &past})
	require.NoError(t, err)
	// 高等级未关闭风险。
	_, err = svc.CreateRisk(ctx, tid, p.ID, admin.ID, &RiskCreateRequest{Title: "高风险未关闭", Level: RiskLevelHigh})
	require.NoError(t, err)
	// 被拒阶段门。
	_, err = client.ProjectGate.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetGate("planning").
		SetStatus(GateStatusRejected).SetRejectCount(2).Save(ctx)
	require.NoError(t, err)

	// ① 摘要：确定性降级 + 事实派生文本。
	sum, err := svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, &AISummaryRequest{})
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByDeterministic, sum.GeneratedBy)
	assert.True(t, sum.Degraded)
	assert.Equal(t, AIDegradeAIUnavailable, sum.DegradeReason)
	assert.Empty(t, sum.Model)
	assert.Contains(t, sum.Summary, "事实派生")
	assert.Contains(t, sum.Summary, "AI 降级项目")
	assert.Greater(t, sum.PromptChars, 0)
	assert.Equal(t, ReportCadenceWeekly, sum.Cadence)

	// ② 风险预警：规则信号恒返回（六类全命中）。
	risk, err := svc.GenerateAIRiskWarnings(ctx, tid, p.ID, admin.ID, false)
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByRules, risk.GeneratedBy)
	assert.True(t, risk.Degraded)
	assert.Equal(t, AIDegradeAIUnavailable, risk.DegradeReason)
	assert.Empty(t, risk.Warnings)
	assert.Equal(t, len(risk.Signals), risk.SignalTotal)
	assert.False(t, risk.Truncated)
	types := make(map[string]bool, len(risk.Signals))
	for _, sig := range risk.Signals {
		types[sig.Type] = true
	}
	for _, want := range []string{
		AIRiskSignalOverdueProject, AIRiskSignalOverdueTask, AIRiskSignalBlockedTask,
		AIRiskSignalOverdueMilestone, AIRiskSignalHighRisk, AIRiskSignalGateRejected,
	} {
		assert.True(t, types[want], "规则信号应命中 %s", want)
	}
	// 任务/里程碑引用回带（前端可跳转）。
	refIDs := make(map[int]bool)
	for _, sig := range risk.Signals {
		refIDs[sig.RefID] = true
	}
	assert.True(t, refIDs[overdueTask.ID])

	// ③ 纪要转任务：空提案 + none。
	content := "会议：张三负责补充压测方案。"
	min, err := svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{Content: content})
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByNone, min.GeneratedBy)
	assert.True(t, min.Degraded)
	assert.Equal(t, AIDegradeAIUnavailable, min.DegradeReason)
	assert.Empty(t, min.Proposals)
	assert.Equal(t, len([]rune(strings.TrimSpace(content))), min.ContentChars)

	// 词表校验：非法 cadence / 空 content / 超长 content / 越界 limit。
	_, err = svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, &AISummaryRequest{Cadence: "daily"})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{Content: "   "})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{
		Content: strings.Repeat("字", AIMaxMinutesChars+1),
	})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{Content: "纪要", Limit: 1})
	require.ErrorIs(t, err, ErrValidation)
}

// TestAIWithPortSuccess 装配 AI 端口（成功路径）：generatedBy=ai + 模型透出 + 输入事实/词表归一（PE-25）。
func TestAIWithPortSuccess(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	admin := seedUserWithMembership(t, client, tid, "ai-ok", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "AI 成功项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, admin.ID, &MemberAddRequest{UserID: admin.ID})
	require.NoError(t, err)

	fake := &fakeProjectAIPort{
		model:   "fake-ai-1",
		summary: "## AI 摘要\n- 整体进展正常",
		warnings: []AIRiskWarning{
			{Severity: "bogus", Title: " W1 ", Detail: "d1"},
			{Severity: AISeverityWarning, Title: "   "}, // 空标题 → 丢弃
			{Severity: AISeverityCritical, Title: "W2", Suggestion: "s2"},
		},
		proposals: []AITaskProposal{
			{Name: " 补充压测 ", Priority: "high", EstimateHours: ptrF(t, 8), OwnerHint: " 张三 "},
			{Name: "   "}, // 空名 → 丢弃
			{Name: "整理验收材料", Priority: "urgent", EstimateHours: ptrF(t, -3)}, // 非法优先级/负工时 → 字段丢弃
		},
	}
	svc.SetAIPort(fake)

	// ① 摘要：ai 文本 + 模型名 + 事实提示文本回带。
	sum, err := svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, &AISummaryRequest{})
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByAI, sum.GeneratedBy)
	assert.False(t, sum.Degraded)
	assert.Empty(t, sum.DegradeReason)
	assert.Equal(t, "fake-ai-1", sum.Model)
	assert.Contains(t, sum.Summary, "AI 摘要")
	assert.Equal(t, 1, fake.summaryCalls)
	assert.Equal(t, ReportCadenceWeekly, fake.lastCadence)
	assert.Equal(t, tid, fake.lastTID)
	assert.Equal(t, admin.ID, fake.lastUID)
	require.NotNil(t, fake.lastFacts)
	assert.Contains(t, fake.lastFacts.PromptText, "AI 成功项目")
	assert.LessOrEqual(t, len([]rune(fake.lastFacts.PromptText)), aiFactsPromptCap)

	// biweekly 周期透传给端口。
	_, err = svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, &AISummaryRequest{Cadence: ReportCadenceBiweekly})
	require.NoError(t, err)
	assert.Equal(t, ReportCadenceBiweekly, fake.lastCadence)

	// ② 风险预警：AI 段归一（未知 severity → warning、空标题丢弃、上限截断）+ 规则信号仍在。
	risk, err := svc.GenerateAIRiskWarnings(ctx, tid, p.ID, admin.ID, false)
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByAI, risk.GeneratedBy)
	assert.False(t, risk.Degraded)
	assert.Equal(t, "fake-ai-1", risk.Model)
	require.Len(t, risk.Warnings, 2)
	assert.Equal(t, AISeverityWarning, risk.Warnings[0].Severity) // bogus → warning
	assert.Equal(t, "W1", risk.Warnings[0].Title)
	assert.Equal(t, AISeverityCritical, risk.Warnings[1].Severity)
	assert.Equal(t, 1, fake.riskCalls)

	// ③ 纪要转任务：归一（trim/非法优先级丢弃/负工时丢弃/limit 生效）。
	min, err := svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{
		Content: "  会议纪要正文…  ", Limit: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, AIGeneratedByAI, min.GeneratedBy)
	assert.False(t, min.Degraded)
	require.Len(t, min.Proposals, 2)
	assert.Equal(t, "补充压测", min.Proposals[0].Name)
	assert.Equal(t, "high", min.Proposals[0].Priority)
	require.NotNil(t, min.Proposals[0].EstimateHours)
	assert.Equal(t, 8.0, *min.Proposals[0].EstimateHours)
	assert.Equal(t, "张三", min.Proposals[0].OwnerHint)
	assert.Empty(t, min.Proposals[1].Priority)
	assert.Nil(t, min.Proposals[1].EstimateHours)
	assert.Equal(t, 5, fake.lastLimit)
	assert.Equal(t, "会议纪要正文…", fake.lastContent)
}

// TestAIDegradationOnFailure AI 失败/解析失败 → 降级（llm_error/parse_error）且主流程不受影响（PE-25/D-44）。
func TestAIDegradationOnFailure(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	admin := seedUserWithMembership(t, client, tid, "ai-fail", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "AI 失败项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, admin.ID, &MemberAddRequest{UserID: admin.ID})
	require.NoError(t, err)

	// LLM 调用失败 → llm_error。
	svc.SetAIPort(&fakeProjectAIPort{err: errors.New("boom")})
	sum, err := svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, nil)
	require.NoError(t, err)
	assert.True(t, sum.Degraded)
	assert.Equal(t, AIDegradeLLMError, sum.DegradeReason)
	assert.Equal(t, AIGeneratedByDeterministic, sum.GeneratedBy)

	risk, err := svc.GenerateAIRiskWarnings(ctx, tid, p.ID, admin.ID, false)
	require.NoError(t, err)
	assert.True(t, risk.Degraded)
	assert.Equal(t, AIDegradeLLMError, risk.DegradeReason)
	assert.Equal(t, AIGeneratedByRules, risk.GeneratedBy)

	min, err := svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{Content: "会议纪要"})
	require.NoError(t, err)
	assert.True(t, min.Degraded)
	assert.Equal(t, AIDegradeLLMError, min.DegradeReason)
	assert.Empty(t, min.Proposals)

	// 解析失败 → parse_error。
	svc.SetAIPort(&fakeProjectAIPort{err: fmt.Errorf("%w: bad json", ErrAIParseOutput)})
	sum, err = svc.GenerateAISummary(ctx, tid, p.ID, admin.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, AIDegradeParseError, sum.DegradeReason)
	min, err = svc.ExtractTasksFromMinutes(ctx, tid, p.ID, admin.ID, false, &AIMinutesTasksRequest{Content: "会议纪要"})
	require.NoError(t, err)
	assert.Equal(t, AIDegradeParseError, min.DegradeReason)

	// 主流程不受影响：AI 降级后仍可正常建任务/读列表。
	task, err := svc.CreateTask(ctx, tid, p.ID, admin.ID, &TaskCreateRequest{Name: "降级后仍可建任务"})
	require.NoError(t, err)
	require.NotNil(t, task)
	tasks, err := svc.ListTasks(ctx, tid, p.ID, admin.ID, false, "", 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

// TestAIVisibility 可见性：非成员三端点统一 404（与详情同谓词；PE-25）。
func TestAIVisibility(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "ai-vis", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "AI 可见性项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	outsider := seedUserWithMembership(t, client, tid, "ai-out", "home", nil)
	_, err = svc.GenerateAISummary(ctx, tid, p.ID, outsider.ID, false, nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GenerateAIRiskWarnings(ctx, tid, p.ID, outsider.ID, false)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.ExtractTasksFromMinutes(ctx, tid, p.ID, outsider.ID, false, &AIMinutesTasksRequest{Content: "纪要"})
	require.ErrorIs(t, err, ErrNotFound)

	// 成员（非 admin）可见。
	_, err = svc.GenerateAIRiskWarnings(ctx, tid, p.ID, member.ID, false)
	require.NoError(t, err)
}

// TestHandlerAIEndpoints 端点级：降级也 200 + 校验 400 + 审计留痕（resource=project）（PE-25/D-47）。
func TestHandlerAIEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "AI 接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// ① 摘要：无端口 → 200 + deterministic 降级。
	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/summary", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var sum AISummaryResponse
	require.NoError(t, json.Unmarshal(env.Data, &sum))
	assert.Equal(t, AIGeneratedByDeterministic, sum.GeneratedBy)
	assert.True(t, sum.Degraded)
	// 非法 cadence → 400。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/summary", created.ID), map[string]any{"cadence": "daily"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// ② 风险预警：200 + rules；空项目信号为 []。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/risk-warnings", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var risk AIRiskWarningsResponse
	require.NoError(t, json.Unmarshal(env.Data, &risk))
	assert.Equal(t, AIGeneratedByRules, risk.GeneratedBy)
	assert.NotNil(t, risk.Signals)
	assert.Empty(t, risk.Signals)

	// ③ 纪要转任务：校验 400（空/超长/limit）、成功 200 + none。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/minutes/tasks", created.ID), map[string]any{"content": "  "})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/minutes/tasks", created.ID), map[string]any{
		"content": strings.Repeat("字", AIMaxMinutesChars+1),
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/minutes/tasks", created.ID), map[string]any{"content": "纪要", "limit": 99})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/ai/minutes/tasks", created.ID), map[string]any{"content": "会议纪要正文"})
	require.Equal(t, http.StatusOK, w.Code)
	var minResp AIMinutesTasksResponse
	require.NoError(t, json.Unmarshal(env.Data, &minResp))
	assert.Equal(t, AIGeneratedByNone, minResp.GeneratedBy)
	assert.Empty(t, minResp.Proposals)

	// 不存在的项目 → 404。
	w, _ = doJSON(t, r, http.MethodPost, "/projects/999999/ai/summary", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 审计：三 action 各留痕且 body 记录降级标记（resource=project）。
	for _, action := range []string{"ai_summary_generate", "ai_risk_warnings", "ai_minutes_extract"} {
		audits, err := client.AuditLog.Query().
			Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ(action)).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, audits, 1, "action=%s 应留痕一次", action)
		require.NotNil(t, audits[0].RequestBody)
		assert.Contains(t, *audits[0].RequestBody, `"degraded":true`)
		assert.Equal(t, tid, audits[0].TenantID)
	}
}