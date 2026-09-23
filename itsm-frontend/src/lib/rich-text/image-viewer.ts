/**
 * 详情页图片查看器（放大 / 缩放 / 旋转 / 翻转 / 拖动）的纯函数与常量。
 *
 * 背景：工单详情页（`/tickets/:id`）的富文本描述是只读回显（`.ticket-rich-text`），
 * 图片被 `max-width: 100%` 限制在正文宽度内，小字截图/长图无法看清。
 * 这里抽出与 DOM 无关的变换数学，供 `RichTextImageViewer` 组件复用并被单测覆盖。
 * 见 docs/architecture/ticket-create-page-rich-input-optimization.md §4.9。
 */

/** 缩放下限（10%）：允许缩小以查看长图全貌 */
export const MIN_IMAGE_SCALE = 0.1;

/** 缩放上限（800%）：避免放大到无意义的像素噪点 */
export const MAX_IMAGE_SCALE = 8;

/** 每次「放大 / 缩小」的倍率（等比，手感与系统看图器一致） */
export const IMAGE_SCALE_STEP = 1.25;

/** 旋转步进（度） */
export const IMAGE_ROTATION_STEP = 90;

export type ScaleDirection = 'in' | 'out';

export interface ImageOffset {
  x: number;
  y: number;
}

export interface ImageSize {
  width: number;
  height: number;
}

export interface FitScaleOptions {
  /** 是否为小图放大到铺满（默认 false：适应窗口但不超过原始尺寸） */
  allowUpscale?: boolean;
}

function roundTo(value: number, digits = 4): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

/** 夹取缩放到 [min, max]；非法输入回落到 1（原始尺寸） */
export function clampImageScale(
  scale: number,
  min: number = MIN_IMAGE_SCALE,
  max: number = MAX_IMAGE_SCALE
): number {
  if (!Number.isFinite(scale)) return 1;
  return roundTo(Math.min(max, Math.max(min, scale)));
}

/** 按倍率放大 / 缩小一步 */
export function nextImageScale(
  current: number,
  direction: ScaleDirection,
  step: number = IMAGE_SCALE_STEP
): number {
  const ratio = Number.isFinite(step) && step > 1 ? step : IMAGE_SCALE_STEP;
  const factor = direction === 'in' ? ratio : 1 / ratio;
  return clampImageScale(clampImageScale(current) * factor);
}

/** 归一化角度到 [0, 360) */
export function normalizeRotation(degrees: number): number {
  if (!Number.isFinite(degrees)) return 0;
  const normalized = degrees % 360;
  return normalized < 0 ? normalized + 360 : normalized;
}

/** 旋转一步（正数顺时针） */
export function rotateImage(degrees: number, delta: number = IMAGE_ROTATION_STEP): number {
  return normalizeRotation(normalizeRotation(degrees) + delta);
}

/** 是否处于 90° / 270°（此时图片的宽高在视觉上互换） */
export function isQuarterTurn(degrees: number): boolean {
  return normalizeRotation(degrees) % 180 !== 0;
}

/**
 * 「适应窗口」缩放比：在给定画布内完整显示图片，默认不把小于画布的图放大。
 * 任一尺寸缺失（jsdom / 图片未加载）时返回 1，调用方无需额外判空。
 */
export function computeFitScale(
  naturalWidth: number,
  naturalHeight: number,
  boxWidth: number,
  boxHeight: number,
  options: FitScaleOptions = {}
): number {
  const { allowUpscale = false } = options;
  if (!(naturalWidth > 0) || !(naturalHeight > 0) || !(boxWidth > 0) || !(boxHeight > 0)) return 1;

  const raw = Math.min(boxWidth / naturalWidth, boxHeight / naturalHeight);
  if (!Number.isFinite(raw) || raw <= 0) return 1;

  const bounded = allowUpscale ? Math.min(raw, MAX_IMAGE_SCALE) : Math.min(raw, 1);
  return clampImageScale(bounded);
}

/** 缩放比 → 展示文案（四舍五入到整数百分比） */
export function formatScalePercent(scale: number): string {
  return `${Math.round(clampImageScale(scale) * 100)}%`;
}

/**
 * 拖动平移的边界夹取：内容小于画布时不允许平移（居中显示），
 * 大于画布时最多平移到边缘贴合，避免把图片拖出视野。
 */
export function clampImageOffset(offset: ImageOffset, content: ImageSize, box: ImageSize): ImageOffset {
  const limitX = Math.max(0, (content.width - box.width) / 2);
  const limitY = Math.max(0, (content.height - box.height) / 2);
  const x = Number.isFinite(offset.x) ? Math.min(limitX, Math.max(-limitX, offset.x)) : 0;
  const y = Number.isFinite(offset.y) ? Math.min(limitY, Math.max(-limitY, offset.y)) : 0;
  return { x: roundTo(x, 2), y: roundTo(y, 2) };
}
