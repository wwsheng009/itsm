// O1-02 实时健康与告警（方案 §13.5）。
//
// 组成：
//   - 快照：运行/工具/确认（bot.MetricsService）+ Provider（ai_llm_calls 聚合）+ 埋点质量
//     （error_shown/error_recovered、latency p95）+ 队列积压（ToolQueue.Stats）；
//   - SLO：纯函数评估（任务成功率 / 工具错误率 / 首 token P95 / 错误恢复率 / LLM 成功率），
//     样本不足返回 `insufficient_data`（不误报）；
//   - 告警：违规项投递到**通知中心**（复用 notifications 表；收件人=租户管理员，缺失时回退活跃用户），
//     同租户同标题 30 分钟内去重（避免告警风暴）。
//
// 边界：只读聚合 + 显式触发的告警投递（不自动弹窗）；Provider 维度口径为平台级
// （ai_llm_calls 无 tenant_id，与既有 /ai/metrics 一致）。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/notification"
	"itsm-backend/ent/notificationpreference"
	"itsm-backend/ent/user"
	"itsm-backend/service/bot"
)

// AIHealthDefaultWindowDays 默认窗口（天）。
const AIHealthDefaultWindowDays = 7

// AIHealthMaxWindowDays 窗口上限（天），防止大表全扫。
const AIHealthMaxWindowDays = 30

// 告警去重窗口与收件人上限。
const (
	aiHealthAlertDedupeWindow = 30 * time.Minute
	aiHealthMaxRecipients     = 50
)

// SLO 阈值（方案 §13.5 建议值，评审后可按租户策略覆盖）。
const (
	SLOTaskSuccessRateThreshold = 0.95
	SLOToolErrorRateThreshold   = 0.05
	SLOFirstTokenP95MsThreshold = 3000.0
	SLOErrorRecoveryThreshold   = 0.90
	SLOLLMSuccessRateThreshold  = 0.95
	aiHealthMinSample           = 5
)

// AIHealthQueueStats 队列积压只读视图（*ToolQueue 实现；nil → 队列段降级）。
type AIHealthQueueStats interface {
	Len() int
	Cap() int
}

// AIHealthService 实时健康快照与告警。
type AIHealthService struct {
	client    *ent.Client
	metrics   *bot.MetricsService
	telemetry *AITelemetryService
	queue     AIHealthQueueStats
	now       func() time.Time
}

// NewAIHealthService 构造健康服务（metrics/telemetry 允许 nil → 对应段降级并写入 Notes）。
func NewAIHealthService(client *ent.Client, metrics *bot.MetricsService, telemetry *AITelemetryService) *AIHealthService {
	return &AIHealthService{client: client, metrics: metrics, telemetry: telemetry, now: time.Now}
}

// SetQueue 注入队列统计（可选）。
func (s *AIHealthService) SetQueue(queue AIHealthQueueStats) { s.queue = queue }

// —— 快照 DTO ——

type AIHealthRuns struct {
	Total         int     `json:"total"`
	Completed     int     `json:"completed"`
	Failed        int     `json:"failed"`
	Running       int     `json:"running"`
	SuccessRate   float64 `json:"successRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
}

type AIHealthTools struct {
	Total         int     `json:"total"`
	Errors        int     `json:"errors"`
	ErrorRate     float64 `json:"errorRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
}

type AIHealthConfirmations struct {
	Approved   int     `json:"approved"`
	Rejected   int     `json:"rejected"`
	Expired    int     `json:"expired"`
	Pending    int     `json:"pending"`
	ExpireRate float64 `json:"expireRate"`
}

type AIHealthQueue struct {
	Available bool `json:"available"`
	Length    int  `json:"length"`
	Capacity  int  `json:"capacity"`
}

type AIHealthProviders struct {
	Available    bool              `json:"available"`
	CallCount    int               `json:"callCount"`
	SuccessRate  float64           `json:"successRate"`
	Known        bool              `json:"known"`
	AvgLatencyMs float64           `json:"avgLatencyMs"`
	ByProvider   []LLMProviderStat `json:"byProvider,omitempty"`
}

