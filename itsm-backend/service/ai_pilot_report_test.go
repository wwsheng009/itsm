package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// V0-04 试点对照报告：口径纯函数 + sqlmock 聚合接线。

func TestBuildPilotReport_PassedWhenTwoPilotsImproved(t *testing.T) {
	generatedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	baseline := &PilotCounters{
		InlinePreview: 100, InlineApplied: 50, InlineUndone: 10,
		PlanPreview: 20, PlanApplied: 5, PlanCompleted: 20, PlanLatencyP95: 4000,
		SelfAnswered: 100, SelfResolved: 30, SelfTicketCreated: 50, FieldCorrected: 10, AICreateCompleted: 40, AICreateLatencyP95: 30000,
		SuggestionShown: 100, SuggestionIgnored: 30,
		ProactiveShown: 50, ProactiveDismissed: 10,
		TaskStarted: 100, TaskFailed: 10,
	}
	current := &PilotCounters{
		// V0-01：采纳率 50% → 80%（+60%）；撤销率 20% → 10%（改善 50%）。
		InlinePreview: 100, InlineApplied: 80, InlineUndone: 8,
		// V0-02：解决率 30% → 60%（+100%）；字段纠正率 25% → 5%（改善 80%）；时长 30s → 12s（改善 60%）。
		SelfAnswered: 100, SelfResolved: 60, SelfTicketCreated: 35, FieldCorrected: 2, AICreateCompleted: 40, AICreateLatencyP95: 12000,
		// V0-03：采纳率 25% → 50%（+100%）；时长 4s → 2s（改善 50%）。
		PlanPreview: 20, PlanApplied: 10, PlanCompleted: 20, PlanLatencyP95: 2000,
		// 负向：忽略率 30% → 20%（改善），关闭率 20% → 10%，失败率 10% → 5%。
		SuggestionShown: 100, SuggestionIgnored: 20,
		ProactiveShown: 50, ProactiveDismissed: 5,
		TaskStarted: 100, TaskFailed: 5,
	}

	report := BuildPilotReportFromCounters(1,
		PilotWindow{Days: 7, Since: "2026-10-11T12:00:00Z", Until: "2026-10-18T12:00:00Z"},
		PilotWindow{Days: 7, Since: "2026-10-04T12:00:00Z", Until: "2026-10-11T12:00:00Z"},
		current, baseline, generatedAt)

	require.Len(t, report.Sections, 3)
	for _, section := range report.Sections {
		assert.True(t, section.Met, "%s 应达标：%v", section.Key, section.Reasons)
	}
	assert.Equal(t, 3, report.Gate.PilotsMet)
	assert.True(t, report.Gate.Passed, "≥2 项达标且负向未恶化 → 放行")
	assert.Empty(t, report.Notes, "有样本 → 无样本不足提示")

	// 指标口径：采纳率 0.5 → 0.8 = +60%（≥30%），样本=本期分母。
	inlineApplied, ok := metricByKey(report.Sections[0].Metrics, "inline_adoption_rate")
	require.True(t, ok)
	assert.InDelta(t, 0.5, inlineApplied.Baseline, 1e-9)
	assert.InDelta(t, 0.8, inlineApplied.Current, 1e-9)
	assert.InDelta(t, 0.6, inlineApplied.Improvement, 1e-9)
	assert.True(t, inlineApplied.Improved)
	assert.Equal(t, 100, inlineApplied.Sample)

	// 时延指标：down 方向改善。
	planLatency, ok := metricByKey(report.Sections[2].Metrics, "plan_latency_p95_ms")
	require.True(t, ok)
	assert.InDelta(t, 0.5, planLatency.Improvement, 1e-9)
	assert.True(t, planLatency.Improved)
}

