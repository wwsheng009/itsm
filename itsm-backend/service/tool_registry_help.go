package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service/bot"
)

// HelpDocPublisher 是帮助写面到知识库的桥（T5-09a）。
//
// 实现在 handlers/knowledge.Service，避免 service 包反向依赖 handler 包。
type HelpDocPublisher interface {
	CreateHelpDraft(ctx context.Context, tenantID, authorID int, title, content string) (int, error)
	PublishHelpDraft(ctx context.Context, tenantID, publisherID, articleID int, changeLog string) error
}

// H0-03 / T5-09a Help Agent 工具（读面 + plan 草稿 + 写面）。
//
// 口径（方案 §5.4）：
//   - 读/plan 工具不落业务库（Risk=read/plan、ReadOnly=true）；
//   - 数据源为构建期内嵌索引（docs/help 真源），与 /ai/help/* 端点同源；
//   - 与场景 Bot 的绑定由授权决定：S3 知识自助助手默认授予（见 bot/scenarios.go），
//     管理员可通过 Bot 授权面调整；
//   - 写面 help_submit_review（act_low）与 help_publish（act_medium）经审批队列执行。
func helpToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "help_search",
			Description: "检索产品帮助文档（关键词；可选 pageKey 限定当前页面）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskRead,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"q":       map[string]interface{}{"type": "string", "description": "检索关键词"},
					"pageKey": map[string]interface{}{"type": "string", "description": "页面键（如 ticket.detail）"},
					"limit":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 20},
				},
				"required": []string{"q"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"results": map[string]interface{}{"type": "array"},
					"count":   map[string]interface{}{"type": "integer"},
				},
			},
		},
		{
			Name:        "help_get_page",
			Description: "获取指定页面的帮助文档与覆盖状态（status: ok/missing/stale/unknown）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskRead,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pageKey": map[string]interface{}{"type": "string", "description": "页面键（如 ticket.detail）"},
				},
				"required": []string{"pageKey"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{"type": "string"},
					"docs":   map[string]interface{}{"type": "array"},
				},
			},
		},
		{
			Name:        "help_read_doc",
			Description: "按路径读取帮助文档全文（docs/help/**）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskRead,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "文档相对路径（如 docs/help/tickets/detail.md）"},
				},
				"required": []string{"path"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":  map[string]interface{}{"type": "string"},
					"title": map[string]interface{}{"type": "string"},
					"text":  map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "help_list_gaps",
			Description: "列出缺少或过时帮助文档的页面（覆盖率缺口；供文档维护任务）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskRead,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"gaps":     map[string]interface{}{"type": "array"},
					"coverage": map[string]interface{}{"type": "number"},
				},
			},
		},
		{
			Name:        "help_create_draft",
			Description: "把给定的 Markdown 整理为页面帮助文档草稿（plan：只落 bot_artifacts 产物，不写业务库、不发布）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskPlan,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pageKey": map[string]interface{}{"type": "string", "description": "目标页面键（如 ticket.detail；未注册页面也可起草）"},
					"title":   map[string]interface{}{"type": "string", "description": "草稿标题（缺省按页面键生成）"},
					"content": map[string]interface{}{"type": "string", "description": "Markdown 草稿正文"},
					"sourceDocs": map[string]interface{}{
						"type": "array", "items": map[string]interface{}{"type": "string"},
						"description": "参考的既有文档路径（证据引用）",
					},
				},
				"required": []string{"pageKey", "content"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId": map[string]interface{}{"type": "integer"},
					"kind":       map[string]interface{}{"type": "string"},
					"title":      map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "help_update_draft",
			Description: "更新已有帮助文档草稿（plan：只更新 bot_artifacts 产物，不写业务库、不发布）",
			ReadOnly:    true,
			Resource:    "ai",
			Action:      "read",

			Risk:             ToolRiskPlan,
			Category:         "help",
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId": map[string]interface{}{"type": "integer", "description": "help_create_draft 返回的产物 ID"},
					"title":      map[string]interface{}{"type": "string", "description": "可选：更新后的标题"},
					"content":    map[string]interface{}{"type": "string", "description": "更新后的 Markdown 正文"},
					"sourceDocs": map[string]interface{}{
						"type": "array", "items": map[string]interface{}{"type": "string"},
						"description": "参考的既有文档路径（证据引用）",
					},
				},
				"required": []string{"artifactId", "content"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId": map[string]interface{}{"type": "integer"},
					"revision":   map[string]interface{}{"type": "integer"},
					"status":     map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "help_submit_review",
			Description: "把帮助草稿提交人工审阅（需审批）：落知识库「用户指南」草稿，不发布",
			ReadOnly:    false,
			Resource:    "knowledge",
			Action:      "write",

			Risk:             ToolRiskActLow,
			Category:         "help",
			Idempotent:       true,
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId": map[string]interface{}{"type": "integer", "description": "帮助草稿产物 ID"},
				},
				"required": []string{"artifactId"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId":         map[string]interface{}{"type": "integer"},
					"knowledgeArticleId": map[string]interface{}{"type": "integer"},
					"status":             map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "help_publish",
			Description: "发布已提交审阅的帮助草稿（需审批；管理员）：发布关联的知识库文章并产生版本",
			ReadOnly:    false,
			Resource:    "knowledge",
			Action:      "admin",

			Risk:             ToolRiskActMedium,
			Category:         "help",
			Idempotent:       true,
			TimeoutMs:        DefaultToolTimeoutMs,
			MaxOutputBytes:   DefaultToolMaxOutputBytes,
			RedactionProfile: ToolRedactionDefault,
			ArgsSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId": map[string]interface{}{"type": "integer", "description": "已提交审阅的帮助草稿产物 ID"},
					"changeLog":  map[string]interface{}{"type": "string", "description": "可选：发布说明"},
				},
				"required": []string{"artifactId"},
			},
			ResultSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"artifactId":         map[string]interface{}{"type": "integer"},
					"knowledgeArticleId": map[string]interface{}{"type": "integer"},
					"status":             map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

