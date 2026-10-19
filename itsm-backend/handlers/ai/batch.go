package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/ticket"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// V1-01 列表页 AI 批量助手（方案 §7 V1-01：选中 N 行 → 建议分类 / 批量草稿 / 批量分派）。
//
// 口径：
//   - 两个端点均为**只读生成**：分类/优先级/建议处理人与草稿文本只作为建议返回；
//     采纳与分派由列表页逐行调用既有写端点（PUT /tickets/:id、POST /tickets/:id/assign）
//     完成，绝不自动提交（沿用 I1-01「Inline Apply」写边界）；
//   - 租户隔离：`ticket.IDIn(ids) + ticket.TenantID(tenantID)` 同查；缺失/跨租户 id 逐项
//     返回 status=not_found（不泄露存在性），与 ReplyDraft 的 404 同口径；
//   - 批量上限 fail-visible：分诊 20 / 草稿 10（LLM 逐条调用）；空集/超限 400；
//   - 有界并发（3）保序返回；单条失败只降级该条（degraded=true），不整包失败；
//   - 审计：ticket_batch_triage / ticket_batch_reply_draft 落 ai_analysis_results。
const (
	batchTriageMaxTickets     = 20
	batchReplyDraftMaxTickets = 10
	batchAIWorkers            = 3

	batchTriageAnalysisType     = "ticket_batch_triage"
	batchReplyDraftAnalysisType = "ticket_batch_reply_draft"
)

// batchAIRequest 是批量端点的请求体（两个端点同形）。
type batchAIRequest struct {
	TicketIDs []int `json:"ticketIds" binding:"required"`
}

