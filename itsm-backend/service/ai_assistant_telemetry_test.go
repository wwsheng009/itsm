package service

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O0-02 埋点仓储/服务单测（sqlmock，不依赖真实数据库）。

func newAssistantTelemetryService(t *testing.T) (*AITelemetryService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewAITelemetryService(db), mock
}

func TestIngestAssistantEvents_NilRepo(t *testing.T) {
	svc := NewAITelemetryService(nil)
	_, err := svc.IngestAssistantEvents(context.Background(), 1, 9, []AssistantTelemetryEvent{{Name: "assistant_opened"}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))

	_, err = svc.GetAssistantTelemetrySummary(context.Background(), 1, 7, AssistantTelemetryFilter{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))
}

func TestIngestAssistantEvents_BatchTooLarge(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)
	events := make([]AssistantTelemetryEvent, AssistantTelemetryMaxBatchSize+1)
	for i := range events {
		events[i] = AssistantTelemetryEvent{Name: "message_sent"}
	}

	_, err := svc.IngestAssistantEvents(context.Background(), 1, 9, events)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryBatchTooLarge))
	// 超限必须在触库前拒绝
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIngestAssistantEvents_Insert(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	conversationID := 42
	latency := int64(180)
	occurred := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	events := []AssistantTelemetryEvent{
		{
			Name:         "assistant_opened",
			SessionID:    "sid-1",
			PageKey:      "ticket.detail",
			Route:        "/tickets/1",
			Entrypoint:   "ticket_detail",
			Role:         "agent",
			Model:        "doubao-pro",
			QueryHash:    "q-open",
			SourcesCount: nil,
			Feedback:     "",
			OccurredAt:   occurred,
			Payload:      map[string]interface{}{"name": "assistant_opened", "source": "fab"},
		},
		{
			Name:           "message_sent",
			SessionID:      "sid-1",
			ConversationID: &conversationID,
			RunID:          "run-7",
			LatencyMs:      &latency,
			Model:          "doubao-pro",
			QueryHash:      "q-answer",
			SourcesCount:   nil,
			Feedback:       "unhelpful",
			OccurredAt:     occurred,
			Payload:        map[string]interface{}{"name": "message_sent"},
		},
	}

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO ai_telemetry_events")).
		WithArgs(
			int64(1), int64(9), "sid-1", "assistant_opened", "ticket.detail", "/tickets/1", "ticket_detail",
			"agent",
			nil, "", nil, "", sqlmock.AnyArg(), occurred,
			"doubao-pro", "q-open", nil, "",
			int64(1), int64(9), "sid-1", "message_sent", "", "", "",
			"",
			int64(42), "run-7", int64(180), "", sqlmock.AnyArg(), occurred,
			"doubao-pro", "q-answer", nil, "unhelpful",
		).
		WillReturnResult(sqlmock.NewResult(0, 2))

	accepted, err := svc.IngestAssistantEvents(context.Background(), 1, 9, events)
	require.NoError(t, err)
	assert.Equal(t, 2, accepted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIngestAssistantEvents_EmptyBatch(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)
	accepted, err := svc.IngestAssistantEvents(context.Background(), 1, 9, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, accepted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantTelemetrySummary_Aggregates(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"count", "sessions", "errors"}).AddRow(10, 4, 2))

	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY name")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"name", "count"}).
			AddRow("assistant_opened", 6).
			AddRow("message_sent", 4))

	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "count"}).
			AddRow("ticket.detail", 5))

	mock.ExpectQuery(regexp.QuoteMeta("percentile_cont(0.95)")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"avg", "p95", "max"}).AddRow(120.5, 250.0, 800))

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(*) FILTER")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"answers", "cited", "feedback", "downvotes"}).
			AddRow(10, 6, 4, 1))

	mock.ExpectQuery(regexp.QuoteMeta("GROUP BY model")).
		WithArgs(int64(1), int64(30), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"model", "count"}).
			AddRow("doubao-pro", 6).
			AddRow("gpt-4o", 4))

	summary, err := svc.GetAssistantTelemetrySummary(context.Background(), 1, 30, AssistantTelemetryFilter{})
	require.NoError(t, err)
	assert.Equal(t, 30, summary.Days)
	assert.Equal(t, 10, summary.TotalEvents)
	assert.Equal(t, 4, summary.DistinctSessions)
	assert.Equal(t, 2, summary.ErrorEvents)
	require.Len(t, summary.ByName, 2)
	assert.Equal(t, AssistantTelemetryBucket{Key: "assistant_opened", Count: 6}, summary.ByName[0])
	require.Len(t, summary.ByPage, 1)
	assert.Equal(t, "ticket.detail", summary.ByPage[0].Key)
	require.NotNil(t, summary.LatencyMs)
	assert.Equal(t, 120.5, summary.LatencyMs.Avg)
	assert.Equal(t, 250.0, summary.LatencyMs.P95)
	assert.Equal(t, int64(800), summary.LatencyMs.Max)
	require.NotNil(t, summary.Quality)
	assert.Equal(t, 10, summary.Quality.AnswerCount)
	assert.Equal(t, 6, summary.Quality.CitationAnswerCount)
	assert.Equal(t, 4, summary.Quality.FeedbackCount)
	assert.Equal(t, 1, summary.Quality.DownvoteCount)
	require.NotNil(t, summary.Quality.CitationCoverage)
	assert.InDelta(t, 0.6, *summary.Quality.CitationCoverage, 1e-9)
	require.NotNil(t, summary.Quality.NoCitationRate)
	assert.InDelta(t, 0.4, *summary.Quality.NoCitationRate, 1e-9)
	require.NotNil(t, summary.Quality.DownvoteRate)
	assert.InDelta(t, 0.25, *summary.Quality.DownvoteRate, 1e-9)
	require.Len(t, summary.Models, 2)
	assert.Equal(t, AssistantTelemetryBucket{Key: "doubao-pro", Count: 6}, summary.Models[0])
	assert.NotEmpty(t, summary.GeneratedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantTelemetrySummary_ClampsDays(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	expectSummaryQueries := func(days int64) {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"count", "sessions", "errors"}).AddRow(0, 0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("GROUP BY name")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"name", "count"}))
		mock.ExpectQuery(regexp.QuoteMeta("GROUP BY page_key")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"page_key", "count"}))
		mock.ExpectQuery(regexp.QuoteMeta("percentile_cont(0.95)")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"avg", "p95", "max"}).AddRow(0, 0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("COUNT(*) FILTER")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"answers", "cited", "feedback", "downvotes"}).
				AddRow(0, 0, 0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("GROUP BY model")).
			WithArgs(int64(1), days, "", "").
			WillReturnRows(sqlmock.NewRows([]string{"model", "count"}))
	}

	// 超上限 → 90；0/负数 → 7
	expectSummaryQueries(90)
	summary, err := svc.GetAssistantTelemetrySummary(context.Background(), 1, 500, AssistantTelemetryFilter{})
	require.NoError(t, err)
	assert.Equal(t, 90, summary.Days)
	require.NotNil(t, summary.Quality)
	assert.Nil(t, summary.Quality.CitationCoverage)
	assert.Nil(t, summary.Quality.NoCitationRate)
	assert.Nil(t, summary.Quality.DownvoteRate)

	expectSummaryQueries(7)
	zeroDays, err := svc.GetAssistantTelemetrySummary(context.Background(), 1, 0, AssistantTelemetryFilter{})
	require.NoError(t, err)
	assert.Equal(t, 7, zeroDays.Days)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantTelemetrySummary_AppliesFilters(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)

	expect := func(match string) *sqlmock.ExpectedQuery {
		return mock.ExpectQuery(regexp.QuoteMeta(match)).
			WithArgs(int64(1), int64(7), "ticket.detail", "agent")
	}
	expect("SELECT COUNT(*)").
		WillReturnRows(sqlmock.NewRows([]string{"count", "sessions", "errors"}).AddRow(3, 1, 0))
	expect("GROUP BY name").
		WillReturnRows(sqlmock.NewRows([]string{"name", "count"}).AddRow("assistant_opened", 3))
	expect("GROUP BY page_key").
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "count"}).AddRow("ticket.detail", 3))
	expect("percentile_cont(0.95)").
		WillReturnRows(sqlmock.NewRows([]string{"avg", "p95", "max"}).AddRow(10.0, 20.0, 30))
	expect("COUNT(*) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"answers", "cited", "feedback", "downvotes"}).
			AddRow(3, 2, 1, 0))
	expect("GROUP BY model").
		WillReturnRows(sqlmock.NewRows([]string{"model", "count"}).AddRow("doubao-pro", 3))

	// 过滤值前后空白由服务层归一化后下发（防注入式拼接另由参数化保证）。
	summary, err := svc.GetAssistantTelemetrySummary(
		context.Background(), 1, 7, AssistantTelemetryFilter{PageKey: " ticket.detail ", Role: "agent"},
	)
	require.NoError(t, err)
	assert.Equal(t, 3, summary.TotalEvents)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// H1-01：文档缺口聚类聚合（help_gap / doc_draft_created）——默认值/上限钳制与扫描口径。

