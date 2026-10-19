package ai

import (
	"encoding/json"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/internal/helpindex"

	"github.com/gin-gonic/gin"
)

// T5-09a：帮助草稿审阅队列。
//
// 数据源为 bot_artifacts 中的 help_create_draft/help_update_draft 产物。
// 该端点按 `knowledge:write` 授权，可跨发起人读取同租户草稿（文档审阅场景）；
// 跨租户仍由 bot.ArtifactStore 的 tenant_id 条件收敛。

type helpDraftReviewItem struct {
	ArtifactID         int                   `json:"artifactId"`
	OwnerUserID        int                   `json:"ownerUserId"`
	PageKey            string                `json:"pageKey"`
	Title              string                `json:"title"`
	Status             string                `json:"status"`
	Revision           int                   `json:"revision"`
	KnowledgeArticleID int                   `json:"knowledgeArticleId,omitempty"`
	Markdown           string                `json:"markdown"`
	BaselineMarkdown   string                `json:"baselineMarkdown"`
	Diff               []helpDraftReviewLine `json:"diff"`
	SourceDocs         []string              `json:"sourceDocs,omitempty"`
	SubmittedAt        string                `json:"submittedAt,omitempty"`
	PublishedAt        string                `json:"publishedAt,omitempty"`
	CreatedAt          string                `json:"createdAt"`
}

type helpDraftReviewLine struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type helpDraftReviewContent struct {
	PageKey            string   `json:"pageKey"`
	Title              string   `json:"title"`
	Markdown           string   `json:"markdown"`
	Status             string   `json:"status"`
	Revision           int      `json:"revision"`
	KnowledgeArticleID int      `json:"knowledgeArticleId"`
	BaselineMarkdown   string   `json:"baselineMarkdown"`
	SourceDocs         []string `json:"sourceDocs"`
	SubmittedAt        string   `json:"submittedAt"`
	PublishedAt        string   `json:"publishedAt"`
}

// ListHelpDrafts handles GET /api/v1/ai/help/drafts.
func (h *Handler) ListHelpDrafts(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID <= 0 {
		common.AuthFailed(c, "租户信息缺失")
		return
	}
	if h == nil || h.svc == nil || h.svc.artifacts == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助草稿队列未就绪")
		return
	}
	rows, err := h.svc.artifacts.ListTenantHelpDrafts(c.Request.Context(), tenantID, 100)
	if err != nil {
		common.Fail(c, common.ServiceUnavailableCode, "读取帮助草稿失败")
		return
	}
	items := make([]helpDraftReviewItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, h.toHelpDraftReviewItem(row))
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) toHelpDraftReviewItem(row *ent.BotArtifact) helpDraftReviewItem {
	var content helpDraftReviewContent
	_ = json.Unmarshal([]byte(row.ContentJSON), &content)
	if content.Status == "" {
		content.Status = "draft"
	}
	if content.Revision <= 0 {
		content.Revision = 1
	}
	baseline := content.BaselineMarkdown
	if strings.TrimSpace(baseline) == "" {
		_, docs := helpindex.Load().DocsForPage(content.PageKey)
		parts := make([]string, 0, len(docs))
		for _, doc := range docs {
			if strings.TrimSpace(doc.Text) != "" {
				parts = append(parts, doc.Text)
			}
		}
		baseline = strings.Join(parts, "\n\n")
	}
	title := content.Title
	if title == "" {
		title = row.Title
	}
	return helpDraftReviewItem{
		ArtifactID:         row.ID,
		OwnerUserID:        row.OwnerUserID,
		PageKey:            content.PageKey,
		Title:              title,
		Status:             content.Status,
		Revision:           content.Revision,
		KnowledgeArticleID: content.KnowledgeArticleID,
		Markdown:           content.Markdown,
		BaselineMarkdown:   baseline,
		Diff:               helpDraftDiff(baseline, content.Markdown),
		SourceDocs:         content.SourceDocs,
		SubmittedAt:        content.SubmittedAt,
		PublishedAt:        content.PublishedAt,
		CreatedAt:          row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// helpDraftDiff 生成按行的轻量 diff（先删除、后新增）。
// 不引入第三方依赖；审阅只需看到内容变化，不要求最小编辑距离。
func helpDraftDiff(before, after string) []helpDraftReviewLine {
	beforeLines := splitHelpLines(before)
	afterLines := splitHelpLines(after)
	beforeCount := make(map[string]int, len(beforeLines))
	for _, line := range beforeLines {
		beforeCount[line]++
	}
	added := make([]helpDraftReviewLine, 0, len(afterLines))
	for _, line := range afterLines {
		if beforeCount[line] > 0 {
			beforeCount[line]--
			continue
		}
		added = append(added, helpDraftReviewLine{Type: "added", Content: line})
	}
	removed := make([]helpDraftReviewLine, 0)
	for _, line := range beforeLines {
		if beforeCount[line] > 0 {
			beforeCount[line]--
			removed = append(removed, helpDraftReviewLine{Type: "removed", Content: line})
		}
	}
	return append(removed, added...)
}

func splitHelpLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 500 {
		lines = lines[:500]
	}
	return lines
}
