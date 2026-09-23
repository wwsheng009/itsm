/**
 * 工单评论API
 * 提供工单评论的创建、查询、更新、删除功能
 */

import { httpClient } from './http-client';
import type { CommentAttachment } from '@/types/comment';

export interface TicketComment {
  id: number;
  ticketId: number;
  userId: number;
  content: string;
  isInternal: boolean;
  mentions: number[];
  attachments: number[];
  /**
   * 评论附件展示元数据（BE-11）：服务端按「同租户 + 同工单 + usage=comment_attachment
   * + 存活」过滤后下发（顺序与 `attachments` 一致），普通用户无需再经通用 A3/A6 反查。
   */
  attachmentRefs?: CommentAttachment[];
  user?: {
    id: number;
    username: string;
    name: string;
    email: string;
    role?: string;
    department?: string;
    tenantId?: number;
  };
  createdAt: string;
  updatedAt: string;
}

export interface CreateTicketCommentRequest {
  content: string;
  isInternal?: boolean;
  mentions?: number[];
  attachments?: number[];
}

export interface UpdateTicketCommentRequest {
  content?: string;
  isInternal?: boolean;
  mentions?: number[];
  /** 三态语义：省略 = 不修改；`[]` = 清空引用；非空 = 全量替换 */
  attachments?: number[];
}

export interface ListTicketCommentsResponse {
  items: TicketComment[];
  total: number;
}

export class TicketCommentApi {
  /**
   * 获取工单评论列表
   */
  static async getComments(ticketId: number): Promise<ListTicketCommentsResponse> {
    return httpClient.get<ListTicketCommentsResponse>(`/api/v1/tickets/${ticketId}/comments`);
  }

  /**
   * 创建工单评论
   */
  static async createComment(
    ticketId: number,
    data: CreateTicketCommentRequest
  ): Promise<TicketComment> {
    return httpClient.post<TicketComment>(`/api/v1/tickets/${ticketId}/comments`, data);
  }

  /**
   * 更新工单评论
   */
  static async updateComment(
    ticketId: number,
    commentId: number,
    data: UpdateTicketCommentRequest
  ): Promise<TicketComment> {
    return httpClient.put<TicketComment>(`/api/v1/tickets/${ticketId}/comments/${commentId}`, data);
  }

  /**
   * 删除工单评论
   */
  static async deleteComment(ticketId: number, commentId: number): Promise<void> {
    return httpClient.delete(`/api/v1/tickets/${ticketId}/comments/${commentId}`);
  }
}
