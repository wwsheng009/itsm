package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LLMCallFilter 是租户级 LLM 调用历史的查询条件。
//
// 所有查询强制 tenant_id；历史平台级行（tenant_id IS NULL）不会出现在租户报表中。
type LLMCallFilter struct {
	Days        int
	ProviderKey string
	Model       string
	TaskType    string
	ErrorClass  string
	Success     *bool
	Page        int
	PageSize    int
}

// LLMCallRecord 是一条 LLM 调用历史（API camelCase 契约）。
type LLMCallRecord struct {
	ID              int64  `json:"id"`
	CreatedAt       string `json:"createdAt"`
	TenantID        int    `json:"tenantId"`
	ProviderKey     string `json:"providerKey"`
	Provider        string `json:"provider"`
	ProviderSource  string `json:"providerSource"`
	Model           string `json:"model"`
	TaskType        string `json:"taskType"`
	Tokens          int    `json:"tokens"`
	TokensEstimated bool   `json:"tokensEstimated"`
	LatencyMs       int64  `json:"latencyMs"`
	Success         bool   `json:"success"`
	ErrorClass      string `json:"errorClass"`
	HTTPStatus      int    `json:"httpStatus,omitempty"`
	ErrorMessage    string `json:"errorMessage"`
}

// LLMCallList 是分页调用历史。
type LLMCallList struct {
	Items    []LLMCallRecord `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

// LLMCallBucket 是按某个维度聚合的调用统计。
type LLMCallBucket struct {
	Key          string  `json:"key"`
	Count        int     `json:"count"`
	Success      int     `json:"success"`
	SuccessRate  float64 `json:"successRate"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
	Tokens       int64   `json:"tokens"`
}

// LLMCallSummary 是租户级 LLM 调用汇总（不含金额/预算口径）。
type LLMCallSummary struct {
	WindowDays      int             `json:"windowDays"`
	Since           string          `json:"since"`
	GeneratedAt     string          `json:"generatedAt"`
	Total           int             `json:"total"`
	Success         int             `json:"success"`
	Failed          int             `json:"failed"`
	SuccessRate     float64         `json:"successRate"`
	AvgLatencyMs    float64         `json:"avgLatencyMs"`
	Tokens          int64           `json:"tokens"`
	TokensEstimated bool            `json:"tokensEstimated"`
	ByProvider      []LLMCallBucket `json:"byProvider"`
	ByTask          []LLMCallBucket `json:"byTask"`
	ByModel         []LLMCallBucket `json:"byModel"`
	ByErrorClass    []LLMCallBucket `json:"byErrorClass"`
}

const (
	llmCallDefaultDays     = 7
	llmCallMaxDays         = 90
	llmCallDefaultPageSize = 20
	llmCallMaxPageSize     = 100
)

func normalizeLLMCallFilter(filter LLMCallFilter) LLMCallFilter {
	if filter.Days <= 0 {
		filter.Days = llmCallDefaultDays
	}
	if filter.Days > llmCallMaxDays {
		filter.Days = llmCallMaxDays
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = llmCallDefaultPageSize
	}
	if filter.PageSize > llmCallMaxPageSize {
		filter.PageSize = llmCallMaxPageSize
	}
	filter.ProviderKey = strings.TrimSpace(filter.ProviderKey)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.TaskType = strings.TrimSpace(filter.TaskType)
	filter.ErrorClass = strings.TrimSpace(filter.ErrorClass)
	return filter
}

// buildLLMCallWhere 生成租户 + 时间窗口 + 可选过滤条件；所有值均参数化。
func buildLLMCallWhere(tenantID int, filter LLMCallFilter, since time.Time) (string, []interface{}) {
	args := []interface{}{tenantID, since}
	clauses := []string{"tenant_id = $1", "created_at >= $2"}
	add := func(expression string, value interface{}) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expression, len(args)))
	}
	if filter.ProviderKey != "" {
		add("COALESCE(provider_key, '') = $%d", filter.ProviderKey)
	}
	if filter.Model != "" {
		add("model = $%d", filter.Model)
	}
	if filter.TaskType != "" {
		add("task_type = $%d", filter.TaskType)
	}
	if filter.ErrorClass != "" {
		add("error_class = $%d", filter.ErrorClass)
	}
	if filter.Success != nil {
		add("success = $%d", *filter.Success)
	}
	return strings.Join(clauses, " AND "), args
}

// ObserveLLMCallDetailed 写入一条带租户/任务/错误分类的 LLM 调用记录。
func (r *aiTelemetryRepository) ObserveLLMCallDetailed(ctx context.Context, call LLMCallObservation) error {
	const query = `
		INSERT INTO ai_llm_calls (
			tenant_id, provider, provider_key, provider_source, model, task_type,
			tokens, tokens_estimated, latency_ms, success, error_class, http_status, error_message
		)
		VALUES (
			NULLIF($1, 0), $2, NULLIF($3, ''), $4, $5, $6,
			$7, $8, $9, $10, $11, NULLIF($12, 0), $13
		)
	`
	if _, err := r.db.ExecContext(ctx, query,
		call.TenantID, call.Provider, call.ProviderKey, call.ProviderSource,
		call.Model, NormalizeLLMTaskType(call.TaskType), call.Tokens, true,
		call.Latency.Milliseconds(), call.Success, call.ErrorClass, call.HTTPStatus,
		SanitizeLLMCallErrorText(call.ErrorMessage),
	); err != nil {
		return fmt.Errorf("observe llm call detailed: %w", err)
	}
	return nil
}

// SanitizeLLMCallErrorText 对已经字符串化的错误摘要做二次脱敏/截断。
func SanitizeLLMCallErrorText(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	return SanitizeLLMCallError(fmt.Errorf("%s", message))
}

