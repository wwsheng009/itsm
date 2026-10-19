package ai

import (
	"context"
	"encoding/json"
	"strings"

	"itsm-backend/service"
)

// 会话历史回放（2026-10-19 修复：流式聊天链路此前只把当前 query 送进 LLM，
// 导致模型每轮都是"失忆"状态，无法理解"他/它/上一步/我叫什么"这类指代）。
//
// 除了连贯性，这里同样为 **provider 前缀缓存** 而设计：
//   - OpenAI/DeepSeek/Qwen 等 provider 按「消息前缀逐字节一致」判定 KV cache 命中；
//   - 因此历史回放必须是确定性的：同一批 DB 行每次重建得到完全相同的 role/content/顺序；
//   - 易变内容（本轮检索上下文、页面上下文）只能出现在消息序列末尾，绝不插到历史之前；
//   - 裁剪采用"高水位 → 一次性回收到低水位"的块式淘汰（整轮对齐），而不是每轮滑窗一格：
//     后者会让前缀每轮都变化，缓存永远无法命中。正常增长期是纯 append，前缀逐轮稳定。
const (
	// chatHistoryMaxMessages 历史消息条数高水位（24 条 = 12 轮 user+assistant）。
	chatHistoryMaxMessages = 24
	// chatHistoryKeepMessages 触发淘汰后保留的条数低水位（16 条 = 8 轮）。
	chatHistoryKeepMessages = 16
	// chatHistoryMaxChars 历史内容字符高水位（按 rune 计；预算约束，非 provider 计费口径）。
	chatHistoryMaxChars = 8000
	// chatHistoryKeepChars 触发淘汰后保留的字符低水位。
	chatHistoryKeepChars = 6000
	// chatHistoryMaxMessageChars 单条历史消息字符上限：防御用户一次粘贴几万字符把历史预算打满。
	chatHistoryMaxMessageChars = 2000
)

// loadChatHistory 读取 convID 的最近历史，转换为可回放的 LLM 消息前缀。
//
// 安全（fail-closed）：会话必须同时属于 tenantID 与 userID；跨用户/跨租户、会话不存在
// 或读取失败时一律返回 nil——绝不把归属不明的历史送进模型。历史读取失败不打断聊天主链路
// （与既有"持久化失败只告警"的容错语义一致），仅降级为无历史。
func (s *Service) loadChatHistory(ctx context.Context, tenantID, userID, convID int) []service.LLMMessage {
	if s == nil || s.repo == nil || convID <= 0 || tenantID <= 0 || userID <= 0 {
		return nil
	}
	conv, err := s.repo.GetConversation(ctx, convID, tenantID)
	if err != nil || conv == nil || conv.UserID != userID {
		if conv != nil && conv.UserID != userID && s.logger != nil {
			s.logger.Warnw("AI ChatStream: conversationId 不归属当前用户，历史回放已跳过",
				"tenant_id", tenantID, "user_id", userID, "conversation_id", convID)
		}
		return nil
	}
	stored, err := s.repo.GetMessages(ctx, convID)
	if err != nil {
		if s.logger != nil {
			s.logger.Debugw("AI ChatStream: 会话历史读取失败，按无历史继续",
				"tenant_id", tenantID, "conversation_id", convID, "error", err)
		}
		return nil
	}
	return buildChatHistory(stored)
}

// buildChatHistory 把持久化消息转换为稳定可回放的 user/assistant 序列。
//
// 确定性要求：同一批 DB 行必须得到逐字节相同的输出——这是 provider 前缀缓存命中的前提。
// 因此这里只做固定规则的解析（role 归一、assistant 信封解包、裁剪），不引入任何依赖
// 当前时间、随机数或请求上下文的加工。
func buildChatHistory(stored []*Message) []service.LLMMessage {
	if len(stored) == 0 {
		return nil
	}
	out := make([]service.LLMMessage, 0, len(stored))
	for _, msg := range stored {
		if msg == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(historyMessageContent(role, msg.Content))
		if content == "" {
			continue
		}
		out = append(out, service.LLMMessage{
			Role:    role,
			Content: truncateHistoryRunes(content, chatHistoryMaxMessageChars),
		})
	}
	return trimHistoryWatermark(out)
}

