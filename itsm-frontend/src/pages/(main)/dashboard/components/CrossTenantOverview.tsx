/**
 * CrossTenantOverview：/dashboard 的「跨租户总览」只读分区（MSP 授权用户专用）。
 *
 * 缺口治理背景：
 * - /dashboard 数据源（GET /api/v1/dashboard/overview）在服务端按当前租户过滤，
 *   MSP 服务商员工只能看到 provider 自身租户，客户侧统计全部缺失；
 * - 本分区复用 GET /api/v1/msp/workbench/summary（allocation 授权面 + 服务端 30s 缓存），
 *   只做「汇总 + Top-N 风险客户 + 下钻深链」，不复制工作台的列表与写操作；
 * - 非 MSP 用户由父级 PermissionGate 整体隐藏，本组件不做权限判定、不发起请求。
 *
 * 安全边界：全部只读；下钻链接携带 customerTenantIds，越界由服务端 403，
 * 前端不自行合并跨租户明细。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { Button, Card, Col, Empty, Row, Skeleton, Space, Tag, Typography } from 'antd';
import {
  AlertTriangle,
  ArrowRight,
  Clock,
  Inbox,
  RefreshCw,
  UserX,
  Users,
} from 'lucide-react';
import {
  CUSTOMER_TENANT_IDS_PARAM,
  getWorkbenchSummary,
  type WorkbenchSummary,
  type WorkbenchSummaryCustomer,
} from '@/lib/api/msp-workbench-api';

/** Top 风险客户列表长度。 */
export const TOP_RISK_LIMIT = 5;
/** 服务端未下发 slaDueSoonWindowHours 时的展示缺省值（与后端默认一致）。 */
const DEFAULT_WINDOW_HOURS = 24;

export interface CrossTenantOverviewProps {
  /** 测试注入用；缺省请求服务端 summary。 */
  fetchSummary?: () => Promise<WorkbenchSummary>;
  /** 测试注入用；缺省走 react-router navigate。 */
  onNavigate?: (path: string) => void;
}

export interface CrossTenantTotals {
  customers: number;
  open: number;
  slaRisk: number;
  slaDueSoon: number;
  unassigned: number;
}

