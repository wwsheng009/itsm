package ai

import (
	"regexp"
	"strings"
)

// ordinalWriteReference 匹配没有稳定对象 ID 的序数/近指代。
//
// 只用于写工具的确定性兜底：出现这些说法时，即使模型已经生成了看似完整的
// create_ticket 参数，也必须先让用户确认具体对象，避免把“第二张单”猜成事件单。
var ordinalWriteReference = regexp.MustCompile(
	`(?i)(?:第[一二三四五六七八九十百零两0-9]+(?:张|个|条)(?:单|工单|请求|资产)?|上一张|上一条|那张单|这个单|那个单|这张单|随便|任意)`,
)

// stableObjectReference 是写操作可安全执行所需的稳定标识（编号/ID）。
// 出现稳定标识时，序数词通常只是叙述，不构成歧义。
var stableObjectReference = regexp.MustCompile(
	`(?i)(?:#\s*\d+|\b[A-Z]{2,}-\d{4,}(?:-\d+)?|CI[:：]\s*\d{3,}[:：]\s*\d+|(?:工单|资产|编号|ID)\s*[:：#]?\s*\d+)`,
)

// explicitWriteConfirmation 表示用户已明确确认某个候选（用于“第二张单是事件单，提交吧”）。
var explicitWriteConfirmation = regexp.MustCompile(
	`(?:确认|就按|按这个|按第[一二三四五六七八九十百零两0-9]+|是的|对的|提交吧|就这样)`,
)

// explicitWriteTarget 表示用户已给出可执行的类型或对象锚点。
// 仅有“确认”而没有类型/对象时仍应澄清：确认序数不等于确认了类型。
var explicitWriteTarget = regexp.MustCompile(
	`(?:事件单|服务请求|变更单|问题单|工单类型|CI[:：]?\s*\d+|\bID\s*[:：#]?\s*\d+)`,
)

// arbitraryChoice 表示仍然没有指定具体对象，必须继续澄清。
var arbitraryChoice = regexp.MustCompile(`随便|任意`)

// ambiguousWriteReason 返回写操作需要澄清的原文片段；空字符串表示未命中。
func ambiguousWriteReason(query string) string {
	trimmed := strings.TrimSpace(query)
	if stableObjectReference.MatchString(trimmed) {
		return ""
	}
	match := ordinalWriteReference.FindString(trimmed)
	if match == "" {
		return ""
	}
	if explicitWriteConfirmation.MatchString(trimmed) &&
		explicitWriteTarget.MatchString(trimmed) &&
		!arbitraryChoice.MatchString(trimmed) {
		return ""
	}
	return strings.TrimSpace(match)
}