type AIHealthQuality struct {
	ErrorShown     int     `json:"errorShown"`
	ErrorRecovered int     `json:"errorRecovered"`
	RecoveryRate   float64 `json:"recoveryRate"`
	// LatencyP95Ms 是兼容字段，语义固定为**整体时延（stream_end）P95**。
	LatencyP95Ms float64 `json:"latencyP95Ms"`
	// T5-07a：首 token 与整体分开下发，首 token 用于健康 SLO。
	FirstTokenP50Ms float64 `json:"firstTokenP50Ms"`
	FirstTokenP95Ms float64 `json:"firstTokenP95Ms"`
	FirstTokenCount int     `json:"firstTokenCount"`
	OverallP50Ms    float64 `json:"overallP50Ms"`
	OverallP95Ms    float64 `json:"overallP95Ms"`
	OverallCount    int     `json:"overallCount"`
	TaskStarted     int     `json:"taskStarted"`
	TaskCompleted   int     `json:"taskCompleted"`
}

// AIHealthSnapshot 一次健康快照（看板/告警共用同一份口径）。
type AIHealthSnapshot struct {
	TenantID      int                   `json:"tenantId"`
	WindowDays    int                   `json:"windowDays"`
	GeneratedAt   string                `json:"generatedAt"`
	Runs          AIHealthRuns          `json:"runs"`
	Tools         AIHealthTools         `json:"tools"`
	Confirmations AIHealthConfirmations `json:"confirmations"`
	Queue         AIHealthQueue         `json:"queue"`
	Providers     AIHealthProviders     `json:"providers"`
	Quality       AIHealthQuality       `json:"quality"`
	Notes         []string              `json:"notes,omitempty"`
}

// SLOResult 单条 SLO 的评估结果。
type SLOResult struct {
	Rule       string  `json:"rule"`
	Title      string  `json:"title"`
	Status     string  `json:"status"` // ok | breach | insufficient_data
	Actual     float64 `json:"actual"`
	Threshold  float64 `json:"threshold"`
	Comparator string  `json:"comparator"` // gte | lte
	Sample     int     `json:"sample"`
	Message    string  `json:"message,omitempty"`
}

// —— 快照采集 ——

func (s *AIHealthService) clampWindow(days int) int {
	if days <= 0 {
		return AIHealthDefaultWindowDays
	}
	if days > AIHealthMaxWindowDays {
		return AIHealthMaxWindowDays
	}
	return days
}

func countByName(buckets []AssistantTelemetryBucket, name string) int {
	for _, item := range buckets {
		if item.Key == name {
			return item.Count
		}
	}
	return 0
}

