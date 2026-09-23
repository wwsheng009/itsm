// Package sanitize 提供富文本 HTML 的服务端兜底清洗。
//
// 白名单与前端 itsm-frontend/src/lib/rich-text/sanitize.ts 对齐
// （架构文档 docs/architecture/ticket-create-page-rich-input-optimization.md §5.3/§5.4）：
// 前端 DOMPurify 只做体验层防护，服务端必须二次清洗，不可信任客户端输入。
//
// 约束：
//   - a[href] 仅允许 http/https/mailto；
//   - img[src] 仅允许 http/https 或站内相对路径（以 / 开头），拒绝 data: 与协议相对 //；
//   - 其余标签/属性（script、on*、style 等）一律剥离。
package sanitize

import (
	"html"
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

var (
	policyOnce sync.Once
	policy     *bluemonday.Policy

	// hrefPattern 只放行 http/https/mailto 绝对 URL；相对路径、协议相对 //、
	// javascript:/data:/vbscript: 等一律拒绝（后续 AllowURLSchemes 再兜底一层）。
	hrefPattern = regexp.MustCompile(`(?i)^(?:https?://[^\s]+|mailto:[^\s]+)$`)

	// imgSrcPattern 只放行 http/https 或站内相对路径。
	// RE2 不支持环视，这里用「/ 后紧跟的字符不能是 / 或 \」排除协议相对 // 与
	// 反斜杠变体 /\（浏览器会把 \ 归一化为 /），从而避免外链图片与追踪像素。
	imgSrcPattern = regexp.MustCompile(`(?i)^(?:https?://[^\s]+|/[^/\\\s][^\s]*)$`)

	// imgAlignPattern 限制图片对齐属性（前端富文本编辑器写入 data-align），
	// 只接受 left/center/right，避免任意值落库。
	imgAlignPattern = regexp.MustCompile(`(?i)^(?:left|center|right)$`)

	tagPattern    = regexp.MustCompile(`<[^>]*>`)
	imgTagPattern = regexp.MustCompile(`(?i)<img[\s>/]`)

	// 已清洗输出中的 <img> 整标签与其 src 属性（bluemonday 输出恒为双引号、值已转义）。
	imgTagFullPattern = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	imgSrcAttrPattern = regexp.MustCompile(`(?is)\bsrc\s*=\s*"([^"]*)"`)
)

// richTextPolicy 构建（并缓存）富文本白名单策略。
// bluemonday 的 Policy 在构建后是只读的，可安全并发复用。
func richTextPolicy() *bluemonday.Policy {
	policyOnce.Do(func() {
		p := bluemonday.NewPolicy()

		// 块级与行内标签白名单（与前端 RICH_TEXT_ALLOWED_TAGS 一致）。
		p.AllowElements(
			"p", "br", "strong", "b", "em", "i", "u", "s",
			"h1", "h2", "h3", "ul", "ol", "li", "blockquote", "code", "pre", "hr",
			"table", "thead", "tbody", "tr", "th", "td",
		)

		// 链接：href 仅 http/https/mailto；rel/target 由策略强制注入，不信任客户端值。
		p.AllowAttrs("href").Matching(hrefPattern).OnElements("a")
		p.AllowAttrs("rel", "target").OnElements("a")
		p.RequireNoReferrerOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)

		// 图片：src 仅 http/https 或站内相对路径；data-attachment-id 用于回链附件；
		// width/height 承载富文本图片缩放尺寸，data-align 承载 left/center/right 对齐。
		p.AllowAttrs("src").Matching(imgSrcPattern).OnElements("img")
		p.AllowAttrs("alt", "title", "width", "height", "data-attachment-id").OnElements("img")
		p.AllowAttrs("data-align").Matching(imgAlignPattern).OnElements("img")

		p.AllowURLSchemes("http", "https", "mailto")
		// 允许站内相对路径（img src 以 / 开头）；协议相对 // 已被 imgSrcPattern 拒绝。
		p.AllowRelativeURLs(true)

		policy = p
	})
	return policy
}

// SanitizeRichTextHTML 对富文本 HTML 执行白名单清洗。
// 清洗后仅剩空白内容（无可读文本且无图片）时返回空串，调用方据此回退为纯文本语义。
func SanitizeRichTextHTML(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	clean := richTextPolicy().Sanitize(input)
	clean = stripImagesWithDisallowedSrc(clean)
	if isEffectivelyEmpty(clean) {
		return ""
	}
	return clean
}

// stripImagesWithDisallowedSrc 移除 src 缺失或不在白名单内的 <img> 整标签。
// bluemonday 只会剥离非法的 src 属性而保留 <img>，与前端 hardenCleanHtml
// 「非法图片整元素移除」的语义不一致，这里补齐，避免留下无来源的图片占位。
func stripImagesWithDisallowedSrc(sanitized string) string {
	if !strings.Contains(strings.ToLower(sanitized), "<img") {
		return sanitized
	}
	return imgTagFullPattern.ReplaceAllStringFunc(sanitized, func(tag string) string {
		m := imgSrcAttrPattern.FindStringSubmatch(tag)
		if m == nil {
			return ""
		}
		src := strings.TrimSpace(html.UnescapeString(m[1]))
		if src == "" || !imgSrcPattern.MatchString(src) {
			return ""
		}
		return tag
	})
}

// isEffectivelyEmpty 判断已清洗 HTML 是否为空。
// 与前端 isRichTextEmpty 语义对齐：仅空白文本视为空；仅含图片（<img>）视为非空。
func isEffectivelyEmpty(sanitized string) bool {
	if sanitized == "" {
		return true
	}
	if imgTagPattern.MatchString(sanitized) {
		return false
	}
	text := html.UnescapeString(tagPattern.ReplaceAllString(sanitized, ""))
	return strings.TrimSpace(text) == ""
}
