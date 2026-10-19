package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// O0-02/O0-03 AI Copilot 埋点事件（服务端侧）。
//
// 契约：docs/plan/ai-assistant-dock-plan-2026-10-18.md §13.2（事件字典）/ §13.7（落地组件）。
// 数据落 `ai_telemetry_events`（migrations/20261018_ai_telemetry_events.sql + _filters.sql，租户 RLS）。
// 说明：事件名白名单与载荷校验在 handler 层完成；本层只做批量写与聚合，保持仓储单一职责。

const (
	// AssistantTelemetryMaxBatchSize 是单批事件上限（超出由调用方拒绝）。
	AssistantTelemetryMaxBatchSize = 100
	// assistantTelemetryMaxLookbackDays 聚合回溯上限（与 /ai/metrics 的 90 天口径一致）。
	assistantTelemetryMaxLookbackDays = 90
)

var (
	// ErrAssistantTelemetryUnavailable 仓储未装配（db=nil 的测试/降级态）。
	ErrAssistantTelemetryUnavailable = errors.New("assistant telemetry repository not initialized")
	// ErrAssistantTelemetryBatchTooLarge 单批事件超过上限。
	ErrAssistantTelemetryBatchTooLarge = errors.New("assistant telemetry batch too large")
)

// AssistantTelemetryEvent 是一条待落库的埋点事件（已由前端脱敏）。
type AssistantTelemetryEvent struct {
	Name       string
	SessionID  string
	PageKey    string
	Route      string
	Entrypoint string
	// Role 是请求上下文的角色（由服务端 RBAC 中间件提供，非前端上报）：O0-03 角色维度过滤。
	Role           string
	ConversationID *int
	RunID          string
	LatencyMs      *int64
	ErrorCode      string
	OccurredAt     time.Time
	Payload        map[string]interface{}
}

// AssistantTelemetryFilter 是聚合过滤条件（看板 O0-03 的"页面/角色"维度；租户为隐式条件）。
type AssistantTelemetryFilter struct {
	PageKey string
	Role    string
}

// AssistantTelemetryBucket 是聚合维度的一个桶。
type AssistantTelemetryBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// AssistantTelemetryLatency 是时延分位统计（毫秒）。
type AssistantTelemetryLatency struct {
	Avg float64 `json:"avg"`
	P95 float64 `json:"p95"`
	Max int64   `json:"max"`
}

// AssistantTelemetrySummary 是观测看板（O0-03）消费的聚合口径。
type AssistantTelemetrySummary struct {
	Days             int                        `json:"days"`
	TotalEvents      int                        `json:"totalEvents"`
	DistinctSessions int                        `json:"distinctSessions"`
	ErrorEvents      int                        `json:"errorEvents"`
	ByName           []AssistantTelemetryBucket `json:"byName"`
	ByPage           []AssistantTelemetryBucket `json:"byPage"`
	LatencyMs        *AssistantTelemetryLatency `json:"latencyMs,omitempty"`
	GeneratedAt      string                     `json:"generatedAt"`
}

// AssistantHelpGapCluster 是「文档缺口」事件按页面的聚类（H1-01 文档待办队列数据源）。
//
// 数据口径：events ∈ {help_gap, doc_draft_created}，按 page_key 聚合；
// LastSeen 为该页最近一次缺口事件时间（RFC3339 UTC；无有效时间时省略）。
type AssistantHelpGapCluster struct {
	PageKey    string `json:"pageKey"`
	GapCount   int    `json:"gapCount"`
	DraftCount int    `json:"draftCount"`
	LastSeen   string `json:"lastSeen,omitempty"`
}

// IngestAssistantEvents 批量写入埋点事件（单条多值 INSERT，避免 N 次往返）。
func (s *AITelemetryService) IngestAssistantEvents(ctx context.Context, tenantID, userID int, events []AssistantTelemetryEvent) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrAssistantTelemetryUnavailable
	}
	if len(events) > AssistantTelemetryMaxBatchSize {
		return 0, ErrAssistantTelemetryBatchTooLarge
	}
	if len(events) == 0 {
		return 0, nil
	}
	return s.repo.InsertAssistantEvents(ctx, tenantID, userID, events)
}

