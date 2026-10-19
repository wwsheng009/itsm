package ai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/ticket"
	"itsm-backend/handlers/common/knowledgeaccess"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// V0-01 工单详情「答复草稿」Inline 数据面（POST /api/v1/ai/tickets/:id/reply-draft）。
//
// 口径（方案 §7 V0-01：分诊建议 + 答复草稿 + 知识推荐，Inline 优先）：
//   - 只读生成：返回草稿文本与知识引用（citations），**不落任何业务字段**；采纳/提交仍由页面完成
//     （Inline Apply 只填入回复框，绝不自动提交）；
//   - grounding：工单标题/描述 + 知识库 TopN（复用 RAGService.Ask，访问者身份注入 → 分类可见性过滤）；
//   - 生成走 LLMGateway（空 model = 部署配置默认模型）；未装配 → 503 fail-visible；
//   - 审计：落 ai_analysis_results（场景 ticket_reply_draft），与既有 AI 分析同源。
//
// 该端点不经过工具链（无写动作），风险面 = 只读 LLM 调用 + 只读检索。

// replyDraftCitation 是草稿引用的知识片段（来源编号供正文引用）。
type replyDraftCitation struct {
	Title   string  `json:"title"`
	Source  string  `json:"source"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

// 草稿 grounding 的知识片段上限。
const replyDraftCitationLimit = 3

// ReplyDraft handles POST /api/v1/ai/tickets/:id/reply-draft
func (h *Handler) ReplyDraft(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "invalid ticket id")
		return
	}
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	role := c.GetString("role")
	if tenantID <= 0 || userID <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}
	if h.svc == nil || h.svc.entClient == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 数据服务未就绪")
		return
	}
	if h.svc.llmGateway == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 服务未就绪（LLM Provider 未配置）")
		return
	}

	t, err := h.svc.entClient.Ticket.Query().
		Where(ticket.ID(id), ticket.TenantID(tenantID)).
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			common.Fail(c, common.NotFoundCode, "工单不存在")
			return
		}
		common.FailWithErr(c, err, "工单查询失败")
		return
	}

	citations := h.replyDraftCitations(c.Request.Context(), tenantID, userID, role, t)
	prompt := buildReplyDraftPrompt(t, citations)

	startedAt := time.Now()
	out, err := h.svc.llmGateway.Chat(c.Request.Context(), "", []service.LLMMessage{
		{Role: "system", Content: replyDraftSystemPrompt},
		{Role: "user", Content: prompt},
	})
	latencyMs := int(time.Since(startedAt).Milliseconds())
	if err != nil {
		if h.svc.logger != nil {
			h.svc.logger.Warnw("reply draft generation failed",
				"error", err, "ticketID", id, "tenantID", tenantID)
		}
		common.Fail(c, common.ServiceUnavailableCode, "AI 草稿生成失败，请稍后重试")
		return
	}

	draft := strings.TrimSpace(out)
	h.svc.persistAIResult(c.Request.Context(), "ticket_reply_draft", tenantID, userID, prompt,
		gin.H{"ticketId": id, "draft": draft, "citations": len(citations)}, "llm", latencyMs, 0, nil)

	common.Success(c, gin.H{
		"ticketId":  id,
		"draft":     draft,
		"citations": citations,
		"latencyMs": latencyMs,
		"degraded":  false,
	})
}

const replyDraftSystemPrompt = "你是 ITSM 服务台助手。请基于工单信息与知识片段，起草一段可直接编辑的中文答复。" +
	"要求：3–6 句，礼貌、具体、给出下一步动作；不得承诺\"已解决/已修复\"等未发生事实；不得编造工单或知识中不存在的信息；" +
	"知识不足时，改为列出需要向用户补充确认的问题清单。只输出答复正文，不要附加解释。"

func buildReplyDraftPrompt(t *ent.Ticket, citations []replyDraftCitation) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("工单 #%d\n标题：%s\n描述：%s\n",
		t.ID, strings.TrimSpace(t.Title), strings.TrimSpace(t.Description)))
	if len(citations) == 0 {
		b.WriteString("\n（无可用知识片段；如信息不足请列出需确认的问题）\n")
		return b.String()
	}
	b.WriteString("\n知识片段（正文引用时标注 [编号]）：\n")
	for i, citation := range citations {
		b.WriteString(fmt.Sprintf("[%d] %s — %s\n", i+1, citation.Title, citation.Snippet))
	}
	return b.String()
}

// replyDraftCitations 检索工单相关的知识片段（best-effort：RAG 未装配/失败 → 空引用，不阻塞草稿生成）。
func (h *Handler) replyDraftCitations(ctx context.Context, tenantID, userID int, role string, t *ent.Ticket) []replyDraftCitation {
	out := make([]replyDraftCitation, 0)
	if h.svc == nil || h.svc.rag == nil || t == nil {
		return out
	}
	query := strings.TrimSpace(t.Title + " " + t.Description)
	if query == "" {
		return out
	}
	viewerCtx := knowledgeaccess.WithViewer(ctx, knowledgeaccess.Viewer{UserID: userID, Role: role})
	results, err := h.svc.rag.Ask(viewerCtx, tenantID, query, replyDraftCitationLimit)
	if err != nil {
		if h.svc.logger != nil {
			h.svc.logger.Warnw("reply draft grounding failed", "error", err, "tenantID", tenantID)
		}
		return out
	}
	for _, item := range results {
		title, _ := item["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		citation := replyDraftCitation{Title: title}
		if rawID, ok := item["id"]; ok {
			citation.Source = fmt.Sprintf("kb#%v", rawID)
		}
		if snippet, ok := item["snippet"].(string); ok {
			citation.Snippet = snippet
		}
		if score, ok := item["score"].(float64); ok {
			citation.Score = score
		}
		out = append(out, citation)
	}
	return out
}
