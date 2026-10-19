package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
)

// I2-02 合规：会话导出（只读快照）。
//
// 契约：
//   - 租户内可导出（`ai:read`；与 `GET /ai/conversations/:id` 同一边界——跨租户表现为不存在）；
//   - 返回 Markdown 快照（前端据此生成 `.md` 下载）：标题/导出时间/消息数 + 逐条角色与时间；
//   - AI 回答若为结构化 JSON（RAG 载荷），优先抽取 answer/content 文本，避免导出无可读内容。

// ConversationExport 会话导出结果（JSON 契约）。
type ConversationExport struct {
	ConversationID int       `json:"conversationId"`
	Title          string    `json:"title"`
	ExportedAt     time.Time `json:"exportedAt"`
	MessageCount   int       `json:"messageCount"`
	Markdown       string    `json:"markdown"`
}

const (
	exportRoleUser      = "user"
	exportRoleAssistant = "assistant"
)

var exportRoleLabels = map[string]string{
	exportRoleUser:      "用户",
	exportRoleAssistant: "AI",
	"system":            "系统",
	"tool":              "工具",
}

// renderExportContent 渲染单条消息内容：结构化 JSON → 抽取可读文本；否则原文。
func renderExportContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") {
		return content
	}
	var payload struct {
		Answer  string `json:"answer"`
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return content
	}
	switch {
	case payload.Answer != "":
		return payload.Answer
	case payload.Content != "":
		return payload.Content
	case payload.Message != "":
		return payload.Message
	default:
		return content
	}
}

// ExportConversation 生成会话的 Markdown 快照（租户内；不存在/跨租户 → 错误）。
func (s *Service) ExportConversation(ctx context.Context, tenantID, convID int) (*ConversationExport, error) {
	conv, err := s.repo.GetConversation(ctx, convID, tenantID)
	if err != nil {
		return nil, err
	}
	messages, err := s.repo.GetMessages(ctx, convID)
	if err != nil {
		return nil, err
	}

	exportedAt := time.Now()
	title := strings.TrimSpace(conv.Title)
	if title == "" {
		title = fmt.Sprintf("会话 #%d", conv.ID)
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("# %s\n\n", title))
	builder.WriteString(fmt.Sprintf("- 会话 ID：%d\n", conv.ID))
	builder.WriteString(fmt.Sprintf("- 导出时间：%s\n", exportedAt.Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("- 消息数：%d\n\n", len(messages)))
	builder.WriteString("> 导出内容为会话原文快照（含 AI 回答）；请按内部合规要求留存。\n\n---\n\n")

	for _, message := range messages {
		role := message.Role
		if role == "" {
			role = exportRoleAssistant
		}
		label, ok := exportRoleLabels[role]
		if !ok {
			label = role
		}
		builder.WriteString(fmt.Sprintf("## %s · %s\n\n%s\n\n", label, message.CreatedAt.Format(time.RFC3339), renderExportContent(message.Content)))
	}

	return &ConversationExport{
		ConversationID: conv.ID,
		Title:          title,
		ExportedAt:     exportedAt,
		MessageCount:   len(messages),
		Markdown:       builder.String(),
	}, nil
}

// ExportConversation handles GET /api/v1/ai/conversations/:id/export
//
// 只读导出；跨租户与不存在同口径 404（不泄漏存在性）。
func (h *Handler) ExportConversation(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "无效的会话ID")
		return
	}
	export, err := h.svc.ExportConversation(c.Request.Context(), tenantID, id)
	if err != nil {
		h.svc.logger.Warnw("导出会话失败", "error", err, "tenantID", tenantID, "conversationID", id)
		common.NotFound(c, "会话不存在或无权导出")
		return
	}
	common.Success(c, export)
}