// ListLLMCalls 分页查询租户调用历史。
func (r *aiTelemetryRepository) ListLLMCalls(ctx context.Context, tenantID int, filter LLMCallFilter) (*LLMCallList, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant id required")
	}
	filter = normalizeLLMCallFilter(filter)
	since := time.Now().AddDate(0, 0, -filter.Days)
	where, args := buildLLMCallWhere(tenantID, filter, since)

	countQuery := "SELECT COUNT(*) FROM ai_llm_calls WHERE " + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count llm calls: %w", err)
	}

	offset := (filter.Page - 1) * filter.PageSize
	argsWithPage := append(append([]interface{}{}, args...), filter.PageSize, offset)
	listQuery := fmt.Sprintf(`
		SELECT id, created_at, COALESCE(tenant_id, 0), COALESCE(provider_key, ''), provider,
		       provider_source, model, task_type, tokens, tokens_estimated, latency_ms,
		       success, error_class, http_status, error_message
		FROM ai_llm_calls
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)
	rows, err := r.db.QueryContext(ctx, listQuery, argsWithPage...)
	if err != nil {
		return nil, fmt.Errorf("list llm calls: %w", err)
	}
	defer rows.Close()

	items := make([]LLMCallRecord, 0, filter.PageSize)
	for rows.Next() {
		var item LLMCallRecord
		var createdAt time.Time
		var httpStatus sql.NullInt64
		if scanErr := rows.Scan(
			&item.ID, &createdAt, &item.TenantID, &item.ProviderKey, &item.Provider,
			&item.ProviderSource, &item.Model, &item.TaskType, &item.Tokens,
			&item.TokensEstimated, &item.LatencyMs, &item.Success, &item.ErrorClass,
			&httpStatus, &item.ErrorMessage,
		); scanErr != nil {
			return nil, fmt.Errorf("scan llm call: %w", scanErr)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if httpStatus.Valid {
			item.HTTPStatus = int(httpStatus.Int64)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm calls: %w", err)
	}
	return &LLMCallList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// SummarizeLLMCalls 聚合租户调用总数、成功率、平均延迟与维度分布。
func (r *aiTelemetryRepository) SummarizeLLMCalls(ctx context.Context, tenantID int, filter LLMCallFilter) (*LLMCallSummary, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant id required")
	}
	filter = normalizeLLMCallFilter(filter)
	sinceTime := time.Now().AddDate(0, 0, -filter.Days)
	where, args := buildLLMCallWhere(tenantID, filter, sinceTime)
	summary := &LLMCallSummary{
		WindowDays:      filter.Days,
		Since:           sinceTime.UTC().Format(time.RFC3339),
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		TokensEstimated: true,
	}

	const totalsQuery = `
		SELECT COUNT(*),
		       COUNT(CASE WHEN success THEN 1 END),
		       COALESCE(AVG(latency_ms * 1.0), 0),
		       COALESCE(SUM(tokens), 0)
		FROM ai_llm_calls
		WHERE `
	if err := r.db.QueryRowContext(ctx, totalsQuery+where, args...).Scan(
		&summary.Total, &summary.Success, &summary.AvgLatencyMs, &summary.Tokens,
	); err != nil {
		return nil, fmt.Errorf("summarize llm calls: %w", err)
	}
	summary.Failed = summary.Total - summary.Success
	if summary.Total > 0 {
		summary.SuccessRate = float64(summary.Success) / float64(summary.Total)
	}

	var err error
	if summary.ByProvider, err = r.aggregateLLMCalls(ctx, where, args,
		"COALESCE(NULLIF(provider_key, ''), NULLIF(provider, ''), 'unknown')"); err != nil {
		return nil, err
	}
	if summary.ByTask, err = r.aggregateLLMCalls(ctx, where, args,
		"COALESCE(NULLIF(task_type, ''), 'unknown')"); err != nil {
		return nil, err
	}
	if summary.ByModel, err = r.aggregateLLMCalls(ctx, where, args,
		"COALESCE(NULLIF(model, ''), 'unknown')"); err != nil {
		return nil, err
	}
	if summary.ByErrorClass, err = r.aggregateLLMCalls(ctx,
		where+" AND success = FALSE AND error_class <> ''", args, "error_class"); err != nil {
		return nil, err
	}
	return summary, nil
}

func (r *aiTelemetryRepository) aggregateLLMCalls(
	ctx context.Context,
	where string,
	args []interface{},
	keyExpression string,
) ([]LLMCallBucket, error) {
	query := fmt.Sprintf(`
		SELECT %s, COUNT(*),
		       COUNT(CASE WHEN success THEN 1 END),
		       COALESCE(AVG(latency_ms * 1.0), 0),
		       COALESCE(SUM(tokens), 0)
		FROM ai_llm_calls
		WHERE %s
		GROUP BY 1
		ORDER BY COUNT(*) DESC, 1
		LIMIT 20
	`, keyExpression, where)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate llm calls by %s: %w", keyExpression, err)
	}
	defer rows.Close()

	buckets := make([]LLMCallBucket, 0, 8)
	for rows.Next() {
		var item LLMCallBucket
		if scanErr := rows.Scan(&item.Key, &item.Count, &item.Success, &item.AvgLatencyMs, &item.Tokens); scanErr != nil {
			return nil, fmt.Errorf("scan llm call bucket: %w", scanErr)
		}
		if item.Count > 0 {
			item.SuccessRate = float64(item.Success) / float64(item.Count)
		}
		buckets = append(buckets, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm call buckets: %w", err)
	}
	return buckets, nil
}
