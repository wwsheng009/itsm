/**
 * CommentItemBody —— 评论条目通用渲染（身份行 + 正文 + 槽位）。
 *
 * 分层（§13.1 前端复用收敛 #1）：
 * - 只读列表 `TicketCommentTimeline`（跨租户工作台）与富交互 `CommentPanel`（客户侧详情 Tab）
 *   共用本组件渲染单条评论的「作者/时间/正文」；
 * - 差异部分（头像、@提及、附件、编辑/删除按钮、编辑态）由调用方经槽位注入，
 *   本组件不发起请求、不感知业务域。
 */
import React from 'react';
import { Space, Tag, Typography } from 'antd';

export interface CommentItemBodyItem {
  id: number;
  userId?: number;
  authorName?: string;
  /** customer | provider；缺省不渲染侧标签（客户侧自用）。 */
  authorSide?: string;
  content: string;
  isInternal?: boolean;
  createdAt: string;
  updatedAt?: string;
}

export interface CommentItemBodyProps {
  item: CommentItemBodyItem;
  /** 头像节点（客户侧传 Avatar；工作台不传）。 */
  avatar?: React.ReactNode;
  /** 身份行附加标签（如 @提及计数）。 */
  metaExtra?: React.ReactNode;
  /** 正文下方附加区（如附件列表）。 */
  footer?: React.ReactNode;
  /** 操作区（如编辑/删除按钮）。 */
  actions?: React.ReactNode;
  sideLabels?: Record<string, string>;
  internalLabel?: string;
  /** 提供时，updatedAt ≠ createdAt 显示「（已编辑）」；缺省不显示。 */
  editedLabel?: string;
  unknownUserLabel?: string;
  formatDateTime?: (value: string) => string;
}

const DEFAULT_SIDE_LABEL: Record<string, string> = { customer: '客户', provider: '服务商' };

function defaultFormatDateTime(value: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

export default function CommentItemBody({
  item,
  avatar,
  metaExtra,
  footer,
  actions,
  sideLabels = DEFAULT_SIDE_LABEL,
  internalLabel = '内部备注',
  editedLabel,
  unknownUserLabel = '未知用户',
  formatDateTime = defaultFormatDateTime,
}: CommentItemBodyProps) {
  const author = item.authorName || (item.userId ? `#${item.userId}` : unknownUserLabel);
  const edited = !!(item.updatedAt && item.updatedAt !== item.createdAt);

  const body = (
    <div style={{ width: '100%' }}>
      <Space size={8} wrap>
        <Typography.Text strong>{author}</Typography.Text>
        {item.authorSide ? (
          <Tag color={item.authorSide === 'provider' ? 'blue' : 'default'} style={{ margin: 0 }}>
            {sideLabels[item.authorSide] ?? item.authorSide}
          </Tag>
        ) : null}
        {item.isInternal ? (
          <Tag color="orange" style={{ margin: 0 }}>
            {internalLabel}
          </Tag>
        ) : null}
        {metaExtra}
        <Typography.Text type="secondary">{formatDateTime(item.createdAt)}</Typography.Text>
        {edited && editedLabel ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            （{editedLabel}）
          </Typography.Text>
        ) : null}
      </Space>
      <Typography.Paragraph
        style={{ whiteSpace: 'pre-wrap', marginBottom: footer || actions ? 8 : 0, marginTop: 4 }}
      >
        {item.content}
      </Typography.Paragraph>
      {footer}
      {actions ? <div style={{ marginTop: 4 }}>{actions}</div> : null}
    </div>
  );

  if (!avatar) return body;
  return (
    <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start', width: '100%' }}>
      {avatar}
      <div style={{ flex: 1, minWidth: 0 }}>{body}</div>
    </div>
  );
}
