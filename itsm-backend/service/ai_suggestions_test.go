package service

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O2-01 分析增强：建议反馈权重聚合 + 分群对比（sqlmock + 纯比率口径）。

func TestGetSuggestionWeights_Aggregates(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	mock.ExpectQuery(regexp.QuoteMeta("-- ai_suggestion_weights")).
		WithArgs(int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"card_id", "name", "age_days", "count"}).
			AddRow("attention_today", "suggestion_shown", 0, 10).
			AddRow("attention_today", "suggestion_shown", 14, 20).
			AddRow("attention_today", "suggestion_clicked", 1, 5).
			AddRow("attention_today", "suggestion_ignored", 3, 3).
			AddRow("triage_plan", "suggestion_shown", 0, 20).
			AddRow("triage_plan", "suggestion_clicked", 0, 1).
			AddRow("triage_plan", "suggestion_ignored", 0, 9))

	weights, err := svc.GetSuggestionWeights(context.Background(), 7, 30)
	require.NoError(t, err)
	require.Len(t, weights, 2)

	// 原始计数 = 桶求和；衰减有效计数 = Σ count × 0.5^(桶中点年龄/14)。
	attention := weights["attention_today"]
	assert.Equal(t, 30, attention.Shown)
	assert.Equal(t, 5, attention.Clicked)
	assert.Equal(t, 3, attention.Ignored)
	assert.InDelta(t, 19.511, attention.DecayedShown, 1e-9)
	assert.InDelta(t, 4.6421, attention.DecayedClicked, 1e-9)
	assert.InDelta(t, 2.5227, attention.DecayedIgnored, 1e-9)

	// 同日（age=0）桶的衰减因子 = 0.5^(0.5/14) ≈ 0.9755。
	triage := weights["triage_plan"]
	assert.Equal(t, 20, triage.Shown)
	assert.Equal(t, 1, triage.Clicked)
	assert.Equal(t, 9, triage.Ignored)
	assert.InDelta(t, 19.511, triage.DecayedShown, 1e-9)
	assert.InDelta(t, 0.9755, triage.DecayedClicked, 1e-9)
	assert.InDelta(t, 8.7799, triage.DecayedIgnored, 1e-9)
	assert.NoError(t, mock.ExpectationsWereMet())

	// 未装配/非法租户 → 明确错误。
	_, err = NewAITelemetryService(nil).GetSuggestionWeights(context.Background(), 1, 30)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))
	_, err = svc.GetSuggestionWeights(context.Background(), 0, 30)
	require.Error(t, err)
}

// 半衰期口径（纯函数）：桶中点计龄、单调衰减、未来时间戳不放大权重。
func TestDecayedSuggestionCount_HalfLife(t *testing.T) {
	// 今天（桶中点 0.5 天）的权重略低于 1；越旧越小（半衰期 14 天）。
	assert.InDelta(t, 0.9755, decayedSuggestionCount(1, 0), 1e-3)
	assert.Greater(t, decayedSuggestionCount(1, 0), decayedSuggestionCount(1, 7))
	assert.Greater(t, decayedSuggestionCount(1, 7), decayedSuggestionCount(1, 13))
	// 一个半衰期（14 天）后 ≈ 一半（桶中点口径）。
	assert.InDelta(t, 0.4878, decayedSuggestionCount(1, 14), 1e-3)
	// 未来时间戳（时钟偏差）按 0 计龄：不放大（0.5^0 = 1）。
	assert.Equal(t, 1.0, decayedSuggestionCount(1, -1))
	assert.Equal(t, 1.0, decayedSuggestionCount(1, -3))
}

func TestGetAssistantSegments_BuildsRatesAndClamps(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	mock.ExpectQuery(regexp.QuoteMeta("-- ai_segment_breakdown")).
		WithArgs(int64(7), int64(AssistantSegmentsMaxDays)).
		WillReturnRows(sqlmock.NewRows([]string{
			"segment", "events", "sessions",
			"suggestion_shown", "suggestion_clicked",
			"task_started", "task_completed",
			"inline_preview", "inline_applied", "errors",
		}).
			AddRow("agent", 200, 45, 40, 12, 10, 6, 20, 9, 4).
			AddRow("super_admin", 50, 8, 10, 2, 4, 4, 0, 0, 0))

	report, err := svc.GetAssistantSegments(context.Background(), 7, 999, AssistantSegmentDimensionRole)
	require.NoError(t, err)
	assert.Equal(t, AssistantSegmentsMaxDays, report.Days, "窗口上限 clamp")
	assert.Equal(t, AssistantSegmentDimensionRole, report.Dimension)
	require.Len(t, report.Segments, 2)

	agent := report.Segments[0]
	assert.Equal(t, "agent", agent.Key)
	assert.InDelta(t, 0.3, agent.SuggestionCtr, 1e-9)
	assert.InDelta(t, 0.6, agent.TaskCompletionRate, 1e-9)
	assert.InDelta(t, 0.45, agent.InlineAdoptionRate, 1e-9)
	assert.InDelta(t, 0.02, agent.ErrorRate, 1e-9)

	// 分母为 0 → 0（不产生 NaN）。
	adminSegment := report.Segments[1]
	assert.InDelta(t, 0.2, adminSegment.SuggestionCtr, 1e-9)
	assert.InDelta(t, 1.0, adminSegment.TaskCompletionRate, 1e-9)
	assert.Zero(t, adminSegment.InlineAdoptionRate)
	assert.Zero(t, adminSegment.ErrorRate)
	assert.NoError(t, mock.ExpectationsWereMet())

	// pageKey 维度别名归一 + 非法维度 → 明确错误（触库前拒绝）。
	mock.ExpectQuery(regexp.QuoteMeta("-- ai_segment_breakdown")).
		WithArgs(int64(7), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"segment", "events", "sessions",
			"suggestion_shown", "suggestion_clicked",
			"task_started", "task_completed",
			"inline_preview", "inline_applied", "errors",
		}).AddRow("ticket.detail", 30, 5, 1, 1, 1, 1, 0, 0, 0))
	byPage, err := svc.GetAssistantSegments(context.Background(), 7, 7, "page_key")
	require.NoError(t, err)
	assert.Equal(t, AssistantSegmentDimensionPageKey, byPage.Dimension)
	assert.Equal(t, "ticket.detail", byPage.Segments[0].Key)

	_, err = svc.GetAssistantSegments(context.Background(), 7, 7, "tenant")
	assert.True(t, errors.Is(err, ErrAssistantSegmentsDimension))

	// 空报告 → 显式说明。
	mock.ExpectQuery(regexp.QuoteMeta("-- ai_segment_breakdown")).
		WithArgs(int64(7), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"segment", "events", "sessions",
			"suggestion_shown", "suggestion_clicked",
			"task_started", "task_completed",
			"inline_preview", "inline_applied", "errors",
		}))
	empty, err := svc.GetAssistantSegments(context.Background(), 7, 7, AssistantSegmentDimensionRole)
	require.NoError(t, err)
	assert.Empty(t, empty.Segments)
	assert.NotEmpty(t, empty.Notes)
}

func TestBuildAssistantSegments_ZeroDenominator(t *testing.T) {
	out := buildAssistantSegments([]AssistantSegment{{Key: "empty"}})
	require.Len(t, out, 1)
	assert.Zero(t, out[0].SuggestionCtr)
	assert.Zero(t, out[0].TaskCompletionRate)
	assert.Zero(t, out[0].InlineAdoptionRate)
	assert.Zero(t, out[0].ErrorRate)
}
