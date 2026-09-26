/**
 * AI 助手回答 → 新建知识文章的路由预填契约（会话页「补充为知识文章」入口）。
 *
 * 背景：该入口此前只做一次裸跳转（`navigate('/knowledge/articles/create')`，不带任何数据），
 * 回答正文既没被采集、也没被承接，用户拿到的始终是一张空表单。本模块把两侧共用的契约
 * 收敛到一处，避免"改了一头、忘了另一头"：
 * - 会话页：`buildArticlePrefillState(markdown)` 生成路由 state；
 * - 新建页：`readArticlePrefillState(location.state)` 校验并取回预填。
 *
 * 正文按 **Markdown 原文** 携带：助手回答由 `MarkdownMessage`（react-markdown + remark-gfm）
 * 渲染，含标题 / 列表 / 表格 / 代码块；整篇塞进 TipTap 会被解析成纯段落而丢语义（与 edit 页
 * 「HTML / Markdown 双读」策略一致），因此承接页在有预填时固定走 Markdown 正文，
 * 落库后由 `isHtmlContent` 判定为 Markdown，详情页继续用 react-markdown 渲染。
 *
 * React Router 的 `location.state` 不受类型约束（刷新后来自 `history.state`），读取一律走校验，
 * 结构不符即当作"没有预填"，退化成普通空白新建，不抛错。
 */

/** 预填来源标识：只认自家会话页投递的 state，避免误吃其它页面的 state。 */
export const ARTICLE_PREFILL_SOURCE = 'ai-chat';

/** 标题兜底文案（回答里没有任何可提取文本时）。 */
export const ARTICLE_PREFILL_FALLBACK_TITLE = 'AI 助手回答';

/** 推导标题的长度上限（新建页标题输入框 maxLength=200，这里取更保守的 80）。 */
const TITLE_MAX_LENGTH = 80;

const IMAGE_RE = /!\[[^\]]*\]\([^)]*\)/g;
const LINK_RE = /\[([^\]]*)\]\([^)]*\)/g;
const CODE_FENCE_RE = /^(```|~~~)/;

/** 新建页预填数据。 */
export interface ArticlePrefill {
  /** 由回答推导的文章标题 */
  title: string;
  /** 回答 Markdown 原文 */
  content: string;
}

/** 路由 state 形状。 */
export interface ArticlePrefillState {
  source: typeof ARTICLE_PREFILL_SOURCE;
  prefill: ArticlePrefill;
}

/**
 * 从 Markdown 回答推导文章标题：取首个可用非空行（跳过代码围栏），剥掉常见 Markdown 标记后截断。
 * 逐级降级：ATX 标题 / 列表项 / 引用 / 表格行 → 普通段落 → 兜底文案；返回值永远非空。
 */
export function deriveArticleTitle(markdown: string, maxLength: number = TITLE_MAX_LENGTH): string {
  const firstLine = (markdown || '')
    .split('\n')
    .map(line => line.trim())
    .find(line => line !== '' && !CODE_FENCE_RE.test(line));
  if (!firstLine) return ARTICLE_PREFILL_FALLBACK_TITLE;

  const title = firstLine
    .replace(IMAGE_RE, '') // 图片整段去掉（先于链接，否则会剩下一个 `!`）
    .replace(LINK_RE, '$1') // 链接只保留可见文字
    .replace(/^#{1,6}\s*/, '') // ATX 标题
    .replace(/^>\s*/, '') // 引用
    .replace(/^([-*+]|\d+[.)])\s+/, '') // 列表项
    .replace(/[*_`~]/g, '') // 行内强调 / 代码标记
    .replace(/\|/g, ' ') // 表格竖线
    .replace(/\s+/g, ' ')
    .trim();

  if (!title) return ARTICLE_PREFILL_FALLBACK_TITLE;
  return title.length > maxLength ? `${title.slice(0, maxLength)}…` : title;
}

/** 生成路由 state（会话页调用）。 */
export function buildArticlePrefillState(markdown: string): ArticlePrefillState {
  return {
    source: ARTICLE_PREFILL_SOURCE,
    prefill: { title: deriveArticleTitle(markdown), content: markdown },
  };
}

/**
 * 校验并取回路由 state（新建页调用）。
 * 非对象 / 来源不符 / 缺少正文 → 返回 null（视为无预填）。
 * 标题缺失或为空时按正文重新推导，保证下游拿到的就是可提交的字段。
 */
export function readArticlePrefillState(state: unknown): ArticlePrefill | null {
  if (!state || typeof state !== 'object') return null;
  const record = state as Record<string, unknown>;
  if (record.source !== ARTICLE_PREFILL_SOURCE) return null;
  const raw = record.prefill;
  if (!raw || typeof raw !== 'object') return null;
  const { title, content } = raw as Record<string, unknown>;
  if (typeof content !== 'string' || !content.trim()) return null;
  return {
    title: typeof title === 'string' && title.trim() ? title : deriveArticleTitle(content),
    content,
  };
}
