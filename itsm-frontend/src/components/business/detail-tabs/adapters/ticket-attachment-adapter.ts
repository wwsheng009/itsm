import {
  AttachmentApi,
  ticketAttachmentContentUrl,
  ticketAttachmentPreviewUrl,
} from '@/lib/api/attachment-api';
import type { AttachmentRef } from '@/lib/upload/types';
import type { AttachmentAdapter, AttachmentItem } from '../types';

/** 工单宿主上下文（普通附件：usage 缺省 attachment，端点仍是域内别名路由） */
const ticketHost = (targetId: number | string) => ({
  bizType: 'ticket',
  bizId: Number(targetId),
});

/**
 * 统一附件契约（AttachmentRef）→ 详情页 Tab 展示结构。
 * 通用响应只给 `uploadedBy`，故 `uploader` 仅保留 ID（`AttachmentPanel` 的归属判定用之）。
 */
function toAttachmentItem(ref: AttachmentRef): AttachmentItem {
  return {
    id: ref.id,
    fileName: ref.fileName,
    fileSize: ref.fileSize,
    mimeType: ref.mimeType,
    fileUrl: ref.fileUrl,
    createdAt: ref.createdAt ?? '',
    uploader: ref.uploader ?? (ref.uploadedBy ? { id: ref.uploadedBy } : undefined),
  };
}

export const ticketAttachmentAdapter: AttachmentAdapter = {
  async list(targetId) {
    const res = await AttachmentApi.list(ticketHost(targetId));
    return (res.attachments || []).map(toAttachmentItem);
  },
  async upload(targetId, file, onProgress) {
    const ref = await AttachmentApi.upload(file, ticketHost(targetId), { onProgress });
    return toAttachmentItem(ref);
  },
  getDownloadUrl(targetId, attachmentId) {
    // 详情面板的下载/预览必须走域内端点：通用 A4 的静态码是兜底码 attachment:read
    // （仅 admin/sysadmin），普通用户会 403（BE-10 修订）。
    return ticketAttachmentContentUrl(Number(targetId), attachmentId);
  },
  getPreviewUrl(targetId, attachmentId) {
    return ticketAttachmentPreviewUrl(Number(targetId), attachmentId);
  },
  async remove(targetId, attachmentId) {
    await AttachmentApi.removeById(attachmentId, ticketHost(targetId));
  },
};
