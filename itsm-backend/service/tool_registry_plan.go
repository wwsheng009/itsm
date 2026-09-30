package service

import (
	"context"
	"fmt"
	"strings"

	"itsm-backend/ent/configurationitem"
	"itsm-backend/service/bot"
)

// B3-06 plan/analysis/draft 工具实现。
//
// 边界（与方案 §4.4 B3-06 一致）：
//   - **只读业务库**：三件工具不写业务表（产物写 `bot_artifacts`，属 Bot 自有存储）；
//   - 归属与会话隔离由 `bot.ArtifactStore` 保证（租户 + 发起人）；
//   - 输出含证据引用（CI 基础信息/RAG 线索），供证据面板回溯；
//   - 预算与超时沿用注册表元数据（TimeoutMs/MaxOutputBytes），执行侧统一生效。

// artifactContext 从 ctx 解析产物归属（发起人/会话/运行）。
func (t *ToolRegistry) artifactContext(ctx context.Context) (ownerUserID, conversationID, runID int) {
	return ToolActorFromContext(ctx), ToolConversationFromContext(ctx), bot.RunIDFromContext(ctx)
}

func (t *ToolRegistry) saveArtifact(ctx context.Context, tenantID int, kind, toolName, title string, content, evidence interface{}) (interface{}, error) {
	if t.artifacts == nil {
		return nil, fmt.Errorf("plan/analysis/draft 能力未启用（产物存储未注入）")
	}
	ownerUserID, conversationID, runID := t.artifactContext(ctx)
	if ownerUserID <= 0 {
		return nil, fmt.Errorf("产物缺少发起人上下文（服务端未注入）")
	}
	artifact, err := t.artifacts.Create(ctx, bot.ArtifactInput{
		TenantID:       tenantID,
		OwnerUserID:    ownerUserID,
		ConversationID: conversationID,
		RunID:          runID,
		Kind:           kind,
		ToolName:       toolName,
		Title:          title,
		Content:        content,
		Evidence:       evidence,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"artifactId": artifact.ID,
		"kind":       artifact.Kind,
		"title":      artifact.Title,
		"content":    content,
		"evidence":   evidence,
	}, nil
}

// executeDraftTicketFields：把自然语言描述整理为工单字段草案（plan）。
func (t *ToolRegistry) executeDraftTicketFields(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	description := strings.TrimSpace(stringArg(args, "description"))
	if description == "" {
		return nil, fmt.Errorf("draft_ticket_fields: description 必填")
	}
	title := strings.TrimSpace(stringArg(args, "title"))
	if title == "" {
		title = firstRunes(description, 60)
	}
	priority := strings.TrimSpace(stringArg(args, "priority"))
	if priority == "" {
		priority = suggestPriority(description)
	}
	content := map[string]interface{}{
		"fields": map[string]interface{}{
			"title":       title,
			"description": description,
			"priority":    priority,
		},
		"rationale": "字段草案由模型依据描述生成；提交前需人工确认（plan 类工具不落业务库）。",
	}
	evidence := map[string]interface{}{
		"source":      "user_description",
		"description": firstRunes(description, 200),
	}
	return t.saveArtifact(ctx, tenantID, bot.ArtifactKindPlan, "draft_ticket_fields", "工单字段草案", content, evidence)
}

// executeCIImpactPlan：为配置项变更生成影响面分析草案（analysis；证据引用 = CI 基础信息）。
func (t *ToolRegistry) executeCIImpactPlan(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	ciID := intArg(args, "ciId")
	if ciID <= 0 {
		ciID = intArg(args, "ci_id")
	}
	if ciID <= 0 {
		return nil, fmt.Errorf("analyze_ci_impact_plan: ciId 必填")
	}
	// 只读查询：CI 必须在本租户内存在（跨租户一律表现为不可用）。
	ci, err := t.client.ConfigurationItem.Query().
		Where(configurationitem.ID(ciID), configurationitem.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("analyze_ci_impact_plan: 配置项不可用")
	}
	change := strings.TrimSpace(stringArg(args, "change"))
	summary := strings.TrimSpace(stringArg(args, "summary"))
	content := map[string]interface{}{
		"target": map[string]interface{}{
			"type": "ci", "id": ci.ID, "name": ci.Name,
		},
		"change":  change,
		"summary": summary,
		"checklist": []string{
			"确认上游依赖与下游被依赖服务的责任人",
			"确认变更窗口与回滚方案",
			"确认监控覆盖（指标/告警）就绪",
			"变更后按验证清单复核关键业务链路",
		},
		"note": "分析草案由模型生成，仅作建议；具体变更评审按既有流程执行。",
	}
	evidence := map[string]interface{}{
		"ciId": ci.ID, "ciName": ci.Name,
	}
	return t.saveArtifact(ctx, tenantID, bot.ArtifactKindAnalysis, "analyze_ci_impact_plan", "配置项变更影响分析草案", content, evidence)
}

// executeDraftKBArticle：把内容整理为知识文章草稿（draft；**不发布**）。
func (t *ToolRegistry) executeDraftKBArticle(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	title := strings.TrimSpace(stringArg(args, "title"))
	contentBody := strings.TrimSpace(stringArg(args, "content"))
	if title == "" || contentBody == "" {
		return nil, fmt.Errorf("draft_kb_article: title 与 content 必填")
	}
	content := map[string]interface{}{
		"title":   title,
		"content": contentBody,
		"status":  "draft",
		"note":    "草稿产物；发布须走知识库既有流程（本工具不自动发布）。",
	}
	evidence := map[string]interface{}{
		"source": "conversation_or_model",
	}
	return t.saveArtifact(ctx, tenantID, bot.ArtifactKindDraft, "draft_kb_article", title, content, evidence)
}

// —— 参数读取小工具（registry 内多处使用 float64/字符串混合入参）——

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func intArg(args map[string]interface{}, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// firstRunes 按字符截断（避免切坏多字节）。
func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// suggestPriority 依据描述给出**保守**优先级建议（仅建议，不改业务数据）。
func suggestPriority(description string) string {
	lower := strings.ToLower(description)
	for _, keyword := range []string{"宕机", "中断", "无法使用", "outage", "down", "紧急"} {
		if strings.Contains(lower, keyword) {
			return "high"
		}
	}
	return "medium"
}
