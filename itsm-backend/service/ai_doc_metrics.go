package service

import (
	"context"
	"fmt"
	"time"
)

// P2 文档命中率/解决率运营报表（方案 §9 帮助度量 / §13.3 运营层 / §13.6 分析增强）。
//
// 数据口径（全部来自 ai_telemetry_events，租户为隐式条件）：
//   - 文档命中率 hitRate = 1 - 有 help_gap 的会话 / 有 help_opened 的会话（会话级，抗"同页多次打开"放大）；
//   - 解决率 resolveRate（未升级人工率）= 1 - 有 handoff 的会话 / 有 help_opened 的会话；
//   - 反馈解决率 feedbackResolveRate = 显式 doc_feedback(helpful=true) / 全部 doc_feedback；
//   - 起草落地率 draftPublishRate = doc_draft_published / doc_draft_created（AI 起草动作 → 实际保存成文章；
//     分母为 0 时省略 = null，不虚报 0%）；
//   - 缺口收敛趋势 trend：按天计数（opened/gaps/drafts/published/helpful/unhelpful），供运营判断缺口是否收敛；
//   - 按页 Top：按缺口数降序（复用 H1-01 的运维视角，含打开/草稿/反馈明细）。
//
// 信号口径：`doc_draft_created` 在会话页「补充为知识文章 / 保存成文章」动作处上报；
// `doc_draft_published` 在新建页由 AI 预填**创建成功**后上报——两者共同构成
// 「AI 起草 → 落地成文」的窗口级漏斗（非逐篇归因：草稿在窗口末尾产生、成文落在下一窗口属已知近似，
// 以窗口计数而非跨天归因，避免伪造逐篇链路）。
//
// 红线：只读聚合；无有效分母时对应比率为 null（不虚报 0%）；窗口缺省 30 天、上限 90 天。

// AssistantDocMetricsTotals 是窗口内的原始事件计数与会话级分母。
type AssistantDocMetricsTotals struct {
	Opened int `json:"opened"`
	Gaps   int `json:"gaps"`
	Drafts int `json:"drafts"`
	// Published 是 AI 起草的草稿被实际保存成知识库文章的条数（doc_draft_published）。
	Published         int `json:"published"`
	FeedbackHelpful   int `json:"feedbackHelpful"`
	FeedbackUnhelpful int `json:"feedbackUnhelpful"`
	Handoffs          int `json:"handoffs"`
	OpenedSessions    int `json:"openedSessions"`
	GapSessions       int `json:"gapSessions"`
	// HandoffSessions 是「打开过帮助的会话中升级人工」的会话数（解决率分子；会话交集，不用全量 handoff 误伤分母）。
	HandoffSessions int `json:"handoffSessions"`
	Events          int `json:"events"`
}

// AssistantDocMetricsRates 是三口径比率（无有效分母时省略 = null）。
type AssistantDocMetricsRates struct {
	HitRate             *float64 `json:"hitRate,omitempty"`
	ResolveRate         *float64 `json:"resolveRate,omitempty"`
	FeedbackResolveRate *float64 `json:"feedbackResolveRate,omitempty"`
	// DraftPublishRate 起草落地率 = published/drafts（无起草动作时 null）。
	DraftPublishRate *float64 `json:"draftPublishRate,omitempty"`
}

// AssistantDocMetricsPage 是页面维度的帮助度量（byPage Top 项）。
type AssistantDocMetricsPage struct {
	PageKey   string `json:"pageKey"`
	Opened    int    `json:"opened"`
	Gaps      int    `json:"gaps"`
	Drafts    int    `json:"drafts"`
	Published int    `json:"published"`
	Helpful   int    `json:"helpful"`
	Unhelpful int    `json:"unhelpful"`
}

// AssistantDocMetricsTrendPoint 是按天（UTC）的趋势点（缺口收敛趋势）。
type AssistantDocMetricsTrendPoint struct {
	Day       string `json:"day"`
	Opened    int    `json:"opened"`
	Gaps      int    `json:"gaps"`
	Drafts    int    `json:"drafts"`
	Published int    `json:"published"`
	Helpful   int    `json:"helpful"`
	Unhelpful int    `json:"unhelpful"`
}

// AssistantDocMetricsReport 是运营报表的服务端聚合结果（帮助文档闭环）。
type AssistantDocMetricsReport struct {
	Days        int                             `json:"days"`
	Totals      AssistantDocMetricsTotals       `json:"totals"`
	Rates       AssistantDocMetricsRates        `json:"rates"`
	ByPage      []AssistantDocMetricsPage       `json:"byPage"`
	Trend       []AssistantDocMetricsTrendPoint `json:"trend"`
	GeneratedAt string                          `json:"generatedAt"`
}

const (
	assistantDocMetricsDefaultDays = 30
	assistantDocMetricsPageLimit   = 20
)

// GetAssistantDocMetrics 聚合最近 days 天的帮助文档运营指标。
//
// 未装配仓储 → ErrAssistantTelemetryUnavailable（调用方降级为「仅覆盖率」的报表，不 5xx）。
func (s *AITelemetryService) GetAssistantDocMetrics(ctx context.Context, tenantID, days int) (*AssistantDocMetricsReport, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAssistantTelemetryUnavailable
	}
	if days <= 0 {
		days = assistantDocMetricsDefaultDays
	}
	if days > assistantTelemetryMaxLookbackDays {
		days = assistantTelemetryMaxLookbackDays
	}
	return s.repo.AggregateDocMetrics(ctx, tenantID, days)
}

