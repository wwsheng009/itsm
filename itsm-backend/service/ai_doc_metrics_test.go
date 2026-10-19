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

// P2 文档命中率/解决率运营报表：聚合口径（比率/按页/趋势）与降级。

func TestGetAssistantDocMetrics_NilRepo(t *testing.T) {
	svc := NewAITelemetryService(nil)
	_, err := svc.GetAssistantDocMetrics(context.Background(), 1, 30)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))
}

func TestGetAssistantDocMetrics_ClampsDaysAndNilRates(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)
	empty := sqlmock.NewRows([]string{
		"opened", "gaps", "drafts", "published", "helpful", "unhelpful",
		"handoffs", "opened_sessions", "gap_sessions", "handoff_sessions", "events",
	}).AddRow(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(CASE WHEN name = 'help_opened'")).
		WithArgs(int64(1), int64(90)).
		WillReturnRows(empty)
	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
		WithArgs(int64(1), int64(90), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}))
	mock.ExpectQuery(regexp.QuoteMeta("date_trunc")).
		WithArgs(int64(1), int64(90)).
		WillReturnRows(sqlmock.NewRows([]string{"day", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}))

	// days 超上限 → 钳制到 90；零分母 → 比率为 null（不虚报 0%）；无起草动作 → 起草落地率 null。
	report, err := svc.GetAssistantDocMetrics(context.Background(), 1, 500)
	require.NoError(t, err)
	assert.Equal(t, 90, report.Days)
	assert.Nil(t, report.Rates.HitRate)
	assert.Nil(t, report.Rates.ResolveRate)
	assert.Nil(t, report.Rates.FeedbackResolveRate)
	assert.Nil(t, report.Rates.DraftPublishRate)
	assert.Empty(t, report.ByPage)
	assert.Empty(t, report.Trend)
	assert.NotEmpty(t, report.GeneratedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantDocMetrics_AggregatesRatesAndBuckets(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(CASE WHEN name = 'help_opened'")).
		WithArgs(int64(7), int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{
			"opened", "gaps", "drafts", "published", "helpful", "unhelpful",
			"handoffs", "opened_sessions", "gap_sessions", "handoff_sessions", "events",
		}).AddRow(10, 2, 3, 2, 3, 1, 1, 8, 1, 1, 17))

	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
		WithArgs(int64(7), int64(30), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}).
			AddRow("ticket.list", 4, 2, 2, 1, 2, 0).
			AddRow("dashboard", 3, 0, 1, 1, 1, 1))

	mock.ExpectQuery(regexp.QuoteMeta("date_trunc")).
		WithArgs(int64(7), int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"day", "opened", "gaps", "drafts", "published", "helpful", "unhelpful"}).
			AddRow("2026-10-17", 6, 1, 1, 0, 2, 0).
			AddRow("2026-10-18", 4, 1, 2, 2, 1, 1))

	report, err := svc.GetAssistantDocMetrics(context.Background(), 7, 30)
	require.NoError(t, err)
	assert.Equal(t, 30, report.Days)
	assert.Equal(t, AssistantDocMetricsTotals{
		Opened: 10, Gaps: 2, Drafts: 3, Published: 2,
		FeedbackHelpful: 3, FeedbackUnhelpful: 1, Handoffs: 1,
		OpenedSessions: 8, GapSessions: 1, HandoffSessions: 1, Events: 17,
	}, report.Totals)

	require.NotNil(t, report.Rates.HitRate)
	assert.InDelta(t, 0.875, *report.Rates.HitRate, 0.0001) // 1 - 1/8
	require.NotNil(t, report.Rates.ResolveRate)
	assert.InDelta(t, 0.875, *report.Rates.ResolveRate, 0.0001) // 1 - 1/8（未升级人工率）
	require.NotNil(t, report.Rates.FeedbackResolveRate)
	assert.InDelta(t, 0.75, *report.Rates.FeedbackResolveRate, 0.0001) // 3/(3+1)
	require.NotNil(t, report.Rates.DraftPublishRate)
	assert.InDelta(t, 2.0/3.0, *report.Rates.DraftPublishRate, 0.0001) // 草稿→成文 2/3

	require.Len(t, report.ByPage, 2)
	assert.Equal(t, AssistantDocMetricsPage{PageKey: "ticket.list", Opened: 4, Gaps: 2, Drafts: 2, Published: 1, Helpful: 2, Unhelpful: 0}, report.ByPage[0])
	assert.Equal(t, AssistantDocMetricsPage{PageKey: "dashboard", Opened: 3, Gaps: 0, Drafts: 1, Published: 1, Helpful: 1, Unhelpful: 1}, report.ByPage[1])

	require.Len(t, report.Trend, 2)
	assert.Equal(t, AssistantDocMetricsTrendPoint{Day: "2026-10-17", Opened: 6, Gaps: 1, Drafts: 1, Published: 0, Helpful: 2, Unhelpful: 0}, report.Trend[0])
	assert.Equal(t, AssistantDocMetricsTrendPoint{Day: "2026-10-18", Opened: 4, Gaps: 1, Drafts: 2, Published: 2, Helpful: 1, Unhelpful: 1}, report.Trend[1])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRatioOrNil(t *testing.T) {
	assert.Nil(t, ratioOrNil(1, 0))
	assert.Nil(t, ratioOrNil(0, 0))
	require.NotNil(t, ratioOrNil(0, 4))
	assert.InDelta(t, 0, *ratioOrNil(0, 4), 0.0001)
	require.NotNil(t, ratioOrNil(3, 4))
	assert.InDelta(t, 0.75, *ratioOrNil(3, 4), 0.0001)
	// 越界钳制（计数器异常时不产生 >1 或负比率）。
	require.NotNil(t, ratioOrNil(5, 4))
	assert.InDelta(t, 1, *ratioOrNil(5, 4), 0.0001)
	require.NotNil(t, ratioOrNil(-1, 4))
	assert.InDelta(t, 0, *ratioOrNil(-1, 4), 0.0001)
}
