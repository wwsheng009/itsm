/**
 * TicketCommentTimeline —— 只读评论时间线（跨租户工作台 P2 首个共享渲染件）。
 *
 * 设计约束：
 * - 纯展示：不发请求、不提供写入口（回复/编辑由调用方按 allowedActions 另行提供）；
 * - 数据源无关：客户侧详情与工作台抽屉可共用（作者侧标签仅在提供 authorSide 时渲染）；
 * - 内部备注（isInternal=true）预期已被服务端过滤；组件仍显式标注以防误配。
 */
import React from 'react';
import { Alert, Empty, List, Space, Spin, Tag, Typography } from 'antd';

export interface TicketCommentTimelineItem {
  id: number;
  userId?: number;
  authorName?: string;
  /** customer | provider；缺省不渲染侧标签（客户侧自用）。 */
  authorSide?: string;
  content: string;
  isInternal?: boolean;
  createdAt: string;
}

export interface TicketCommentTimelineProps {
  items: TicketCommentTimelineItem[];
  loading?: boolean;
  error?: string | null;
  title?: string;
  emptyText?: string;
  testIdPrefix?: string;
}

const SIDE_LABEL: Record<string, string> = { customer: '客户', provider: '服务商' };

function formatDateTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
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
              <div style={{ width: '100%' }}>
                <Space size={8} wrap>
                  <Typography.Text strong>
                    {item.authorName || (item.userId ? `#${item.userId}` : '未知用户')}
                  </Typography.Text>
                  {item.authorSide ? (
                    <Tag
                      color={item.authorSide === 'provider' ? 'blue' : 'default'}
                      style={{ margin: 0 }}
                    >
                      {SIDE_LABEL[item.authorSide] ?? item.authorSide}
                    </Tag>
                  ) : null}
                  {item.isInternal ? (
                    <Tag color="orange" style={{ margin: 0 }}>
                      内部备注
                    </Tag>
                  ) : null}
                  <Typography.Text type="secondary">{formatDateTime(item.createdAt)}</Typography.Text>
                </Space>
                <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0, marginTop: 4 }}>
                  {item.content}
                </Typography.Paragraph>
              </div>
            </List.Item>
          )}
        />
      )}
    </div>
  );
}