// Snapshot 采集窗口内的健康快照（租户维度；数据源缺失时降级并记录 Notes）。
func (s *AIHealthService) Snapshot(ctx context.Context, tenantID, windowDays int) (*AIHealthSnapshot, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("ai health service not initialized")
	}
	if tenantID <= 0 {
		return nil, errors.New("tenant id required")
	}
	days := s.clampWindow(windowDays)
	snapshot := &AIHealthSnapshot{
		TenantID:    tenantID,
		WindowDays:  days,
		GeneratedAt: s.now().UTC().Format(time.RFC3339),
	}

	if s.metrics != nil {
		summary, err := s.metrics.Summary(ctx, tenantID, bot.MetricsQuery{Days: days})
		if err != nil {
			return nil, fmt.Errorf("health: 运行指标不可用: %w", err)
		}
		snapshot.Runs = AIHealthRuns{
			Total:         summary.Runs.Total,
			Completed:     summary.Runs.Completed,
			Failed:        summary.Runs.Failed,
			Running:       summary.Runs.Running,
			SuccessRate:   summary.Runs.SuccessRate,
			AvgDurationMs: summary.Runs.AvgDurationMs,
		}
		snapshot.Tools = AIHealthTools{
			Total:         summary.Tools.Total,
			Errors:        summary.Tools.Errors,
			ErrorRate:     summary.Tools.ErrorRate,
			AvgDurationMs: summary.Tools.AvgDurationMs,
		}
		snapshot.Confirmations = AIHealthConfirmations{
			Approved:   summary.Confirmations.Approved,
			Rejected:   summary.Confirmations.Rejected,
			Expired:    summary.Confirmations.Expired,
			Pending:    summary.Confirmations.Pending,
			ExpireRate: summary.Confirmations.ExpireRate,
		}
	} else {
		snapshot.Notes = append(snapshot.Notes, "运行指标服务未装配：runs/tools/confirmations 段为 0")
	}

	if s.queue != nil {
		snapshot.Queue = AIHealthQueue{Available: true, Length: s.queue.Len(), Capacity: s.queue.Cap()}
	}

	// Provider：ai_llm_calls 聚合（平台级）。窗口天数与租户口径不变。
	if s.telemetry != nil {
		metrics, err := s.telemetry.GetMetrics(ctx, tenantID, days)
		if err != nil {
			snapshot.Notes = append(snapshot.Notes, fmt.Sprintf("Provider 指标不可用：%v", err))
		} else if metrics != nil {
			providers := AIHealthProviders{
				CallCount:    metrics.LLMCallCount,
				AvgLatencyMs: metrics.AvgResponseTimeSeconds * 1000,
				ByProvider:   metrics.ByProvider,
			}
			calls, success := 0, 0
			for _, item := range metrics.ByProvider {
				calls += item.CallCount
				success += item.SuccessCount
			}
			if calls > 0 {
				providers.SuccessRate = float64(success) / float64(calls)
				providers.Known = true
			}
			providers.Available = metrics.LLMCallCount > 0
			snapshot.Providers = providers
		}

		// 质量：埋点事件（error_shown/error_recovered + latency p95 + 任务漏斗）。
		summary, err := s.telemetry.GetAssistantTelemetrySummary(ctx, tenantID, days, AssistantTelemetryFilter{})
		if err != nil {
			snapshot.Notes = append(snapshot.Notes, fmt.Sprintf("埋点质量指标不可用：%v", err))
		} else if summary != nil {
			quality := AIHealthQuality{
				ErrorShown:     countByName(summary.ByName, "error_shown"),
				ErrorRecovered: countByName(summary.ByName, "error_recovered"),
				TaskStarted:    countByName(summary.ByName, "task_started"),
				TaskCompleted:  countByName(summary.ByName, "task_completed"),
			}
			if quality.ErrorShown > 0 {
				quality.RecoveryRate = float64(quality.ErrorRecovered) / float64(quality.ErrorShown)
			}
			if summary.LatencyMs != nil {
				quality.LatencyP95Ms = summary.LatencyMs.P95
				if summary.LatencyMs.FirstToken != nil {
					quality.FirstTokenP50Ms = summary.LatencyMs.FirstToken.P50
					quality.FirstTokenP95Ms = summary.LatencyMs.FirstToken.P95
					quality.FirstTokenCount = summary.LatencyMs.FirstToken.Count
				}
				if summary.LatencyMs.Overall != nil {
					quality.OverallP50Ms = summary.LatencyMs.Overall.P50
					quality.OverallP95Ms = summary.LatencyMs.Overall.P95
					quality.OverallCount = summary.LatencyMs.Overall.Count
				}
			}
			snapshot.Quality = quality
		}
	} else {
		snapshot.Notes = append(snapshot.Notes, "AI 遥测服务未装配：Provider/质量段降级")
	}

	if !snapshot.Providers.Available && s.telemetry != nil {
		snapshot.Notes = append(snapshot.Notes, "窗口内无 LLM 调用记录（Provider 段不可判定）")
	}
	return snapshot, nil
}

// —— SLO 评估（纯函数） ——

func sloResult(rule, title string, status string, actual, threshold float64, comparator string, sample int, message string) SLOResult {
	return SLOResult{
		Rule: rule, Title: title, Status: status, Actual: actual,
		Threshold: threshold, Comparator: comparator, Sample: sample, Message: message,
	}
}

func evaluateThreshold(rule, title string, actual, threshold float64, comparator string, sample, minSample int) SLOResult {
	if sample < minSample {
		return sloResult(rule, title, "insufficient_data", actual, threshold, comparator, sample,
			fmt.Sprintf("样本不足（%d/%d），暂不判定", sample, minSample))
	}
	ok := actual >= threshold
	if comparator == "lte" {
		ok = actual <= threshold
	}
	if ok {
		return sloResult(rule, title, "ok", actual, threshold, comparator, sample, "")
	}
	if comparator == "lte" {
		return sloResult(rule, title, "breach", actual, threshold, comparator, sample,
			fmt.Sprintf("%s %.2f%% 超过阈值 %.2f%%（样本 %d）", title, actual*100, threshold*100, sample))
	}
	return sloResult(rule, title, "breach", actual, threshold, comparator, sample,
		fmt.Sprintf("%s %.2f%% 低于阈值 %.2f%%（样本 %d）", title, actual*100, threshold*100, sample))
}

