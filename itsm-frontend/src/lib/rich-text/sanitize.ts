/**
 * 富文本描述：净化 + 纯文本推导工具（唯一权威实现）
 *
 * 约束（架构规范 §4.3 / §4.5）：
 * - 写入后端前必须使用 DOMPurify 白名单净化，阻断 XSS；
 * - 渲染历史富文本时同样二次净化，禁止 script/iframe/on* / javascript:；
 * - 提交给工单 description/summary 的是「去标签 + 折叠空白 + 截断」的纯文本，
 *   列表页/搜索/通知无需改动渲染逻辑。
 *
 * 说明：本文件是富文本工具的唯一来源（htmlToPlainText / isRichTextEmpty /
 * isRichTextEnabled / PLAIN_TEXT_MAX_LENGTH 均在此定义），不要再另建副本。
 */
import DOMPurify from 'dompurify';

/** 纯文本摘要最大长度（后端 description/summary 字段） */
export const PLAIN_TEXT_MAX_LENGTH = 500;

/** 富文本白名单标签 */
export const RICH_TEXT_ALLOWED_TAGS = [
  'p',
  'br',
  'strong',
  'b',
  'em',
  'i',
  'u',
  's',
  'h1',
  'h2',
  'h3',
  'ul',
  'ol',
  'li',
  'blockquote',
  'code',
  'pre',
  'a',
  'img',
  'hr',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
];

/** 富文本白名单属性（data-attachment-id 用于回链附件） */
export const RICH_TEXT_ALLOWED_ATTR = [
  'href',
  'target',
  'rel',
  'src',
  'alt',
  'title',
  'width',
  'height',
  'data-attachment-id',
];

/** 允许的内联图片主机（逗号分隔，来自环境变量；相对路径始终允许） */
const ENV_IMAGE_HOSTS = (process.env.NEXT_PUBLIC_RICH_TEXT_IMAGE_HOSTS || '')
  .split(',')
  .map((h) => h.trim().toLowerCase())
  .filter(Boolean);

/**
 * 富文本功能开关。
 * NEXT_PUBLIC_RICH_TEXT=off 时回退到原有 Input.TextArea。
 */
export function isRichTextEnabled(): boolean {
  return (process.env.NEXT_PUBLIC_RICH_TEXT || 'on').toLowerCase() !== 'off';
}

const MAX_PLAIN_LENGTH = PLAIN_TEXT_MAX_LENGTH;

const BLOCK_END_RE = /<\/(p|div|li|h[1-6]|blockquote|tr|pre)>/gi;
const BR_RE = /<br\s*\/?>/gi;
const TAG_RE = /<[^>]+>/g;
const IMG_RE = /<img[\s>/]/i;

/** 常见实体解码（纯文本摘要可读性） */
function decodeBasicEntities(text: string): string {
  return text
    .replace(/&nbsp;/gi, ' ')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;|&apos;/gi, "'")
    .replace(/&amp;/gi, '&');
}

/**
 * HTML → 纯文本：
 * 块级结束标签与 <br> 转为换行，剥离其余标签，折叠空白，截断到 maxLength。
 */
export function htmlToPlainText(html: string, maxLength: number = MAX_PLAIN_LENGTH): string {
  if (!html) return '';

  const withBreaks = html
    .replace(BLOCK_END_RE, '\n')
    .replace(BR_RE, '\n');

  const text = decodeBasicEntities(withBreaks.replace(TAG_RE, ''))
    .replace(/[ \t\f\v]+/g, ' ')
    .replace(/ *\n */g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();

  if (text.length <= maxLength) return text;
  return text.slice(0, maxLength).replace(/\s+$/, '');
}

/**
 * 富文本是否为空：仅有空白文本视为空；仅含图片（<img>）视为非空。
 */
export function isRichTextEmpty(html: string | null | undefined): boolean {
  if (!html) return true;
  if (IMG_RE.test(html)) return false;
  return htmlToPlainText(html, Number.MAX_SAFE_INTEGER).length === 0;
}

function isAllowedImageSrc(src: string): boolean {
  const value = src.trim();
  if (!value) return false;
  // 相对路径（附件代理 /api/... 或站内静态资源）
  if (value.startsWith('/')) return true;
  // 站内绝对路径（同源 http(s)）
  try {
    const url = new URL(value, typeof window !== 'undefined' ? window.location.origin : 'http://localhost');
    if (typeof window !== 'undefined' && url.origin === window.location.origin) return true;
    return ENV_IMAGE_HOSTS.includes(url.host.toLowerCase());
  } catch {
    return false;
  }
}

/**
 * 白名单净化后二次加固：
 * - <a> 强制 rel="noopener noreferrer" target="_blank"；
 * - <img> 仅保留站内/附件代理/白名单主机，其余移除（防外链追踪与混合内容）。
 * 仅在浏览器环境生效（富文本渲染本身也只在客户端）。
 */
export function hardenCleanHtml(html: string): string {
  if (!html || typeof window === 'undefined' || typeof DOMParser === 'undefined') return html;

  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html');
  const root = doc.body.firstElementChild;
  if (!root) return html;

  root.querySelectorAll('a').forEach((a) => {
    a.setAttribute('rel', 'noopener noreferrer');
    a.setAttribute('target', '_blank');
  });

  root.querySelectorAll('img').forEach((img) => {
    if (!isAllowedImageSrc(img.getAttribute('src') || '')) {
      img.remove();
    }
  });

  return root.innerHTML;
}

/**
 * 富文本净化（写入与渲染共用入口）。
 */
export function sanitizeRichTextHtml(html: string): string {
  if (!html) return '';
  const clean = DOMPurify.sanitize(html, {
    ALLOWED_TAGS: RICH_TEXT_ALLOWED_TAGS,
    ALLOWED_ATTR: RICH_TEXT_ALLOWED_ATTR,
    ALLOW_DATA_ATTR: false,
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'style', 'form', 'input'],
    FORBID_ATTR: ['style', 'onerror', 'onload', 'onclick'],
  });
  return hardenCleanHtml(clean);
}
