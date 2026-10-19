// V0-04 试点对照报告（§11.5 放量门槛 / §13.6 自动产出）。
//
// 口径：
//   - **对照**：本期窗口（最近 currentDays 天）vs 基线窗口（其前 baselineDays 天）——
//     基线用同一埋点采集（§7 V 系列：对照数据不可回溯补采），因此窗口可配置；
//   - **达标**：试点主指标相对基线改善 ≥30%（§11.5「降幅 ≥30% 或等值业务指标」）；
//   - **负向指标**：误分类率代理 / 建议忽略率 / 主动提示关闭率 / 任务失败率——
//     恶化判定含 10% 小样本噪声容差（实现口径，证据中记录）；
//   - **门槛**：≥2 项试点达标且负向指标未恶化 → 允许按 §7 顺序放量 Dock（§11.5）。
//
// 数据来源：`ai_telemetry_events`（O0-02 表），单次 FILTER 聚合（PG），租户前置。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	PilotReportDefaultCurrentDays  = 7
	PilotReportDefaultBaselineDays = 7
	PilotReportMaxDays             = 30
	// §11.5：相对基线改善 ≥30% 视为达标。
	pilotImproveThreshold = 0.30
	// 负向指标不恶化的噪声容差（小样本；超过 +10% 才判恶化）。
	pilotDeteriorationTolerance = 0.10
	// §11.5：至少 2 项试点达标。
	pilotRequiredMet = 2
)

// PilotReportQuery 对照窗口配置（0/负数回落默认；上限 clamp）。
type PilotReportQuery struct {
	CurrentDays  int
	BaselineDays int
}

// PilotCounters 是一次窗口的计数集合（单条 SQL FILTER 聚合）。
type PilotCounters struct {
	InlinePreview      int
	InlineApplied      int
	InlineUndone       int
	PlanPreview        int
	PlanApplied        int
	PlanCompleted      int
	PlanLatencyP95     float64
	SelfAnswered       int
	SelfResolved       int
	SelfTicketCreated  int
	FieldCorrected     int
	AICreateCompleted  int
	AICreateLatencyP95 float64
	SuggestionShown    int
	SuggestionIgnored  int
	ProactiveShown     int
	ProactiveDismissed int
	ErrorShown         int
	ErrorRecovered     int
	TaskStarted        int
	TaskFailed         int
}

// PilotMetric 单指标对照结果。
type PilotMetric struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Unit        string  `json:"unit"`      // rate | ms
	Direction   string  `json:"direction"` // up | down
	Kind        string  `json:"kind"`      // primary | secondary | negative
	Baseline    float64 `json:"baseline"`
	Current     float64 `json:"current"`
	Delta       float64 `json:"delta"`
	Improvement float64 `json:"improvement"` // 相对基线改善比（正=改善）
	Comparable  bool    `json:"comparable"`
	Improved    bool    `json:"improved"` // 改善幅度 ≥30%
	Sample      int     `json:"sample"`   // 本期分母（可比性/置信度提示）
	Note        string  `json:"note,omitempty"`
}

// PilotSection 一个试点的对照段（V0-01/V0-02/V0-03）。
type PilotSection struct {
	Key     string        `json:"key"`
	Title   string        `json:"title"`
	Pilot   string        `json:"pilot"`
	Metrics []PilotMetric `json:"metrics"`
	Met     bool          `json:"met"`
	Reasons []string      `json:"reasons,omitempty"`
}

// PilotWindow 窗口描述（回显口径，便于报告引用）。
type PilotWindow struct {
	Days  int    `json:"days"`
	Since string `json:"since"`
	Until string `json:"until"`
}

// PilotGate 放量门槛判定（§11.5）。
type PilotGate struct {
	Passed     bool     `json:"passed"`
	PilotsMet  int      `json:"pilotsMet"`
	Required   int      `json:"required"`
	Reasons    []string `json:"reasons"`
	Thresholds []string `json:"thresholds"`
}

