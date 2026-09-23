/**
 * 正文形态判定（HTML / Markdown 双读兼容）。
 *
 * 背景：知识库文章的 `content` 字段历史上存 Markdown（`# 标题`、`- 列表`），FE-5 起新建 /
 * 富文本路径落库 HTML（TipTap 输出，含 `<p>`、`<img data-attachment-id>`）。两种格式共用
 * 同一字段且没有格式标记，因此渲染与编辑前必须做形态判定：
 * - HTML → `sanitizeRichTextHtml` 净化后渲染 / 交给 `RichTextEditor` 编辑；
 * - Markdown → 继续走 `react-markdown` + rehype-sanitize / `Input.TextArea`，保持既有观感。
 *
 * 判定口径（保守）：**块级 / 结构性标签**命中即视为 HTML。仅含 `<br>` 等内联标签的文本
 * 仍按 Markdown 处理——Markdown 允许内联 HTML，误判会把整篇 Markdown 塞进富文本编辑器，
 * 反而丢掉标题与列表语义。
 */

/** 块级 / 结构性标签：命中即认为正文是富文本 HTML */
const BLOCK_TAG_RE =
  /<(p|div|h[1-6]|ul|ol|li|blockquote|pre|table|thead|tbody|tr|td|th|figure|figcaption|hr|img)\b[^>]*>/i;

/**
 * 正文是否为富文本 HTML。
 * 空值（`''` / `null` / `undefined`）一律按「非 HTML」处理，由调用方决定空态展示。
 */
export function isHtmlContent(content?: string | null): boolean {
  if (!content) return false;
  return BLOCK_TAG_RE.test(content);
}
