package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// aiTelemetryRepository 封装 AI 遥测相关 raw SQL。
//
// 设计原因：ai_feedbacks / ai_llm_calls 两张表暂无 Ent schema（platform-level
// 数据，刻意不带 tenant_id），保留 raw SQL 但集中在一个 repository 内，便于：
//  1. 强制时间窗口与平台级聚合一致；
//  2. 表结构变更只改本文件；
//  3. 单元测试只需 mock 本仓储。
type aiTelemetryRepository struct {
	db *sql.DB
}

func newAITelemetryRepository(db *sql.DB) *aiTelemetryRepository {
	if db == nil {
		return nil
	}
	return &aiTelemetryRepository{db: db}
}

// ObserveLLMCall 在 ai_llm_calls 写一条 LLM 调用记录。
func (r *aiTelemetryRepository) ObserveLLMCall(ctx context.Context, provider, model string, tokens int, latencyMs int64, success bool) error {
	const query = `
		INSERT INTO ai_llm_calls (provider, model, tokens, latency_ms, success)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := r.db.ExecContext(ctx, query, provider, model, tokens, latencyMs, success); err != nil {
		return fmt.Errorf("observe llm call: %w", err)
	}
	return nil
}

// ObserveLLMCallWithProviderKey 同 ObserveLLMCall，并写入生效的 DB 实例 key
// （BE-6，§3.7）：providerKey 为空串时写 NULL（静态回退/旧路径语义不变）。
//
// 单条 SQL 保持一条记录一次插入，与既有计数/延迟口径一一对应。
func (r *aiTelemetryRepository) ObserveLLMCallWithProviderKey(ctx context.Context, providerKey, provider, model string, tokens int, latencyMs int64, success bool) error {
	const query = `
		INSERT INTO ai_llm_calls (provider, model, tokens, latency_ms, success, provider_key)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
	`
	if _, err := r.db.ExecContext(ctx, query, provider, model, tokens, latencyMs, success, providerKey); err != nil {
		return fmt.Errorf("observe llm call with provider key: %w", err)
	}
	return nil
}

// SaveFeedback 在 ai_feedbacks 写一条用户反馈。
func (r *aiTelemetryRepository) SaveFeedback(ctx context.Context, tenantID, userID int, requestID, kind, query, itemType string, itemID *int, useful bool, score *int, notes *string) error {
	const sqlStr = `
		INSERT INTO ai_feedbacks (tenant_id, user_id, request_id, kind, query, item_type, item_id, useful, score, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	if _, err := r.db.ExecContext(ctx, sqlStr,
		tenantID, userID, requestID, kind, query, itemType,
		nullableInt(itemID), useful, nullableInt(score), nullableString(notes),
	); err != nil {
		return fmt.Errorf("save ai feedback: %w", err)
	}
	return nil
}

// FeedbackAggregate 是按租户聚合的反馈指标。
type FeedbackAggregate struct {
	TotalFeedback  int
	UsefulFeedback int
	ByKind         map[string]int
	LookbackDays   int
}

// CountFeedbacks 返回按 kind 聚合的反馈计数与 useful 计数。
func (r *aiTelemetryRepository) AggregateFeedback(ctx context.Context, tenantID, lookbackDays int) (*FeedbackAggregate, error) {
	const summaryQuery = `
		SELECT COUNT(*), COUNT(CASE WHEN useful THEN 1 END)
		FROM ai_feedbacks
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '1 day' * $2
	`
	const byKindQuery = `
		SELECT kind, COUNT(*)
		FROM ai_feedbacks
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '1 day' * $2
		GROUP BY kind
	`

	agg := &FeedbackAggregate{
		ByKind:       map[string]int{},
		LookbackDays: lookbackDays,
	}
	if err := r.db.QueryRowContext(ctx, summaryQuery, tenantID, lookbackDays).Scan(&agg.TotalFeedback, &agg.UsefulFeedback); err != nil {
		return nil, fmt.Errorf("aggregate feedback summary: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, byKindQuery, tenantID, lookbackDays)
	if err != nil {
		return nil, fmt.Errorf("aggregate feedback by kind: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int
		if scanErr := rows.Scan(&kind, &count); scanErr != nil {
			return nil, fmt.Errorf("scan feedback by kind: %w", scanErr)
		}
		agg.ByKind[kind] = count
	}
	return agg, nil
}

// LLMCallAggregate 平台级（无 tenant）LLM 调用延迟聚合。
type LLMCallAggregate struct {
	AvgLatencySeconds float64
	CallCount         int
}

// CountAIAuditLogs 通过 audit_logs 在指定租户下统计"近 lookbackDays 内、含 ai 动作"的条数。
// 优先由调用方走 Ent，仅在 entClient 不可用时回退到本方法。
func (r *aiTelemetryRepository) CountAIAuditLogs(ctx context.Context, tenantID, lookbackDays int) (int, error) {
	const query = `
		SELECT COUNT(*) FROM audit_logs
		WHERE tenant_id = $1 AND action LIKE '%ai%'
		AND created_at >= NOW() - INTERVAL '1 day' * $2
	`
	var total int
	if err := r.db.QueryRowContext(ctx, query, tenantID, lookbackDays).Scan(&total); err != nil {
		return 0, fmt.Errorf("count ai audit logs: %w", err)
	}
	return total, nil
}

// AggregateLLMLatency 计算全平台 LLM 平均延迟（秒）。
func (r *aiTelemetryRepository) AggregateLLMLatency(ctx context.Context, lookbackDays int) (*LLMCallAggregate, error) {
	const query = `
		SELECT COALESCE(AVG(latency_ms)::float / 1000.0, 0), COUNT(*)
		FROM ai_llm_calls
		WHERE created_at >= NOW() - INTERVAL '1 day' * $1
	`
	agg := &LLMCallAggregate{}
	if err := r.db.QueryRowContext(ctx, query, lookbackDays).Scan(&agg.AvgLatencySeconds, &agg.CallCount); err != nil {
		return nil, fmt.Errorf("aggregate llm latency: %w", err)
	}
	return agg, nil
}

// LLMProviderStat 是按生效实例（provider_key）聚合的 LLM 调用统计（BE-6 §3.6）。
//
// provider_key IS NULL（静态回退与开关关闭期的历史数据）归入空 key 桶并置
// StaticFallback=true——不伪造 key，便于前端/运营区分「静态回退」与「某个实例」。
//
// JSON tag 全 camelCase：与 AIMetrics 既有契约（P1-4 UAT 修复）一致，前端 TS 类型直接对齐。
type LLMProviderStat struct {
	ProviderKey       string  `json:"providerKey"`
	Provider          string  `json:"provider"`
	StaticFallback    bool    `json:"staticFallback"`
	CallCount         int     `json:"callCount"`
	SuccessCount      int     `json:"successCount"`
	SuccessRate       float64 `json:"successRate"`
	AvgLatencySeconds float64 `json:"avgLatencySeconds"`
	TotalTokens       int64   `json:"totalTokens"`
}

// AggregateLLMByProvider 按 provider_key 分组聚合调用数/成功数/平均延迟/令牌数（BE-6 §3.6）。
//
// 时间边界由 Go 计算后以参数传入（与 ai_evaluator.go 同策略：SQL 不依赖 NOW()/INTERVAL，
// 保证 sqlite 单测可用），窗口口径与 AggregateLLMLatency 的 lookbackDays 一一对应。
func (r *aiTelemetryRepository) AggregateLLMByProvider(ctx context.Context, lookbackDays int) ([]LLMProviderStat, error) {
	since := time.Now().AddDate(0, 0, -lookbackDays)
	const query = `
		SELECT COALESCE(provider_key, ''), COALESCE(MIN(provider), ''), COUNT(*),
		       COUNT(CASE WHEN success THEN 1 END),
		       COALESCE(AVG(latency_ms * 1.0) / 1000.0, 0),
		       COALESCE(SUM(tokens), 0)
		FROM ai_llm_calls
		WHERE created_at >= $1
		GROUP BY 1
		ORDER BY COUNT(*) DESC, 1
	`
	rows, err := r.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("aggregate llm by provider: %w", err)
	}
	defer rows.Close()

	stats := make([]LLMProviderStat, 0, 4)
	for rows.Next() {
		var stat LLMProviderStat
		if scanErr := rows.Scan(
			&stat.ProviderKey, &stat.Provider, &stat.CallCount, &stat.SuccessCount,
			&stat.AvgLatencySeconds, &stat.TotalTokens,
		); scanErr != nil {
			return nil, fmt.Errorf("scan llm by provider: %w", scanErr)
		}
		stat.StaticFallback = stat.ProviderKey == ""
		if stat.CallCount > 0 {
			stat.SuccessRate = float64(stat.SuccessCount) / float64(stat.CallCount)
		}
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm by provider: %w", err)
	}
	return stats, nil
}

func nullableInt(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func nullableString(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// safeLog 防止 nil logger 时 panic；调用方要么传 zap.NewN() 进来，要么允许 nil。
func safeLog(logger *zap.SugaredLogger, msg string, kv ...interface{}) {
	if logger == nil {
		return
	}
	logger.Warnw(msg, kv...)
}
