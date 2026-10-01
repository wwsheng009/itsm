/**
 * MSP 跨客户工作台（IP-P0-8；WB-A1–WB-A6）。
 *
 * - 路由 `/msp/workbench`（由父会话在 src/routes/** 注册；本页 default export）。
 * - 顶栏 CustomerFilter 与 URL query `customerTenantIds` 联动，本页读取并作为列表过滤条件。
 * - 行内操作**严格按每条 allowedActions[] 渲染**：reply → POST /msp/tickets/:id/reply，
 *   status → POST /msp/tickets/:id/status；allowedActions 为空或 reasonCode=CUSTOMER_INACTIVE
 *   时整条只读（WB-A5）。
 * - 分页使用服务端 nextCursor（复合游标），不落 OFFSET。
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  Alert,
  Button,
  Card,
  Dropdown,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { RefreshCw } from 'lucide-react';
import { DESIGN } from '@/design-system/tokens';
import {
  CUSTOMER_TENANT_IDS_PARAM,
  changeWorkbenchTicketStatus,
  findAllowedAction,
  isCustomerInactive,
  isTicketReadOnly,
  listWorkbenchTickets,
  parseCustomerTenantIds,
  replyWorkbenchTicket,
  type WorkbenchTicketItem,
  type WorkbenchTicketQuery,
} from '@/lib/api/msp-workbench-api';

const PAGE_SIZE = 50;

export const WORKBENCH_STATUS_OPTIONS = [
  { value: 'open', label: '待处理' },
  { value: 'in_progress', label: '处理中' },
  { value: 'pending', label: '挂起' },
  { value: 'resolved', label: '已解决' },
  { value: 'closed', label: '已关闭' },
  { value: 'cancelled', label: '已取消' },
] as const;

const STATUS_LABEL = new Map<string, string>(
  WORKBENCH_STATUS_OPTIONS.map(item => [item.value, item.label])
);

export const WORKBENCH_PRIORITY_OPTIONS = [
  { value: 'urgent', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
] as const;

const PRIORITY_LABEL = new Map<string, string>(
  WORKBENCH_PRIORITY_OPTIONS.map(item => [item.value, item.label])
);

const PRIORITY_COLOR: Record<string, string> = {
  urgent: 'red',
  high: 'orange',
  medium: 'blue',
  low: 'default',
};

const TERMINAL_STATUSES = new Set(['resolved', 'closed', 'cancelled']);

interface WorkbenchFilters {
  customerTenantIdsParam: string;
  status: string;
  priority: string;
  q: string;
  sort: 'updated' | 'sla';
  assigneeId?: number;
  updatedAfter?: string;
}

function readFilters(params: URLSearchParams): WorkbenchFilters {
  const assigneeRaw = params.get('assigneeId');
  const assigneeId = assigneeRaw ? Number(assigneeRaw) : NaN;
  return {
    customerTenantIdsParam: params.get(CUSTOMER_TENANT_IDS_PARAM) ?? 'all',
    status: params.get('status') ?? '',
    priority: params.get('priority') ?? '',
    q: params.get('q') ?? '',
    sort: params.get('sort') === 'sla' ? 'sla' : 'updated',
    assigneeId: Number.isFinite(assigneeId) && assigneeId > 0 ? assigneeId : undefined,
    updatedAfter: params.get('updatedAfter') ?? undefined,
  };
}

function toApiQuery(filters: WorkbenchFilters, cursor?: string): WorkbenchTicketQuery {
  return {
    customerTenantIds: parseCustomerTenantIds(filters.customerTenantIdsParam),
    status: filters.status || undefined,
    priority: filters.priority || undefined,
    assigneeId: filters.assigneeId,
    q: filters.q || undefined,
    updatedAfter: filters.updatedAfter,
    sort: filters.sort,
    cursor,
    limit: PAGE_SIZE,
  };
}

function formatDateTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export default function MSPWorkbenchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const queryString = searchParams.toString();
  // 游标不参与过滤键（由分页状态自持），过滤/排序变化即重新拉取第一页
  const filterKey = useMemo(() => {
    const params = new URLSearchParams(queryString);
    params.delete('cursor');
    return params.toString();
  }, [queryString]);

  const filters = useMemo(() => readFilters(new URLSearchParams(filterKey)), [filterKey]);
  const scopeLabel =
    filters.customerTenantIdsParam.toLowerCase() === 'all'
      ? '全部客户'
      : `${parseCustomerTenantIds(filters.customerTenantIdsParam).length} 个客户`;

  const [tickets, setTickets] = useState<WorkbenchTicketItem[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestSeq = useRef(0);

  const loadFirstPage = useCallback(async () => {
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    try {
      const response = await listWorkbenchTickets(toApiQuery(filters));
      if (seq !== requestSeq.current) return;
      setTickets(response?.items ?? []);
      setNextCursor(response?.nextCursor || null);
    } catch (err) {
      if (seq !== requestSeq.current) return;
      setTickets([]);
      setNextCursor(null);
      setError(errorMessage(err, '加载工作台工单失败'));
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [filters]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const response = await listWorkbenchTickets(toApiQuery(filters, nextCursor));
      setTickets(prev => [...prev, ...(response?.items ?? [])]);
      setNextCursor(response?.nextCursor || null);
    } catch (err) {
      message.error(errorMessage(err, '加载更多失败'));
    } finally {
      setLoadingMore(false);
    }
  }, [filters, loadingMore, nextCursor]);

  const updateFilter = useCallback(
    (patch: Record<string, string | null>) => {
      const params = new URLSearchParams(queryString);
      Object.entries(patch).forEach(([key, value]) => {
        if (value === null || value === '') params.delete(key);
        else params.set(key, value);
      });
      params.delete('cursor');
      setSearchParams(params);
    },
    [queryString, setSearchParams]
  );

  // ==================== 行内操作（严格按 allowedActions） ====================

  const [replyTarget, setReplyTarget] = useState<WorkbenchTicketItem | null>(null);
  const [replyContent, setReplyContent] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const openReply = useCallback((ticket: WorkbenchTicketItem) => {
    setReplyTarget(ticket);
    setReplyContent('');
  }, []);

  const submitReply = useCallback(async () => {
    if (!replyTarget) return;
    const content = replyContent.trim();
    if (!content) {
      message.warning('请输入回复内容');
      return;
    }
    setSubmitting(true);
    try {
      await replyWorkbenchTicket(replyTarget.id, {
        customerTenantId: replyTarget.customerTenantId,
        content,
      });
      message.success('回复已发送');
      setReplyTarget(null);
      setReplyContent('');
      void loadFirstPage();
    } catch (err) {
      message.error(errorMessage(err, '回复工单失败'));
    } finally {
      setSubmitting(false);
    }
  }, [loadFirstPage, replyContent, replyTarget]);

  const handleStatusChange = useCallback(
    async (ticket: WorkbenchTicketItem, status: string) => {
      try {
        await changeWorkbenchTicketStatus(ticket.id, {
          customerTenantId: ticket.customerTenantId,
          status,
        });
        message.success('状态已更新');
        void loadFirstPage();
      } catch (err) {
        message.error(errorMessage(err, '更新工单状态失败'));
      }
    },
    [loadFirstPage]
  );

  const renderActions = useCallback(
    (record: WorkbenchTicketItem) => {
      if (isTicketReadOnly(record)) {
        const reason = isCustomerInactive(record)
          ? '客户租户已暂停或过期，仅可查看'
          : '当前条目无可用操作';
        return (
          <Tooltip title={reason}>
            <Tag data-testid={`ticket-readonly-${record.id}`} style={{ margin: 0 }} title={reason}>
              只读
            </Tag>
          </Tooltip>
        );
      }

      const reply = findAllowedAction(record, 'reply');
      const status = findAllowedAction(record, 'status');

      return (
        <Space size={0}>
          {reply ? (
            <Tooltip title={reply.allowed ? '' : reply.reasonText ?? '当前不可回复'}>
              <Button
                type="link"
                size="small"
                disabled={!reply.allowed}
                onClick={() => openReply(record)}
                data-testid={`action-reply-${record.id}`}
              >
                回复
              </Button>
            </Tooltip>
          ) : null}
          {status ? (
            <Tooltip title={status.allowed ? '' : status.reasonText ?? '当前不可改状态'}>
              <Dropdown
                disabled={!status.allowed}
                menu={{
                  items: WORKBENCH_STATUS_OPTIONS.filter(
                    option => option.value !== record.status
                  ).map(option => ({ key: option.value, label: option.label })),
                  onClick: ({ key }) => {
                    void handleStatusChange(record, key);
                  },
                }}
              >
                <Button
                  type="link"
                  size="small"
                  disabled={!status.allowed}
                  data-testid={`action-status-${record.id}`}
                >
                  改状态
                </Button>
              </Dropdown>
            </Tooltip>
          ) : null}
        </Space>
      );
    },
    [handleStatusChange, openReply]
  );

  const columns: TableColumnsType<WorkbenchTicketItem> = useMemo(
    () => [
      {
        title: '客户',
        dataIndex: 'customerName',
        key: 'customerName',
        width: 160,
        fixed: 'left',
        render: (value: string) => (
          <Space size={4}>
            <Tag color="geekblue" style={{ margin: 0 }}>
              {value || '未知客户'}
            </Tag>
          </Space>
        ),
      },
      {
        title: '工单号',
        dataIndex: 'ticketNumber',
        key: 'ticketNumber',
        width: 150,
        render: (value: string) => value || '—',
      },
      {
        title: '标题',
        dataIndex: 'title',
        key: 'title',
        ellipsis: true,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 100,
        render: (value: string) => <Tag>{STATUS_LABEL.get(value) ?? value}</Tag>,
      },
      {
        title: '优先级',
        dataIndex: 'priority',
        key: 'priority',
        width: 90,
        render: (value: string) => (
          <Tag color={PRIORITY_COLOR[value] ?? 'default'}>{PRIORITY_LABEL.get(value) ?? value}</Tag>
        ),
      },
      {
        title: '负责人',
        dataIndex: 'assigneeName',
        key: 'assigneeName',
        width: 120,
        render: (value: string | undefined) =>
          value ? value : <span style={{ color: DESIGN.colors.textMuted }}>未指派</span>,
      },
      {
        title: 'SLA 截止',
        dataIndex: 'slaDeadline',
        key: 'slaDeadline',
        width: 170,
        render: (value: string | undefined, record) => {
          if (!value) return <span style={{ color: DESIGN.colors.textMuted }}>—</span>;
          const overdue =
            !TERMINAL_STATUSES.has(record.status) && new Date(value).getTime() < Date.now();
          return (
            <Space size={4}>
              <span>{formatDateTime(value)}</span>
              {overdue && (
                <Tag color="red" style={{ margin: 0 }}>
                  超期
                </Tag>
              )}
            </Space>
          );
        },
      },
      {
        title: '更新时间',
        dataIndex: 'updatedAt',
        key: 'updatedAt',
        width: 170,
        render: (value: string) => formatDateTime(value),
      },
      {
        title: '操作',
        key: 'actions',
        width: 150,
        fixed: 'right',
        render: (_: unknown, record) => renderActions(record),
      },
    ],
    [renderActions]
  );

  return (
    <div style={{ padding: 24 }} data-testid="msp-workbench-page">
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: 8,
          marginBottom: 16,
        }}
      >
        <Space align="center" size={8}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            跨客户工作台
          </Typography.Title>
          <Tag color="blue" data-testid="workbench-scope" style={{ margin: 0 }}>
            {scopeLabel}
          </Tag>
          <span style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
            顶栏客户过滤器只改视图不改会话；行内操作按客户授权实时判定
          </span>
        </Space>
        <Button
          icon={<RefreshCw size={14} />}
          onClick={() => void loadFirstPage()}
          loading={loading}
          data-testid="workbench-refresh"
        >
          刷新
        </Button>
      </div>

      <Card size="small" style={{ marginBottom: 16 }}>
        <Space wrap size={8}>
          <Input.Search
            allowClear
            placeholder="搜索工单号/标题"
            defaultValue={filters.q}
            style={{ width: 240 }}
            onSearch={value => updateFilter({ q: value.trim() })}
            data-testid="workbench-search"
          />
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 120 }}
            value={filters.status || undefined}
            options={WORKBENCH_STATUS_OPTIONS.map(option => ({ ...option }))}
            onChange={value => updateFilter({ status: value ?? null })}
            data-testid="workbench-status-filter"
          />
          <Select
            allowClear
            placeholder="优先级"
            style={{ width: 110 }}
            value={filters.priority || undefined}
            options={WORKBENCH_PRIORITY_OPTIONS.map(option => ({ ...option }))}
            onChange={value => updateFilter({ priority: value ?? null })}
            data-testid="workbench-priority-filter"
          />
          <Select
            style={{ width: 150 }}
            value={filters.sort}
            options={[
              { value: 'updated', label: '按更新时间' },
              { value: 'sla', label: '按 SLA 紧迫度' },
            ]}
            onChange={value => updateFilter({ sort: value })}
            data-testid="workbench-sort"
          />
        </Space>
      </Card>

      {error && (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginBottom: 16 }}
          action={
            <Button size="small" onClick={() => void loadFirstPage()}>
              重试
            </Button>
          }
        />
      )}

      <Table<WorkbenchTicketItem>
        rowKey="id"
        columns={columns}
        dataSource={tickets}
        loading={loading}
        pagination={false}
        scroll={{ x: 1280 }}
        locale={{ emptyText: '当前过滤条件下暂无工单' }}
      />

      {nextCursor && (
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Button
            onClick={() => void loadMore()}
            loading={loadingMore}
            data-testid="workbench-load-more"
          >
            加载更多
          </Button>
        </div>
      )}

      <Modal
        open={!!replyTarget}
        title={replyTarget ? `回复工单 ${replyTarget.ticketNumber}` : '回复工单'}
        okText="发送"
        cancelText="取消"
        confirmLoading={submitting}
        onOk={() => void submitReply()}
        onCancel={() => setReplyTarget(null)}
        destroyOnHidden
      >
        <Input.TextArea
          rows={4}
          maxLength={2000}
          showCount
          value={replyContent}
          onChange={event => setReplyContent(event.target.value)}
          placeholder="输入回复内容（将写入该客户工单评论并按条目审计）"
          data-testid="reply-content"
        />
      </Modal>
    </div>
  );
}