// executeHelpTool 执行 help_* 工具（无 DB 依赖，fail 时返回模型可读错误）。
func executeHelpTool(_ context.Context, name string, args map[string]interface{}) (interface{}, error) {
	idx := helpindex.Load()
	switch name {
	case "help_search":
		query, _ := args["q"].(string)
		pageKey, _ := args["pageKey"].(string)
		limit := 5
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}
		results := idx.Search(strings.TrimSpace(query), strings.TrimSpace(pageKey), limit)
		return map[string]interface{}{"results": results, "count": len(results)}, nil
	case "help_get_page":
		pageKey, _ := args["pageKey"].(string)
		pageKey = strings.TrimSpace(pageKey)
		if pageKey == "" {
			return nil, fmt.Errorf("help_get_page: pageKey is required")
		}
		info, docs := idx.DocsForPage(pageKey)
		return map[string]interface{}{
			"pageKey": info.PageKey,
			"status":  info.Status,
			"title":   info.Title,
			"docs":    docs,
		}, nil
	case "help_read_doc":
		path, _ := args["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, fmt.Errorf("help_read_doc: path is required")
		}
		doc, ok := idx.ReadDoc(path)
		if !ok {
			return nil, fmt.Errorf("help_read_doc: doc not found: %s", path)
		}
		return doc, nil
	case "help_list_gaps":
		coverage, totals := idx.Coverage()
		return map[string]interface{}{
			"gaps":     idx.Gaps(),
			"coverage": coverage,
			"totals":   totals,
		}, nil
	default:
		return nil, fmt.Errorf("unknown help tool: %s", name)
	}
}

// helpDraftContent 是帮助草稿产物的内容契约（bot_artifacts.content_json）。
type helpDraftContent struct {
	PageKey            string   `json:"pageKey"`
	Title              string   `json:"title"`
	Markdown           string   `json:"markdown"`
	Status             string   `json:"status"`
	Note               string   `json:"note,omitempty"`
	Revision           int      `json:"revision,omitempty"`
	KnowledgeArticleID int      `json:"knowledgeArticleId,omitempty"`
	BaselineMarkdown   string   `json:"baselineMarkdown,omitempty"`
	SourceDocs         []string `json:"sourceDocs,omitempty"`
	SubmittedAt        string   `json:"submittedAt,omitempty"`
	PublishedAt        string   `json:"publishedAt,omitempty"`
}

func (t *ToolRegistry) helpDraftForUser(ctx context.Context, tenantID, userID, artifactID int) (*ent.BotArtifact, helpDraftContent, error) {
	if t.artifacts == nil {
		return nil, helpDraftContent{}, fmt.Errorf("help 写面未启用（产物存储未注入）")
	}
	if userID <= 0 {
		return nil, helpDraftContent{}, fmt.Errorf("缺少发起人上下文（服务端未注入）")
	}
	artifact, err := t.artifacts.Get(ctx, tenantID, userID, artifactID)
	if err != nil {
		return nil, helpDraftContent{}, fmt.Errorf("帮助草稿不存在或不可访问")
	}
	if artifact.Kind != bot.ArtifactKindDraft ||
		(artifact.ToolName != "help_create_draft" && artifact.ToolName != "help_update_draft") {
		return nil, helpDraftContent{}, fmt.Errorf("产物不是帮助草稿")
	}
	content, err := decodeHelpDraftContent(artifact)
	if err != nil {
		return nil, helpDraftContent{}, err
	}
	return artifact, content, nil
}

