/**
 * CrossTenantBoard：工作台「待办任务投影 / 跨租户单据」面板（跨租户单据流转 P2-②/P2-③ 前端入口）。
 *
 * 设计约束：
 * - 行内操作**严格按每条 allowedActions[] 渲染**（XF5）：allowed=true 才渲染按钮；
 *   动作存在但 allowed=false 时渲染禁用按钮 + 原因（reasonText/reasonCode），不做权限推断；
 * - 客户范围读取顶栏 CustomerFilter 的 URL query（customerTenantIds），只作筛选，越界由服务端 403；
 * - 写操作一律携带 customerTenantId（资源租户声明），服务端二次校验「声明租户 == 资源租户」；
 * - 任务完成（complete）可选透传流程变量；当前 UI 不采集变量（后端允许为空），
 *   需要时经 Modal 扩展，不影响既有契约。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { RefreshCw } from 'lucide-react';
import { DESIGN } from '@/design-system/tokens';
import { CUSTOMER_TENANT_IDS_PARAM, parseCustomerTenantIds } from '@/lib/api/msp-workbench-api';
import {
  MSP_RECORD_TYPE_LABEL,
  MSP_STATUS_OPTIONS,
  claimWorkbenchTask,
  completeWorkbenchTask,
  findRecordAction,
  getWorkbenchRecord,
  listWorkbenchRecords,
  listWorkbenchTasks,
  recordStatusLabel,
  updateWorkbenchRecordStatus,
  type MSPRecordDetail,
  type MSPRecordItem,
  type MSPRecordType,
  type MSPTaskItem,
  type UpdateMSPRecordStatusRequest,
} from '@/lib/api/msp-records-api';

export type CrossTenantBoardKind = 'tasks' | 'records';

/** 面板数据源（可注入，测试替身用；默认绑定真实 API 模块）。 */
export interface CrossTenantBoardApi {
  listTasks: typeof listWorkbenchTasks;
  claimTask: typeof claimWorkbenchTask;
  completeTask: typeof completeWorkbenchTask;
  listRecords: typeof listWorkbenchRecords;
  getRecord: typeof getWorkbenchRecord;
  updateRecordStatus: typeof updateWorkbenchRecordStatus;
}

const DEFAULT_API: CrossTenantBoardApi = {
  listTasks: listWorkbenchTasks,
  claimTask: claimWorkbenchTask,
  completeTask: completeWorkbenchTask,
  listRecords: listWorkbenchRecords,
  getRecord: getWorkbenchRecord,
  updateRecordStatus: updateWorkbenchRecordStatus,
};

export interface CrossTenantBoardProps {
  kind: CrossTenantBoardKind;
  api?: CrossTenantBoardApi;
}

const PAGE_LIMIT = 50;

const PRIORITY_COLOR: Record<string, string> = {
  urgent: 'red',
  high: 'orange',
  medium: 'blue',
  low: 'default',
};

const RECORD_TYPE_ORDER: MSPRecordType[] = ['change', 'problem', 'service_request'];

function formatDateTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

/** 动作按钮：allowed=true → 可点；动作存在但 false → 禁用 + 原因提示；动作缺失 → 不渲染。 */
function ActionButton(props: {
  item: { allowedActions?: { action: string; allowed: boolean; reasonCode?: string; reasonText?: string }[] };
  action: string;
  label: string;
  testId: string;
  onClick: () => void;
  danger?: boolean;
}) {
  const entry = findRecordAction(props.item, props.action);
  if (!entry) return null;
  if (!entry.allowed) {
    const reason = [entry.reasonText, entry.reasonCode].filter(Boolean).join(' · ');
    return (
      <Tooltip title={reason || '当前不可操作'}>
        <Button size="small" disabled data-testid={props.testId}>
          {props.label}
        </Button>
      </Tooltip>
    );
  }
  return (
    <Button
      size="small"
      danger={props.danger}
      onClick={props.onClick}
      data-testid={props.testId}
    >
      {props.label}
    </Button>
  );
}