// batchTriageItem 是单条分诊建议（status: ok | not_found）。
type batchTriageItem struct {
	TicketID    int     `json:"ticketId"`
	Title       string  `json:"title,omitempty"`
	Status      string  `json:"status"`
	Category    string  `json:"category,omitempty"`
	Priority    string  `json:"priority,omitempty"`
	Urgency     string  `json:"urgency,omitempty"`
	AssigneeID  int     `json:"assigneeId,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Explanation string  `json:"explanation,omitempty"`
}

// batchReplyDraftItem 是单条答复草稿（status: ok | failed | not_found）。
type batchReplyDraftItem struct {
	TicketID  int                  `json:"ticketId"`
	Status    string               `json:"status"`
	Draft     string               `json:"draft,omitempty"`
	Citations []replyDraftCitation `json:"citations,omitempty"`
	LatencyMs int                  `json:"latencyMs,omitempty"`
	// Reason 是失败时的用户可见原因（不泄露上游错误细节）。
	Reason string `json:"reason,omitempty"`
}

// BatchTriage handles POST /api/v1/ai/tickets/batch-triage
// 对选中的工单批量生成分类/优先级/建议处理人（只读，不写业务字段）。
func (h *Handler) BatchTriage(c *gin.Context) {
	var req batchAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.BindValidationError(c, err, "请求参数错误")
		return
	}
	tenantID, userID, _, ok := batchAIIdentity(c)
	if !ok {
		return
	}
	ids, ok := normalizeBatchTicketIDs(c, req.TicketIDs, batchTriageMaxTickets)
	if !ok {
		return
	}
	if h.svc == nil || h.svc.entClient == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 数据服务未就绪")
		return
	}
	if h.svc.triageService == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 分诊服务未就绪")
		return
	}

	ctx := service.WithLLMTaskType(c.Request.Context(), service.LLMTaskBatch)
	found, ok := h.loadBatchTickets(c, ctx, tenantID, ids)
	if !ok {
		return
	}

	items := make([]batchTriageItem, len(ids))
	jobs := make(chan struct {
		idx int
		row *ent.Ticket
	})
	var wg sync.WaitGroup
	startedAt := time.Now()
	for w := 0; w < batchAIWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				res := h.svc.triageService.SuggestForTenant(ctx, job.row.Title, job.row.Description, tenantID)
				items[job.idx] = batchTriageItem{
					TicketID:    job.row.ID,
					Title:       job.row.Title,
					Status:      "ok",
					Category:    res.Category,
					Priority:    res.Priority,
					Urgency:     h.svc.determineUrgency(res.Priority),
					AssigneeID:  res.AssigneeID,
					Confidence:  res.Confidence,
					Explanation: res.Explanation,
				}
			}
		}()
	}
	foundCount := 0
	for i, id := range ids {
		row, exists := found[id]
		if !exists {
			items[i] = batchTriageItem{TicketID: id, Status: "not_found"}
			continue
		}
		foundCount++
		jobs <- struct {
			idx int
			row *ent.Ticket
		}{idx: i, row: row}
	}
	close(jobs)
	wg.Wait()
	latencyMs := int(time.Since(startedAt).Milliseconds())

	processed := 0
	for _, item := range items {
		if item.Status == "ok" {
			processed++
		}
	}
	h.svc.persistAIResult(ctx, batchTriageAnalysisType, tenantID, userID,
		fmt.Sprintf("batch triage request tickets=%d", len(ids)),
		gin.H{"ticketIds": ids, "processed": processed, "latencyMs": latencyMs},
		"triage", latencyMs, 0, nil)

	common.Success(c, gin.H{
		"items":     items,
		"processed": processed,
		"degraded":  processed < foundCount,
		"latencyMs": latencyMs,
	})
}

// BatchReplyDrafts handles POST /api/v1/ai/tickets/batch-reply-drafts
// 对选中的工单批量生成答复草稿（只读；草稿不落任何业务字段、不自动评论）。
func (h *Handler) BatchReplyDrafts(c *gin.Context) {
	var req batchAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.BindValidationError(c, err, "请求参数错误")
		return
	}
	tenantID, userID, role, ok := batchAIIdentity(c)
	if !ok {
		return
	}
	ids, ok := normalizeBatchTicketIDs(c, req.TicketIDs, batchReplyDraftMaxTickets)
	if !ok {
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

	ctx := service.WithLLMTaskType(c.Request.Context(), service.LLMTaskBatch)
	found, ok := h.loadBatchTickets(c, ctx, tenantID, ids)
	if !ok {
		return
	}

	items := make([]batchReplyDraftItem, len(ids))
	jobs := make(chan struct {
		idx int
		row *ent.Ticket
	})
	var wg sync.WaitGroup
	startedAt := time.Now()
	for w := 0; w < batchAIWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				items[job.idx] = h.generateOneReplyDraft(ctx, tenantID, userID, role, job.row)
			}
		}()
	}
	succeeded := 0
	foundCount := 0
	for i, id := range ids {
		row, exists := found[id]
		if !exists {
			items[i] = batchReplyDraftItem{TicketID: id, Status: "not_found"}
			continue
		}
		foundCount++
		jobs <- struct {
			idx int
			row *ent.Ticket
		}{idx: i, row: row}
	}
	close(jobs)
	wg.Wait()
	latencyMs := int(time.Since(startedAt).Milliseconds())
	for _, item := range items {
		if item.Status == "ok" {
			succeeded++
		}
	}

	h.svc.persistAIResult(ctx, batchReplyDraftAnalysisType, tenantID, userID,
		fmt.Sprintf("batch reply draft request tickets=%d", len(ids)),
		gin.H{"ticketIds": ids, "succeeded": succeeded, "latencyMs": latencyMs},
		"llm", latencyMs, 0, nil)

	common.Success(c, gin.H{
		"items":     items,
		"succeeded": succeeded,
		"degraded":  succeeded < foundCount,
		"latencyMs": latencyMs,
	})
}

// generateOneReplyDraft 生成单条草稿（与 ReplyDraft 同 grounding/提示词，逐条隔离失败）。
func (h *Handler) generateOneReplyDraft(ctx context.Context, tenantID, userID int, role string, row *ent.Ticket) batchReplyDraftItem {
	item := batchReplyDraftItem{TicketID: row.ID, Status: "failed", Reason: "AI 草稿生成失败，请稍后重试"}
	citations := h.replyDraftCitations(ctx, tenantID, userID, role, row)
	prompt := buildReplyDraftPrompt(row, citations)
	startedAt := time.Now()
	out, err := h.svc.llmGateway.Chat(ctx, "", []service.LLMMessage{
		{Role: "system", Content: replyDraftSystemPrompt},
		{Role: "user", Content: prompt},
	})
	latencyMs := int(time.Since(startedAt).Milliseconds())
	if err != nil {
		if h.svc.logger != nil {
			h.svc.logger.Warnw("batch reply draft generation failed",
				"error", err, "ticketID", row.ID, "tenantID", tenantID)
		}
		return item
	}
	draft := strings.TrimSpace(out)
	if draft == "" {
		return item
	}
	item.Status = "ok"
	item.Draft = draft
	item.Citations = citations
	item.LatencyMs = latencyMs
	item.Reason = ""
	return item
}

// loadBatchTickets 加载租户内的目标工单（跨租户/缺失 id 由调用方按 not_found 处理）。
// ok=false 时已写错误响应。
func (h *Handler) loadBatchTickets(c *gin.Context, ctx context.Context, tenantID int, ids []int) (map[int]*ent.Ticket, bool) {
	rows, err := h.svc.entClient.Ticket.Query().
		Where(ticket.IDIn(ids...), ticket.TenantID(tenantID)).
		All(ctx)
	if err != nil {
		common.FailWithErr(c, err, "工单查询失败")
		return nil, false
	}
	found := make(map[int]*ent.Ticket, len(rows))
	for _, row := range rows {
		found[row.ID] = row
	}
	return found, true
}

// batchAIIdentity 提取并校验身份上下文（与 ReplyDraft 同口径）。
func batchAIIdentity(c *gin.Context) (tenantID, userID int, role string, ok bool) {
	tenantID = c.GetInt("tenant_id")
	userID = c.GetInt("user_id")
	role = c.GetString("role")
	if tenantID <= 0 || userID <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return 0, 0, "", false
	}
	return tenantID, userID, role, true
}

// normalizeBatchTicketIDs 去重 + 过滤非正数 + 上限校验；ok=false 时已写错误响应。
func normalizeBatchTicketIDs(c *gin.Context, raw []int, max int) ([]int, bool) {
	seen := make(map[int]struct{}, len(raw))
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		common.Fail(c, common.ParamErrorCode, "ticketIds 不能为空")
		return nil, false
	}
	if len(ids) > max {
		common.Fail(c, common.ParamErrorCode, fmt.Sprintf("单次最多处理 %d 张工单", max))
		return nil, false
	}
	return ids, true
}
