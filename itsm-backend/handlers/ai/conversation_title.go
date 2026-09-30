package ai

import "strings"

// defaultConversationTitle 无法从用户输入推导标题时的兜底文案。
const defaultConversationTitle = "AI 会话"

// conversationTitleMaxRunes 会话标题长度上限（按 Unicode 码点计，
// 中英文 / emoji 混排都不会截断半个字符）。
const conversationTitleMaxRunes = 30

// conversationTitleEllipsis 截断标记。
const conversationTitleEllipsis = "…"

// deriveConversationTitle 从首条用户 prompt 推导会话标题，让历史列表可读：
//  1. 空白（含换行 / 制表 / 全角空格）折叠为单个半角空格，标题保持单行；
//  2. 超过 conversationTitleMaxRunes 按码点截断并追加省略号；
//  3. 输入为空（或纯空白）时兜底 defaultConversationTitle。
//
// 采用确定性推导而不是额外调用一次 LLM：标题在会话创建时立即可用，
// 不增加首轮对话延迟与 token 成本，LLM 不可用时标题仍能反映会话内容。
func deriveConversationTitle(query string) string {
	title := strings.Join(strings.Fields(query), " ")
	if title == "" {
		return defaultConversationTitle
	}

	runes := []rune(title)
	if len(runes) <= conversationTitleMaxRunes {
		return title
	}
	return string(runes[:conversationTitleMaxRunes]) + conversationTitleEllipsis
}
