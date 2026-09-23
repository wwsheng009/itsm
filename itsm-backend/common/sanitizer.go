package common

import (
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// HTML sanitizer 单例：UGC 策略适合知识库正文（保留常见富文本标签，剔除 script/on* 等 XSS 载体）
// StrictText 策略用于标题/摘要类字段：只保留纯文本
var (
	ugcPolicyOnce    sync.Once
	ugcPolicy        *bluemonday.Policy
	strictPolicyOnce sync.Once
	strictPolicy     *bluemonday.Policy

	// BE-7 净化对齐：内嵌图片的附件回链与展示属性取值受限（与
	// internal/sanitize 的富文本白名单保持一致）。
	attachmentIDAttrPattern = regexp.MustCompile(`^\d+$`)
	imageAlignAttrPattern   = regexp.MustCompile(`(?i)^(left|center|right)$`)
)

func UGCPolicy() *bluemonday.Policy {
	ugcPolicyOnce.Do(func() {
		p := bluemonday.UGCPolicy()
		// 允许 code 高亮常用 class 属性
		p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("code", "pre", "span", "div")
		// BE-7：`data-attachment-id` 是富文本图片回链附件（删除引用保护 / 宿主归属校验）
		// 的唯一锚点，`data-align` 承载编辑器对齐；UGCPolicy 默认剥离 data-*，
		// 若不显式放行，知识库正文里的引用会在落库时被静默抹掉。
		p.AllowAttrs("data-attachment-id").Matching(attachmentIDAttrPattern).OnElements("img")
		p.AllowAttrs("data-align").Matching(imageAlignAttrPattern).OnElements("img")
		ugcPolicy = p
	})
	return ugcPolicy
}

func StrictPolicy() *bluemonday.Policy {
	strictPolicyOnce.Do(func() {
		strictPolicy = bluemonday.StrictPolicy()
	})
	return strictPolicy
}

// SanitizeHTML 消毒富文本 HTML：剔除 <script>、on* 事件处理器、javascript: URL 等
// 用于知识库正文这类允许富文本的字段。
func SanitizeHTML(html string) string {
	if strings.TrimSpace(html) == "" {
		return html
	}
	return UGCPolicy().Sanitize(html)
}

// SanitizeText 消毒纯文本字段（标题/摘要）：完全剥离 HTML 标签
func SanitizeText(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return StrictPolicy().Sanitize(text)
}
