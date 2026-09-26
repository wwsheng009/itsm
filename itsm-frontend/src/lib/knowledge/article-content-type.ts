/**
 * 知识库正文「内容类型」模型（与后端 common/knowledgecontent 对齐）。
 *
 * 背景：文章 `content` 单字段混存 Markdown / 富文本 HTML / 纯文本，历史上没有类型标记，
 * 详情页只能靠块级标签启发式猜测（`isHtmlContent`）。猜错的表现很直观：
 * Markdown 文章把「## 标题」「| 表格 |」当普通段落整段暴露，或把 HTML 当 Markdown 转义。
 *
 * 本模块提供四件事：
 *   1. 类型词汇表：text / markdown / html / rich_text；
 *   2. `normalizeArticleContentType`：把后端 / 表单传入的值归一化（兼容别名与脏值）；
 *   3. `detectArticleContentType`：对没有类型标记的历史数据做兜底判定（与原启发式一致）；
 *   4. `resolveArticleContentType`：显式类型优先、缺失时兜底——渲染分发的唯一入口。
 *
 * 约定：后端响应始终返回「解析后的生效类型」，因此正常链路上显式值总是存在；
 * 兜底逻辑只服务于历史数据与本地未刷新的旧响应。
 */

import { isHtmlContent } from '@/lib/rich-text/content-format';

export type ArticleContentType = 'text' | 'markdown' | 'html' | 'rich_text';

export const ARTICLE_CONTENT_TYPE_LABELS: Record<ArticleContentType, string> = {
  text: '纯文本',
  markdown: 'Markdown',
  html: 'HTML',
  rich_text: '富文本',
};

export const ARTICLE_CONTENT_TYPE_HINTS: Record<ArticleContentType, string> = {
  text: '原样展示，保留换行，不解析任何标记',
  markdown: '支持标题、列表、表格、代码块等 Markdown 语法',
  html: '按 HTML 语义渲染（白名单净化后）',
  rich_text: '编辑器产出的富文本 HTML，支持站内图片引用',
};

/** 表单 / 切换器可选项（顺序即展示顺序）。 */
export const ARTICLE_CONTENT_TYPE_OPTIONS: Array<{ value: ArticleContentType; label: string }> = (
  ['rich_text', 'markdown', 'text', 'html'] as ArticleContentType[]
).map(value => ({ value, label: ARTICLE_CONTENT_TYPE_LABELS[value] }));

/** 别名归一化表：兼容历史写法（md / richtext / plain…）与大小写差异。 */
const CONTENT_TYPE_ALIASES: Record<string, ArticleContentType> = {
  text: 'text',
  plain: 'text',
  plaintext: 'text',
  markdown: 'markdown',
  md: 'markdown',
  html: 'html',
  rich_text: 'rich_text',
  richtext: 'rich_text',
  'rich-text': 'rich_text',
};

/** 归一化显式传入的类型；无法识别返回 null（调用方决定兜底或报错）。 */
export function normalizeArticleContentType(raw?: string | null): ArticleContentType | null {
  if (raw == null) return null;
  const key = String(raw).trim().toLowerCase();
  if (!key) return null;
  return CONTENT_TYPE_ALIASES[key] ?? null;
}

/**
 * 历史数据兜底判定：
 * - 空内容 → text；
 * - 命中块级 / 结构性 HTML 标签 → rich_text（沿用既有 isHtmlContent 口径）；
 * - 其余 → markdown（历史正文以 Markdown 为主，纯文本在 Markdown 渲染下也不会变形）。
 */
export function detectArticleContentType(content?: string | null): ArticleContentType {
  if (!content || !content.trim()) return 'text';
  return isHtmlContent(content) ? 'rich_text' : 'markdown';
}

/** 解析文章最终生效的正文类型：显式类型优先，缺失 / 非法时按内容兜底。 */
export function resolveArticleContentType(article?: {
  contentType?: string | null;
  content?: string | null;
} | null): ArticleContentType {
  return (
    normalizeArticleContentType(article?.contentType) ??
    detectArticleContentType(article?.content)
  );
}

/**
 * 编辑器形态：
 * - rich_text 走富文本编辑器（WYSIWYG，粘贴 / 拖拽图片即时上传）；
 * - markdown / html / text 走纯文本输入域——markdown 与 text 进富文本编辑器会丢语义；
 *   html 保留源码直编，避免 TipTap 归一化把导入的复杂结构（表格 / 锚点 / 内联样式）洗掉。
 */
export function editorKindForContentType(type: ArticleContentType): 'rich' | 'plain' {
  return type === 'rich_text' ? 'rich' : 'plain';
}