// AggregateDocMetrics 执行三段只读聚合（总量/按页/按天），比率在 Go 侧计算（NULL 语义明确）。
func (r *aiTelemetryRepository) AggregateDocMetrics(ctx context.Context, tenantID, days int) (*AssistantDocMetricsReport, error) {
	report := &AssistantDocMetricsReport{
		Days:        days,
		ByPage:      []AssistantDocMetricsPage{},
		Trend:       []AssistantDocMetricsTrendPoint{},
		GeneratedAt: time.Now().Format(time.RFC3339),
	}

	const totalsQuery = `
		SELECT COUNT(CASE WHEN name = 'help_opened' THEN 1 END),
		       COUNT(CASE WHEN name = 'help_gap' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_created' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_published' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'true' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'false' THEN 1 END),
		       COUNT(CASE WHEN name = 'handoff' THEN 1 END),
		       COUNT(DISTINCT CASE WHEN name = 'help_opened' AND session_id <> '' THEN session_id END),
		       COUNT(DISTINCT CASE WHEN name = 'help_gap' AND session_id <> '' THEN session_id END),
		       COUNT(DISTINCT CASE WHEN name = 'handoff' AND session_id <> ''
		             AND session_id IN (
		               SELECT session_id FROM ai_telemetry_events
		               WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		                 AND name = 'help_opened' AND session_id <> ''
		             ) THEN session_id END),
		       COUNT(*)
		FROM ai_telemetry_events
		WHERE tenant_id = $1 AND occurred_at >= NOW() - INTERVAL '1 day' * $2
	`
	if err := r.db.QueryRowContext(ctx, totalsQuery, tenantID, days).Scan(
		&report.Totals.Opened,
		&report.Totals.Gaps,
		&report.Totals.Drafts,
		&report.Totals.Published,
		&report.Totals.FeedbackHelpful,
		&report.Totals.FeedbackUnhelpful,
		&report.Totals.Handoffs,
		&report.Totals.OpenedSessions,
		&report.Totals.GapSessions,
		&report.Totals.HandoffSessions,
		&report.Totals.Events,
	); err != nil {
		return nil, fmt.Errorf("aggregate doc metrics totals: %w", err)
	}
	report.Rates = AssistantDocMetricsRates{
		HitRate:             ratioOrNil(report.Totals.OpenedSessions-report.Totals.GapSessions, report.Totals.OpenedSessions),
		ResolveRate:         ratioOrNil(report.Totals.OpenedSessions-report.Totals.HandoffSessions, report.Totals.OpenedSessions),
		FeedbackResolveRate: ratioOrNil(report.Totals.FeedbackHelpful, report.Totals.FeedbackHelpful+report.Totals.FeedbackUnhelpful),
		DraftPublishRate:    ratioOrNil(report.Totals.Published, report.Totals.Drafts),
	}

	const byPageQuery = `
		SELECT page_key,
		       COUNT(CASE WHEN name = 'help_opened' THEN 1 END),
		       COUNT(CASE WHEN name = 'help_gap' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_created' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_published' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'true' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'false' THEN 1 END)
		FROM ai_telemetry_events
		WHERE tenant_id = $1
		  AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND page_key <> ''
		  AND name IN ('help_opened', 'help_gap', 'doc_draft_created', 'doc_draft_published', 'doc_feedback')
		GROUP BY page_key
		ORDER BY COUNT(CASE WHEN name = 'help_gap' THEN 1 END) DESC, COUNT(*) DESC, page_key ASC
		LIMIT $3
	`
	pageRows, err := r.db.QueryContext(ctx, byPageQuery, tenantID, days, assistantDocMetricsPageLimit)
	if err != nil {
		return nil, fmt.Errorf("aggregate doc metrics by page: %w", err)
	}
	defer func() { _ = pageRows.Close() }()
	for pageRows.Next() {
		var item AssistantDocMetricsPage
		if err := pageRows.Scan(&item.PageKey, &item.Opened, &item.Gaps, &item.Drafts, &item.Published, &item.Helpful, &item.Unhelpful); err != nil {
			return nil, fmt.Errorf("scan doc metrics page: %w", err)
		}
		report.ByPage = append(report.ByPage, item)
	}
	if err := pageRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate doc metrics by page: %w", err)
	}

	const trendQuery = `
		SELECT to_char(date_trunc('day', occurred_at), 'YYYY-MM-DD'),
		       COUNT(CASE WHEN name = 'help_opened' THEN 1 END),
		       COUNT(CASE WHEN name = 'help_gap' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_created' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_draft_published' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'true' THEN 1 END),
		       COUNT(CASE WHEN name = 'doc_feedback' AND payload->'extra'->>'helpful' = 'false' THEN 1 END)
		FROM ai_telemetry_events
		WHERE tenant_id = $1
		  AND occurred_at >= NOW() - INTERVAL '1 day' * $2
		  AND name IN ('help_opened', 'help_gap', 'doc_draft_created', 'doc_draft_published', 'doc_feedback')
		GROUP BY 1
		ORDER BY 1 ASC
	`
	trendRows, err := r.db.QueryContext(ctx, trendQuery, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("aggregate doc metrics trend: %w", err)
	}
	defer func() { _ = trendRows.Close() }()
	for trendRows.Next() {
		var point AssistantDocMetricsTrendPoint
		if err := trendRows.Scan(&point.Day, &point.Opened, &point.Gaps, &point.Drafts, &point.Published, &point.Helpful, &point.Unhelpful); err != nil {
			return nil, fmt.Errorf("scan doc metrics trend: %w", err)
		}
		report.Trend = append(report.Trend, point)
	}
	if err := trendRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate doc metrics trend: %w", err)
	}

	return report, nil
}

// ratioOrNil 返回 num/den；den<=0 时返回 nil（JSON 省略，前端渲染「—」而不是虚报 0%）。
func ratioOrNil(num, den int) *float64 {
	if den <= 0 {
		return nil
	}
	value := float64(num) / float64(den)
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return &value
}
