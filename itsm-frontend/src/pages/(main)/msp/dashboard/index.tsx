/**
 * MSP 服务台仪表盘（/msp/dashboard）。
 *
 * 与 /dashboard（单租户运营仪表盘）的差异：
 * - 数据源全部走 MSP 授权面（GET /api/v1/msp/*，allocation 收口），
 *   只读；越界由服务端 403，前端不自行合并跨租户明细；
 * - 门控 = 服务商租户上下文 + 数据面操作码（MspProviderGate，双条件 fail-closed）；
 * - 页面内提供客户范围过滤（URL query customerTenantIds，与顶栏 CustomerFilter 同参），
 *   选择后停留在本页，不跳转工作台。
 *
 * 直观查看多个客户的单据：
 * - 顶部 KPI 汇总（客户数/未关闭/SLA 超期/临近/未指派）；
 * - 协助单据 KPI（事件/问题/已知错误/变更/服务请求 的未结数）；
 * - 全部客户概览表（每客户一行计数徽标 + 风险占比条，点击行 = 收窄客户范围）；
 * - SLA 风险 Top-N（复用 SlaRiskBoard 排序语义）；
 * - 跨租户协助单据总览（每类未结/总量 + 最近更新条目）；
 * - 汇总标签分布（工单状态 × 客户数量）与最近更新的跨客户工单；
 * - 深链：进入工作台 / 待办任务 / 跨租户单据。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import {
  Button,
  Card,
  Empty,
  Segmented,
  Skeleton,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  AlertTriangle,
  ArrowRight,
  BookOpen,
  Bug,
  CheckSquare,
  Clock,
  Inbox,
  RefreshCw,
  Server,
  UserX,
  Users,
  Zap,
} from 'lucide-react';
import { MspProviderGate } from '@/components/auth/MspProviderGate';
import {
  CUSTOMER_TENANT_IDS_PARAM,
  getWorkbenchRecordsSummary,
  parseCustomerTenantIds,
  serializeCustomerTenantIds,
} from '@/lib/api/msp-workbench-api';
import {
  getWorkbenchSummary,
  listWorkbenchTickets,
  type MspRecordsSummary,
  type MspRecordsSummaryRecentItem,
  type MspRecordsSummaryType,
  type MspRecordsSummaryTypeSummary,
  type WorkbenchSummary,
  type WorkbenchSummaryCustomer,
  type WorkbenchTicketItem,
} from '@/lib/api/msp-workbench-api';

const RECENT_TICKETS_LIMIT = 10;
/** 全部客户概览表行数上限：超过则提示进入工作台查看完整列表。 */
const CUSTOMER_TABLE_LIMIT = 20;
/** 服务端未下发 slaDueSoonWindowHours 时的展示缺省值（与后端默认一致）。 */
const DEFAULT_WINDOW_HOURS = 24;

// ==================== 展示口径（与 workbench 同源镜像；仅 UI 标签，不用于授权） ====================

export const MSP_DASH_STATUS_OPTIONS = [
  { value: 'open', label: '待处理' },
  { value: 'in_progress', label: '处理中' },
  { value: 'pending', label: '挂起' },
  { value: 'resolved', label: '已解决' },
  { value: 'closed', label: '已关闭' },
  { value: 'cancelled', label: '已取消' },
] as const;

const STATUS_LABEL = new Map<string, string>(
  MSP_DASH_STATUS_OPTIONS.map(item => [item.value, item.label])
);