func TestGetAssistantHelpGapClusters_ClampsAndQueries(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)
	occurred := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM ai_telemetry_events").
		WithArgs(int64(1), int64(90), int64(200)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "gap_count", "draft_count", "last_seen"}).
			AddRow("knowledge.list", 4, 1, occurred).
			AddRow("dashboard", 2, 0, nil))

	clusters, err := svc.GetAssistantHelpGapClusters(context.Background(), 1, 1000, 500)
	require.NoError(t, err)
	require.Len(t, clusters, 2)
	assert.Equal(t, "knowledge.list", clusters[0].PageKey)
	assert.Equal(t, 4, clusters[0].GapCount)
	assert.Equal(t, 1, clusters[0].DraftCount)
	assert.Equal(t, "2026-10-18T02:00:00Z", clusters[0].LastSeen)
	assert.Equal(t, "dashboard", clusters[1].PageKey)
	assert.Empty(t, clusters[1].LastSeen, "无有效时间时 LastSeen 省略")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantHelpGapClusters_Defaults(t *testing.T) {
	svc, mock := newAssistantTelemetryService(t)
	mock.ExpectQuery("FROM ai_telemetry_events").
		WithArgs(int64(1), int64(30), int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "gap_count", "draft_count", "last_seen"}))

	clusters, err := svc.GetAssistantHelpGapClusters(context.Background(), 1, 0, 0)
	require.NoError(t, err)
	assert.Empty(t, clusters)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAssistantHelpGapClusters_Unavailable(t *testing.T) {
	_, err := NewAITelemetryService(nil).GetAssistantHelpGapClusters(context.Background(), 1, 30, 50)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAssistantTelemetryUnavailable))
}
