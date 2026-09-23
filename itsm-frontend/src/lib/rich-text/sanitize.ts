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

import { IMAGE_ALIGN_ATTR } from './image-size';

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

/** 富文本白名单属性（data-attachment-id 用于回链附件；data-align 用于图片对齐） */
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
  IMAGE_ALIGN_ATTR,
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

/**
 * 提取富文本中的附件图片 ID（`<img data-attachment-id="123">`）。
 *
 * 用途：编辑态保存后对「被移除的图片」调用附件解绑接口（方案 §5.2 第 4 条），
 * 以及创建页两段式上传后的回填校验。
 */
export function extractAttachmentImageIds(html: string | null | undefined): number[] {
  if (!html || typeof DOMParser === 'undefined') return [];
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html');
  const ids = Array.from(doc.querySelectorAll('img[data-attachment-id]'))
    .map((img) => Number(img.getAttribute('data-attachment-id')))
    .filter((id) => Number.isFinite(id) && id > 0);
  return Array.from(new Set(ids));
}

function isAllowedImageSrc(src: string): boolean {
  const value = src.trim();
  if (!value) return false;

  // 创建页「先占位、后上传」的暂存图片是 blob: 本地预览地址（见 staged-images.ts）。
  // blob: 的 origin 由其内层 URL 决定（WHATWG URL 规范），这里显式解析，避免被当作外链移除，
  // 导致保存后图片凭空消失。只放行同源 blob，跨源 blob 仍移除；blob 地址不会落库
  // （提交时 stripStagedImages 移除占位图，后端 imgSrc 白名单也不接受 blob:）。
  if (value.startsWith('blob:')) {
    if (typeof window === 'undefined') return false;
    try {
      return new URL(value.slice('blob:'.length)).origin === window.location.origin;
    } catch {
      return false;
    }
  }

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
 * URI 协议白名单：与 DOMPurify 默认策略一致，额外放行 `blob:`。
 *
 * 必要性：DOMPurify 默认策略会在 rich-text 富文本里剥掉 `src="blob:..."`，而创建页
 * 粘贴/拖拽的图片在工单创建成功前只能是 blob: 占位地址；一旦 src 被剥掉，暂存图片
 * 就无法在提交后被替换成正式附件地址（表现为「保存后图片消失」）。
 * 真正的放行判定仍由 hardenCleanHtml 的同源校验（图片）与后端白名单（落库）负责。
 */
export const RICH_TEXT_ALLOWED_URI_REGEXP =
  /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp|blob):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i;

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
    ALLOWED_URI_REGEXP: RICH_TEXT_ALLOWED_URI_REGEXP,
    ALLOW_DATA_ATTR: false,
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'style', 'form', 'input'],
    FORBID_ATTR: ['style', 'onerror', 'onload', 'onclick'],
  });
  return hardenCleanHtml(clean);
}