export const MSP_DASH_PRIORITY_OPTIONS = [
  { value: 'urgent', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
] as const;

const PRIORITY_LABEL = new Map<string, string>(
  MSP_DASH_PRIORITY_OPTIONS.map(item => [item.value, item.label])
);

const PRIORITY_COLOR: Record<string, string> = {
  urgent: 'red',
  high: 'orange',
  medium: 'blue',
  low: 'default',
};

const STATUS_TAG_COLOR: Record<string, string> = {
  open: 'blue',
  in_progress: 'processing',
  pending: 'gold',
  resolved: 'success',
  closed: 'default',
  cancelled: 'default',
};

export function statusLabel(status?: string): string {
  return STATUS_LABEL.get(status ?? '') ?? (status || '—');
}

export function priorityLabel(priority?: string): string {
  return PRIORITY_LABEL.get(priority ?? '') ?? (priority || '—');
}

// ==================== 协助单据（事件/问题/已知错误/变更/服务请求）展示口径 ====================

/** 协助单据类型 → 中文名 + 图标 + 主题色 + 工作台深链。 */
export const MSP_RECORD_TYPES: Array<{
  key: MspRecordsSummaryType;
  summaryKey: keyof Pick<
    MspRecordsSummary,
    'incident' | 'problem' | 'knownError' | 'change' | 'serviceRequest'
  >;
  label: string;
  shortLabel: string;
  color: string;
  icon: React.ReactNode;
  workbenchPath: string;
}> = [
  {
    key: 'incident',
    summaryKey: 'incident',
    label: '事件',
    shortLabel: '事件',
    color: '#ef4444',
    icon: <Zap size={18} />,
    workbenchPath: '/incidents',
  },
  {
    key: 'problem',
    summaryKey: 'problem',
    label: '问题',
    shortLabel: '问题',
    color: '#f59e0b',
    icon: <Bug size={18} />,
    workbenchPath: '/msp/workbench?board=records&recordType=problem',
  },
  {
    key: 'known_error',
    summaryKey: 'knownError',
    label: '已知错误',
    shortLabel: '已知错误',
    color: '#8b5cf6',
    icon: <BookOpen size={18} />,
    workbenchPath: '/known-errors',
  },
  {
    key: 'change',
    summaryKey: 'change',
    label: '变更',
    shortLabel: '变更',
    color: '#0ea5e9',
    icon: <Server size={18} />,
    workbenchPath: '/msp/workbench?board=records&recordType=change',
  },
  {
    key: 'service_request',
    summaryKey: 'serviceRequest',
    label: '服务请求',
    shortLabel: '服务请求',
    color: '#10b981',
    icon: <CheckSquare size={18} />,
    workbenchPath: '/msp/workbench?board=records&recordType=service_request',
  },
];

/** 协助单据域状态标签（跨域并集；未收录时回落原值）。 */
const RECORD_STATUS_LABEL = new Map<string, string>([
  // incident
  ['new', '新建'],
  ['assigned', '已指派'],
  ['in_progress', '处理中'],
  ['pending', '挂起'],
  ['resolved', '已解决'],
  ['closed', '已关闭'],
  ['cancelled', '已取消'],
  // problem
  ['open', '待处理'],
  ['investigating', '调查中'],
  ['identified', '已定位'],
  ['known_error', '已知错误'],
  // known_error
  ['draft', '草稿'],
  ['active', '生效中'],
  ['deprecated', '已废弃'],
  // change
  ['submitted', '已提交'],
  ['approved', '已批准'],
  ['rejected', '已拒绝'],
  ['scheduled', '已排期'],
  ['completed', '已完成'],
  ['rolled_back', '已回滚'],
  // service_request
  ['pending_approval', '待审批'],
  ['security_approved', '安全审批通过'],
  ['provisioning', '开通中'],
  ['delivered', '已交付'],
  ['failed', '失败'],
]);

export function recordStatusLabel(status?: string): string {
  return RECORD_STATUS_LABEL.get(status ?? '') ?? (status || '—');
}

const RECORD_STATUS_COLOR: Record<string, string> = {
  new: 'blue',
  open: 'blue',
  assigned: 'processing',
  in_progress: 'processing',
  investigating: 'processing',
  provisioning: 'processing',
  pending: 'gold',
  pending_approval: 'gold',
  identified: 'cyan',
  known_error: 'purple',
  active: 'green',
  approved: 'green',
  security_approved: 'green',
  scheduled: 'cyan',
  resolved: 'success',
  completed: 'success',
  delivered: 'success',
  closed: 'default',
  cancelled: 'default',
  deprecated: 'default',
  draft: 'default',
  rejected: 'red',
  failed: 'red',
  rolled_back: 'red',
};

const RECORD_TYPE_LABEL = new Map<string, string>(
  MSP_RECORD_TYPES.map(item => [item.key, item.label])
);

/** 空汇总（请求失败/加载中占位；保持 recent 为空数组以稳定渲染）。 */
function emptyTypeSummary(): MspRecordsSummaryTypeSummary {
  return { total: 0, open: 0, recent: [] };
}

function formatDateTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function formatTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

// ==================== 汇总口径（与 CrossTenantOverview.summarizeCustomers 同语义） ====================

export interface MspDashTotals {
  customers: number;
  open: number;
  slaRisk: number;
  slaDueSoon: number;
  unassigned: number;
}

/** 跨客户汇总：全部为客户维度计数之和（与 summary 契约字段一一对应）。 */
export function summarizeCustomers(customers: WorkbenchSummaryCustomer[]): MspDashTotals {
  return customers.reduce<MspDashTotals>(
    (acc, customer) => ({
      customers: acc.customers + 1,
      open: acc.open + (customer.open || 0),
      slaRisk: acc.slaRisk + (customer.slaRisk || 0),
      slaDueSoon: acc.slaDueSoon + (customer.slaDueSoon || 0),
      unassigned: acc.unassigned + (customer.unassigned || 0),
    }),
    { customers: 0, open: 0, slaRisk: 0, slaDueSoon: 0, unassigned: 0 }
  );
}

/** 风险排序：超期 desc → 临近 desc → 未关闭 desc → 名称（与 SlaRiskBoard 同语义）。 */
export function topRiskCustomers(
  customers: WorkbenchSummaryCustomer[],
  limit = 5
): WorkbenchSummaryCustomer[] {
  return [...customers]
    .sort(
      (x, y) =>
        y.slaRisk - x.slaRisk ||
        y.slaDueSoon - x.slaDueSoon ||
        y.open - x.open ||
        x.customerName.localeCompare(y.customerName)
    )
    .slice(0, limit);
}

export interface MspDashPageProps {
  /** 测试注入用；缺省请求服务端。 */
  fetchSummary?: () => Promise<WorkbenchSummary>;
  fetchTickets?: () => Promise<{ items: WorkbenchTicketItem[]; total: number }>;
  /** 协助单据总览（事件/问题/已知错误/变更/服务请求）；缺省请求服务端。 */
  fetchRecordsSummary?: (query: {
    customerTenantIds?: 'all' | number[];
  }) => Promise<MspRecordsSummary>;
}

// ==================== 子组件 ====================

function KpiTile(props: {
  testId: string;
  label: string;
  value: number;
  icon: React.ReactNode;
  color: string;
  danger?: boolean;
  onClick?: () => void;
}) {
  const valueColor = props.danger && props.value > 0 ? '#dc2626' : undefined;
  const body = (
    <div
      className='flex items-center gap-3 h-full'
      style={{ cursor: props.onClick ? 'pointer' : 'default' }}
    >
      <div
        className='w-10 h-10 rounded-lg flex items-center justify-center shrink-0'
        style={{ backgroundColor: `${props.color}14`, color: props.color }}
      >
        {props.icon}
      </div>
      <div className='min-w-0'>
        <Statistic
          value={props.value}
          valueStyle={{ fontSize: 22, fontWeight: 700, color: valueColor, lineHeight: 1.2 }}
        />
        <div className='text-xs text-gray-500 truncate'>{props.label}</div>
      </div>
    </div>
  );
  if (!props.onClick) {
    return (
      <Card
        size='small'
        className='rounded-xl border border-gray-100 shadow-sm h-full'
        data-testid={props.testId}
      >
        {body}
      </Card>
    );
  }
  return (
    <button
      type='button'
      onClick={props.onClick}
      className='text-left w-full'
      data-testid={props.testId}
    >
      <Card size='small' className='rounded-xl border border-gray-100 shadow-sm h-full hover:shadow-md transition-shadow'>
        {body}
      </Card>
    </button>
  );
}

/** 单客户风险占比条：红=超期 / 橙=临近 / 灰=其余（与 SlaRiskBoard 同视觉语义）。 */
function RiskBar({ customer }: { customer: WorkbenchSummaryCustomer }) {
  const open = Math.max(customer.open, customer.slaRisk + customer.slaDueSoon, 1);
  const riskPct = (customer.slaRisk / open) * 100;
  const duePct = (customer.slaDueSoon / open) * 100;
  const restPct = Math.max(0, 100 - riskPct - duePct);
  return (
    <div
      style={{
        width: 120,
        height: 8,
        background: '#f5f5f5',
        borderRadius: 4,
        overflow: 'hidden',
        display: 'flex',
      }}
    >
      <div style={{ width: `${riskPct}%`, background: '#cf1322', height: '100%' }} />
      <div style={{ width: `${duePct}%`, background: '#fa8c16', height: '100%' }} />
      <div style={{ width: `${restPct}%`, background: '#d9d9d9', height: '100%' }} />
    </div>
  );
}

/** 协助单据 KPI 卡片：未结数为主指标 + 总量/未结占比说明。 */
function RecordKpiTile(props: {
  testId: string;
  label: string;
  summary: MspRecordsSummaryTypeSummary;
  color: string;
  icon: React.ReactNode;
  onClick?: () => void;
}) {
  const { total, open } = props.summary;
  const closed = Math.max(0, total - open);
  const body = (
    <div className='flex items-start gap-3 h-full'>
      <div
        className='w-10 h-10 rounded-lg flex items-center justify-center shrink-0'
        style={{ backgroundColor: `${props.color}14`, color: props.color }}
      >
        {props.icon}
      </div>
      <div className='min-w-0 flex-1'>
        <div className='flex items-baseline gap-2'>
          <Statistic
            value={open}
            valueStyle={{ fontSize: 22, fontWeight: 700, lineHeight: 1.2 }}
          />
          <span className='text-xs text-gray-400'>未结</span>
        </div>
        <div className='text-xs text-gray-500 truncate'>{props.label}</div>
        <div className='text-[11px] text-gray-400 mt-0.5 truncate'>
          总 {total} · 已结 {closed}
        </div>
      </div>
    </div>
  );
  if (!props.onClick) {
    return (
      <Card
        size='small'
        className='rounded-xl border border-gray-100 shadow-sm h-full'
        data-testid={props.testId}
      >
        {body}
      </Card>
    );
  }
  return (
    <button
      type='button'
      onClick={props.onClick}
      className='text-left w-full'
      data-testid={props.testId}
    >
      <Card
        size='small'
        className='rounded-xl border border-gray-100 shadow-sm h-full hover:shadow-md transition-shadow'
      >
        {body}
      </Card>
    </button>
  );
}

/** 跨租户协助单据总览卡：每类一行（未结/总量 + 最近更新条目）。 */
function RecordsOverviewCard(props: {
  summary: MspRecordsSummary | null;
  loading: boolean;
  scopeLabel: string;
  onOpenType: (path: string) => void;
}) {
  const { summary, loading, scopeLabel, onOpenType } = props;
  const columns: TableColumnsType<MspRecordsSummaryRecentItem> = [
    {
      title: '类型',
      dataIndex: 'recordType',
      key: 'recordType',
      width: 92,
      render: (value: string) => (
        <Tag style={{ margin: 0 }}>{RECORD_TYPE_LABEL.get(value) ?? value}</Tag>
      ),
    },
    {
      title: '客户',
      dataIndex: 'customerName',
      key: 'customerName',
      width: 120,
      ellipsis: true,
      render: (value: string | undefined, record) => (
        <span className='text-sm text-gray-800'>
          {value || `#${record.customerTenantId}`}
        </span>
      ),
    },
    {
      title: '编号',
      dataIndex: 'number',
      key: 'number',
      width: 140,
      render: (value: string) => <span className='font-mono text-xs'>{value}</span>,
    },
    {
      title: '标题',
      dataIndex: 'title',
      key: 'title',
      ellipsis: true,
      render: (value: string) => <span className='text-sm'>{value}</span>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (value: string) => (
        <Tag color={RECORD_STATUS_COLOR[value] ?? 'default'} style={{ margin: 0 }}>
          {recordStatusLabel(value)}
        </Tag>
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 150,
      render: (value: string) => (
        <span className='text-xs text-gray-500'>{formatDateTime(value)}</span>
      ),
    },
  ];

  const recentRows = useMemo(() => {
    if (!summary) return [];
    const rows: MspRecordsSummaryRecentItem[] = [];
    for (const type of MSP_RECORD_TYPES) {
      rows.push(...(summary[type.summaryKey]?.recent ?? []));
    }
    return rows
      .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
      .slice(0, 10);
  }, [summary]);

  return (
    <Card
      className='rounded-xl border-0 shadow-sm mb-6'
      styles={{ body: { padding: '20px 24px' } }}
      title={
        <div>
          <div className='text-lg font-bold text-gray-900'>跨租户协助单据总览</div>
          <p className='text-sm text-gray-600 font-normal mt-1 mb-0'>
            范围：{scopeLabel} · 事件 / 问题 / 已知错误 / 变更 / 服务请求 的未结与总量
          </p>
        </div>
      }
      data-testid='msp-dash-records-overview'
    >
      {loading ? (
        <Skeleton active paragraph={{ rows: 4 }} />
      ) : (
        <>
          <div className='grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 mb-4'>
            {MSP_RECORD_TYPES.map(type => {
              const item = summary?.[type.summaryKey] ?? emptyTypeSummary();
              return (
                <button
                  key={type.key}
                  type='button'
                  onClick={() => onOpenType(type.workbenchPath)}
                  data-testid={`msp-dash-record-stat-${type.key}`}
                  className='text-left rounded-lg border border-gray-100 px-3 py-2 hover:border-blue-200 hover:bg-blue-50/40 transition-colors'
                >
                  <div className='flex items-center gap-2'>
                    <span style={{ color: type.color }}>{type.icon}</span>
                    <span className='text-xs text-gray-500'>{type.label}</span>
                  </div>
                  <div className='mt-1 flex items-baseline gap-1.5'>
                    <span className='text-xl font-bold text-gray-900'>{item.open}</span>
                    <span className='text-[11px] text-gray-400'>未结</span>
                    <span className='text-[11px] text-gray-400 ml-auto'>/ {item.total}</span>
                  </div>
                </button>
              );
            })}
          </div>
          <Table<MspRecordsSummaryRecentItem>
            rowKey={record => `${record.recordType}-${record.id}`}
            size='small'
            columns={columns}
            dataSource={recentRows}
            pagination={false}
            locale={{ emptyText: '当前范围内暂无协助单据' }}
            data-testid='msp-dash-records-recent'
          />
        </>
      )}
    </Card>
  );
}

// ==================== 页面 ====================

function MspDashboardInner({ fetchSummary, fetchTickets, fetchRecordsSummary }: MspDashPageProps) {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const scopeRaw = searchParams.get(CUSTOMER_TENANT_IDS_PARAM);
  const customerScope = useMemo(() => parseCustomerTenantIds(scopeRaw), [scopeRaw]);

  // ==================== summary（客户计数徽标，服务端 30s 缓存） ====================
  const [summary, setSummary] = useState<WorkbenchSummary | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(true);
  const [summaryError, setSummaryError] = useState<string | null>(null);

  const loadSummary = useCallback(async () => {
    setSummaryLoading(true);
    setSummaryError(null);
    try {
      const response = await (fetchSummary ? fetchSummary() : getWorkbenchSummary());
      setSummary(response ?? null);
    } catch (err) {
      setSummary(null);
      setSummaryError(errorMessage(err, '加载跨客户汇总失败'));
    } finally {
      setSummaryLoading(false);
    }
  }, [fetchSummary]);

  useEffect(() => {
    void loadSummary();
  }, [loadSummary]);

  // ==================== 最近工单（跨客户列表，只读摘要） ====================
  const [recentTickets, setRecentTickets] = useState<WorkbenchTicketItem[]>([]);
  const [recentLoading, setRecentLoading] = useState(true);
  const [recentError, setRecentError] = useState<string | null>(null);

  const loadRecent = useCallback(async () => {
    setRecentLoading(true);
    setRecentError(null);
    try {
      const loader = fetchTickets
        ? fetchTickets
        : () =>
            listWorkbenchTickets({
              customerTenantIds: customerScope,
              sort: 'updated',
              limit: RECENT_TICKETS_LIMIT,
            });
      const response = await loader();
      setRecentTickets(response?.items ?? []);
    } catch (err) {
      setRecentTickets([]);
      setRecentError(errorMessage(err, '加载最近工单失败'));
    } finally {
      setRecentLoading(false);
    }
  }, [customerScope, fetchTickets]);

  useEffect(() => {
    void loadRecent();
  }, [loadRecent]);

  // ==================== 协助单据总览（事件/问题/已知错误/变更/服务请求） ====================
  const [recordsSummary, setRecordsSummary] = useState<MspRecordsSummary | null>(null);
  const [recordsLoading, setRecordsLoading] = useState(true);
  const [recordsError, setRecordsError] = useState<string | null>(null);

  const loadRecordsSummary = useCallback(async () => {
    setRecordsLoading(true);
    setRecordsError(null);
    try {
      const loader = fetchRecordsSummary
        ? fetchRecordsSummary
        : getWorkbenchRecordsSummary;
      const response = await loader({ customerTenantIds: customerScope });
      setRecordsSummary(response ?? null);
    } catch (err) {
      setRecordsSummary(null);
      setRecordsError(errorMessage(err, '加载协助单据总览失败'));
    } finally {
      setRecordsLoading(false);
    }
  }, [customerScope, fetchRecordsSummary]);

  useEffect(() => {
    void loadRecordsSummary();
  }, [loadRecordsSummary]);

  // ==================== 状态分布（基于当前范围的 summary 汇总标签） ====================
  const allCustomers = useMemo(() => summary?.customers ?? [], [summary]);
  const scopedCustomers = useMemo(
    () =>
      customerScope === 'all'
        ? allCustomers
        : allCustomers.filter(c => customerScope.includes(c.customerTenantId)),
    [allCustomers, customerScope]
  );
  const totals = useMemo(() => summarizeCustomers(scopedCustomers), [scopedCustomers]);
  const windowHours = summary?.slaDueSoonWindowHours ?? DEFAULT_WINDOW_HOURS;

  /** 客户范围收窄：点击客户行 → 本页 URL query（与顶栏 CustomerFilter 同参）。 */
  const selectCustomer = useCallback(
    (customerTenantId: number) => {
      const params = new URLSearchParams(searchParams);
      params.set(CUSTOMER_TENANT_IDS_PARAM, String(customerTenantId));
      setSearchParams(params);
    },
    [searchParams, setSearchParams]
  );

  /** 客户范围切换：全部 / 单客户（与顶栏 CustomerFilter 语义一致：空数组 = 全部）。 */
  const changeScope = useCallback(
    (value: string) => {
      const params = new URLSearchParams(searchParams);
      if (value === 'all') {
        params.delete(CUSTOMER_TENANT_IDS_PARAM);
      } else {
        params.set(CUSTOMER_TENANT_IDS_PARAM, serializeCustomerTenantIds([Number(value)]));
      }
      setSearchParams(params);
    },
    [searchParams, setSearchParams]
  );

  const goWorkbench = useCallback(
    (extra?: { board?: 'tasks' | 'records' }) => {
      const params = new URLSearchParams();
      if (customerScope !== 'all') {
        params.set(CUSTOMER_TENANT_IDS_PARAM, serializeCustomerTenantIds(customerScope));
      }
      if (extra?.board) params.set('board', extra.board);
      const suffix = params.toString();
      navigate(`/msp/workbench${suffix ? `?${suffix}` : ''}`);
    },
    [customerScope, navigate]
  );

  /** 协助单据卡片点击：工作台类深链带上当前客户范围；客户侧页面直接跳转。 */
  const openRecordType = useCallback(
    (path: string) => {
      if (!path.startsWith('/msp/workbench')) {
        navigate(path);
        return;
      }
      const params = new URLSearchParams(path.split('?')[1] ?? '');
      if (customerScope !== 'all') {
        params.set(CUSTOMER_TENANT_IDS_PARAM, serializeCustomerTenantIds(customerScope));
      }
      const suffix = params.toString();
      navigate(`/msp/workbench${suffix ? `?${suffix}` : ''}`);
    },
    [customerScope, navigate]
  );

  const scopeLabel =
    customerScope === 'all' ? '全部客户' : `${customerScope.length} 个客户`;

  // ==================== 全部客户概览表列 ====================
  const customerColumns: TableColumnsType<WorkbenchSummaryCustomer> = useMemo(
    () => [
      {
        title: '客户',
        dataIndex: 'customerName',
        key: 'customerName',
        render: (name: string | undefined, record) => (
          <span className='text-sm text-gray-800'>
            {name || `#${record.customerTenantId}`}
          </span>
        ),
      },
      {
        title: '未关闭',
        dataIndex: 'open',
        key: 'open',
        width: 90,
        align: 'right',
        render: (value: number) => <Tag color='blue' style={{ margin: 0 }}>{value}</Tag>,
      },
      {
        title: 'SLA 超期',
        dataIndex: 'slaRisk',
        key: 'slaRisk',
        width: 100,
        align: 'right',
        render: (value: number) =>
          value > 0 ? <Tag color='red' style={{ margin: 0 }}>{value}</Tag> : <span className='text-gray-400'>0</span>,
      },
      {
        title: `临近到期（${windowHours}h）`,
        dataIndex: 'slaDueSoon',
        key: 'slaDueSoon',
        width: 130,
        align: 'right',
        render: (value: number) =>
          value > 0 ? <Tag color='orange' style={{ margin: 0 }}>{value}</Tag> : <span className='text-gray-400'>0</span>,
      },
      {
        title: '未指派',
        dataIndex: 'unassigned',
        key: 'unassigned',
        width: 90,
        align: 'right',
        render: (value: number) =>
          value > 0 ? <Tag style={{ margin: 0 }}>{value}</Tag> : <span className='text-gray-400'>0</span>,
      },
      {
        title: '成员',
        dataIndex: 'members',
        key: 'members',
        width: 70,
        align: 'right',
        render: (value: number | undefined) => <span className='text-gray-600'>{value ?? '—'}</span>,
      },
      {
        title: '近30天新建',
        dataIndex: 'ticketsCreated30d',
        key: 'ticketsCreated30d',
        width: 110,
        align: 'right',
        render: (value: number | undefined) => <span className='text-gray-600'>{value ?? '—'}</span>,
      },
      {
        title: '风险占比',
        key: 'risk',
        width: 140,
        render: (_: unknown, record) => <RiskBar customer={record} />,
      },
    ],
    [windowHours]
  );

  // ==================== 最近工单列 ====================
  const recentColumns: TableColumnsType<WorkbenchTicketItem> = useMemo(
    () => [
      {
        title: '客户',
        dataIndex: 'customerName',
        key: 'customerName',
        width: 130,
        render: (name: string | undefined, record) => (
          <span className='text-sm text-gray-800 truncate'>
            {name || `#${record.customerTenantId}`}
          </span>
        ),
      },
      {
        title: '工单号',
        dataIndex: 'ticketNumber',
        key: 'ticketNumber',
        width: 120,
        render: (value: string) => <span className='font-mono text-xs'>{value}</span>,
      },
      {
        title: '标题',
        dataIndex: 'title',
        key: 'title',
        ellipsis: true,
        render: (value: string) => <span className='text-sm'>{value}</span>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 90,
        render: (value: string) => (
          <Tag color={STATUS_TAG_COLOR[value] ?? 'default'} style={{ margin: 0 }}>
            {statusLabel(value)}
          </Tag>
        ),
      },
      {
        title: '优先级',
        dataIndex: 'priority',
        key: 'priority',
        width: 80,
        render: (value: string) => (
          <Tag color={PRIORITY_COLOR[value] ?? 'default'} style={{ margin: 0 }}>
            {priorityLabel(value)}
          </Tag>
        ),
      },
      {
        title: '处理人',
        dataIndex: 'assigneeName',
        key: 'assigneeName',
        width: 100,
        render: (value: string | undefined) =>
          value ? <span className='text-sm text-gray-700'>{value}</span> : <Tag style={{ margin: 0 }}>未指派</Tag>,
      },
      {
        title: '更新时间',
        dataIndex: 'updatedAt',
        key: 'updatedAt',
        width: 150,
        render: (value: string) => <span className='text-xs text-gray-500'>{formatDateTime(value)}</span>,
      },
    ],
    []
  );

  const recentDataSource = useMemo(
    () => recentTickets.slice(0, RECENT_TICKETS_LIMIT),
    [recentTickets]
  );

  const showCustomerTable = scopedCustomers.length > 0;
  const customerTableRows = useMemo(
    () => scopedCustomers.slice(0, CUSTOMER_TABLE_LIMIT),
    [scopedCustomers]
  );

  return (
    <div
      className='p-6 min-h-screen'
      style={{ backgroundColor: 'var(--color-bg-secondary, #f9fafb)' }}
    >
      {/* 顶栏 */}
      <div className='flex items-center justify-between mb-6 pb-4 border-b border-gray-200 flex-wrap gap-3'>
        <div>
          <h1 className='text-2xl font-bold text-gray-900 flex items-center gap-2 flex-wrap'>
            MSP 服务台仪表盘
            <Tag color='geekblue' style={{ margin: 0 }}>
              服务商视角 · 跨客户
            </Tag>
          </h1>
          <p className='text-sm text-gray-600 mt-1'>
            直观查看多个客户的服务单据与 SLA 风险；明细操作请进入跨客户工作台
          </p>
        </div>
        <Space size='small' wrap>
          <Segmented
            data-testid='msp-dash-scope'
            value={customerScope === 'all' ? 'all' : String(customerScope[0])}
            options={[
              { value: 'all', label: '全部客户' },
              ...allCustomers.slice(0, 8).map(customer => ({
                value: String(customer.customerTenantId),
                label: customer.customerName || `#${customer.customerTenantId}`,
              })),
            ]}
            onChange={value => changeScope(String(value))}
          />
          <Button
            icon={
              <RefreshCw
                className={summaryLoading || recentLoading || recordsLoading ? 'animate-spin' : ''}
              />
            }
            onClick={() => {
              void loadSummary();
              void loadRecent();
              void loadRecordsSummary();
            }}
            loading={summaryLoading || recentLoading || recordsLoading}
            data-testid='msp-dash-refresh'
          >
            刷新
          </Button>
          <Button
            type='primary'
            icon={<ArrowRight size={14} />}
            onClick={() => goWorkbench()}
            data-testid='msp-dash-goto-workbench'
          >
            进入工作台
          </Button>
        </Space>
      </div>

      {/* KPI 汇总 */}
      <div className='mb-6 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4' data-testid='msp-dash-kpis'>
        <KpiTile
          testId='msp-dash-kpi-customers'
          label='服务客户数'
          value={totals.customers}
          color='#6366f1'
          icon={<Users size={18} />}
        />
        <KpiTile
          testId='msp-dash-kpi-open'
          label='未关闭工单'
          value={totals.open}
          color='#3b82f6'
          icon={<Inbox size={18} />}
        />
        <KpiTile
          testId='msp-dash-kpi-risk'
          label='已超 SLA（超期）'
          value={totals.slaRisk}
          color='#ef4444'
          danger
          icon={<AlertTriangle size={18} />}
        />
        <KpiTile
          testId='msp-dash-kpi-due-soon'
          label={`临近到期（${windowHours}h）`}
          value={totals.slaDueSoon}
          color='#f59e0b'
          danger
          icon={<Clock size={18} />}
        />
        <KpiTile
          testId='msp-dash-kpi-unassigned'
          label='未指派工单'
          value={totals.unassigned}
          color='#8b5cf6'
          icon={<UserX size={18} />}
        />
      </div>

      {/* 协助单据 KPI（事件/问题/已知错误/变更/服务请求 的未结数） */}
      <div
        className='mb-6 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4'
        data-testid='msp-dash-record-kpis'
      >
        {MSP_RECORD_TYPES.map(type => (
          <RecordKpiTile
            key={type.key}
            testId={`msp-dash-kpi-${type.key.replace('_', '-')}`}
            label={`${type.label}（未结）`}
            summary={recordsSummary?.[type.summaryKey] ?? emptyTypeSummary()}
            color={type.color}
            icon={type.icon}
            onClick={() => openRecordType(type.workbenchPath)}
          />
        ))}
      </div>

      {recordsError && (
        <div className='mb-6' data-testid='msp-dash-records-error'>
          <Typography.Text type='danger'>协助单据总览加载失败：{recordsError}</Typography.Text>
          <div className='mt-2'>
            <Button
              size='small'
              onClick={() => void loadRecordsSummary()}
              data-testid='msp-dash-records-retry'
            >
              重试
            </Button>
          </div>
        </div>
      )}

      {summaryError && (
        <div className='mb-6' data-testid='msp-dash-summary-error'>
          <Typography.Text type='danger'>跨客户汇总加载失败：{summaryError}</Typography.Text>
          <div className='mt-2'>
            <Button size='small' onClick={() => void loadSummary()} data-testid='msp-dash-summary-retry'>
              重试
            </Button>
          </div>
        </div>
      )}

      {summaryLoading ? (
        <div className='mb-6'>
          <Skeleton active paragraph={{ rows: 4 }} />
        </div>
      ) : !showCustomerTable ? (
        <div className='mb-6'>
          <Card className='rounded-xl border-0 shadow-sm'>
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description='暂无已分配的客户'
              data-testid='msp-dash-empty'
            />
          </Card>
        </div>
      ) : (
        <>
          {/* 全部客户概览 */}
          <Card
            className='rounded-xl border-0 shadow-sm mb-6'
            styles={{ body: { padding: '20px 24px' } }}
            title={
              <div>
                <div className='text-lg font-bold text-gray-900'>全部客户概览</div>
                <p className='text-sm text-gray-600 font-normal mt-1 mb-0'>
                  当前范围：{scopeLabel} · 点击客户行可在本页收窄范围
                </p>
              </div>
            }
            extra={
              <Typography.Text type='secondary' style={{ fontSize: 12 }}>
                {summary?.generatedAt ? `数据生成于 ${formatTime(summary.generatedAt)}` : null}
              </Typography.Text>
            }
            data-testid='msp-dash-customer-table'
          >
            <Table<WorkbenchSummaryCustomer>
              rowKey='customerTenantId'
              size='small'
              columns={customerColumns}
              dataSource={customerTableRows}
              pagination={false}
              onRow={record => ({
                onClick: () => selectCustomer(record.customerTenantId),
                style: { cursor: 'pointer' },
                'data-testid': `msp-dash-customer-row-${record.customerTenantId}`,
              })}
              rowClassName='hover:bg-blue-50/40'
            />
            {scopedCustomers.length > CUSTOMER_TABLE_LIMIT && (
              <div className='mt-3 text-xs text-gray-500'>
                已展示前 {CUSTOMER_TABLE_LIMIT} 个客户，
                <Button
                  type='link'
                  size='small'
                  onClick={() => goWorkbench()}
                  className='px-0'
                >
                  进入工作台查看完整列表
                </Button>
              </div>
            )}
          </Card>

          {/* SLA 风险 Top-N */}
          <Card
            className='rounded-xl border-0 shadow-sm mb-6'
            styles={{ body: { padding: '20px 24px' } }}
            title={<div className='text-lg font-bold text-gray-900'>SLA 风险 Top {Math.min(5, scopedCustomers.length)}</div>}
            data-testid='msp-dash-top-risk'
          >
            {(() => {
              const topRisk = topRiskCustomers(scopedCustomers, 5);
              if (topRisk.length === 0) {
                return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description='无数据' />;
              }
              return (
                <Space direction='vertical' size={6} style={{ width: '100%' }}>
                  {topRisk.map(customer => (
                    <button
                      key={customer.customerTenantId}
                      type='button'
                      onClick={() => selectCustomer(customer.customerTenantId)}
                      data-testid={`msp-dash-risk-row-${customer.customerTenantId}`}
                      className='w-full text-left flex items-center justify-between gap-3 px-3 py-2 rounded-lg border border-gray-100 hover:border-blue-200 hover:bg-blue-50/40 transition-colors cursor-pointer'
                    >
                      <span className='text-sm text-gray-800 truncate'>
                        {customer.customerName || `#${customer.customerTenantId}`}
                      </span>
                      <Space size={4} wrap>
                        <Tag color='blue' style={{ margin: 0 }}>未关闭 {customer.open}</Tag>
                        {customer.slaRisk > 0 && (
                          <Tag color='red' style={{ margin: 0 }}>超期 {customer.slaRisk}</Tag>
                        )}
                        {customer.slaDueSoon > 0 && (
                          <Tag color='orange' style={{ margin: 0 }}>临近 {customer.slaDueSoon}</Tag>
                        )}
                        {customer.unassigned > 0 && (
                          <Tag style={{ margin: 0 }}>未指派 {customer.unassigned}</Tag>
                        )}
                      </Space>
                    </button>
                  ))}
                </Space>
              );
            })()}
          </Card>
        </>
      )}

      {/* 跨租户协助单据总览（事件/问题/已知错误/变更/服务请求） */}
      <RecordsOverviewCard
        summary={recordsSummary}
        loading={recordsLoading}
        scopeLabel={scopeLabel}
        onOpenType={openRecordType}
      />

      {/* 最近更新的跨客户工单 */}
      <Card
        className='rounded-xl border-0 shadow-sm mb-6'
        styles={{ body: { padding: '20px 24px' } }}
        title={
          <div className='flex items-center justify-between flex-wrap gap-2'>
            <div>
              <div className='text-lg font-bold text-gray-900'>最近更新的工单</div>
              <p className='text-sm text-gray-600 font-normal mt-1 mb-0'>
                范围：{scopeLabel} · 最多 {RECENT_TICKETS_LIMIT} 条 · 点击「进入工作台」处理
              </p>
            </div>
            <Button size='small' onClick={() => goWorkbench()} data-testid='msp-dash-goto-workbench-list'>
              在工作台中查看全部
            </Button>
          </div>
        }
        data-testid='msp-dash-recent'
      >
        {recentError ? (
          <div data-testid='msp-dash-recent-error'>
            <Typography.Text type='danger'>最近工单加载失败：{recentError}</Typography.Text>
            <div className='mt-2'>
              <Button size='small' onClick={() => void loadRecent()}>
                重试
              </Button>
            </div>
          </div>
        ) : (
          <Table<WorkbenchTicketItem>
            rowKey='id'
            size='small'
            columns={recentColumns}
            dataSource={recentDataSource}
            loading={recentLoading}
            pagination={false}
            locale={{ emptyText: '当前范围内暂无工单' }}
          />
        )}
      </Card>

      {/* 底部说明 */}
      <div className='text-xs text-gray-500 flex items-center justify-between flex-wrap gap-2 pt-4 border-t border-gray-100'>
        <span>
          本页为跨客户只读摘要（allocation 授权面 + 服务端 30s 缓存）；写操作一律在工作台内完成
        </span>
        <Space size='small' wrap>
          <Button size='small' onClick={() => goWorkbench({ board: 'tasks' })} data-testid='msp-dash-goto-tasks'>
            待办任务
          </Button>
          <Button size='small' onClick={() => goWorkbench({ board: 'records' })} data-testid='msp-dash-goto-records'>
            跨租户单据
          </Button>
        </Space>
      </div>
    </div>
  );
}

/**
 * MSP 服务台仪表盘（路由 /msp/dashboard）。
 *
 * 门控：MspProviderGate（服务商租户上下文 + msp_ticket:read）；
 * 客户租户（即使角色克隆残留 msp_* 码）整体隐藏，不渲染、不发请求。
 * 可选 fetch 注入仅用于测试（生产渲染不传，走真实 API）。
 */
export default function MspDashboardPage(props: MspDashPageProps = {}) {
  return (
    <MspProviderGate>
      <MspDashboardInner {...props} />
    </MspProviderGate>
  );
}