// PilotReport 试点对照报告（§11 自动产出）。
type PilotReport struct {
	TenantID    int            `json:"tenantId"`
	Current     PilotWindow    `json:"current"`
	Baseline    PilotWindow    `json:"baseline"`
	GeneratedAt string         `json:"generatedAt"`
	Sections    []PilotSection `json:"sections"`
	Negative    []PilotMetric  `json:"negative"`
	Gate        PilotGate      `json:"gate"`
	Notes       []string       `json:"notes,omitempty"`
}

func clampPilotDays(days, fallback int) int {
	if days <= 0 {
		return fallback
	}
	if days > PilotReportMaxDays {
		return PilotReportMaxDays
	}
	return days
}

func newRateMetric(key, title, kind, direction string, currentNum, currentDen, baseNum, baseDen int, note string) PilotMetric {
	metric := PilotMetric{
		Key: key, Title: title, Unit: "rate", Kind: kind, Direction: direction,
		Sample: currentDen, Note: note,
	}
	if currentDen > 0 {
		metric.Current = float64(currentNum) / float64(currentDen)
	}
	if baseDen > 0 {
		metric.Baseline = float64(baseNum) / float64(baseDen)
	}
	metric.Delta = metric.Current - metric.Baseline
	metric.Comparable = currentDen > 0 && baseDen > 0 && metric.Baseline > 0
	if metric.Comparable {
		if direction == "up" {
			metric.Improvement = (metric.Current - metric.Baseline) / metric.Baseline
		} else {
			metric.Improvement = (metric.Baseline - metric.Current) / metric.Baseline
		}
		metric.Improved = metric.Improvement >= pilotImproveThreshold
	}
	return metric
}

func newLatencyMetric(key, title, kind string, currentP95, baseP95 float64, currentSamples, baseSamples int, note string) PilotMetric {
	metric := PilotMetric{
		Key: key, Title: title, Unit: "ms", Kind: kind, Direction: "down",
		Current: currentP95, Baseline: baseP95, Sample: currentSamples, Note: note,
	}
	metric.Delta = metric.Current - metric.Baseline
	metric.Comparable = currentSamples > 0 && baseSamples > 0 && baseP95 > 0 && currentP95 > 0
	if metric.Comparable {
		metric.Improvement = (metric.Baseline - metric.Current) / metric.Baseline
		metric.Improved = metric.Improvement >= pilotImproveThreshold
	}
	return metric
}

func metricByKey(metrics []PilotMetric, key string) (PilotMetric, bool) {
	for _, metric := range metrics {
		if metric.Key == key {
			return metric, true
		}
	}
	return PilotMetric{}, false
}