export default function CrossTenantBoard({ kind, api = DEFAULT_API }: CrossTenantBoardProps) {
  const [searchParams] = useSearchParams();
  const scopeRaw = searchParams.get(CUSTOMER_TENANT_IDS_PARAM);
  const customerTenantIds = useMemo(() => parseCustomerTenantIds(scopeRaw), [scopeRaw]);

  // ==================== 任务投影 ====================
  const [tasks, setTasks] = useState<MSPTaskItem[]>([]);
  const [tasksLoading, setTasksLoading] = useState(false);
  const [tasksError, setTasksError] = useState<string | null>(null);
  const [taskSubmitting, setTaskSubmitting] = useState<string | null>(null);

  // ==================== 单据读面 ====================
  const [recordType, setRecordType] = useState<MSPRecordType>('change');
  const [records, setRecords] = useState<MSPRecordItem[]>([]);
  const [recordsCursor, setRecordsCursor] = useState<string | null>(null);
  const [recordsLoading, setRecordsLoading] = useState(false);
  const [recordsLoadingMore, setRecordsLoadingMore] = useState(false);
  const [recordsError, setRecordsError] = useState<string | null>(null);

  // ==================== 状态推进弹窗 ====================
  const [statusTarget, setStatusTarget] = useState<MSPRecordItem | null>(null);
  const [targetStatus, setTargetStatus] = useState<string>('');
  const [statusComment, setStatusComment] = useState('');
  const [statusSubmitting, setStatusSubmitting] = useState(false);

  // ==================== 详情抽屉 ====================
  const [detailTarget, setDetailTarget] = useState<MSPRecordItem | null>(null);
  const [detail, setDetail] = useState<MSPRecordDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const loadTasks = useCallback(async () => {
    setTasksLoading(true);
    setTasksError(null);
    try {
      const response = await api.listTasks({ customerTenantIds, limit: PAGE_LIMIT });
      setTasks(response?.items ?? []);
    } catch (error) {
      setTasksError(errorMessage(error, '加载待办任务失败'));
    } finally {
      setTasksLoading(false);
    }
  }, [api, customerTenantIds]);

  const loadRecords = useCallback(
    async (cursor?: string) => {
      const first = !cursor;
      if (first) {
        setRecordsLoading(true);
      } else {
        setRecordsLoadingMore(true);
      }
      setRecordsError(null);
      try {
        const response = await api.listRecords(recordType, {
          customerTenantIds,
          limit: PAGE_LIMIT,
          cursor,
        });
        setRecords(prev => (first ? response?.items ?? [] : [...prev, ...(response?.items ?? [])]));
        setRecordsCursor(response?.nextCursor ?? null);
      } catch (error) {
        setRecordsError(errorMessage(error, '加载跨租户单据失败'));
      } finally {
        setRecordsLoading(false);
        setRecordsLoadingMore(false);
      }
    },
    [api, customerTenantIds, recordType]
  );

  useEffect(() => {
    if (kind === 'tasks') {
      void loadTasks();
    }
  }, [kind, loadTasks]);

  useEffect(() => {
    if (kind === 'records') {
      void loadRecords();
    }
  }, [kind, loadRecords]);

  const handleClaim = useCallback(
    async (item: MSPTaskItem) => {
      setTaskSubmitting(item.taskId);
      try {
        await api.claimTask(item.taskId, { customerTenantId: item.customerTenantId });
        message.success('已认领任务');
        await loadTasks();
      } catch (error) {
        message.error(errorMessage(error, '认领任务失败'));
      } finally {
        setTaskSubmitting(null);
      }
    },
    [api, loadTasks]
  );

  const handleComplete = useCallback(
    async (item: MSPTaskItem) => {
      setTaskSubmitting(item.taskId);
      try {
        await api.completeTask(item.taskId, { customerTenantId: item.customerTenantId });
        message.success('任务已完成，流程已推进');
        await loadTasks();
      } catch (error) {
        message.error(errorMessage(error, '完成任务失败'));
      } finally {
        setTaskSubmitting(null);
      }
    },
    [api, loadTasks]
  );

  const openStatusModal = useCallback((item: MSPRecordItem) => {
    setStatusTarget(item);
    setTargetStatus('');
    setStatusComment('');
  }, []);

  const submitStatus = useCallback(async () => {
    if (!statusTarget || !targetStatus) return;
    const body: UpdateMSPRecordStatusRequest = {
      customerTenantId: statusTarget.customerTenantId,
      status: targetStatus,
    };
    if (statusTarget.recordType === 'problem') {
      body.resolution = statusComment.trim() || undefined;
    } else if (statusTarget.recordType === 'change') {
      body.comment = statusComment.trim() || undefined;
    }
    setStatusSubmitting(true);
    try {
      await api.updateRecordStatus(statusTarget.recordType, statusTarget.id, body);
      message.success('状态已推进');
      setStatusTarget(null);
      await loadRecords();
    } catch (error) {
      message.error(errorMessage(error, '状态推进失败'));
    } finally {
      setStatusSubmitting(false);
    }
  }, [api, loadRecords, statusComment, statusTarget, targetStatus]);

  const openDetail = useCallback(
    async (item: MSPRecordItem) => {
      setDetailTarget(item);
      setDetail(null);
      setDetailError(null);
      setDetailLoading(true);
      try {
        const response = await api.getRecord(item.recordType, item.id, item.customerTenantId);
        setDetail(response);
      } catch (error) {
        setDetailError(errorMessage(error, '加载详情失败'));
      } finally {
        setDetailLoading(false);
      }
    },
    [api]
  );

  // ==================== 任务表格 ====================
  const taskColumns: TableColumnsType<MSPTaskItem> = [
    {
      title: '任务',
      dataIndex: 'taskName',
      key: 'taskName',
      render: (value: string, record) => (
        <Space direction="vertical" size={0}>
          <span>{value}</span>
          <span style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
            {record.processDefinitionKey}
          </span>
        </Space>
      ),
    },
    {
      title: '客户',
      dataIndex: 'customerTenantName',
      key: 'customerTenantName',
      render: (value: string | undefined, record) => value || `#${record.customerTenantId}`,
    },
    {
      title: '来源',
      dataIndex: 'source',
      key: 'source',
      render: (value: string) =>
        value === 'queue' ? <Tag color="purple">待处理池</Tag> : <Tag color="cyan">指派给我</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (value: string) => <Tag>{value || '—'}</Tag>,
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      render: (value: string) =>
        value ? <Tag color={PRIORITY_COLOR[value] ?? 'default'}>{value}</Tag> : '—',
    },
    {
      title: '截止',
      dataIndex: 'dueDate',
      key: 'dueDate',
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, record) => (
        <Space size={4}>
          <Popconfirm
            title="认领该任务？"
            okText="认领"
            cancelText="取消"
            onConfirm={() => void handleClaim(record)}
          >
            <span>
              <ActionButton
                item={record}
                action="claim"
                label="认领"
                testId={`task-claim-${record.taskId}`}
                onClick={() => undefined}
              />
            </span>
          </Popconfirm>
          <Popconfirm
            title="确认完成该任务并推进客户流程？"
            okText="完成"
            cancelText="取消"
            onConfirm={() => void handleComplete(record)}
          >
            <span>
              <ActionButton
                item={record}
                action="complete"
                label="完成"
                testId={`task-complete-${record.taskId}`}
                onClick={() => undefined}
              />
            </span>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  // ==================== 单据表格 ====================
  const recordColumns: TableColumnsType<MSPRecordItem> = [
    {
      title: '编号',
      dataIndex: 'number',
      key: 'number',
      render: (value: string, record) => (
        <Button type="link" size="small" style={{ padding: 0 }} onClick={() => void openDetail(record)}>
          {value || `#${record.id}`}
        </Button>
      ),
    },
    { title: '标题', dataIndex: 'title', key: 'title', ellipsis: true },
    {
      title: '客户',
      dataIndex: 'customerName',
      key: 'customerName',
      render: (value: string | undefined, record) => value || `#${record.customerTenantId}`,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (value: string) => <Tag>{recordStatusLabel(value)}</Tag>,
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      render: (value?: string) =>
        value ? <Tag color={PRIORITY_COLOR[value] ?? 'default'}>{value}</Tag> : '—',
    },
    {
      title: '处理人',
      dataIndex: 'assigneeName',
      key: 'assigneeName',
      render: (value: string | undefined) => value || '—',
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, record) => (
        <ActionButton
          item={record}
          action="status"
          label="推进状态"
          testId={`record-status-${record.recordType}-${record.id}`}
          onClick={() => openStatusModal(record)}
        />
      ),
    },
  ];

  const statusOptions = statusTarget
    ? MSP_STATUS_OPTIONS[statusTarget.recordType]
        .filter(option => option.value !== statusTarget.status)
        .map(option => ({ value: option.value, label: option.label }))
    : [];

  return (
    <div data-testid={`cross-tenant-board-${kind}`}>
      {kind === 'tasks' ? (
        <Card
          size="small"
          title="待办任务投影"
          extra={
            <Button
              size="small"
              icon={<RefreshCw size={14} />}
              onClick={() => void loadTasks()}
              loading={tasksLoading}
              data-testid="board-tasks-refresh"
            >
              刷新
            </Button>
          }
        >
          {tasksError && (
            <Alert type="error" showIcon message={tasksError} style={{ marginBottom: 12 }} />
          )}
          <Table<MSPTaskItem>
            rowKey={record => `${record.taskId}`}
            size="small"
            columns={taskColumns}
            dataSource={tasks}
            loading={tasksLoading || taskSubmitting !== null}
            pagination={false}
            locale={{ emptyText: <Empty description="暂无待办任务" /> }}
            data-testid="board-tasks-table"
          />
        </Card>
      ) : (
        <Card
          size="small"
          title={
            <Space size={8}>
              <span>跨租户单据</span>
              <Segmented
                size="small"
                value={recordType}
                options={RECORD_TYPE_ORDER.map(type => ({
                  label: MSP_RECORD_TYPE_LABEL[type],
                  value: type,
                }))}
                onChange={value => setRecordType(value as MSPRecordType)}
                data-testid="board-record-type"
              />
            </Space>
          }
          extra={
            <Button
              size="small"
              icon={<RefreshCw size={14} />}
              onClick={() => void loadRecords()}
              loading={recordsLoading}
              data-testid="board-records-refresh"
            >
              刷新
            </Button>
          }
        >
          {recordsError && (
            <Alert type="error" showIcon message={recordsError} style={{ marginBottom: 12 }} />
          )}
          <Table<MSPRecordItem>
            rowKey={record => `${record.recordType}-${record.id}`}
            size="small"
            columns={recordColumns}
            dataSource={records}
            loading={recordsLoading}
            pagination={false}
            locale={{ emptyText: <Empty description="暂无跨租户单据" /> }}
            data-testid="board-records-table"
          />
          {recordsCursor && (
            <div style={{ textAlign: 'center', marginTop: 12 }}>
              <Button
                size="small"
                loading={recordsLoadingMore}
                onClick={() => void loadRecords(recordsCursor)}
                data-testid="board-records-load-more"
              >
                加载更多
              </Button>
            </div>
          )}
        </Card>
      )}

      {/* 状态推进弹窗（目标状态 = 域白名单 - 当前状态；审批类不在选项内） */}
      <Modal
        open={!!statusTarget}
        title={
          statusTarget
            ? `推进${MSP_RECORD_TYPE_LABEL[statusTarget.recordType]}状态 · ${statusTarget.number}`
            : '推进状态'
        }
        okText="推进"
        cancelText="取消"
        okButtonProps={{ disabled: !targetStatus, loading: statusSubmitting }}
        onOk={() => void submitStatus()}
        onCancel={() => setStatusTarget(null)}
        destroyOnHidden
      >
        {statusTarget && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <div>
              <span style={{ color: DESIGN.colors.textMuted }}>当前状态：</span>
              <Tag>{recordStatusLabel(statusTarget.status)}</Tag>
            </div>
            <Select
              style={{ width: '100%' }}
              placeholder="选择目标状态"
              value={targetStatus || undefined}
              options={statusOptions}
              onChange={value => setTargetStatus(value)}
              data-testid="record-status-select"
            />
            {statusTarget.recordType === 'problem' && (
              <Input.TextArea
                rows={2}
                placeholder="解决方案（可选）"
                value={statusComment}
                onChange={event => setStatusComment(event.target.value)}
                data-testid="record-status-resolution"
              />
            )}
            {statusTarget.recordType === 'change' && (
              <Input.TextArea
                rows={2}
                placeholder="备注（可选）"
                value={statusComment}
                onChange={event => setStatusComment(event.target.value)}
                data-testid="record-status-comment"
              />
            )}
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              审批类动作（变更批准/拒绝、服务请求审批）保留在客户侧治理面，不在工作台开放。
            </Typography.Text>
          </Space>
        )}
      </Modal>

      {/* 详情抽屉（读面；声明租户随请求下发） */}
      <Drawer
        open={!!detailTarget}
        width={560}
        title={detailTarget ? `${detailTarget.number} · ${detailTarget.title}` : '详情'}
        onClose={() => {
          setDetailTarget(null);
          setDetail(null);
          setDetailError(null);
        }}
        destroyOnHidden
      >
        {detailLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}>
            <Spin />
          </div>
        ) : detailError ? (
          <Alert type="error" showIcon message={detailError} />
        ) : detail ? (
          <Descriptions column={1} size="small" bordered data-testid="record-detail">
            <Descriptions.Item label="客户">
              {detail.customerName || `#${detail.customerTenantId}`}
            </Descriptions.Item>
            <Descriptions.Item label="状态">{recordStatusLabel(detail.status)}</Descriptions.Item>
            <Descriptions.Item label="优先级">{detail.priority || '—'}</Descriptions.Item>
            <Descriptions.Item label="处理人">{detail.assigneeName || '—'}</Descriptions.Item>
            <Descriptions.Item label="申请人">{detail.requesterName || '—'}</Descriptions.Item>
            <Descriptions.Item label="创建时间">{formatDateTime(detail.createdAt)}</Descriptions.Item>
            <Descriptions.Item label="更新时间">{formatDateTime(detail.updatedAt)}</Descriptions.Item>
            {detail.description && (
              <Descriptions.Item label="描述">{detail.description}</Descriptions.Item>
            )}
            {detail.recordType === 'change' && (
              <>
                <Descriptions.Item label="变更类型">{detail.type || '—'}</Descriptions.Item>
                <Descriptions.Item label="风险等级">{detail.riskLevel || '—'}</Descriptions.Item>
                <Descriptions.Item label="影响范围">{detail.impactScope || '—'}</Descriptions.Item>
                <Descriptions.Item label="变更理由">{detail.justification || '—'}</Descriptions.Item>
                <Descriptions.Item label="计划开始">
                  {formatDateTime(detail.plannedStartDate)}
                </Descriptions.Item>
                <Descriptions.Item label="计划结束">
                  {formatDateTime(detail.plannedEndDate)}
                </Descriptions.Item>
              </>
            )}
            {detail.recordType === 'problem' && (
              <>
                <Descriptions.Item label="分类">{detail.category || '—'}</Descriptions.Item>
                <Descriptions.Item label="根因">{detail.rootCause || '—'}</Descriptions.Item>
                <Descriptions.Item label="临时措施">{detail.workaround || '—'}</Descriptions.Item>
                <Descriptions.Item label="解决方案">{detail.resolution || '—'}</Descriptions.Item>
                <Descriptions.Item label="影响">{detail.impact || '—'}</Descriptions.Item>
                <Descriptions.Item label="解决时间">
                  {formatDateTime(detail.resolvedAt)}
                </Descriptions.Item>
                <Descriptions.Item label="关闭时间">{formatDateTime(detail.closedAt)}</Descriptions.Item>
              </>
            )}
            {detail.recordType === 'service_request' && (
              <>
                <Descriptions.Item label="审批层级">
                  {detail.currentLevel && detail.totalLevels
                    ? `${detail.currentLevel}/${detail.totalLevels}`
                    : '—'}
                </Descriptions.Item>
                <Descriptions.Item label="当前审批人">
                  {detail.currentApprover || '—'}
                </Descriptions.Item>
                <Descriptions.Item label="审批意见">
                  {detail.approverComment || '—'}
                </Descriptions.Item>
                <Descriptions.Item label="批准时间">
                  {formatDateTime(detail.approvedAt)}
                </Descriptions.Item>
                <Descriptions.Item label="开始时间">
                  {formatDateTime(detail.startedAt)}
                </Descriptions.Item>
                <Descriptions.Item label="完成时间">
                  {formatDateTime(detail.completedAt)}
                </Descriptions.Item>
                <Descriptions.Item label="完成说明">
                  {detail.completionNote || '—'}
                </Descriptions.Item>
              </>
            )}
          </Descriptions>
        ) : null}
      </Drawer>
    </div>
  );
}
