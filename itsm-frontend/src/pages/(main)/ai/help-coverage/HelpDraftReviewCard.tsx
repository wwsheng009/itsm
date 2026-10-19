/**
 * T5-09a 帮助草稿审阅卡：diff 预览 + 提交审阅/发布动作。
 *
 * 边界：写动作只调用 `help_submit_review` / `help_publish` 工具；
 * 两者都会进入既有审批队列，页面不直接写 KB、不绕过审批。
 */
import React, { useMemo, useState } from 'react';
import { Alert, Button, Card, Modal, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import type { HelpDraftReviewItem } from '@/lib/api/ai-api';

const { Text } = Typography;

const DRAFT_STATUS_META: Record<string, { color: string; label: string }> = {
  draft: { color: 'default', label: '草稿' },
  in_review: { color: 'blue', label: '审阅中' },
  published: { color: 'green', label: '已发布' },
};

export interface HelpDraftReviewCardProps {
  items: HelpDraftReviewItem[];
  loading?: boolean;
  error?: string | null;
  actionError?: string | null;
  notice?: string | null;
  actionPendingId?: number | null;
  canPublish?: boolean;
  onSubmitReview: (item: HelpDraftReviewItem) => void;
  onPublish: (item: HelpDraftReviewItem) => void;
  onRefresh?: () => void;
}

const HelpDraftReviewCard: React.FC<HelpDraftReviewCardProps> = ({
  items,
  loading = false,
  error,
  actionError,
  notice,
  actionPendingId,
  canPublish = true,
  onSubmitReview,
  onPublish,
  onRefresh,
}) => {
  const [active, setActive] = useState<HelpDraftReviewItem | null>(null);

  const columns = useMemo<ColumnsType<HelpDraftReviewItem>>(
    () => [
      {
        title: '页面键',
        dataIndex: 'pageKey',
        key: 'pageKey',
        width: 170,
        render: value => <Text code>{value || '—'}</Text>,
      },
      {
        title: '草稿',
        dataIndex: 'title',
        key: 'title',
        render: (value, record) => (
          <Space direction="vertical" size={0}>
            <Text>{value || `#${record.artifactId}`}</Text>
            <Text type="secondary" style={{ fontSize: 11 }}>
              #{record.artifactId} · rev {record.revision} · owner {record.ownerUserId}
            </Text>
          </Space>
        ),
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 100,
        render: (value: string) => {
          const meta = DRAFT_STATUS_META[value] ?? DRAFT_STATUS_META.draft;
          return (
            <Tag color={meta.color} data-testid={`help-draft-status-${value}`}>
              {meta.label}
            </Tag>
          );
        },
      },
      {
        title: '操作',
        key: 'actions',
        width: 220,
        render: (_, record) => {
          const busy = actionPendingId === record.artifactId;
          return (
            <Space size={4} wrap>
              <Button
                size="small"
                type="link"
                data-testid={`help-draft-diff-${record.artifactId}`}
                onClick={() => setActive(record)}
              >
                查看 diff
              </Button>
              {record.status === 'draft' ? (
                <Button
                  size="small"
                  type="link"
                  loading={busy}
                  data-testid={`help-draft-submit-${record.artifactId}`}
                  onClick={() => onSubmitReview(record)}
                >
                  提交审阅
                </Button>
              ) : null}
              {record.status === 'in_review' && canPublish ? (
                <Button
                  size="small"
                  type="link"
                  loading={busy}
                  data-testid={`help-draft-publish-${record.artifactId}`}
                  onClick={() => onPublish(record)}
                >
                  发布
                </Button>
              ) : null}
              {record.status === 'in_review' && !canPublish ? (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  需知识库管理员发布
                </Text>
              ) : null}
              {record.status === 'published' && record.knowledgeArticleId ? (
                <a
                  href={`/knowledge/articles/${record.knowledgeArticleId}`}
                  data-testid={`help-draft-open-${record.artifactId}`}
                >
                  打开文章
                </a>
              ) : null}
            </Space>
          );
        },
      },
    ],
    [actionPendingId, canPublish, onPublish, onSubmitReview]
  );

  return (
    <Card
      size="small"
      title="帮助草稿审阅（AI 起草 → 人工 diff → 审批发布）"
      extra={
        onRefresh ? (
          <Button size="small" type="link" onClick={onRefresh} loading={loading}>
            刷新草稿
          </Button>
        ) : null
      }
      style={{ marginBottom: 16 }}
      data-testid="help-draft-review-card"
    >
      {error ? (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 8 }}
          message={error}
          data-testid="help-draft-error"
        />
      ) : null}
      {actionError ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 8 }}
          message={actionError}
          data-testid="help-draft-action-error"
        />
      ) : null}
      {notice ? (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 8 }}
          message={notice}
          data-testid="help-draft-notice"
        />
      ) : null}
      <Table
        rowKey="artifactId"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={items}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        data-testid="help-draft-table"
        locale={{ emptyText: '暂无 AI 帮助草稿' }}
      />

      <Modal
        open={active !== null}
        title={active ? `帮助草稿 diff · ${active.title}` : '帮助草稿 diff'}
        footer={null}
        width={860}
        onCancel={() => setActive(null)}
        destroyOnClose
      >
        {active ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {active.pageKey || '未注册页面'} · {active.status} · rev {active.revision} · artifact #
              {active.artifactId}
            </Text>
            {active.sourceDocs && active.sourceDocs.length > 0 ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                参考文档：{active.sourceDocs.join('、')}
              </Text>
            ) : null}
            <div
              data-testid="help-draft-diff-body"
              style={{
                maxHeight: '60vh',
                overflow: 'auto',
                border: '1px solid rgba(5,5,5,0.12)',
                borderRadius: 6,
                background: '#fff',
              }}
            >
              {active.diff.length === 0 ? (
                <div style={{ padding: 12 }}>
                  <Text type="secondary">无行级差异</Text>
                </div>
              ) : (
                active.diff.map((line, index) => (
                  <div
                    key={`${line.type}-${index}-${line.content}`}
                    data-testid={`help-draft-diff-${line.type}`}
                    style={{
                      padding: '2px 10px',
                      whiteSpace: 'pre-wrap',
                      fontFamily: 'monospace',
                      fontSize: 12,
                      background: line.type === 'added' ? '#f6ffed' : '#fff1f0',
                      color: line.type === 'added' ? '#237804' : '#a8071a',
                    }}
                  >
                    {line.type === 'added' ? '+ ' : '- '}
                    {line.content}
                  </div>
                ))
              )}
            </div>
          </Space>
        ) : null}
      </Modal>
    </Card>
  );
};

export default HelpDraftReviewCard;
