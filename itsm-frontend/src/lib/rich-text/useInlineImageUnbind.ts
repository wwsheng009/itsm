/**
 * 编辑态正文内嵌图片解绑（跨域统一收敛，方案 §5.2 第 4 条）
 *
 * 数据流（与 TicketDetail / 知识库文章编辑既有范式一致）：
 *  1. 宿主加载完成（编辑态）后 captureInlineImageBaseline(html) 记录基线图片附件 id；
 *  2. 主保存成功后调用 unbindRemovedInlineImages(host, html)：
 *     对当前 HTML 再次 extractAttachmentImageIds，差集即「被从正文里移除的图片」；
 *  3. 逐个 AttachmentApi.removeById(id, { ...host, usage: 'inline_image' })——有域内别名路由的
 *     宿主沿用宿主资源码（change:delete / incident:delete / …），不走通用 A5 的兜底码
 *     `attachment:delete`（该码仅 admin/sysadmin 持有，普通用户会 403）；
 *  4. 失败降级：Promise.allSettled + message.warning + console.warn，不阻断主保存，也不吞错误信息。
 *
 * 仅在编辑态接线（宿主 id 已知）：创建态的图片是 blob: 占位（staged-images），没有附件可解绑。
 * 多富文本字段的宿主（已知错误 / 发布表单）把各字段 HTML 一起传入，按并集口径求差集，
 * 只要某个 id 仍留在任一字段中就不会被误删。
 */
import { useCallback, useRef } from 'react';
import { message } from 'antd';

import { AttachmentApi } from '@/lib/api/attachment-api';
import type { AttachmentHostContext } from '@/lib/upload/types';

import { extractAttachmentImageIds } from './sanitize';

/** 解绑宿主：内嵌图片固定 usage='inline_image'，调用方只需给出宿主单据 */
export type InlineImageUnbindHost = Pick<AttachmentHostContext, 'bizType' | 'bizId'>;

/** 单个或一组富文本字段值 */
export type InlineImageSource =
  | string
  | null
  | undefined
  | ReadonlyArray<string | null | undefined>;

/** 单次解绑结果（供调用方按需使用；失败已由本 hook 统一降级告警） */
export interface InlineImageUnbindResult {
  /** 本次判定为「已移除」并尝试解绑的图片数 */
  removed: number;
  /** 其中解绑失败的图片数 */
  failed: number;
}

const joinHtml = (source: InlineImageSource): string => {
  if (Array.isArray(source)) {
    return source.filter((html): html is string => typeof html === 'string').join('\n');
  }
  return typeof source === 'string' ? source : '';
};

export function useInlineImageUnbind() {
  /** 编辑基线：打开编辑面时正文里的图片附件 id（保存后据此求差集） */
  const baselineRef = useRef<number[]>([]);

  /** 记录编辑基线（宿主数据加载完成 / 每次进入编辑态时调用） */
  const captureInlineImageBaseline = useCallback((source: InlineImageSource) => {
    baselineRef.current = extractAttachmentImageIds(joinHtml(source));
  }, []);

  /** 清空基线（回到创建态 / 关闭编辑面时调用，避免误删上一次的图片） */
  const resetInlineImageBaseline = useCallback(() => {
    baselineRef.current = [];
  }, []);

  /**
   * 主保存成功后调用：解绑「基线里有、当前正文里没有」的内嵌图片附件。
   * 无论成功失败都不抛出（失败仅告警），保证主保存结果不被清理动作回滚。
   */
  const unbindRemovedInlineImages = useCallback(
    async (
      host: InlineImageUnbindHost,
      source: InlineImageSource
    ): Promise<InlineImageUnbindResult> => {
      const { bizType, bizId } = host;
      if (!bizType || !Number.isFinite(bizId) || bizId <= 0) {
        return { removed: 0, failed: 0 };
      }

      const nextImageIds = extractAttachmentImageIds(joinHtml(source));
      const removedImageIds = baselineRef.current.filter(id => !nextImageIds.includes(id));
      // 基线前移：本次已处理的图片不再重复解绑（与知识库文章编辑一致）
      baselineRef.current = nextImageIds;
      if (removedImageIds.length === 0) {
        return { removed: 0, failed: 0 };
      }

      const outcomes = await Promise.allSettled(
        removedImageIds.map(id =>
          AttachmentApi.removeById(id, { bizType, bizId, usage: 'inline_image' })
        )
      );

      const failures = outcomes.filter(
        (outcome): outcome is PromiseRejectedResult => outcome.status === 'rejected'
      );
      if (failures.length > 0) {
        const reasons = failures.map(failure =>
          failure.reason instanceof Error ? failure.reason.message : String(failure.reason ?? '')
        );
        console.warn('[inline-image-unbind] 正文图片解绑失败', {
          bizType,
          bizId,
          removedImageIds,
          reasons,
        });
        const detail = reasons.find(reason => reason) || '未知错误';
        message.warning(
          `正文中有 ${failures.length} 张已移除的图片解绑失败：${detail}（可稍后在附件列表手动清理）`
        );
      }

      return { removed: removedImageIds.length, failed: failures.length };
    },
    []
  );

  return {
    captureInlineImageBaseline,
    resetInlineImageBaseline,
    unbindRemovedInlineImages,
  };
}