// buildPilotSections 组装三项试点对照段（纯函数，便于单测）。
func buildPilotSections(cur, base *PilotCounters) []PilotSection {
	inline := PilotSection{
		Key: "ticket_inline", Pilot: "V0-01", Title: "工单 Inline 处理（分诊/草稿/知识推荐）",
		Metrics: []PilotMetric{
			newRateMetric("inline_adoption_rate", "Inline 采纳率（preview→applied）", "primary", "up",
				cur.InlineApplied, cur.InlinePreview, base.InlineApplied, base.InlinePreview,
				"§11.5：相对基线改善 ≥30% 视为达标（省步数的等值业务指标）"),
			newRateMetric("inline_undo_rate", "Inline 撤销率（applied→undone）", "secondary", "down",
				cur.InlineUndone, cur.InlineApplied, base.InlineUndone, base.InlineApplied, ""),
		},
	}
	inline.Met, inline.Reasons = evaluateSectionMet(inline)

	selfService := PilotSection{
		Key: "self_service_create", Pilot: "V0-02", Title: "自助问答 + 一句话建单（员工侧）",
		Metrics: []PilotMetric{
			newRateMetric("self_service_resolve_rate", "自助解决率（resolved/answered）", "primary", "up",
				cur.SelfResolved, cur.SelfAnswered, base.SelfResolved, base.SelfAnswered, ""),
			newRateMetric("ai_create_field_correction_rate", "误分类率代理（字段纠正/建单完成）", "negative", "down",
				cur.FieldCorrected, cur.AICreateCompleted, base.FieldCorrected, base.AICreateCompleted,
				"无人工标注真值：以用户修改 AI 建议 type/priority 为代理"),
			newLatencyMetric("ai_create_latency_p95_ms", "建单时长 P95（generate→提交成功）", "secondary",
				cur.AICreateLatencyP95, base.AICreateLatencyP95, cur.AICreateCompleted, base.AICreateCompleted, ""),
		},
	}
	selfService.Met, selfService.Reasons = evaluateSectionMet(selfService)

	plan := PilotSection{
		Key: "change_impact_plan", Pilot: "V0-03", Title: "变更影响分析任务卡（plan 产物）",
		Metrics: []PilotMetric{
			newRateMetric("plan_adoption_rate", "计划采纳率（preview→applied）", "primary", "up",
				cur.PlanApplied, cur.PlanPreview, base.PlanApplied, base.PlanPreview, ""),
			newLatencyMetric("plan_latency_p95_ms", "分析耗时 P95（ms）", "secondary",
				cur.PlanLatencyP95, base.PlanLatencyP95, cur.PlanCompleted, base.PlanCompleted, ""),
		},
	}
	plan.Met, plan.Reasons = evaluateSectionMet(plan)

	return []PilotSection{inline, selfService, plan}
}

// evaluateSectionMet 段达标 = 所有 primary 可比且改善 ≥30%，且段内 negative 未恶化。
func evaluateSectionMet(section PilotSection) (bool, []string) {
	reasons := make([]string, 0, 4)
	primaryOK := true
	for _, metric := range section.Metrics {
		if metric.Kind != "primary" {
			continue
		}
		switch {
		case !metric.Comparable:
			primaryOK = false
			reasons = append(reasons, fmt.Sprintf("%s：样本不足或基线为 0，不可比（本期样本 %d）", metric.Title, metric.Sample))
		case !metric.Improved:
			primaryOK = false
			reasons = append(reasons, fmt.Sprintf("%s：改善 %.1f%%（低于 30%% 门槛）", metric.Title, metric.Improvement*100))
		}
	}
	for _, metric := range section.Metrics {
		if metric.Kind == "negative" && metric.Comparable && metric.Current > metric.Baseline*(1+pilotDeteriorationTolerance) {
			primaryOK = false
			reasons = append(reasons, fmt.Sprintf("%s：恶化（%.3f → %.3f）", metric.Title, metric.Baseline, metric.Current))
		}
	}
	if primaryOK && len(reasons) == 0 {
		reasons = append(reasons, "达标：主指标相对基线改善 ≥30% 且负向指标未恶化")
	}
	return primaryOK, reasons
}

// buildNegativeMetrics 全局负向指标（§11.5：不恶化）。
func buildNegativeMetrics(cur, base *PilotCounters) []PilotMetric {
	return []PilotMetric{
		newRateMetric("ai_create_field_correction_rate", "误分类率代理（字段纠正率）", "negative", "down",
			cur.FieldCorrected, cur.AICreateCompleted, base.FieldCorrected, base.AICreateCompleted,
			"无人工标注真值：字段纠正为代理"),
		newRateMetric("suggestion_ignore_rate", "建议忽略率（ignored/shown）", "negative", "down",
			cur.SuggestionIgnored, cur.SuggestionShown, base.SuggestionIgnored, base.SuggestionShown, ""),
		newRateMetric("proactive_dismiss_rate", "主动提示关闭率（dismissed/shown）", "negative", "down",
			cur.ProactiveDismissed, cur.ProactiveShown, base.ProactiveDismissed, base.ProactiveShown, ""),
		newRateMetric("task_failure_rate", "任务失败率（failed/started，转人工代理）", "negative", "down",
			cur.TaskFailed, cur.TaskStarted, base.TaskFailed, base.TaskStarted, ""),
	}
}