// historyMessageContent 还原单条消息进入 LLM 时的自然语言内容。
//
// assistant 的落库内容是 {"answer": "...", "sources": [...]} 信封（见 chatStreamInner 持久化
// 段）。若原样回放，模型会看到 JSON 噪音，且与生成时实际看到的内容不一致；这里固定解包
// answer 字段。其他结构化载荷（工具审批回执、legacy /ai/chat 的检索结果数组）没有自然语言
// 答案，跳过而不是把内部结构当作对话内容回放。以 {/[ 开头但不是合法 JSON 的普通文本按原文回放。
//
// 判定顺序必须是「先 json.Valid、后解包」：像 [{"title":"kb"}] 这类 legacy 数组是**合法 JSON**，
// 但解包到 envelope 结构会报错；若把解包错误统一当作"非 JSON 文本"回放，注入的就是内部结构。
func historyMessageContent(role, raw string) string {
	if role != "assistant" {
		return raw
	}
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return raw
	}
	if !json.Valid([]byte(trimmed)) {
		// 只是碰巧以 {/[ 开头的普通文本：按原文回放。
		return raw
	}
	var envelope struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		// 合法 JSON 但不是 {…"answer":…} 信封（如 legacy 检索结果数组）：没有自然语言内容。
		return ""
	}
	return envelope.Answer
}

// truncateHistoryRunes 按 rune 截断单条历史消息（保证 UTF-8 完整），追加省略号标记。
// 同一输入永远得到同一输出，不引入随机/时间因素。
func truncateHistoryRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// trimHistoryWatermark 执行两级水位淘汰（先字符、再条数），并保证历史以 user 消息开头。
func trimHistoryWatermark(msgs []service.LLMMessage) []service.LLMMessage {
	msgs = dropHistoryToCharWatermark(msgs)
	msgs = dropHistoryToMessageWatermark(msgs)
	// 裁剪边界落在 assistant 上说明它的提问已被裁掉：孤儿回复会误导模型，从头部丢弃，
	// 直到序列以 user 提问开始（正常数据下最多丢一条）。
	for len(msgs) > 0 && msgs[0].Role != "user" {
		msgs = msgs[1:]
	}
	return msgs
}

// dropHistoryToCharWatermark 字符预算淘汰：从最新往回保留，直到再收一条就超过低水位为止。
// 只有超过高水位才触发，且一次退到低水位，避免"每轮只丢一条"的滑窗式前缀漂移。
func dropHistoryToCharWatermark(msgs []service.LLMMessage) []service.LLMMessage {
	total := 0
	for _, m := range msgs {
		total += len([]rune(m.Content))
	}
	if total <= chatHistoryMaxChars {
		return msgs
	}
	kept := 0
	start := len(msgs)
	for start > 0 {
		size := len([]rune(msgs[start-1].Content))
		if kept+size > chatHistoryKeepChars {
			break
		}
		kept += size
		start--
	}
	if start == len(msgs) {
		// 兜底：低水位小于单条消息上限时至少保留最新一条（当前常量下不会走到）。
		start = len(msgs) - 1
	}
	return msgs[start:]
}

// dropHistoryToMessageWatermark 条数预算淘汰：超过高水位时一次性回收到低水位。
func dropHistoryToMessageWatermark(msgs []service.LLMMessage) []service.LLMMessage {
	if len(msgs) <= chatHistoryMaxMessages {
		return msgs
	}
	start := len(msgs) - chatHistoryKeepMessages
	if start < 0 {
		start = 0
	}
	return msgs[start:]
}