func TestBuildPilotReport_BlockedOnNoDataAndNegativeDeterioration(t *testing.T) {
	generatedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	// 无样本：全部不可比 → 门槛不放行 + 提示采集要求。
	empty := BuildPilotReportFromCounters(1, PilotWindow{Days: 7}, PilotWindow{Days: 7},
		&PilotCounters{}, &PilotCounters{}, generatedAt)
	assert.False(t, empty.Gate.Passed)
	assert.Equal(t, 0, empty.Gate.PilotsMet)
	assert.NotEmpty(t, empty.Notes, "无样本必须有显式提示（基线不可回溯补采）")
	for _, section := range empty.Sections {
		assert.False(t, section.Met)
		assert.NotEmpty(t, section.Reasons)
	}

	// 试点达标 ≥2，但建议忽略率从 10% 恶化到 40% → 门槛拦截。
	baseline := &PilotCounters{
		InlinePreview: 100, InlineApplied: 50, InlineUndone: 10,
		SelfAnswered: 100, SelfResolved: 30, FieldCorrected: 10, AICreateCompleted: 40,
		SuggestionShown: 100, SuggestionIgnored: 10,
		TaskStarted: 100, TaskFailed: 5,
	}
	current := &PilotCounters{
		InlinePreview: 100, InlineApplied: 80, InlineUndone: 5,
		SelfAnswered: 100, SelfResolved: 60, FieldCorrected: 2, AICreateCompleted: 40,
		SuggestionShown: 100, SuggestionIgnored: 40,
		TaskStarted: 100, TaskFailed: 5,
	}
	report := BuildPilotReportFromCounters(1, PilotWindow{Days: 7}, PilotWindow{Days: 7}, current, baseline, generatedAt)
	assert.GreaterOrEqual(t, report.Gate.PilotsMet, 2)
	assert.False(t, report.Gate.Passed, "负向指标恶化必须拦截")
	assert.Contains(t, joinReasons(report.Gate.Reasons), "负向指标恶化")

	// 容差边界：+10% 以内不算恶化（实现口径）。
	toleranceBaseline := &PilotCounters{InlinePreview: 100, InlineApplied: 80, SuggestionShown: 100, SuggestionIgnored: 10, TaskStarted: 100, TaskFailed: 0}
	toleranceCurrent := &PilotCounters{InlinePreview: 100, InlineApplied: 80, SuggestionShown: 100, SuggestionIgnored: 11, TaskStarted: 100, TaskFailed: 0}
	tolerance := BuildPilotReportFromCounters(1, PilotWindow{Days: 7}, PilotWindow{Days: 7}, toleranceCurrent, toleranceBaseline, generatedAt)
	for _, metric := range tolerance.Negative {
		if metric.Key == "suggestion_ignore_rate" {
			assert.False(t, metric.Current > metric.Baseline*(1+pilotDeteriorationTolerance), "10% 内视为不恶化")
		}
	}
}

func TestBuildPilotReport_ClampsAndQueriesWindows(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	columns := []string{
		"inline_preview", "inline_applied", "inline_undone",
		"plan_preview", "plan_applied", "plan_completed", "plan_p95",
		"self_answered", "self_resolved", "self_ticket_created",
		"field_corrected", "ai_create_completed", "ai_create_p95",
		"suggestion_shown", "suggestion_ignored",
		"proactive_shown", "proactive_dismissed",
		"error_shown", "error_recovered", "task_started", "task_failed",
	}
	expectCounters := func(rows []driver.Value) {
		mock.ExpectQuery(regexp.QuoteMeta("-- ai_pilot_counters")).
			WithArgs(int64(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(columns).AddRow(rows...))
	}
	// 本期（改善）与基线（对照）。
	expectCounters([]driver.Value{
		100, 80, 8, 20, 10, 20, 2000.0,
		100, 60, 35, 2, 40, 12000.0,
		100, 20, 50, 5, 10, 9, 100, 5,
	})
	expectCounters([]driver.Value{
		100, 50, 10, 20, 5, 20, 4000.0,
		100, 30, 50, 10, 40, 30000.0,
		100, 30, 50, 10, 10, 9, 100, 10,
	})

	report, err := svc.BuildPilotReport(context.Background(), 1, PilotReportQuery{CurrentDays: 999, BaselineDays: 7})
	require.NoError(t, err)
	assert.Equal(t, PilotReportMaxDays, report.Current.Days, "窗口上限 clamp")
	assert.Equal(t, 7, report.Baseline.Days)
	assert.Equal(t, 1, report.TenantID)
	require.Len(t, report.Sections, 3)
	assert.Equal(t, 3, report.Gate.PilotsMet)
	assert.True(t, report.Gate.Passed)
	assert.NotEmpty(t, report.Gate.Thresholds)
	assert.NoError(t, mock.ExpectationsWereMet())

	// 未装配 → 不可用错误；非法租户 → 明确错误。
	nilSvc := NewAITelemetryService(nil)
	_, err = nilSvc.BuildPilotReport(context.Background(), 1, PilotReportQuery{})
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))

	_, err = svc.BuildPilotReport(context.Background(), 0, PilotReportQuery{})
	require.Error(t, err)
}
