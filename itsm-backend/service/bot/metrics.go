package bot

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
	"itsm-backend/ent/toolinvocation"
)

// B4-02：Bot 运行维度指标（成功/确认/verify/工具错误/时延/成本代理）。
//
// 口径与阶段一报告 §5.9 对齐；成本维度的 token 计量尚未接线（B1-02 遗留），
// 暂以「步数 / 工具调用数 / 时延」为代理，并在 `TokensRecorded=false` 明示。
//
// 数据来源：`bot_runs`（运行）、`bot_steps`（步骤）、`tool_invocations`（工具调用/确认/verify）。
// 所有查询一律带租户前置；窗口按 `started_at`/`created_at` 收敛到 `[since, now]`。

const (
	// metricsRowCap 单次聚合最多读取的行数（超出时在 Notes 中明示，结果按已读取部分计算）。
	metricsRowCap = 50000
	// MetricsDefaultDays 默认回看窗口（天）。
	MetricsDefaultDays = 7
	// MetricsMaxDays 回看窗口上限（天），防止大表全扫。
	MetricsMaxDays = 90
)

// MetricsQuery 是运行维度指标的查询条件。
type MetricsQuery struct {
	// Days 回看天数（<=0 用默认值；>上限 截断）。
	Days int
	// BotID 可选：仅统计某个 Bot 模板；0 = 全部（含兼容默认）。
	BotID int
	// Entrypoint 可选：仅统计某个入口。
	Entrypoint string
}

// MetricsSummary 是运行维度指标的一次聚合结果。
type MetricsSummary struct {
	WindowDays  int       `json:"windowDays"`
	Since       time.Time `json:"since"`
	GeneratedAt time.Time `json:"generatedAt"`

	Runs          RunMetrics   `json:"runs"`
	Steps         StepMetrics  `json:"steps"`
	Tools         ToolMetrics  `json:"tools"`
	Verify        VerifyStats  `json:"verify"`
	Confirmations ConfirmStats `json:"confirmations"`
	Cost          CostMetrics  `json:"cost"`

	ByEntrypoint []Breakdown `json:"byEntrypoint"`
	ByBot        []Breakdown `json:"byBot"`

	// TokensRecorded 恒为 false（token 计量未接线）；Notes 说明口径与截断。
	TokensRecorded bool     `json:"tokensRecorded"`
	Notes          []string `json:"notes,omitempty"`
}

// RunMetrics 是运行级指标。
type RunMetrics struct {
	Total             int            `json:"total"`
	Completed         int            `json:"completed"`
	Failed            int            `json:"failed"`
	Running           int            `json:"running"`
	Cancelled         int            `json:"cancelled"`
	SuccessRate       float64        `json:"successRate"`   // completed / (completed + failed)
	AvgDurationMs     float64        `json:"avgDurationMs"` // 仅统计已完成/失败的运行
	FailureRateByCode map[string]int `json:"failureRateByCode,omitempty"`
}

// StepMetrics 是步骤级指标。
type StepMetrics struct {
	Total         int     `json:"total"`
	LLMSteps      int     `json:"llmSteps"`
	ToolSteps     int     `json:"toolSteps"`
	ConfirmSteps  int     `json:"confirmSteps"`
	AvgPerRun     float64 `json:"avgPerRun"`
	AvgDurationMs float64 `json:"avgDurationMs"`
}