// BuildPilotReportFromCounters 由两次窗口计数生成报告（纯函数；报告生成的口径单一来源）。
func BuildPilotReportFromCounters(tenantID int, current, baseline PilotWindow, cur, base *PilotCounters, generatedAt time.Time) *PilotReport {
	if cur == nil {
		cur = &PilotCounters{}
	}
	if base == nil {
		base = &PilotCounters{}
	}
	sections := buildPilotSections(cur, base)
	negative := buildNegativeMetrics(cur, base)

	met := 0
	for _, section := range sections {
		if section.Met {
			met++
		}
	}
	deteriorated := make([]string, 0, len(negative))
	for _, metric := range negative {
		if metric.Comparable && metric.Current > metric.Baseline*(1+pilotDeteriorationTolerance) {
			deteriorated = append(deteriorated, fmt.Sprintf("%s（%.3f → %.3f）", metric.Title, metric.Baseline, metric.Current))
		}
	}

	gate := PilotGate{
		PilotsMet: met,
		Required:  pilotRequiredMet,
		Thresholds: []string{
			"§11.5：V0-01～V0-03 中 ≥2 项试点达到对照目标（相对基线改善 ≥30%）",
			fmt.Sprintf("§11.5：负向指标不恶化（实现容差 ≤+%d%%）", int(pilotDeteriorationTolerance*100)),
		},
	}
	switch {
	case met < pilotRequiredMet:
		gate.Reasons = append(gate.Reasons, fmt.Sprintf("达标试点 %d/3 < %d：未达放量门槛", met, pilotRequiredMet))
	case len(deteriorated) > 0:
		gate.Reasons = append(gate.Reasons, fmt.Sprintf("负向指标恶化：%s", joinReasons(deteriorated)))
	default:
		gate.Passed = true
		gate.Reasons = append(gate.Reasons, "达标：≥2 项试点改善 ≥30% 且负向指标未恶化，可按 §7 顺序放量 Dock")
	}
	if met >= pilotRequiredMet && len(deteriorated) > 0 {
		gate.Reasons = append(gate.Reasons, "试点达标数已满足，但负向指标恶化 → 门槛不放行")
	}

	report := &PilotReport{
		TenantID:    tenantID,
		Current:     current,
		Baseline:    baseline,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Sections:    sections,
		Negative:    negative,
		Gate:        gate,
	}
	total := cur.InlinePreview + cur.InlineApplied + cur.SelfAnswered + cur.AICreateCompleted + cur.PlanPreview + cur.TaskStarted
	if total == 0 {
		report.Notes = append(report.Notes,
			"本期窗口无埋点样本：请确认试点已上线且埋点已启用（§7：基线需试点前采集，不可回溯补采）")
	}
	return report
}

func joinReasons(items []string) string {
	out := ""
	for index, item := range items {
		if index > 0 {
			out += "；"
		}
		out += item
	}
	return out
}

