/**
 * 富文本图片尺寸 / 对齐工具（纯函数，唯一权威实现）。
 *
 * 背景：`@tiptap/extension-image` 默认只支持 `src` / `alt` / `title`，
 * 图片节点没有 `width` / `height` 属性，也没有缩放 UI，导致「粘贴或上传后
 * 无法调整图片大小」。本文件提供尺寸解析、拖拽换算、属性保留等无副作用逻辑，
 * 供 `RichTextEditorResizableImage`（扩展 + NodeView）与单测复用。
 *
 * 存储约定（必须与 `lib/rich-text/sanitize.ts` 白名单一致）：
 * - 尺寸写入 `<img width="320">` / `height` **属性**，不写内联 `style`
 *   （DOMPurify 会剥离 style；详情页靠 CSS `height: auto` 保持宽高比）；
 * - 对齐写入 `data-align="left|center|right"`；
 * - 宽度支持像素数字（number 或 "320"）与百分比字符串（"50%"）。
 */

/** 图片宽度下限（px）：防止拖拽成 0 宽后无法再次选中 */
export const MIN_IMAGE_WIDTH = 40;

/** 对齐属性名（同时用于清洗白名单，避免两处字符串漂移） */
export const IMAGE_ALIGN_ATTR = 'data-align';

export const IMAGE_ALIGNS = ['left', 'center', 'right'] as const;

export type ImageAlign = (typeof IMAGE_ALIGNS)[number];

/** 图片宽度：number 表示像素，string 表示百分比（如 "50%"） */
export type ImageWidth = number | string;

/** 缩放预设（相对编辑器内容宽度），与气泡工具栏按钮一一对应 */
export const IMAGE_SIZE_PRESETS = [
  { key: '25', label: '25%', ratio: 0.25 },
  { key: '50', label: '50%', ratio: 0.5 },
  { key: '75', label: '75%', ratio: 0.75 },
  { key: '100', label: '100%', ratio: 1 },
] as const;

const PERCENT_RE = /^(\d+(?:\.\d+)?)%$/;

/** 解析百分比字符串，返回 0~100 的数值；非法或越界返回 null */
export function parsePercent(value: string): number | null {
  const matched = PERCENT_RE.exec(value.trim());
  if (!matched) return null;
  const percent = Number(matched[1]);
  if (!Number.isFinite(percent) || percent <= 0 || percent > 100) return null;
  return percent;
}

/**
 * 解析 HTML 上的 width / height 属性值。
 * 支持 `320` / `320px`（→ number，四舍五入）与 `50%`（→ "50%"）；非法返回 null。
 */
export function parseImageWidth(raw: string | null | undefined): ImageWidth | null {
  if (!raw) return null;
  const value = raw.trim();
  if (!value) return null;

  const percent = parsePercent(value);
  if (percent !== null) return `${percent}%`;

  const numeric = Number(value.replace(/px$/i, ''));
  if (!Number.isFinite(numeric) || numeric <= 0) return null;
  return Math.round(numeric);
}

/** 解析 data-align 属性；仅接受 left / center / right，其余返回 null */
export function parseImageAlign(raw: string | null | undefined): ImageAlign | null {
  if (!raw) return null;
  const value = raw.trim().toLowerCase();
  return (IMAGE_ALIGNS as readonly string[]).includes(value) ? (value as ImageAlign) : null;
}

/**
 * 宽度夹取：下限 MIN_IMAGE_WIDTH，上限为容器内容宽度（容器未知时不设上限）。
 * 上限的存在保证图片不会被拖出编辑区产生横向滚动。
 */
export function clampImageWidth(
  width: number,
  containerWidth: number,
  min: number = MIN_IMAGE_WIDTH
): number {
  const rounded = Math.round(width);
  const upper =
    Number.isFinite(containerWidth) && containerWidth > 0
      ? Math.max(min, Math.floor(containerWidth))
      : Math.max(min, rounded);
  return Math.min(Math.max(rounded, min), upper);
}

/** 将任意存储宽度换算为当前容器下的像素值（百分比按容器宽度换算） */
export function resolveImageWidthPx(
  width: ImageWidth | null | undefined,
  containerWidth: number,
  min: number = MIN_IMAGE_WIDTH
): number | null {
  if (width === null || width === undefined) return null;
  if (typeof width === 'number') return clampImageWidth(width, containerWidth, min);
  const percent = parsePercent(width);
  if (percent === null) return null;
  return clampImageWidth((containerWidth * percent) / 100, containerWidth, min);
}

/** 按比例换算像素宽度（预设按钮使用） */
export function percentToImageWidthPx(
  ratio: number,
  containerWidth: number,
  min: number = MIN_IMAGE_WIDTH
): number {
  return clampImageWidth(containerWidth * ratio, containerWidth, min);
}

export type ResizeHandle = 'nw' | 'ne' | 'sw' | 'se';

/**
 * 拖拽手柄换算新宽度：
 * - 东侧手柄（ne / se）向右拖变大，西侧手柄（nw / sw）向右拖变小；
 * - 结果按容器宽度与最小宽度夹取，保证不越界。
 */
export function resizeWidthFromDrag(params: {
  startWidth: number;
  deltaX: number;
  handle: ResizeHandle;
  containerWidth: number;
  min?: number;
}): number {
  const { startWidth, deltaX, handle, containerWidth, min = MIN_IMAGE_WIDTH } = params;
  const direction = handle === 'ne' || handle === 'se' ? 1 : -1;
  return clampImageWidth(startWidth + direction * deltaX, containerWidth, min);
}

/** 替换 <img> 时需原样保留的表现属性（宽度 / 高度 / 对齐 / 悬浮标题） */
const PRESERVED_IMAGE_ATTRS = ['width', 'height', IMAGE_ALIGN_ATTR, 'title'] as const;

/**
 * 从既有 `<img ...>` 标签中提取需要保留的属性串（含前导空格）。
 *
 * 用于创建页两段式上传：`replaceStagedImages` 会用附件正式地址重建 img 标签，
 * 若不显式保留，用户拖拽设置的 width / 对齐会在提交后丢失。
 */
export function extractPreservedImageAttrs(imgTag: string): string {
  if (!imgTag) return '';
  const attrs: string[] = [];
  for (const name of PRESERVED_IMAGE_ATTRS) {
    const matched = new RegExp(`\\s${name}\\s*=\\s*"([^"]*)"`, 'i').exec(imgTag);
    const value = matched?.[1]?.trim();
    if (value) attrs.push(`${name}="${value}"`);
  }
  return attrs.length > 0 ? ` ${attrs.join(' ')}` : '';
}