// ToolMetrics 是工具调用指标（run 维度）。
type ToolMetrics struct {
	Total         int     `json:"total"`
	Errors        int     `json:"errors"`
	ErrorRate     float64 `json:"errorRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	TopTools      []Count `json:"topTools,omitempty"`
}

// VerifyStats 是执行后回读指标。
type VerifyStats struct {
	Verified int     `json:"verified"`
	Failed   int     `json:"failed"`
	Skipped  int     `json:"skipped"`
	Pending  int     `json:"pending"`
	FailRate float64 `json:"failRate"` // failed / (verified + failed)
}

// ConfirmStats 是确认单指标。
type ConfirmStats struct {
	Approved      int     `json:"approved"`
	Rejected      int     `json:"rejected"`
	Expired       int     `json:"expired"`
	Pending       int     `json:"pending"`
	ApprovalRate  float64 `json:"approvalRate"` // approved / decided
	RejectRate    float64 `json:"rejectRate"`   // rejected / decided
	ExpireRate    float64 `json:"expireRate"`   // expired / decided
	AvgDecisionMs float64 `json:"avgDecisionMs,omitempty"`
}

// CostMetrics 是成本维度（token 未接线，代理口径）。
type CostMetrics struct {
	LLMCalls           int     `json:"llmCalls"`
	ToolCalls          int     `json:"toolCalls"`
	Steps              int     `json:"steps"`
	AvgStepsPerRun     float64 `json:"avgStepsPerRun"`
	AvgToolCallsPerRun float64 `json:"avgToolCallsPerRun"`
}

// Count 是简单计数项（Top 榜单）。
type Count struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// Breakdown 是按入口或 Bot 的分解。
type Breakdown struct {
	Key           string  `json:"key"`
	Runs          int     `json:"runs"`
	Failed        int     `json:"failed"`
	SuccessRate   float64 `json:"successRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	ToolCalls     int     `json:"toolCalls"`
	ToolErrors    int     `json:"toolErrors"`
}

// MetricsService 汇总运行维度指标（只读）。
type MetricsService struct {
	client *ent.Client
	now    func() time.Time
}

// NewMetricsService 构造指标服务；client 为空时各方法返回明确错误。
func NewMetricsService(client *ent.Client) *MetricsService {
	return &MetricsService{client: client, now: time.Now}
}

func (s *MetricsService) window(q MetricsQuery) (days int, since time.Time) {
	days = q.Days
	if days <= 0 {
		days = MetricsDefaultDays
	}
	if days > MetricsMaxDays {
		days = MetricsMaxDays
	}
	return days, s.now().AddDate(0, 0, -days)
}

// Summary 聚合窗口内的运行维度指标。
func (s *MetricsService) Summary(ctx context.Context, tenantID int, q MetricsQuery) (*MetricsSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: 指标服务未初始化")
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("bot: 指标查询需要租户")
	}
	days, since := s.window(q)
	now := s.now()

	runs, runTruncated, err := s.loadRuns(ctx, tenantID, since, q)
	if err != nil {
		return nil, err
	}
	steps, stepTruncated, err := s.loadSteps(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}
	invocations, invTruncated, err := s.loadInvocations(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}

	summary := &MetricsSummary{
		WindowDays:     days,
		Since:          since,
		GeneratedAt:    now,
		TokensRecorded: false,
	}
	summary.Notes = append(summary.Notes,
		"token 计量未接线（B1-02 遗留）：成本维度以步数/工具调用数/时延为代理")
	if runTruncated || stepTruncated || invTruncated {
		summary.Notes = append(summary.Notes,
			fmt.Sprintf("读取行数达到上限 %d，指标按已读取部分计算（请收窄窗口或按 Bot/入口过滤）", metricsRowCap))
	}

	// 仅统计与筛选条件相符的运行（步骤/工具调用按 run 归属收敛）。
	runIDs := make(map[int]bool, len(runs))
	entrypointOf := make(map[int]string, len(runs))
	botOf := make(map[int]string, len(runs))
	for _, r := range runs {
		runIDs[r.ID] = true
		entrypointOf[r.ID] = r.Entrypoint
		botOf[r.ID] = botKey(r.BotID)
	}

	summary.Runs = aggregateRuns(runs, q, now)
	summary.Steps = aggregateSteps(steps, runIDs)
	summary.Tools = aggregateTools(invocations, runIDs)
	summary.Verify = aggregateVerify(invocations, runIDs)
	summary.Confirmations = aggregateConfirmations(invocations, runIDs, now)
	summary.Cost = CostMetrics{
		LLMCalls:  summary.Steps.LLMSteps,
		ToolCalls: summary.Tools.Total,
		Steps:     summary.Steps.Total,
	}
	if summary.Runs.Total > 0 {
		summary.Cost.AvgStepsPerRun = round3(float64(summary.Steps.Total) / float64(summary.Runs.Total))
		summary.Cost.AvgToolCallsPerRun = round3(float64(summary.Tools.Total) / float64(summary.Runs.Total))
	}
	summary.ByEntrypoint = breakdownRuns(runs, entrypointOf, invocations, runIDs)
	summary.ByBot = breakdownRuns(runs, botOf, invocations, runIDs)
	return summary, nil
}