/** 跨租户汇总口径：全部为客户维度计数之和（与 summary 契约字段一一对应）。 */
export function summarizeCustomers(customers: WorkbenchSummaryCustomer[]): CrossTenantTotals {
  return customers.reduce<CrossTenantTotals>(
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
  limit: number = TOP_RISK_LIMIT
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

/** 工作台深链：board 切换 + 客户范围过滤均由工作台 URL 契约承接。 */
export function buildWorkbenchPath(options: {
  board?: 'tasks' | 'records';
  customerTenantId?: number;
} = {}): string {
  const params = new URLSearchParams();
  if (options.board) params.set('board', options.board);
  if (typeof options.customerTenantId === 'number') {
    params.set(CUSTOMER_TENANT_IDS_PARAM, String(options.customerTenantId));
  }
  const suffix = params.toString();
  return `/msp/workbench${suffix ? `?${suffix}` : ''}`;
}

function formatTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

function StatTile(props: {
  testId: string;
  label: string;
  value: number;
  icon: React.ReactNode;
  danger?: boolean;
  color: string;
}) {
  const valueColor = props.danger && props.value > 0 ? '#dc2626' : '#111827';
  return (
    <Card
      size='small'
      className='rounded-xl border border-gray-100 shadow-sm h-full'
      data-testid={props.testId}
    >
      <div className='flex items-center gap-3'>
        <div
          className='w-10 h-10 rounded-lg flex items-center justify-center shrink-0'
          style={{ backgroundColor: `${props.color}14`, color: props.color }}
        >
          {props.icon}
        </div>
        <div className='min-w-0'>
          <div className='text-xl font-bold leading-tight' style={{ color: valueColor }}>
            {props.value}
          </div>
          <div className='text-xs text-gray-500 truncate'>{props.label}</div>
        </div>
      </div>
    </Card>
  );
}

export function CrossTenantOverview({
  fetchSummary,
  onNavigate,
}: CrossTenantOverviewProps) {
  const navigate = useNavigate();
  const [summary, setSummary] = useState<WorkbenchSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await (fetchSummary ? fetchSummary() : getWorkbenchSummary());
      setSummary(response ?? null);
    } catch (err) {
      setSummary(null);
      setError(err instanceof Error && err.message ? err.message : '加载跨租户数据失败');
    } finally {
      setLoading(false);
    }
  }, [fetchSummary]);

  useEffect(() => {
    void load();
  }, [load]);

  const go = useCallback(
    (path: string) => {
      if (onNavigate) {
        onNavigate(path);
        return;
      }
      navigate(path);
    },
    [navigate, onNavigate]
  );

  const customers = useMemo(() => summary?.customers ?? [], [summary]);
  const totals = useMemo(() => summarizeCustomers(customers), [customers]);
  const topRisk = useMemo(() => topRiskCustomers(customers), [customers]);
  const windowHours = summary?.slaDueSoonWindowHours ?? DEFAULT_WINDOW_HOURS;

  return (
    <Card
      className='rounded-xl border-0 shadow-sm'
      styles={{ body: { padding: '20px 24px' } }}
      data-testid='cross-tenant-overview'
      title={
        <div>
          <div className='flex items-center gap-2'>
            <span className='text-lg font-bold text-gray-900'>跨租户总览</span>
            <Tag color='geekblue' style={{ margin: 0 }}>
              服务商视角 · 全部客户
            </Tag>
          </div>
          <p className='text-sm text-gray-600 font-normal mt-1 mb-0'>
            客户侧工单与 SLA 风险的只读摘要，明细操作请进入跨客户工作台
          </p>
        </div>
      }
      extra={
        <Space size='small'>
          {summary?.generatedAt && (
            <Typography.Text type='secondary' style={{ fontSize: 12 }}>
              数据生成于 {formatTime(summary.generatedAt)}
            </Typography.Text>
          )}
          <Button
            size='small'
            icon={<RefreshCw size={14} className={loading ? 'animate-spin' : ''} />}
            onClick={() => void load()}
            loading={loading}
            data-testid='cross-tenant-refresh'
          >
            刷新
          </Button>
        </Space>
      }
    >
      {error ? (
        <div data-testid='cross-tenant-error'>
          <Typography.Text type='danger'>跨租户数据加载失败：{error}</Typography.Text>
          <div className='mt-3'>
            <Button size='small' onClick={() => void load()}>
              重试
            </Button>
          </div>
        </div>
      ) : loading ? (
        <Skeleton active paragraph={{ rows: 3 }} />
      ) : customers.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description='暂无已分配的客户'
          data-testid='cross-tenant-empty'
        />
      ) : (
        <>
          <Row gutter={[12, 12]}>
            <Col xs={12} sm={8} lg={4}>
              <StatTile
                testId='cross-tenant-stat-customers'
                label='服务客户数'
                value={totals.customers}
                color='#6366f1'
                icon={<Users size={18} />}
              />
            </Col>
            <Col xs={12} sm={8} lg={5}>
              <StatTile
                testId='cross-tenant-stat-open'
                label='未关闭工单'
                value={totals.open}
                color='#3b82f6'
                icon={<Inbox size={18} />}
              />
            </Col>
            <Col xs={12} sm={8} lg={5}>
              <StatTile
                testId='cross-tenant-stat-risk'
                label='已超 SLA（超期）'
                value={totals.slaRisk}
                color='#ef4444'
                danger
                icon={<AlertTriangle size={18} />}
              />
            </Col>
            <Col xs={12} sm={8} lg={5}>
              <StatTile
                testId='cross-tenant-stat-due-soon'
                label={`临近到期（${windowHours}h）`}
                value={totals.slaDueSoon}
                color='#f59e0b'
                danger
                icon={<Clock size={18} />}
              />
            </Col>
            <Col xs={12} sm={8} lg={5}>
              <StatTile
                testId='cross-tenant-stat-unassigned'
                label='未指派工单'
                value={totals.unassigned}
                color='#8b5cf6'
                icon={<UserX size={18} />}
              />
            </Col>
          </Row>

          <div className='mt-5' data-testid='cross-tenant-top-risk'>
            <div className='text-sm font-semibold text-gray-900 mb-2'>
              SLA 风险客户 Top {Math.min(TOP_RISK_LIMIT, customers.length)}
            </div>
            <Space direction='vertical' size={6} style={{ width: '100%' }}>
              {topRisk.map(customer => (
                <button
                  key={customer.customerTenantId}
                  type='button'
                  onClick={() =>
                    go(buildWorkbenchPath({ customerTenantId: customer.customerTenantId }))
                  }
                  data-testid={`cross-tenant-customer-${customer.customerTenantId}`}
                  className='w-full text-left flex items-center justify-between gap-3 px-3 py-2 rounded-lg border border-gray-100 hover:border-blue-200 hover:bg-blue-50/40 transition-colors cursor-pointer'
                >
                  <span className='text-sm text-gray-800 truncate'>
                    {customer.customerName || `#${customer.customerTenantId}`}
                  </span>
                  <Space size={4} wrap>
                    <Tag color='blue' style={{ margin: 0 }}>
                      未关闭 {customer.open}
                    </Tag>
                    {customer.slaRisk > 0 && (
                      <Tag color='red' style={{ margin: 0 }}>
                        超期 {customer.slaRisk}
                      </Tag>
                    )}
                    {customer.slaDueSoon > 0 && (
                      <Tag color='orange' style={{ margin: 0 }}>
                        临近 {customer.slaDueSoon}
                      </Tag>
                    )}
                    {customer.unassigned > 0 && (
                      <Tag style={{ margin: 0 }}>未指派 {customer.unassigned}</Tag>
                    )}
                  </Space>
                </button>
              ))}
            </Space>
          </div>
        </>
      )}

      <div className='mt-5 pt-4 border-t border-gray-100 flex items-center justify-between flex-wrap gap-2'>
        <span className='text-xs text-gray-500'>
          /dashboard 展示当前租户口径；本分区为跨租户只读摘要，不含写操作
        </span>
        <Space size='small' wrap>
          <Button
            size='small'
            onClick={() => go(buildWorkbenchPath({ board: 'tasks' }))}
            data-testid='cross-tenant-goto-tasks'
          >
            待办任务
          </Button>
          <Button
            size='small'
            onClick={() => go(buildWorkbenchPath({ board: 'records' }))}
            data-testid='cross-tenant-goto-records'
          >
            跨租户单据
          </Button>
          <Button
            type='primary'
            size='small'
            icon={<ArrowRight size={14} />}
            onClick={() => go(buildWorkbenchPath())}
            data-testid='cross-tenant-goto-workbench'
          >
            进入跨租户工作台
          </Button>
        </Space>
      </div>
    </Card>
  );
}

export default CrossTenantOverview;
