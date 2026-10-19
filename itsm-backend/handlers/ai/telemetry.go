package ai

import (
	"encoding/json"
	"errors"
	"time"

	"itsm-backend/common"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// O0-02 AI Copilot 埋点接入与聚合。
//
// 契约：docs/plan/ai-assistant-dock-plan-2026-10-18.md §13.2（事件字典）/ §13.7（落地组件）。
// 配套前端 SDK：itsm-frontend/src/lib/ai/telemetry.ts（事件名逐项对齐白名单；V2-01 增 memory_updated、
// P2 增 doc_draft_published，任何一侧增删都必须同步另一端与两端单测）。
//
// 边界：
//   - 事件名必须在服务端白名单内，未知事件静默丢弃并计入 dropped（不 400，容忍前端先行）；
//   - 单批上限 AssistantTelemetryMaxBatchSize，超出 400（防止异常客户端放大存储）；
//   - 载荷只做长度与体量护栏，不做业务语义解读；脱敏已由前端入口完成（§13.1 元数据优先）。

// assistantTelemetryEventNames 是服务端事件白名单。
var assistantTelemetryEventNames = map[string]struct{}{
	"assistant_opened":      {},
	"assistant_closed":      {},
	"context_loaded":        {},
	"context_degraded":      {},
	"suggestion_shown":      {},
	"suggestion_clicked":    {},
	"suggestion_ignored":    {},
	"task_started":          {},
	"task_completed":        {},
	"task_failed":           {},
	"message_sent":          {},
	"first_token":           {},
	"stream_end":            {},
	"tool_call":             {},
	"confirmation_shown":    {},
	"confirmation_accepted": {},
	"confirmation_rejected": {},
	"approval_pending":      {},
	"inline_preview":        {},
	"inline_applied":        {},
	"inline_undone":         {},
	"help_opened":           {},
	"help_gap":              {},
	"doc_draft_created":     {},
	// P2 文档起草采纳率：AI 预填草稿实际保存成文章（新建页创建成功）→ draftPublishRate 分子。
	"doc_draft_published": {},
	// P2 文档闭环：用户显式反馈（extra.helpful=true/false）→ 反馈解决率运营报表。
	"doc_feedback":    {},
	"error_shown":     {},
	"error_recovered": {},
	"handoff":         {},
	// V0-02 员工侧自助问答与一句话建单度量（自助解决率 / 误分类率代理 / 建单时长）。
	"self_service_answered":       {},
	"self_service_resolved":       {},
	"self_service_ticket_created": {},
	"ai_create_field_corrected":   {},
	// V2-01 个性化记忆：保存/删除常用语与模板、最近处理记录、用户开关（extra.op + 计数）。
	"memory_updated":     {},
	"telemetry_disabled": {},
}

// 载荷体量护栏：超过该字节数的载荷降级为截断占位，避免异常客户端放大存储。
const assistantTelemetryMaxPayloadBytes = 8 * 1024

type ingestAssistantTelemetryRequest struct {
	Events []map[string]interface{} `json:"events"`
}

// IngestTelemetryEvents handles POST /api/v1/ai/telemetry/events（O0-02）。
//
// 请求体：`{ "events": [ { "name": "...", "ts": 1700000000000, ... } ] }`
// 响应：`{ "accepted": n, "dropped": m }`（dropped = 白名单外事件）。
func (h *Handler) IngestTelemetryEvents(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	userID := c.GetInt("user_id")

	var req ingestAssistantTelemetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	if len(req.Events) > service.AssistantTelemetryMaxBatchSize {
		common.Fail(c, common.ParamErrorCode, "单批事件数超过上限")
		return
	}

	now := time.Now()
	events := make([]service.AssistantTelemetryEvent, 0, len(req.Events))
	dropped := 0
	for _, raw := range req.Events {
		name := telemetryString(raw, "name")
		if _, ok := assistantTelemetryEventNames[name]; !ok {
			dropped++
			continue
		}
		events = append(events, service.AssistantTelemetryEvent{
			Name:       name,
			SessionID:  truncateRunes(telemetryString(raw, "sessionId"), 64),
			PageKey:    truncateRunes(telemetryString(raw, "pageKey"), 128),
			Route:      truncateRunes(telemetryString(raw, "route"), 255),
			Entrypoint: truncateRunes(telemetryString(raw, "entrypoint"), 64),
			// O0-03：角色来自服务端 RBAC 上下文（非前端上报），供看板角色维度过滤。
			Role:           truncateRunes(c.GetString("role"), 64),
			ConversationID: telemetryPositiveInt(raw, "conversationId"),
			RunID:          truncateRunes(telemetryString(raw, "runId"), 64),
			LatencyMs:      telemetryLatencyMs(raw),
			ErrorCode:      truncateRunes(telemetryString(raw, "errorCode"), 64),
			OccurredAt:     telemetryOccurredAt(raw, now),
			Payload:        telemetryPayload(raw),
		})
	}

	accepted, err := h.svc.IngestAssistantTelemetry(c.Request.Context(), tenantID, userID, events)
	if err != nil {
		if errors.Is(err, service.ErrAssistantTelemetryUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "埋点服务未启用")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, gin.H{"accepted": accepted, "dropped": dropped})
}

// GetTelemetrySummary handles GET /api/v1/ai/telemetry/summary（O0-02 聚合读路径）。
//
// 查询参数：`days`（默认 7，上限 90）、`pageKey`（可选）、`role`（可选）。
// 看板（O0-03）与试点对照报告复用该口径；空过滤字段不参与条件（零行为变化）。
func (h *Handler) GetTelemetrySummary(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	days := queryInt(c, "days", 7)
	filter := service.AssistantTelemetryFilter{
		PageKey: truncateRunes(c.Query("pageKey"), 128),
		Role:    truncateRunes(c.Query("role"), 64),
	}

	summary, err := h.svc.GetAssistantTelemetrySummary(c.Request.Context(), tenantID, days, filter)
	if err != nil {
		if errors.Is(err, service.ErrAssistantTelemetryUnavailable) {
			common.Fail(c, common.ServiceUnavailableCode, "埋点服务未启用")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}
	common.Success(c, summary)
}

func telemetryString(event map[string]interface{}, key string) string {
	if value, ok := event[key].(string); ok {
		return value
	}
	return ""
}

// telemetryInt64 读取 JSON 数字（encoding/json 解码为 float64）。
func telemetryInt64(event map[string]interface{}, key string) (int64, bool) {
	if value, ok := event[key].(float64); ok {
		return int64(value), true
	}
	return 0, false
}

func telemetryPositiveInt(event map[string]interface{}, key string) *int {
	value, ok := telemetryInt64(event, key)
	if !ok || value <= 0 {
		return nil
	}
	out := int(value)
	return &out
}

// telemetryLatencyMs 兼容 latencyMs（首选）与 durationMs（兜底）。
func telemetryLatencyMs(event map[string]interface{}) *int64 {
	if value, ok := telemetryInt64(event, "latencyMs"); ok && value >= 0 {
		return &value
	}
	if value, ok := telemetryInt64(event, "durationMs"); ok && value >= 0 {
		return &value
	}
	return nil
}

// telemetryOccurredAt 使用端侧 ts（ms），非法/未来时间回落服务端时钟（容忍 ±5 分钟偏差）。
func telemetryOccurredAt(event map[string]interface{}, now time.Time) time.Time {
	value, ok := telemetryInt64(event, "ts")
	if !ok || value <= 0 {
		return now
	}
	occurred := time.UnixMilli(value)
	if occurred.After(now.Add(5 * time.Minute)) {
		return now
	}
	return occurred
}

// telemetryPayload 保留完整事件载荷（已由前端脱敏）；超体量降级为截断占位。
func telemetryPayload(event map[string]interface{}) map[string]interface{} {
	encoded, err := json.Marshal(event)
	if err != nil || len(encoded) > assistantTelemetryMaxPayloadBytes {
		return map[string]interface{}{"truncated": true}
	}
	return event
}

// truncateRunes 按字符（非字节）截断，避免多字节字符被切坏。
func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