func botKey(id int) string {
	if id <= 0 {
		return "compat-default"
	}
	return fmt.Sprintf("bot#%d", id)
}

type runRow struct {
	ID         int
	Entrypoint string
	BotID      int
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
}

func (s *MetricsService) loadRuns(ctx context.Context, tenantID int, since time.Time, q MetricsQuery) ([]runRow, bool, error) {
	query := s.client.BotRun.Query().
		Where(botrun.TenantID(tenantID), botrun.StartedAtGTE(since)).
		Select(botrun.FieldID, botrun.FieldEntrypoint, botrun.FieldBotID, botrun.FieldStatus,
			botrun.FieldStartedAt, botrun.FieldFinishedAt).
		Limit(metricsRowCap + 1)
	if q.BotID > 0 {
		query = query.Where(botrun.BotID(q.BotID))
	}
	if q.Entrypoint != "" {
		query = query.Where(botrun.Entrypoint(q.Entrypoint))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > metricsRowCap
	if truncated {
		rows = rows[:metricsRowCap]
	}
	out := make([]runRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, runRow{
			ID:         r.ID,
			Entrypoint: r.Entrypoint,
			BotID:      r.BotID,
			Status:     r.Status,
			StartedAt:  r.StartedAt,
			FinishedAt: r.FinishedAt,
		})
	}
	return out, truncated, nil
}

type stepRow struct {
	RunID      int
	Type       string
	DurationMs int
	ErrorCode  string
}

func (s *MetricsService) loadSteps(ctx context.Context, tenantID int, since time.Time) ([]stepRow, bool, error) {
	rows, err := s.client.BotStep.Query().
		Where(botstep.TenantID(tenantID), botstep.CreatedAtGTE(since)).
		Select(botstep.FieldRunID, botstep.FieldType, botstep.FieldDurationMs, botstep.FieldErrorCode).
		Limit(metricsRowCap + 1).
		All(ctx)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > metricsRowCap
	if truncated {
		rows = rows[:metricsRowCap]
	}
	out := make([]stepRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, stepRow{RunID: r.RunID, Type: r.Type, DurationMs: r.DurationMs, ErrorCode: r.ErrorCode})
	}
	return out, truncated, nil
}

type invocationRow struct {
	RunID         int
	ToolName      string
	Status        string
	DurationMs    int
	ErrorCode     string
	NeedsApproval bool
	ApprovalState string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	ApprovedAt    time.Time
	VerifyState   string
}

