// O2-01 分析增强（§13.6）：建议排序权重反馈 + 分群对比。
//
// 口径：
//   - **建议权重**（先规则，后模型）：按近窗口 `suggestion_shown/clicked/ignored` 在 payload.cardId 维度聚合，
//     供 D1-01 `/ai/assistant/context` 组内重排（任务卡/问题卡各自排序，保持 I0-02 分组契约）；
//   - **时间衰减**：反馈按半衰期（`SuggestionWeightsHalfLifeDays`）指数衰减后再参与打分——
//     近期反馈主导、旧反馈自然退场（忽略样本不会永久惩罚一张卡）；
//   - **分群对比**：按 `role`（角色）或 `page_key`（页面）分组，输出采用/建议/任务/Inline/错误子集指标
//     （租户为隐式条件；"租户维度"属平台侧视图，不在本端点范围）。
package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// 建议权重回看窗口（天）：与「近一周反馈」量级一致，避免长期偏好固化。
	SuggestionWeightsDefaultDays = 30
	SuggestionWeightsMaxDays     = 90

	// 半衰期（天）：反馈影响力按 0.5^(age/半衰期) 指数衰减（天数桶按桶中点计龄）。
	SuggestionWeightsHalfLifeDays = 14.0

	// 分群对比窗口。
	AssistantSegmentsDefaultDays = 7
	AssistantSegmentsMaxDays     = 90

	// 分群维度（白名单；列名不可参数化，故显式映射）。
	AssistantSegmentDimensionRole    = "role"
	AssistantSegmentDimensionPageKey = "pageKey"
)

// SuggestionWeight 单卡反馈（近窗口）：Shown/Clicked/Ignored 为原始计数（审计口径），
// Decayed* 为半衰期衰减后的有效计数（排序打分口径；差异可解释"为什么这张卡排在这里"）。
type SuggestionWeight struct {
	Shown   int `json:"shown"`
	Clicked int `json:"clicked"`
	Ignored int `json:"ignored"`

	DecayedShown   float64 `json:"decayedShown"`
	DecayedClicked float64 `json:"decayedClicked"`
	DecayedIgnored float64 `json:"decayedIgnored"`
}

