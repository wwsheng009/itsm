// Package knowledgecontent 统一知识库正文「内容类型」的取值与判定口径。
//
// 背景：知识库正文长期是单字段混存——早期 Markdown、后来的富文本 HTML、
// 以及导入的纯文本共用 `content`，落库时没有格式标记，渲染端只能靠启发式
// 猜测（前端 lib/rich-text/content-format.ts）。猜错的代价是肉眼可见的：
// Markdown 原文（## 标题、| 表格 |）被当成纯文本整段暴露，或反过来把
// HTML 当 Markdown 转义显示。
//
// 因此引入显式 content_type：
//   - text      纯文本：渲染保留换行，不解析任何标记
//   - markdown  Markdown：渲染走 CommonMark/GFM（标题/表格/代码块/任务列表）
//   - html      原始 HTML：按 HTML 语义渲染（白名单净化后）
//   - rich_text 富文本 HTML（编辑器链路，含附件图片引用）：渲染/编辑走富文本组件
//
// 空值语义是「历史数据、类型未知」：由 Resolve 按内容形态兜底判定，
// 保证未回填的老文章渲染行为与引入本字段之前完全一致。
package knowledgecontent

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	TypeText     = "text"
	TypeMarkdown = "markdown"
	TypeHTML     = "html"
	TypeRichText = "rich_text"
)

// blockTagRe 块级 / 结构性标签：命中即认为正文是 HTML。
// 仅含 <br> 等内联标签的文本仍按 Markdown 处理——Markdown 允许内联 HTML，
// 误判会把整篇 Markdown 塞进富文本编辑器，反而丢掉标题与列表语义。
// 口径与前端 isHtmlContent 保持一致，避免前后端判定分叉。
var blockTagRe = regexp.MustCompile(
	`(?i)<(p|div|h[1-6]|ul|ol|li|blockquote|pre|table|thead|tbody|tr|td|th|figure|figcaption|hr|img)\b[^>]*>`,
)

// Normalize 归一化显式传入的类型值；无法识别时返回空串。
// 兼容别名（md / richtext / rich-text），避免调用方因写法差异被拒。
func Normalize(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TypeText:
		return TypeText
	case TypeMarkdown, "md":
		return TypeMarkdown
	case TypeHTML:
		return TypeHTML
	case TypeRichText, "richtext", "rich-text":
		return TypeRichText
	default:
		return ""
	}
}

// Parse 校验显式传入的类型：空值合法（表示「由服务端自动判定」），
// 非法值返回错误，避免脏值静默落库后在渲染端变成未知分支。
func Parse(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	if v := Normalize(raw); v != "" {
		return v, nil
	}
	return "", fmt.Errorf(
		"正文类型仅支持 %s/%s/%s/%s，收到: %s",
		TypeText, TypeMarkdown, TypeHTML, TypeRichText, raw,
	)
}

// Detect 按内容形态兜底判定：HTML 结构 → rich_text，其余 → markdown，空内容 → text。
func Detect(content string) string {
	if strings.TrimSpace(content) == "" {
		return TypeText
	}
	if blockTagRe.MatchString(content) {
		return TypeRichText
	}
	return TypeMarkdown
}

// Resolve 给出最终生效类型：显式值优先，缺失 / 非法时按内容兜底。
// 读取路径（响应映射）与写入路径（落库前归一）都必须走这里，
// 否则前端拿到的类型与实际内容可能不是一套口径。
func Resolve(stored, content string) string {
	if v := Normalize(stored); v != "" {
		return v
	}
	return Detect(content)
}