func (s *MetricsService) loadInvocations(ctx context.Context, tenantID int, since time.Time) ([]invocationRow, bool, error) {
	rows, err := s.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(tenantID), toolinvocation.CreatedAtGTE(since)).
		Select(toolinvocation.FieldRunID, toolinvocation.FieldToolName, toolinvocation.FieldStatus,
			toolinvocation.FieldDurationMs, toolinvocation.FieldErrorCode,
			toolinvocation.FieldNeedsApproval, toolinvocation.FieldApprovalState,
			toolinvocation.FieldExpiresAt, toolinvocation.FieldCreatedAt,
			toolinvocation.FieldApprovedAt, toolinvocation.FieldVerifyState).
		Limit(metricsRowCap + 1).
		All(ctx)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > metricsRowCap
	if truncated {
		rows = rows[:metricsRowCap]
	}
	out := make([]invocationRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, invocationRow{
			RunID:         r.RunID,
			ToolName:      r.ToolName,
			Status:        r.Status,
			DurationMs:    r.DurationMs,
			ErrorCode:     r.ErrorCode,
			NeedsApproval: r.NeedsApproval,
			ApprovalState: r.ApprovalState,
			ExpiresAt:     r.ExpiresAt,
			CreatedAt:     r.CreatedAt,
			ApprovedAt:    r.ApprovedAt,
			VerifyState:   r.VerifyState,
		})
	}
	return out, truncated, nil
}

func aggregateRuns(runs []runRow, q MetricsQuery, now time.Time) RunMetrics {
	out := RunMetrics{FailureRateByCode: map[string]int{}}
	var durationSum float64
	var durationCount int
	for _, r := range runs {
		out.Total++
		switch r.Status {
		case "completed":
			out.Completed++
		case "failed":
			out.Failed++
		case "running":
			out.Running++
		case "cancelled":
			out.Cancelled++
		}
		if r.FinishedAt != nil {
			durationSum += float64(r.FinishedAt.Sub(r.StartedAt).Milliseconds())
			durationCount++
		}
	}
	decided := out.Completed + out.Failed
	if decided > 0 {
		out.SuccessRate = round3(float64(out.Completed) / float64(decided))
	}
	if durationCount > 0 {
		out.AvgDurationMs = round3(durationSum / float64(durationCount))
	}
	return out
}

func aggregateSteps(steps []stepRow, runIDs map[int]bool) StepMetrics {
	out := StepMetrics{}
	var durationSum float64
	for _, s := range steps {
		if !runIDs[s.RunID] {
			continue
		}
		out.Total++
		switch s.Type {
		case "llm":
			out.LLMSteps++
		case "tool":
			out.ToolSteps++
		case "confirm":
			out.ConfirmSteps++
		}
		durationSum += float64(s.DurationMs)
	}
	if out.Total > 0 {
		out.AvgDurationMs = round3(durationSum / float64(out.Total))
	}
	if len(runIDs) > 0 {
		out.AvgPerRun = round3(float64(out.Total) / float64(len(runIDs)))
	}
	return out
}

func aggregateTools(invocations []invocationRow, runIDs map[int]bool) ToolMetrics {
	out := ToolMetrics{}
	top := map[string]int{}
	var durationSum float64
	for _, inv := range invocations {
		if !runIDs[inv.RunID] {
			continue
		}
		// 待确认单尚未执行，不计入工具执行指标（其状态见 Confirmations）。
		if inv.NeedsApproval && (inv.ApprovalState == "pending" || inv.ApprovalState == "rejected") {
			continue
		}
		out.Total++
		if isToolError(inv) {
			out.Errors++
		}
		durationSum += float64(inv.DurationMs)
		top[inv.ToolName]++
	}
	if out.Total > 0 {
		out.ErrorRate = round3(float64(out.Errors) / float64(out.Total))
		out.AvgDurationMs = round3(durationSum / float64(out.Total))
		out.TopTools = topN(top, 5)
	}
	return out
}

func isToolError(inv invocationRow) bool {
	if inv.ErrorCode != "" {
		return true
	}
	switch inv.Status {
	case "success", "", "pending", "approved", "running":
		return false
	default:
		return true
	}
}

func aggregateVerify(invocations []invocationRow, runIDs map[int]bool) VerifyStats {
	out := VerifyStats{}
	for _, inv := range invocations {
		if !runIDs[inv.RunID] {
			continue
		}
		switch inv.VerifyState {
		case "verified":
			out.Verified++
		case "failed":
			out.Failed++
		case "skipped":
			out.Skipped++
		case "pending":
			out.Pending++
		}
	}
	decided := out.Verified + out.Failed
	if decided > 0 {
		out.FailRate = round3(float64(out.Failed) / float64(decided))
	}
	return out
}