// helpDraftForReview 允许具备 knowledge:write 的审阅者读取同租户草稿（含他人草稿）。
func (t *ToolRegistry) helpDraftForReview(ctx context.Context, tenantID, artifactID int) (*ent.BotArtifact, helpDraftContent, error) {
	if t.artifacts == nil {
		return nil, helpDraftContent{}, fmt.Errorf("help 写面未启用（产物存储未注入）")
	}
	artifact, err := t.artifacts.GetTenantDraft(ctx, tenantID, artifactID)
	if err != nil {
		return nil, helpDraftContent{}, fmt.Errorf("帮助草稿不存在或不可访问")
	}
	if artifact.Kind != bot.ArtifactKindDraft ||
		(artifact.ToolName != "help_create_draft" && artifact.ToolName != "help_update_draft") {
		return nil, helpDraftContent{}, fmt.Errorf("产物不是帮助草稿")
	}
	content, err := decodeHelpDraftContent(artifact)
	if err != nil {
		return nil, helpDraftContent{}, err
	}
	return artifact, content, nil
}

func decodeHelpDraftContent(artifact *ent.BotArtifact) (helpDraftContent, error) {
	var content helpDraftContent
	if err := json.Unmarshal([]byte(artifact.ContentJSON), &content); err != nil {
		return helpDraftContent{}, fmt.Errorf("帮助草稿内容损坏")
	}
	if content.Status == "" {
		content.Status = "draft"
	}
	if content.Revision <= 0 {
		content.Revision = 1
	}
	return content, nil
}

func (t *ToolRegistry) saveHelpDraft(ctx context.Context, tenantID, ownerUserID int, artifact *ent.BotArtifact, content helpDraftContent) (interface{}, error) {
	evidence := map[string]interface{}{
		"pageKey":    content.PageKey,
		"sourceDocs": content.SourceDocs,
		"status":     content.Status,
		"revision":   content.Revision,
	}
	title := strings.TrimSpace(content.Title)
	if title == "" {
		title = artifact.Title
	}
	updated, err := t.artifacts.UpdateDraft(ctx, tenantID, ownerUserID, artifact.ID, title, content, evidence)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"artifactId":         updated.ID,
		"kind":               updated.Kind,
		"title":              updated.Title,
		"status":             content.Status,
		"revision":           content.Revision,
		"knowledgeArticleId": content.KnowledgeArticleID,
	}, nil
}

func helpActorID(ctx context.Context, args map[string]interface{}) int {
	if id := intArg(args, "user_id"); id > 0 {
		return id
	}
	return ToolActorFromContext(ctx)
}

func (t *ToolRegistry) executeHelpUpdateDraft(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	artifactID := intArg(args, "artifactId")
	if artifactID <= 0 {
		return nil, fmt.Errorf("help_update_draft: artifactId 必填")
	}
	markdown := strings.TrimSpace(stringArg(args, "content"))
	if markdown == "" {
		return nil, fmt.Errorf("help_update_draft: content 必填")
	}
	userID := helpActorID(ctx, args)
	if userID <= 0 {
		return nil, fmt.Errorf("help_update_draft: 缺少发起人上下文")
	}
	artifact, content, err := t.helpDraftForUser(ctx, tenantID, userID, artifactID)
	if err != nil {
		return nil, err
	}
	if content.Status == "in_review" || content.Status == "published" {
		return nil, fmt.Errorf("help_update_draft: 审阅中/已发布的草稿不可直接修改")
	}
	content.Markdown = markdown
	if title := strings.TrimSpace(stringArg(args, "title")); title != "" {
		content.Title = title
	}
	content.Status = "draft"
	content.Revision++
	if raw, ok := args["sourceDocs"].([]interface{}); ok {
		content.SourceDocs = content.SourceDocs[:0]
		for _, item := range raw {
			if path, ok := item.(string); ok && strings.TrimSpace(path) != "" {
				content.SourceDocs = append(content.SourceDocs, strings.TrimSpace(path))
			}
		}
	}
	return t.saveHelpDraft(ctx, tenantID, userID, artifact, content)
}