// EvaluateAIHealthSLOs 评估 §13.5 的 SLO 集（纯函数；样本不足 → insufficient_data）。
func EvaluateAIHealthSLOs(snapshot *AIHealthSnapshot) []SLOResult {
	if snapshot == nil {
		return nil
	}
	results := []SLOResult{
		evaluateThreshold("ai_task_success_rate", "任务成功率",
			snapshot.Runs.SuccessRate, SLOTaskSuccessRateThreshold, "gte",
			snapshot.Runs.Completed+snapshot.Runs.Failed, aiHealthMinSample),
		evaluateThreshold("ai_tool_error_rate", "工具错误率",
			snapshot.Tools.ErrorRate, SLOToolErrorRateThreshold, "lte",
			snapshot.Tools.Total, aiHealthMinSample),
	}

	// 首 token P95：只取 first_token 事件（T5-07a；不再使用混合 latency_ms 口径）。
	p95 := snapshot.Quality.FirstTokenP95Ms
	if p95 > 0 {
		results = append(results, sloResult("ai_first_token_p95", "首 token P95（ms）",
			map[bool]string{true: "ok", false: "breach"}[p95 <= SLOFirstTokenP95MsThreshold],
			p95, SLOFirstTokenP95MsThreshold, "lte", snapshot.Quality.FirstTokenCount,
			func() string {
				if p95 <= SLOFirstTokenP95MsThreshold {
					return ""
				}
				return fmt.Sprintf("首 token P95 %.0fms 超过阈值 %.0fms", p95, SLOFirstTokenP95MsThreshold)
			}()))
	} else {
		results = append(results, sloResult("ai_first_token_p95", "首 token P95（ms）",
			"insufficient_data", 0, SLOFirstTokenP95MsThreshold, "lte", 0, "窗口内无 latency 埋点"))
	}

	results = append(results, evaluateThreshold("ai_error_recovery_rate", "错误恢复率",
		snapshot.Quality.RecoveryRate, SLOErrorRecoveryThreshold, "gte",
		snapshot.Quality.ErrorShown, aiHealthMinSample))

	if snapshot.Providers.Known && snapshot.Providers.CallCount > 0 {
		results = append(results, evaluateThreshold("ai_llm_success_rate", "LLM 调用成功率",
			snapshot.Providers.SuccessRate, SLOLLMSuccessRateThreshold, "gte",
			snapshot.Providers.CallCount, aiHealthMinSample))
	} else {
		results = append(results, sloResult("ai_llm_success_rate", "LLM 调用成功率",
			"insufficient_data", 0, SLOLLMSuccessRateThreshold, "gte", 0, "窗口内无 Provider 成功率数据"))
	}
	return results
}

// —— 告警投递（通知中心） ——

// AIHealthAlertEventType 是 AI 健康告警的通知偏好事件类型（长词表；与
// dto.ListNotificationEventTypes 及前端通知偏好页 EVENT_TYPES 同源）。
// 无偏好行 → 默认放行；仅显式 in_app_enabled=false 才排除。
const AIHealthAlertEventType = "ai_health_alert"

// AIHealthAlert 一条待投递的告警。
type AIHealthAlert struct {
	Rule      string  `json:"rule"`
	Severity  string  `json:"severity"` // warning | critical
	Title     string  `json:"title"`
	Message   string  `json:"message"`
	Actual    float64 `json:"actual"`
	Threshold float64 `json:"threshold"`
}

// AIHealthDispatchResult 告警派发结果。
type AIHealthDispatchResult struct {
	Snapshot   *AIHealthSnapshot `json:"snapshot"`
	SLOs       []SLOResult       `json:"slos"`
	Alerts     []AIHealthAlert   `json:"alerts"`
	Recipients int               `json:"recipients"`
	Created    int               `json:"created"`
	Skipped    int               `json:"skipped"`
	// SkippedByPreference 是因用户订阅偏好（ai_health_alert 关闭 in_app）被排除的候选收件人数。
	SkippedByPreference int `json:"skippedByPreference"`
}

func alertSeverity(result SLOResult) string {
	// 明显偏离（≥10% 相对差距）按 critical 处理；临界违规按 warning。
	if result.Comparator == "gte" && result.Actual < result.Threshold*0.9 {
		return "critical"
	}
	if result.Comparator == "lte" && result.Actual > result.Threshold*1.5 {
		return "critical"
	}
	return "warning"
}

func buildAlerts(results []SLOResult) []AIHealthAlert {
	alerts := make([]AIHealthAlert, 0, len(results))
	for _, result := range results {
		if result.Status != "breach" {
			continue
		}
		alerts = append(alerts, AIHealthAlert{
			Rule:      result.Rule,
			Severity:  alertSeverity(result),
			Title:     fmt.Sprintf("【AI 健康】%s", result.Title),
			Message:   result.Message,
			Actual:    result.Actual,
			Threshold: result.Threshold,
		})
	}
	return alerts
}

