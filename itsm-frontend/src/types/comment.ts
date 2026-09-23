/**
 * 评论相关类型定义
 * 统一的评论类型，避免多处定义导致冲突
 */

// ==================== 评论类型 ====================

/**
 * 评论类型枚举
 */
export enum CommentType {
  INTERNAL = 'internal', // 内部评论
  PUBLIC = 'public', // 公开评论
  SYSTEM = 'system', // 系统评论
}

/**
 * 评论实体
 * 统一的评论接口定义
 */
export interface Comment {
  id: number;
  ticketId: number;
  userId: number;
  user?: {
    id: number;
    username: string;
    name: string;
    email: string;
    avatar?: string;
  };
  content: string;
  isInternal: boolean;
  type?: CommentType;
  mentions?: number[];
  attachments?: number[];
  createdAt: string;
  updatedAt?: string;
}

/**
 * 创建评论请求
 */
export interface CommentCreateRequest {
  content: string;
  isInternal?: boolean;
  mentions?: number[];
  attachments?: number[];
}

/**
 * 更新评论请求
 */
export interface CommentUpdateRequest {
  content?: string;
  isInternal?: boolean;
}

/**
 * 评论列表查询参数
 */
export interface CommentListQuery {
  ticketId: number;
  type?: CommentType;
  isInternal?: boolean;
  userId?: number;
  page?: number;
  pageSize?: number;
}

/**
 * 评论列表响应
 */
export interface CommentListResponse {
  comments: Comment[];
  total: number;
  page: number;
  pageSize: number;
}

/**
 * 评论统计
 */
export interface CommentStats {
  totalComments: number;
  internalComments: number;
  publicComments: number;
  systemComments: number;
  commentsByUser: Record<number, number>;
}

/**
 * 评论附件展示元数据（BE-11 响应契约）
 *
 * 与后端 `dto.TicketCommentAttachmentRef` 同名同义：只有「同租户 + 同工单
 * + usage=comment_attachment + 未删除」的记录会出现在 `attachmentRefs` 中；
 * 下载 / 预览地址均为工单域内端点（静态权限沿用 `ticket:read`）。
 */
export interface CommentAttachment {
  id: number;
  fileName: string;
  fileSize: number;
  mimeType: string;
  /** 域内下载地址：/api/v1/tickets/:id/attachments/:ref */
  downloadUrl: string;
  /** 仅 image/* 下发（inline 预览） */
  previewUrl?: string;
}