// AggregateSuggestionWeights 按 payload.cardId 聚合建议反馈（租户前置；空 cardId 忽略）。
//
// SQL 侧按「卡 × 事件名 × 天龄」分桶计数（避免逐事件回传），衰减在 Go 侧计算：
// weight = count × 0.5^((ageDays+0.5)/halfLife)，天数桶按中点计龄（桶内事件视为均匀分布）。
func (r *aiTelemetryRepository) AggregateSuggestionWeights(ctx context.Context, tenantID int, since time.Time) (map[string]SuggestionWeight, error) {
	weights := make(map[string]SuggestionWeight)
	const query = `
	-- ai_suggestion_weights
	SELECT payload->>'cardId' AS card_id,
		name,
		(CURRENT_DATE - occurred_at::date) AS age_days,
		COUNT(*)
	FROM ai_telemetry_events
	WHERE tenant_id = $1
	  AND occurred_at >= $2
	  AND name IN ('suggestion_shown', 'suggestion_clicked', 'suggestion_ignored')
	  AND COALESCE(payload->>'cardId', '') <> ''
	GROUP BY 1, 2, 3
	`
	rows, err := r.db.QueryContext(ctx, query, tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("aggregate suggestion weights: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cardID, name string
		var ageDays, count int
		if err := rows.Scan(&cardID, &name, &ageDays, &count); err != nil {
			return nil, fmt.Errorf("scan suggestion weight: %w", err)
		}
		weight := weights[cardID]
		decayed := decayedSuggestionCount(count, ageDays)
		switch name {
		case "suggestion_shown":
			weight.Shown += count
			weight.DecayedShown += decayed
		case "suggestion_clicked":
			weight.Clicked += count
			weight.DecayedClicked += decayed
		case "suggestion_ignored":
			weight.Ignored += count
			weight.DecayedIgnored += decayed
		default:
			continue
		}
		weights[cardID] = weight
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate suggestion weights: %w", err)
	}
	// 收敛到 4 位小数：响应可审计、避免浮点尾数噪声（打分与展示共用同一数值）。
	for cardID, weight := range weights {
		weight.DecayedShown = roundSuggestionWeight(weight.DecayedShown)
		weight.DecayedClicked = roundSuggestionWeight(weight.DecayedClicked)
		weight.DecayedIgnored = roundSuggestionWeight(weight.DecayedIgnored)
		weights[cardID] = weight
	}
	return weights, nil
}

// decayedSuggestionCount 半衰期衰减后的有效计数：
// ageDays 为天数桶编号（0 = 今天），按桶中点（+0.5 天）计龄；未来时间戳（时钟偏差）按 0 计。
func decayedSuggestionCount(count, ageDays int) float64 {
	age := math.Max(float64(ageDays)+0.5, 0)
	return float64(count) * math.Pow(0.5, age/SuggestionWeightsHalfLifeDays)
}

// roundSuggestionWeight 4 位小数（打分与响应共用同一数值，避免"算一套、看一套"）。
func roundSuggestionWeight(value float64) float64 {
	return math.Round(value*10000) / 10000
}

// GetSuggestionWeights 读取近 days 天的建议反馈权重（原始计数 + 半衰期衰减有效计数；0/负 → 默认；上限 clamp）。
func (s *AITelemetryService) GetSuggestionWeights(ctx context.Context, tenantID, days int) (map[string]SuggestionWeight, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant id required")
	}
	if days <= 0 {
		days = SuggestionWeightsDefaultDays
	}
	if days > SuggestionWeightsMaxDays {
		days = SuggestionWeightsMaxDays
	}
	return s.repo.AggregateSuggestionWeights(ctx, tenantID, time.Now().AddDate(0, 0, -days))
}

// AssistantSegment 一个分组的采用/效果/质量子集指标。
type AssistantSegment struct {
	Key                string  `json:"key"`
	Events             int     `json:"events"`
	Sessions           int     `json:"sessions"`
	SuggestionShown    int     `json:"suggestionShown"`
	SuggestionClicked  int     `json:"suggestionClicked"`
	TaskStarted        int     `json:"taskStarted"`
	TaskCompleted      int     `json:"taskCompleted"`
	InlinePreview      int     `json:"inlinePreview"`
	InlineApplied      int     `json:"inlineApplied"`
	Errors             int     `json:"errors"`
	SuggestionCtr      float64 `json:"suggestionCtr"`
	TaskCompletionRate float64 `json:"taskCompletionRate"`
	InlineAdoptionRate float64 `json:"inlineAdoptionRate"`
	ErrorRate          float64 `json:"errorRate"`
}

// AssistantSegmentsReport 分群对比报告。
type AssistantSegmentsReport struct {
	TenantID    int                `json:"tenantId"`
	Days        int                `json:"days"`
	Dimension   string             `json:"dimension"`
	Segments    []AssistantSegment `json:"segments"`
	GeneratedAt string             `json:"generatedAt"`
	Notes       []string           `json:"notes,omitempty"`
}