// alertRecipients 解析收件人：租户活跃管理员优先；无管理员时回退活跃用户（上限 50）。
func (s *AIHealthService) alertRecipients(ctx context.Context, tenantID int) ([]int, error) {
	admins, err := s.client.User.Query().
		Where(user.TenantID(tenantID), user.ActiveEQ(true),
			user.RoleIn("admin", "super_admin", "tenant_admin")).
		Limit(aiHealthMaxRecipients).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("health: 查询管理员失败: %w", err)
	}
	if len(admins) == 0 {
		admins, err = s.client.User.Query().
			Where(user.TenantID(tenantID), user.ActiveEQ(true)).
			Limit(aiHealthMaxRecipients).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("health: 查询活跃用户失败: %w", err)
		}
	}
	ids := make([]int, 0, len(admins))
	for _, item := range admins {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

// filterRecipientsByPreference 在候选收件人上叠加「订阅偏好」过滤：
// notification_preferences(event_type=ai_health_alert).in_app_enabled=false → 排除；
// 无偏好行 → 保留（与平台既有语义「无偏好=放行」一致）。
//
// 查询失败返回错误（fail visible）：本轮该租户不投递，避免在偏好不可读时误发给已退订用户。
func (s *AIHealthService) filterRecipientsByPreference(ctx context.Context, tenantID int, candidates []int) ([]int, int, error) {
	if len(candidates) == 0 {
		return candidates, 0, nil
	}
	prefs, err := s.client.NotificationPreference.Query().
		Where(
			notificationpreference.TenantID(tenantID),
			notificationpreference.EventType(AIHealthAlertEventType),
			notificationpreference.UserIDIn(candidates...),
		).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("health: 订阅偏好查询失败: %w", err)
	}
	optedOut := make(map[int]struct{}, len(prefs))
	for _, pref := range prefs {
		if !pref.InAppEnabled {
			optedOut[pref.UserID] = struct{}{}
		}
	}
	allowed := make([]int, 0, len(candidates))
	for _, id := range candidates {
		if _, out := optedOut[id]; !out {
			allowed = append(allowed, id)
		}
	}
	return allowed, len(candidates) - len(allowed), nil
}

// DispatchAlerts 评估 SLO 并把违规项投递到通知中心（同租户同标题 30 分钟去重）。
func (s *AIHealthService) DispatchAlerts(ctx context.Context, tenantID, windowDays int) (*AIHealthDispatchResult, error) {
	snapshot, err := s.Snapshot(ctx, tenantID, windowDays)
	if err != nil {
		return nil, err
	}
	slos := EvaluateAIHealthSLOs(snapshot)
	alerts := buildAlerts(slos)

	result := &AIHealthDispatchResult{Snapshot: snapshot, SLOs: slos, Alerts: alerts}
	if len(alerts) == 0 {
		return result, nil
	}

	recipients, err := s.alertRecipients(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	recipients, skippedByPreference, err := s.filterRecipientsByPreference(ctx, tenantID, recipients)
	if err != nil {
		return nil, err
	}
	result.Recipients = len(recipients)
	result.SkippedByPreference = skippedByPreference

	since := s.now().Add(-aiHealthAlertDedupeWindow)
	for _, alert := range alerts {
		exists, err := s.client.Notification.Query().
			Where(
				notification.TenantID(tenantID),
				notification.Type("ai_health"),
				notification.Title(alert.Title),
				notification.CreatedAtGTE(since),
			).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("health: 告警去重查询失败: %w", err)
		}
		if exists {
			result.Skipped++
			continue
		}
		for _, userID := range recipients {
			if _, err := s.client.Notification.Create().
				SetTenantID(tenantID).
				SetUserID(userID).
				SetType("ai_health").
				SetTitle(alert.Title).
				SetMessage(alert.Message).
				SetActionURL("/ai/observability").
				SetActionText("查看观测看板").
				// 显式时间：与调用方时钟一致（去重窗口与测试可确定）。
				SetCreatedAt(s.now()).
				SetUpdatedAt(s.now()).
				Save(ctx); err != nil {
				return nil, fmt.Errorf("health: 告警写入失败: %w", err)
			}
			result.Created++
		}
	}
	return result, nil
}