func (t *ToolRegistry) executeHelpSubmitReview(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	artifactID := intArg(args, "artifactId")
	if artifactID <= 0 {
		return nil, fmt.Errorf("help_submit_review: artifactId 必填")
	}
	userID := helpActorID(ctx, args)
	if userID <= 0 {
		return nil, fmt.Errorf("help_submit_review: 缺少审阅人上下文")
	}
	artifact, content, err := t.helpDraftForReview(ctx, tenantID, artifactID)
	if err != nil {
		return nil, err
	}
	if content.Status == "in_review" && content.KnowledgeArticleID > 0 {
		return map[string]interface{}{
			"artifactId":         artifact.ID,
			"knowledgeArticleId": content.KnowledgeArticleID,
			"status":             content.Status,
			"idempotentReplay":   true,
		}, nil
	}
	if content.Status == "published" {
		return nil, fmt.Errorf("help_submit_review: 草稿已发布")
	}
	if t.helpPublisher == nil {
		return nil, fmt.Errorf("help_submit_review: 知识库写面未启用")
	}
	if strings.TrimSpace(content.BaselineMarkdown) == "" {
		idx := helpindex.Load()
		_, docs := idx.DocsForPage(content.PageKey)
		parts := make([]string, 0, len(docs))
		for _, doc := range docs {
			if strings.TrimSpace(doc.Text) != "" {
				parts = append(parts, doc.Text)
			}
		}
		content.BaselineMarkdown = strings.Join(parts, "\n\n")
	}
	articleID, err := t.helpPublisher.CreateHelpDraft(ctx, tenantID, artifact.OwnerUserID, content.Title, content.Markdown)
	if err != nil {
		return nil, fmt.Errorf("help_submit_review: 创建知识库草稿失败: %w", err)
	}
	content.Status = "in_review"
	content.KnowledgeArticleID = articleID
	content.SubmittedAt = time.Now().UTC().Format(time.RFC3339)
	return t.saveHelpDraft(ctx, tenantID, artifact.OwnerUserID, artifact, content)
}

func (t *ToolRegistry) executeHelpPublish(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	artifactID := intArg(args, "artifactId")
	if artifactID <= 0 {
		return nil, fmt.Errorf("help_publish: artifactId 必填")
	}
	userID := helpActorID(ctx, args)
	if userID <= 0 {
		return nil, fmt.Errorf("help_publish: 缺少发布人上下文")
	}
	artifact, content, err := t.helpDraftForReview(ctx, tenantID, artifactID)
	if err != nil {
		return nil, err
	}
	if content.Status == "published" {
		return map[string]interface{}{
			"artifactId":         artifact.ID,
			"knowledgeArticleId": content.KnowledgeArticleID,
			"status":             content.Status,
			"idempotentReplay":   true,
		}, nil
	}
	if content.Status != "in_review" || content.KnowledgeArticleID <= 0 {
		return nil, fmt.Errorf("help_publish: 草稿尚未提交审阅")
	}
	if t.helpPublisher == nil {
		return nil, fmt.Errorf("help_publish: 知识库发布面未启用")
	}
	changeLog := strings.TrimSpace(stringArg(args, "changeLog"))
	if changeLog == "" {
		changeLog = "帮助草稿发布：" + content.Title
	}
	if err := t.helpPublisher.PublishHelpDraft(ctx, tenantID, userID, content.KnowledgeArticleID, changeLog); err != nil {
		return nil, fmt.Errorf("help_publish: 发布知识库草稿失败: %w", err)
	}
	content.Status = "published"
	content.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	return t.saveHelpDraft(ctx, tenantID, artifact.OwnerUserID, artifact, content)
}

// executeHelpCreateDraft：页面帮助文档草稿产物（plan；**零业务写入、不发布**）。
//
// 归属由 ArtifactStore 保证（租户 + 发起人）；发布/审阅通道属 H1（KB 复核流 / PR 通道）。
func (t *ToolRegistry) executeHelpCreateDraft(ctx context.Context, tenantID int, args map[string]interface{}) (interface{}, error) {
	pageKey := strings.TrimSpace(stringArg(args, "pageKey"))
	if pageKey == "" {
		return nil, fmt.Errorf("help_create_draft: pageKey 必填")
	}
	contentBody := strings.TrimSpace(stringArg(args, "content"))
	if contentBody == "" {
		return nil, fmt.Errorf("help_create_draft: content 必填")
	}

	idx := helpindex.Load()
	info, _ := idx.DocsForPage(pageKey)
	title := strings.TrimSpace(stringArg(args, "title"))
	if title == "" {
		if info.Title != "" {
			title = "帮助文档草稿：" + info.Title
		} else {
			title = "帮助文档草稿：" + pageKey
		}
	}

	sourceDocs := make([]string, 0, 4)
	if raw, ok := args["sourceDocs"].([]interface{}); ok {
		for _, item := range raw {
			if path, ok := item.(string); ok && strings.TrimSpace(path) != "" {
				sourceDocs = append(sourceDocs, strings.TrimSpace(path))
			}
		}
	}

	content := map[string]interface{}{
		"pageKey":  pageKey,
		"title":    title,
		"markdown": contentBody,
		"status":   "draft",
		"revision": 1,
		"note":     "草稿产物；发布须走帮助文档既有流程（审核/评审），本工具不自动发布。",
	}
	evidence := map[string]interface{}{
		"pageKey":    pageKey,
		"pageStatus": info.Status,
		"sourceDocs": sourceDocs,
	}
	return t.saveArtifact(ctx, tenantID, bot.ArtifactKindDraft, "help_create_draft", title, content, evidence)
}