func aggregateConfirmations(invocations []invocationRow, runIDs map[int]bool, now time.Time) ConfirmStats {
	out := ConfirmStats{}
	var decisionSum float64
	var decisionCount int
	for _, inv := range invocations {
		if !inv.NeedsApproval || !runIDs[inv.RunID] {
			continue
		}
		switch {
		case inv.ApprovalState == "approved":
			out.Approved++
			if !inv.ApprovedAt.IsZero() {
				decisionSum += float64(inv.ApprovedAt.Sub(inv.CreatedAt).Milliseconds())
				decisionCount++
			}
		case inv.ApprovalState == "rejected":
			out.Rejected++
			if !inv.ApprovedAt.IsZero() {
				decisionSum += float64(inv.ApprovedAt.Sub(inv.CreatedAt).Milliseconds())
				decisionCount++
			}
		case inv.ExpiresAt != nil && inv.ExpiresAt.Before(now):
			out.Expired++
		default:
			out.Pending++
		}
	}
	decided := out.Approved + out.Rejected + out.Expired
	if decided > 0 {
		out.ApprovalRate = round3(float64(out.Approved) / float64(decided))
		out.RejectRate = round3(float64(out.Rejected) / float64(decided))
		out.ExpireRate = round3(float64(out.Expired) / float64(decided))
	}
	if decisionCount > 0 {
		out.AvgDecisionMs = round3(decisionSum / float64(decisionCount))
	}
	return out
}

func breakdownRuns(runs []runRow, keyOf map[int]string, invocations []invocationRow, runIDs map[int]bool) []Breakdown {
	type acc struct {
		runs, failed   int
		durationSum    float64
		durationCount  int
		tools, toolErr int
	}
	buckets := map[string]*acc{}
	order := []string{}
	get := func(key string) *acc {
		if key == "" {
			key = "unknown"
		}
		if _, ok := buckets[key]; !ok {
			buckets[key] = &acc{}
			order = append(order, key)
		}
		return buckets[key]
	}
	for _, r := range runs {
		a := get(keyOf[r.ID])
		a.runs++
		if r.Status == "failed" {
			a.failed++
		}
		if r.FinishedAt != nil {
			a.durationSum += float64(r.FinishedAt.Sub(r.StartedAt).Milliseconds())
			a.durationCount++
		}
	}
	for _, inv := range invocations {
		if !runIDs[inv.RunID] {
			continue
		}
		a := get(keyOf[inv.RunID])
		if !inv.NeedsApproval || inv.ApprovalState == "approved" {
			a.tools++
			if isToolError(inv) {
				a.toolErr++
			}
		}
	}
	out := make([]Breakdown, 0, len(order))
	for _, key := range order {
		a := buckets[key]
		item := Breakdown{Key: key, Runs: a.runs, Failed: a.failed, ToolCalls: a.tools, ToolErrors: a.toolErr}
		decided := a.runs
		if a.runs > 0 {
			item.SuccessRate = round3(float64(a.runs-a.failed) / float64(decided))
		}
		if a.durationCount > 0 {
			item.AvgDurationMs = round3(a.durationSum / float64(a.durationCount))
		}
		out = append(out, item)
	}
	// 稳定排序：运行数降序，其次键名升序。
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Runs > out[i].Runs || (out[j].Runs == out[i].Runs && out[j].Key < out[i].Key) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func topN(counts map[string]int, n int) []Count {
	out := make([]Count, 0, len(counts))
	for k, v := range counts {
		out = append(out, Count{Key: k, Count: v})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Count > out[i].Count || (out[j].Count == out[i].Count && out[j].Key < out[i].Key) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}
