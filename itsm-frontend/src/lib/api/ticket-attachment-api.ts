/**
 * 工单附件 API（**兼容层**，D5：旧 URL 与旧方法签名永不破坏）
 *
 * FE-4 起，附件的 HTTP 实现统一收敛到 `lib/api/attachment-api.ts`：
 *  - 工单 + 默认用途的端点解析结果仍是 `/api/v1/tickets/:id/attachments`（URL 形态不变），
 *    因此本文件的 HTTP 调用一律委托给 `AttachmentApi`，自身不再直连 httpClient；
 *  - 返回值仍按旧的 `TicketAttachment` 形状给出（`filePath`/`fileType` 等字段由契约字段补齐），
 *    保证既有调用点在 FE-3 逐个替换期间行为不变。
 *
 * @deprecated 新代码请直接用 `AttachmentApi` + `@/lib/upload/types` 的 `AttachmentRef`。
 */

import { AttachmentApi } from './attachment-api';
import type { AttachmentRef } from '@/lib/upload/types';

export interface TicketAttachment {
  id: number;
  ticketId: number;
  fileName: string;
  filePath: string;
  fileUrl: string;
  fileSize: number;
  fileType: string;
  mimeType: string;
  uploadedBy: number;
  uploader?: {
    id: number;
    username: string;
    name: string;
    email: string;
    role?: string;
    department?: string;
    tenantId?: number;
  };
  createdAt: string;
}

export interface ListTicketAttachmentsResponse {
  attachments: TicketAttachment[];
  total: number;
}

/** 统一契约 → 旧工单附件形状（过渡期字段补齐，缺失项给安全默认值） */
function toLegacyAttachment(ref: AttachmentRef, ticketId: number): TicketAttachment {
  const fallbackUrl = `/api/v1/tickets/${ticketId}/attachments/${ref.id}/preview`;
  return {
    id: ref.id,
    ticketId,
    fileName: ref.fileName,
    filePath: ref.fileUrl ?? fallbackUrl,
    fileUrl: ref.fileUrl ?? fallbackUrl,
    fileSize: ref.fileSize,
    fileType: ref.mimeType,
    mimeType: ref.mimeType,
    uploadedBy: ref.uploadedBy ?? 0,
    createdAt: ref.createdAt ?? '',
  };
}

export class TicketAttachmentApi {
  /**
   * 获取工单附件列表
   */
  static async listAttachments(ticketId: number): Promise<ListTicketAttachmentsResponse> {
    const { attachments, total } = await AttachmentApi.list({ bizType: 'ticket', bizId: ticketId });
    return { attachments: attachments.map((ref) => toLegacyAttachment(ref, ticketId)), total };
  }

  /**
   * 上传附件
   */
  static async uploadAttachment(
    ticketId: number,
    file: File,
    onProgress?: (progress: number) => void
  ): Promise<TicketAttachment> {
    // 上传必须走 httpClient（经 AttachmentApi）：它统一负责 X-CSRF-Token、withCredentials
    // （httpOnly cookie）、租户 header、CSRF 轮换后重试与 camelCase 转换；自建 XHR 会被后端以
    // 403 "CSRF token missing" 拒绝（见 __tests__ 的回归用例）。
    const ref = await AttachmentApi.upload(
      file,
      { bizType: 'ticket', bizId: ticketId },
      { onProgress }
    );
    return toLegacyAttachment(ref, ticketId);
  }

  /**
   * 下载附件
   */
  static getDownloadUrl(ticketId: number, attachmentId: number): string {
    return `/api/v1/tickets/${ticketId}/attachments/${attachmentId}`;
  }

  /**
   * 预览附件
   */
  static getPreviewUrl(ticketId: number, attachmentId: number): string {
    return `/api/v1/tickets/${ticketId}/attachments/${attachmentId}/preview`;
  }

  /**
   * 删除附件
   */
  static async deleteAttachment(ticketId: number, attachmentId: number): Promise<void> {
    await AttachmentApi.removeById(attachmentId, { bizType: 'ticket', bizId: ticketId });
  }

  /**
   * 格式化文件大小
   */
  static formatFileSize(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
  }

  /**
   * 获取文件图标类型
   */
  static getFileIconType(mimeType: string): string {
    if (mimeType.startsWith('image/')) return 'image';
    if (mimeType.startsWith('video/')) return 'video';
    if (mimeType.startsWith('audio/')) return 'audio';
    if (mimeType.includes('pdf')) return 'pdf';
    if (mimeType.includes('word') || mimeType.includes('document')) return 'word';
    if (mimeType.includes('excel') || mimeType.includes('spreadsheet')) return 'excel';
    if (mimeType.includes('powerpoint') || mimeType.includes('presentation')) return 'powerpoint';
    if (mimeType.includes('zip') || mimeType.includes('rar')) return 'archive';
    return 'file';
  }
}