func safeRate(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// buildAssistantSegments 计算派生比率（纯函数；分母为 0 → 0，不产生 NaN）。
func buildAssistantSegments(raw []AssistantSegment) []AssistantSegment {
	out := make([]AssistantSegment, 0, len(raw))
	for _, segment := range raw {
		segment.SuggestionCtr = safeRate(segment.SuggestionClicked, segment.SuggestionShown)
		segment.TaskCompletionRate = safeRate(segment.TaskCompleted, segment.TaskStarted)
		segment.InlineAdoptionRate = safeRate(segment.InlineApplied, segment.InlinePreview)
		segment.ErrorRate = safeRate(segment.Errors, segment.Events)
		out = append(out, segment)
	}
	return out
}

// segmentDimensionColumn 维度白名单 → 列名（不可参数化的部分显式映射，防注入）。
func segmentDimensionColumn(dimension string) (string, string, bool) {
	switch strings.TrimSpace(dimension) {
	case AssistantSegmentDimensionRole:
		return "role", AssistantSegmentDimensionRole, true
	case AssistantSegmentDimensionPageKey, "page_key", "page":
		return "page_key", AssistantSegmentDimensionPageKey, true
	default:
		return "", "", false
	}
}

// AggregateAssistantSegments 按维度分组聚合（租户前置；空值分组剔除；按事件量倒序 Top 20）。
func (r *aiTelemetryRepository) AggregateAssistantSegments(ctx context.Context, tenantID, days int, dimension string) ([]AssistantSegment, error) {
	column, _, ok := segmentDimensionColumn(dimension)
	if !ok {
		return nil, fmt.Errorf("unsupported segment dimension: %s", dimension)
	}
	query := fmt.Sprintf(`
	-- ai_segment_breakdown
	SELECT %s AS segment,
		COUNT(*),
		COUNT(DISTINCT CASE WHEN session_id <> '' THEN session_id END),
		COUNT(*) FILTER (WHERE name = 'suggestion_shown'),
		COUNT(*) FILTER (WHERE name = 'suggestion_clicked'),
		COUNT(*) FILTER (WHERE name = 'task_started'),
		COUNT(*) FILTER (WHERE name = 'task_completed'),
		COUNT(*) FILTER (WHERE name = 'inline_preview'),
		COUNT(*) FILTER (WHERE name = 'inline_applied'),
		COUNT(*) FILTER (WHERE name = 'error_shown')
	FROM ai_telemetry_events
	WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2 AND %s <> ''
	GROUP BY 1
	ORDER BY COUNT(*) DESC
	LIMIT 20
	`, column, column)

	rows, err := r.db.QueryContext(ctx, query, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("aggregate assistant segments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	segments := make([]AssistantSegment, 0, 20)
	for rows.Next() {
		var segment AssistantSegment
		if err := rows.Scan(
			&segment.Key, &segment.Events, &segment.Sessions,
			&segment.SuggestionShown, &segment.SuggestionClicked,
			&segment.TaskStarted, &segment.TaskCompleted,
			&segment.InlinePreview, &segment.InlineApplied, &segment.Errors,
		); err != nil {
			return nil, fmt.Errorf("scan assistant segment: %w", err)
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assistant segments: %w", err)
	}
	return segments, nil
}

// GetAssistantSegments 生成分群对比报告（维度白名单；窗口 clamp）。
func (s *AITelemetryService) GetAssistantSegments(ctx context.Context, tenantID, days int, dimension string) (*AssistantSegmentsReport, error) {
	// 维度优先校验（与埋点可用性无关）：非法维度一律参数错误，避免错误码被 503 掩盖。
	_, canonical, ok := segmentDimensionColumn(dimension)
	if !ok {
		return nil, ErrAssistantSegmentsDimension
	}
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant id required")
	}
	if days <= 0 {
		days = AssistantSegmentsDefaultDays
	}
	if days > AssistantSegmentsMaxDays {
		days = AssistantSegmentsMaxDays
	}

	raw, err := s.repo.AggregateAssistantSegments(ctx, tenantID, days, dimension)
	if err != nil {
		return nil, err
	}
	report := &AssistantSegmentsReport{
		TenantID:    tenantID,
		Days:        days,
		Dimension:   canonical,
		Segments:    buildAssistantSegments(raw),
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	if len(report.Segments) == 0 {
		report.Notes = append(report.Notes, "窗口内无该维度样本（事件随使用产生）")
	}
	return report, nil
}

// ErrAssistantSegmentsDimension 非法分群维度（handler 映射 400）。
var ErrAssistantSegmentsDimension = fmt.Errorf("unsupported segment dimension")