// AggregatePilotCounters 单次 FILTER 聚合一个窗口的试点计数（租户前置；PG 语法）。
func (r *aiTelemetryRepository) AggregatePilotCounters(ctx context.Context, tenantID int, since, until time.Time) (*PilotCounters, error) {
	counters := &PilotCounters{}
	const query = `
	-- ai_pilot_counters
	SELECT
		COUNT(*) FILTER (WHERE name = 'inline_preview'),
		COUNT(*) FILTER (WHERE name = 'inline_applied'),
		COUNT(*) FILTER (WHERE name = 'inline_undone'),
		COUNT(*) FILTER (WHERE name = 'inline_preview' AND payload->>'toolName' = 'analyze_ci_impact_plan'),
		COUNT(*) FILTER (WHERE name = 'inline_applied' AND payload->>'toolName' = 'analyze_ci_impact_plan'),
		COUNT(*) FILTER (WHERE name = 'task_completed' AND payload->>'taskCardId' = 'impact_plan'),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FILTER (
			WHERE name = 'task_completed' AND payload->>'taskCardId' = 'impact_plan' AND latency_ms IS NOT NULL), 0),
		COUNT(*) FILTER (WHERE name = 'self_service_answered'),
		COUNT(*) FILTER (WHERE name = 'self_service_resolved'),
		COUNT(*) FILTER (WHERE name = 'self_service_ticket_created'),
		COUNT(*) FILTER (WHERE name = 'ai_create_field_corrected'),
		COUNT(*) FILTER (WHERE name = 'task_completed' AND payload->>'taskCardId' = 'ai_create_ticket'),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FILTER (
			WHERE name = 'task_completed' AND payload->>'taskCardId' = 'ai_create_ticket' AND latency_ms IS NOT NULL), 0),
		COUNT(*) FILTER (WHERE name = 'suggestion_shown'),
		COUNT(*) FILTER (WHERE name = 'suggestion_ignored'),
		COUNT(*) FILTER (WHERE name = 'proactive_shown'),
		COUNT(*) FILTER (WHERE name = 'proactive_dismissed'),
		COUNT(*) FILTER (WHERE name = 'error_shown'),
		COUNT(*) FILTER (WHERE name = 'error_recovered'),
		COUNT(*) FILTER (WHERE name = 'task_started'),
		COUNT(*) FILTER (WHERE name = 'task_failed')
	FROM ai_telemetry_events
	WHERE tenant_id = $1 AND occurred_at >= $2 AND occurred_at < $3
	`
	if err := r.db.QueryRowContext(ctx, query, tenantID, since, until).Scan(
		&counters.InlinePreview, &counters.InlineApplied, &counters.InlineUndone,
		&counters.PlanPreview, &counters.PlanApplied, &counters.PlanCompleted, &counters.PlanLatencyP95,
		&counters.SelfAnswered, &counters.SelfResolved, &counters.SelfTicketCreated,
		&counters.FieldCorrected, &counters.AICreateCompleted, &counters.AICreateLatencyP95,
		&counters.SuggestionShown, &counters.SuggestionIgnored,
		&counters.ProactiveShown, &counters.ProactiveDismissed,
		&counters.ErrorShown, &counters.ErrorRecovered,
		&counters.TaskStarted, &counters.TaskFailed,
	); err != nil {
		return nil, fmt.Errorf("aggregate pilot counters: %w", err)
	}
	return counters, nil
}

// BuildPilotReport 生成试点对照报告（本期 vs 基线窗口；§11.5 门槛）。
func (s *AITelemetryService) BuildPilotReport(ctx context.Context, tenantID int, q PilotReportQuery) (*PilotReport, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if tenantID <= 0 {
		return nil, errors.New("tenant id required")
	}
	currentDays := clampPilotDays(q.CurrentDays, PilotReportDefaultCurrentDays)
	baselineDays := clampPilotDays(q.BaselineDays, PilotReportDefaultBaselineDays)

	now := time.Now()
	currentSince := now.AddDate(0, 0, -currentDays)
	baselineUntil := currentSince
	baselineSince := baselineUntil.AddDate(0, 0, -baselineDays)

	current, err := s.repo.AggregatePilotCounters(ctx, tenantID, currentSince, now)
	if err != nil {
		return nil, err
	}
	baseline, err := s.repo.AggregatePilotCounters(ctx, tenantID, baselineSince, baselineUntil)
	if err != nil {
		return nil, err
	}

	currentWindow := PilotWindow{Days: currentDays, Since: currentSince.Format(time.RFC3339), Until: now.Format(time.RFC3339)}
	baselineWindow := PilotWindow{Days: baselineDays, Since: baselineSince.Format(time.RFC3339), Until: baselineUntil.Format(time.RFC3339)}
	return BuildPilotReportFromCounters(tenantID, currentWindow, baselineWindow, current, baseline, now), nil
}