// GetAssistantTelemetrySummary 聚合最近 days 天的埋点摘要（租户维度 + 可选页面/角色过滤）。
//
// 口径与看板一致（方案 §13.3 四层中的采用/质量/性能子集）：
//   - 总量 / 去重会话 / error_shown 计数；
//   - 按事件名与页面分布 Top 50；
//   - latency_ms 非空事件的 avg / p95 / max。
func (s *AITelemetryService) GetAssistantTelemetrySummary(ctx context.Context, tenantID, days int, filter AssistantTelemetryFilter) (*AssistantTelemetrySummary, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if days <= 0 {
		days = 7
	}
	if days > assistantTelemetryMaxLookbackDays {
		days = assistantTelemetryMaxLookbackDays
	}
	filter.PageKey = strings.TrimSpace(filter.PageKey)
	filter.Role = strings.TrimSpace(filter.Role)
	return s.repo.AggregateAssistantEvents(ctx, tenantID, days, filter)
}

// 文档缺口聚合的默认值与上限（H1-01）。
const (
	assistantHelpGapDefaultDays  = 30
	assistantHelpGapDefaultLimit = 50
	assistantHelpGapMaxLimit     = 200
)

// GetAssistantHelpGapClusters 聚合最近 days 天的文档缺口事件（help_gap / doc_draft_created）按页聚类。
//
// 口径（方案 §5.6 H1-01）：无 page_key 的事件不计入；按缺口数降序（同数按草稿数、页码升序）。
// 未装配仓储 → ErrAssistantTelemetryUnavailable（调用方降级为「仅账本缺口」的待办队列）。
func (s *AITelemetryService) GetAssistantHelpGapClusters(ctx context.Context, tenantID, days, limit int) ([]AssistantHelpGapCluster, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if days <= 0 {
		days = assistantHelpGapDefaultDays
	}
	if days > assistantTelemetryMaxLookbackDays {
		days = assistantTelemetryMaxLookbackDays
	}
	if limit <= 0 {
		limit = assistantHelpGapDefaultLimit
	}
	if limit > assistantHelpGapMaxLimit {
		limit = assistantHelpGapMaxLimit
	}
	return s.repo.AggregateHelpGapClusters(ctx, tenantID, days, limit)
}

// InsertAssistantEvents 以单条多值 INSERT 落库，返回实际写入行数。
func (r *aiTelemetryRepository) InsertAssistantEvents(ctx context.Context, tenantID, userID int, events []AssistantTelemetryEvent) (int, error) {
	const columns = 14
	placeholders := make([]string, 0, len(events))
	args := make([]interface{}, 0, len(events)*columns)

	for i, event := range events {
		base := i * columns
		// run_id 空串写 NULL（NULLIF），conversation_id/latency_ms 由 *T nil 表示缺省。
		placeholders = append(placeholders, fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,NULLIF($%d,''),$%d,$%d,$%d::jsonb,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8,
			base+9, base+10, base+11, base+12, base+13, base+14,
		))
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			payload = []byte("{}")
		}
		occurredAt := event.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = time.Now()
		}
		args = append(args,
			tenantID,
			userID,
			event.SessionID,
			event.Name,
			event.PageKey,
			event.Route,
			event.Entrypoint,
			event.Role,
			event.ConversationID,
			event.RunID,
			event.LatencyMs,
			event.ErrorCode,
			string(payload),
			occurredAt,
		)
	}

	query := `INSERT INTO ai_telemetry_events
		(tenant_id, user_id, session_id, name, page_key, route, entrypoint, role, conversation_id, run_id, latency_ms, error_code, payload, occurred_at)
		VALUES ` + strings.Join(placeholders, ",")

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("insert assistant telemetry events: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		// 驱动不支持 RowsAffected 时按整批成功口径返回（不吞掉写入结果）。
		return len(events), nil
	}
	return int(affected), nil
}

