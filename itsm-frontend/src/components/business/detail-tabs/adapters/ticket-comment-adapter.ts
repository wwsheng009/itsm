import { TicketCommentApi } from '@/lib/api/ticket-comment-api';
import { AttachmentApi } from '@/lib/api/attachment-api';
import type { CommentAdapter, CommentItem } from '../types';

/**
 * 评论附件宿主（BE-9 先上传后绑定）：usage 固定 `comment_attachment`，
 * 上传后由评论创建 / 更新请求的 `attachments` 数组建立引用。
 */
const commentAttachmentHost = (targetId: number | string) => ({
  bizType: 'ticket',
  bizId: Number(targetId),
  usage: 'comment_attachment' as const,
});

export const ticketCommentAdapter: CommentAdapter = {
  async list(targetId) {
    const { items, total } = await TicketCommentApi.getComments(Number(targetId));
    return { comments: items as unknown as CommentItem[], total };
  },
  async create(targetId, data) {
    const res = await TicketCommentApi.createComment(Number(targetId), data);
    return res as unknown as CommentItem;
  },
  async update(targetId, commentId, data) {
    const res = await TicketCommentApi.updateComment(Number(targetId), commentId, data);
    return res as unknown as CommentItem;
  },
  async remove(targetId, commentId) {
    await TicketCommentApi.deleteComment(Number(targetId), commentId);
  },
  async uploadAttachment(targetId, file, onProgress) {
    const ref = await AttachmentApi.upload(file, commentAttachmentHost(targetId), { onProgress });
    return { id: ref.id, url: ref.previewUrl ?? ref.fileUrl };
  },
  async removeAttachment(targetId, attachmentId) {
    await AttachmentApi.removeById(attachmentId, commentAttachmentHost(targetId));
  },
};
