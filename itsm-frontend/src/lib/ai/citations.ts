/**
 * 引用来源与正文引用的统一解析（结果落点「可验证」通道）。
 *
 * 背景：后端 RAG 上下文用 `【文档n】` 标注来源，SSE `sources` 按同一顺序返回；
 * 此前前端把标记与来源都当纯文本渲染，导致「点了没反应」。本模块把两者收敛到
 * 同一份 CitationTarget：
 *   - kb：知识文章 → `/knowledge/articles/:id`
 *   - help_doc：帮助文档 → `/ai/help-doc?path=<path>`（只读文档详情）
 *   - ticket / incident / ci / problem / change：对应详情路由
 *   - 其他/缺少 id：不给 href（宁可不渲染链接，也不生成猜测地址）
 *
 * 展示层同时负责剥离 `docs/help/` 前缀与 `.md` 后缀，避免把开发态路径铺到 UI。
 */
import type { RagAnswer } from '@/lib/api/ai-api';

export interface CitationTarget {
  /** 可点击目标；缺省表示当前来源没有已核实的详情地址。 */
  href?: string;
  /** UI 展示名（人类可读；不含目录前缀与扩展名）。 */
  displayName: string;
  /** 稳定标识（用于 React key / 埋点，不进入正文）。 */
  key: string;
}

const HELP_DOC_PREFIX = /^docs\/help\//i;
const MARKDOWN_SUFFIX = /\.md$/i;

/** `docs/help/incidents/create.md` → `incidents/create`；普通标题原样返回。 */
export const stripDocPath = (value?: string): string => {
  const text = (value ?? '').trim();
  if (!text) return '';
  return text.replace(HELP_DOC_PREFIX, '').replace(MARKDOWN_SUFFIX, '');
};

/** 判断一条来源是否为帮助文档（路径形态，或后端 objectType=help_doc）。 */
export const isHelpDocSource = (source: RagAnswer): boolean =>
  source.objectType === 'help_doc' ||
  source.objectType === 'help' ||
  /(^|\/)docs\/help\/.+\.md$/i.test(source.source ?? '');

/** 业务对象详情路由（仅包含已核实存在的路由）。 */
export const objectDetailRoute = (
  objectType: string | undefined,
  id: number | undefined
): string | undefined => {
  if (!objectType || typeof id !== 'number' || id <= 0) return undefined;
  switch (objectType.toLowerCase()) {
    case 'ticket':
      return `/tickets/${id}`;
    case 'incident':
      return `/incidents/${id}`;
    case 'problem':
      return `/problems/${id}`;
    case 'change':
      return `/changes/${id}`;
    case 'ci':
      return `/cmdb/cis/${id}`;
    case 'kb':
    case 'knowledge':
      return `/knowledge/articles/${id}`;
    default:
      return undefined;
  }
};

/** 把一条来源归一为可点击目标（kb / help_doc / 业务对象）。 */
export const citationTarget = (source: RagAnswer, index: number): CitationTarget => {
  const key = `${source.objectType ?? 'source'}:${source.source ?? source.id ?? index}`;
  if (isHelpDocSource(source)) {
    const path = (source.source ?? '').trim();
    const displayName = source.title?.trim() || stripDocPath(path) || `文档 ${index + 1}`;
    return {
      href: path ? `/ai/help-doc?path=${encodeURIComponent(path)}` : undefined,
      displayName,
      key,
    };
  }

  const href = objectDetailRoute(source.objectType, source.id);
  const displayName =
    source.title?.trim() ||
    stripDocPath(source.source) ||
    source.snippet?.trim().slice(0, 30) ||
    `来源 ${index + 1}`;
  return { href, displayName, key };
};

/** 文档序号（1-based）→ 来源；越界返回 undefined。 */
export const citationByOrdinal = (
  sources: readonly RagAnswer[] | undefined,
  ordinal: number
): RagAnswer | undefined => {
  if (!sources || !Number.isFinite(ordinal) || ordinal < 1) return undefined;
  return sources[ordinal - 1];
};

const CITATION_MARKER = /【文档(\d+)】/g;

/**
 * 把正文里的 `【文档n】` 转成 Markdown 链接（仅在 sources 中确有该序号时）。
 *
 * 边界：跳过 fenced code 与行内 code，避免把示例/命令里的标记误改成链接。
 */
export const linkifyCitationMarkers = (
  content: string,
  sources?: readonly RagAnswer[]
): string => {
  if (!content || !sources || sources.length === 0) return content;

  const lines = content.split('\n');
  let inFence = false;

  return lines
    .map(line => {
      if (/^\s*```/.test(line)) {
        inFence = !inFence;
        return line;
      }
      if (inFence) return line;

      // 行内 code 用反引号切分：偶数段是正文，奇数段原样保留。
      return line
        .split(/(`[^`]*`)/g)
        .map((segment, index) => {
          if (index % 2 === 1) return segment;
          return segment.replace(CITATION_MARKER, (marker, rawOrdinal: string) => {
            const ordinal = Number.parseInt(rawOrdinal, 10);
            const source = citationByOrdinal(sources, ordinal);
            if (!source) return marker;
            const target = citationTarget(source, ordinal - 1);
            if (!target.href) return marker;

            // 链接文本保持【文档n】形态，视觉上仍是引用标记；标题放到 title。
            const safeTitle = target.displayName.replace(/[\[\]()]/g, '');
            return `[${marker}](${target.href} "${safeTitle}")`;
          });
        })
        .join('');
    })
    .join('\n');
};