// AggregateAssistantEvents 聚合约定的观测指标；空过滤字段不参与条件（零行为变化）。
func (r *aiTelemetryRepository) AggregateAssistantEvents(ctx context.Context, tenantID, days int, filter AssistantTelemetryFilter) (*AssistantTelemetrySummary, error) {
	summary := &AssistantTelemetrySummary{
		Days:        days,
		ByName:      []AssistantTelemetryBucket{},
		ByPage:      []AssistantTelemetryBucket{},
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	args := []interface{}{tenantID, days, filter.PageKey, filter.Role}

	const totalsQuery = `
		SELECT COUNT(*),
		       COUNT(DISTINCT CASE WHEN session_id <> '' THEN session_id END),
		       COUNT(CASE WHEN name = 'error_shown' THEN 1 END)
		FROM ai_telemetry_events
		WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND ($3 = '' OR page_key = $3)
		  AND ($4 = '' OR role = $4)
	`
	if err := r.db.QueryRowContext(ctx, totalsQuery, args...).Scan(
		&summary.TotalEvents, &summary.DistinctSessions, &summary.ErrorEvents,
	); err != nil {
		return nil, fmt.Errorf("aggregate assistant telemetry totals: %w", err)
	}

	const byNameQuery = `
		SELECT name, COUNT(*)
		FROM ai_telemetry_events
		WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND ($3 = '' OR page_key = $3)
		  AND ($4 = '' OR role = $4)
		GROUP BY name
		ORDER BY COUNT(*) DESC
		LIMIT 50
	`
	byName, err := r.queryBuckets(ctx, byNameQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate assistant telemetry by name: %w", err)
	}
	summary.ByName = byName

	const byPageQuery = `
		SELECT page_key, COUNT(*)
		FROM ai_telemetry_events
		WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND page_key <> ''
		  AND ($3 = '' OR page_key = $3)
		  AND ($4 = '' OR role = $4)
		GROUP BY page_key
		ORDER BY COUNT(*) DESC
		LIMIT 50
	`
	byPage, err := r.queryBuckets(ctx, byPageQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate assistant telemetry by page: %w", err)
	}
	summary.ByPage = byPage

	const latencyQuery = `
		SELECT COALESCE(AVG(latency_ms), 0),
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0),
		       COALESCE(MAX(latency_ms), 0)
		FROM ai_telemetry_events
		WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND latency_ms IS NOT NULL
		  AND ($3 = '' OR page_key = $3)
		  AND ($4 = '' OR role = $4)
	`
	var latency AssistantTelemetryLatency
	if err := r.db.QueryRowContext(ctx, latencyQuery, args...).Scan(
		&latency.Avg, &latency.P95, &latency.Max,
	); err != nil {
		return nil, fmt.Errorf("aggregate assistant telemetry latency: %w", err)
	}
	if latency.Avg != 0 || latency.P95 != 0 || latency.Max != 0 {
		summary.LatencyMs = &latency
	}

	return summary, nil
}

// AggregateHelpGapClusters 按 page_key 聚合文档缺口事件（H1-01；租户为隐式条件）。
func (r *aiTelemetryRepository) AggregateHelpGapClusters(ctx context.Context, tenantID, days, limit int) ([]AssistantHelpGapCluster, error) {
	const query = `
		SELECT page_key,
		       COUNT(CASE WHEN name = 'help_gap' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_created' THEN 1 END),
		       MAX(occurred_at)
		FROM ai_telemetry_events
		WHERE tenant_id = $1
		  AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND page_key <> ''
		  AND name IN ('help_gap', 'doc_draft_created')
		GROUP BY page_key
		ORDER BY COUNT(CASE WHEN name = 'help_gap' THEN 1 END) DESC, COUNT(*) DESC, page_key ASC
		LIMIT $3
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID, days, limit)
	if err != nil {
		return nil, fmt.Errorf("aggregate assistant help gap clusters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	clusters := make([]AssistantHelpGapCluster, 0)
	for rows.Next() {
		var (
			cluster  AssistantHelpGapCluster
			lastSeen sql.NullTime
		)
		if err := rows.Scan(&cluster.PageKey, &cluster.GapCount, &cluster.DraftCount, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan assistant help gap cluster: %w", err)
		}
		if lastSeen.Valid {
			cluster.LastSeen = lastSeen.Time.UTC().Format(time.RFC3339)
		}
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assistant help gap clusters: %w", err)
	}
	return clusters, nil
}

func (r *aiTelemetryRepository) queryBuckets(ctx context.Context, query string, args ...interface{}) ([]AssistantTelemetryBucket, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	buckets := make([]AssistantTelemetryBucket, 0)
	for rows.Next() {
		var bucket AssistantTelemetryBucket
		if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buckets, nil
}
