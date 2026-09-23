/**
 * 详情页 Tab 通用类型
 * 供 CommentPanel / AttachmentPanel / HistoryTimeline / ApprovalTimeline 五模块共用
 */

import type { CommentAttachment } from '@/types/comment';

// ==================== Comment ====================

export interface CommentUser {
  id: number;
  username?: string;
  name?: string;
  email?: string;
  avatar?: string;
}

export interface CommentItem {
  id: number;
  userId: number;
  user?: CommentUser;
  content: string;
  isInternal?: boolean;
  mentions?: number[];
  attachments?: number[];
  /**
   * 评论附件展示元数据（BE-11）：服务端按「同租户 + 同工单 + usage=comment_attachment
   * + 存活」过滤后随评论下发，`attachments` 中拿不到元数据的 ID 由渲染侧记为失效占位。
   */
  attachmentRefs?: CommentAttachment[];
  createdAt: string;
  updatedAt?: string;
}

export interface CreateCommentInput {
  content: string;
  isInternal?: boolean;
  mentions?: number[];
  attachments?: number[];
}

export interface UpdateCommentInput {
  content?: string;
  isInternal?: boolean;
  mentions?: number[];
  /** 三态语义：省略 = 不修改；`[]` = 清空引用；非空 = 全量替换（BE-9） */
  attachments?: number[];
}

/** 评论附件上传结果（先上传后绑定，仅需 ID 与展示地址） */
export interface CommentAttachmentUploadResult {
  id: number;
  url?: string;
}

export interface CommentAdapter {
  list(targetId: number | string): Promise<{ comments: CommentItem[]; total: number }>;
  create(targetId: number | string, data: CreateCommentInput): Promise<CommentItem>;
  update?(
    targetId: number | string,
    commentId: number,
    data: UpdateCommentInput
  ): Promise<CommentItem>;
  remove(targetId: number | string, commentId: number): Promise<void>;
  /**
   * 上传评论附件（先上传后绑定）：宿主与 `usage='comment_attachment'` 由适配器固定，
   * 组件层不直连 HTTP。未实现该方法的域（如事件评论）不展示附件入口。
   */
  uploadAttachment?: (
    targetId: number | string,
    file: File,
    onProgress?: (percent: number) => void
  ) => Promise<CommentAttachmentUploadResult>;
  /** 解绑尚未被评论引用的附件（引用存续时后端 409/6105，由调用方提示） */
  removeAttachment?: (targetId: number | string, attachmentId: number) => Promise<void>;
}

export type TargetType = 'ticket' | 'incident' | 'problem' | 'change' | 'release';

// ==================== Attachment ====================

export interface AttachmentItem {
  id: number;
  fileName: string;
  fileSize: number;
  mimeType: string;
  fileUrl?: string;
  createdAt: string;
  uploader?: CommentUser;
}

export interface AttachmentAdapter {
  list(targetId: number | string): Promise<AttachmentItem[]>;
  upload(
    targetId: number | string,
    file: File,
    onProgress?: (percent: number) => void
  ): Promise<AttachmentItem>;
  getDownloadUrl(targetId: number | string, attachmentId: number): string;
  getPreviewUrl?(targetId: number | string, attachmentId: number): string;
  remove(targetId: number | string, attachmentId: number): Promise<void>;
}

// ==================== History ====================

export interface HistoryRecord {
  id: number | string;
  user?: { name?: string; username?: string };
  action?: string;
  details?: string;
  fieldName?: string;
  oldValue?: string;
  newValue?: string;
  changeReason?: string;
  createdAt: string;
}

// ==================== Approval ====================

export type ApprovalStepStatus =
  | 'pending'
  | 'approved'
  | 'rejected'
  | 'delegated'
  | 'timeout'
  | 'skipped';

export interface ApprovalStep {
  id: number;
  level: number;
  step?: string;
  status: ApprovalStepStatus;
  approverId?: number;
  approverName?: string;
  comment?: string;
  processedAt?: string;
  createdAt?: string;
}

export interface ApprovalActionInput {
  comment: string;
  delegateToUserId?: number;
}
