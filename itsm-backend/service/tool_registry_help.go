package service

import (
	"context"
	"fmt"
	"strings"

	"itsm-backend/internal/helpindex"
	"itsm-backend/service/bot"
)

// H0-03 Help Agent 只读工具（help_search / help_get_page / help_read_doc / help_list_gaps）。
//
// 口径（方案 §5.4）：
//   - 全部只读（Risk=read、ReadOnly=true）：不落业务库、无需确认/审批；
//   - 数据源为构建期内嵌索引（docs/help 真源），与 /ai/help/* 端点同源；
//   - 与场景 Bot 的绑定由授权决定：S3 知识自助助手默认授予（见 bot/scenarios.go），
//     管理员可通过 Bot 授权面调整；写面（help_submit_review/help_publish）属后续批次。
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
		"note":     "草稿产物；发布须走帮助文档既有流程（审核/评审），本工具不自动发布。",
	}
	evidence := map[string]interface{}{
		"pageKey":    pageKey,
		"pageStatus": info.Status,
		"sourceDocs": sourceDocs,
	}
	return t.saveArtifact(ctx, tenantID, bot.ArtifactKindDraft, "help_create_draft", title, content, evidence)
}
