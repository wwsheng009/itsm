/**
 * 富文本暂存图片（粘贴 / 拖拽）工具。
 *
 * 背景：创建页在后端工单尚不存在时，无法调用工单维度附件接口
 * （`POST /api/v1/tickets/:id/attachments`），因此先把图片以 `blob:` 占位插入编辑器，
 * 并通过 `data-attachment-id="staged-xxx"` 标记为「待上传」。
 * 提交拿到 ticketId 后：上传附件 → 用正式地址替换占位 → 再回写 descriptionHtml。
 *
 * 纯函数、无 React 依赖，便于单测覆盖边界（失败图片、无 id、混合标签）。
 * 见 docs/architecture/ticket-create-page-rich-input-optimization.md §4.5 / §5.2。
 */

import { extractPreservedImageAttrs } from './image-size';

export const STAGED_ID_PREFIX = 'staged-';

/** 提取 <img> 标签上的 data-attachment-id（仅当其为暂存 id 时返回） */
export function getStagedImageId(imgTag: string): string | null {
  const matched = imgTag.match(/data-attachment-id="([^"]+)"/i);
  if (!matched) return null;
  const id = matched[1];
  return id.startsWith(STAGED_ID_PREFIX) ? id : null;
}

/** 按出现顺序返回 HTML 中的暂存图片 id（去重） */
export function extractStagedImageIds(html: string): string[] {
  if (!html) return [];
  const ids: string[] = [];
  for (const tag of html.match(/<img\b[^>]*>/gi) || []) {
    const id = getStagedImageId(tag);
    if (id && !ids.includes(id)) ids.push(id);
  }
  return ids;
}

/**
 * 移除全部暂存图片标签。
 * 用于创建请求：`blob:` 地址对服务端无意义，且不应落库。
 */
export function stripStagedImages(html: string): string {
  if (!html) return '';
  return html.replace(/<img\b[^>]*>/gi, (tag) => (getStagedImageId(tag) ? '' : tag));
}

export interface StagedImageReplacement {
  /** 附件 ID（后端返回） */
  id: number | string;
  /** 可直接访问的附件地址 */
  url: string;
  /** 原始文件名，用于 alt 文本 */
  name?: string;
}

/**
 * 用上传结果替换暂存图片标签。
 * 未提供替换结果的暂存图片视为上传失败，直接移除标签（避免把 `blob:` 写进库）。
 *
 * 保留原标签上的 width / height / data-align / title：用户在编辑器里
 * 拖拽调整过的尺寸与对齐必须跟随替换后的正式图片一起落库。
 */
export function replaceStagedImages(
  html: string,
  replacements: Record<string, StagedImageReplacement>
): string {
  if (!html) return '';
  return html.replace(/<img\b[^>]*>/gi, (tag) => {
    const stagedId = getStagedImageId(tag);
    if (!stagedId) return tag;
    const hit = replacements[stagedId];
    if (!hit) return '';
    const alt = (hit.name || '图片').replace(/"/g, '&quot;');
    const preserved = extractPreservedImageAttrs(tag);
    return `<img src="${hit.url}" alt="${alt}" data-attachment-id="${String(hit.id)}"${preserved} />`;
  });
}

/** 是否仍存在暂存图片（提交后可用于判断是否需要回写 descriptionHtml） */
export function hasStagedImages(html: string): boolean {
  return extractStagedImageIds(html).length > 0;
}
