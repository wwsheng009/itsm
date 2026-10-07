/**
 * TicketCommentTimeline —— 只读评论时间线（跨租户工作台 P2；§13.1 复用件 #1）。
 *
 * 设计约束：
 * - 纯展示：不发请求、不提供写入口（回复/编辑由调用方按 allowedActions 另行提供）；
 * - 数据源无关：客户侧富交互列表复用同一条目渲染 `CommentItemBody`（缺省 authorSide 时不渲染侧标签）；
 * - 内部备注（isInternal=true）预期已被服务端过滤；组件仍显式标注以防误配。
 */
import React from 'react';
import { Alert, Empty, List, Spin, Typography } from 'antd';

import CommentItemBody, { type CommentItemBodyItem } from './CommentItemBody';

export type TicketCommentTimelineItem = CommentItemBodyItem;

export interface TicketCommentTimelineProps {
  items: TicketCommentTimelineItem[];
  loading?: boolean;
  error?: string | null;
  title?: string;
  emptyText?: string;
  testIdPrefix?: string;
}

export default function TicketCommentTimeline({
  items,
  loading = false,
  error = null,
  title = '评论',
  emptyText = '暂无评论',
  testIdPrefix = 'comment-timeline',
}: TicketCommentTimelineProps) {
  return (
    <div data-testid={testIdPrefix}>
      <Typography.Text type="secondary">{title}</Typography.Text>
      {loading ? (
        <div style={{ padding: '12px 0' }}>
          <Spin size="small" data-testid={`${testIdPrefix}-loading`} />
        </div>
      ) : error ? (
        <Alert type="error" showIcon message={error} style={{ marginTop: 8 }} />
      ) : items.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={emptyText}
          style={{ margin: '8px 0' }}
        />
      ) : (
        <List
          size="small"
          dataSource={items}
          style={{ marginTop: 8 }}
          renderItem={item => (
            <List.Item key={item.id} data-testid={`${testIdPrefix}-item-${item.id}`}>
              <CommentItemBody item={item} />
            </List.Item>
          )}
        />
      )}
    </div>
  );
}
