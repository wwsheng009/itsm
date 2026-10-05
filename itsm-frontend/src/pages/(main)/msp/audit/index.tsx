/**
 * MSP 审计看板（IP-P1-8）——provider 治理可观测。
 *
 * - 聚合口径：provider home 租户窗口内（默认 30 天）跨租户审计事件；
 * - 面板一：越权尝试/冲突告警（tenant.scope_denied / tenant.probe_denied）明细；
 * - 面板二：按来源 / 动作 / 目标客户 / membership 的分布。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Empty, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import type { TableColumnsType } from 'antd';
import { RefreshCw, ShieldAlert } from 'lucide-react';
import {
  MSP_AUDIT_ACTION_LABEL,
  MSP_AUDIT_SOURCE_LABEL,
  getMspAuditSummary,
  type MspAuditAggRow,
  type MspAuditDenialRow,
  type MspAuditSummary,
} from '@/lib/api/msp-audit-api';

const { Text, Title } = Typography;

export const AUDIT_WINDOW_OPTIONS = [
  { value: 7, label: '近 7 天' },
  { value: 30, label: '近 30 天' },
  { value: 90, label: '近 90 天' },
];

const STATUS_COLOR: Record<number, string> = { 401: 'orange', 403: 'red', 404: 'default' };

function actionLabel(action: string): string {
  return MSP_AUDIT_ACTION_LABEL[action] || action;
}

function sourceLabel(source: string): string {
  return MSP_AUDIT_SOURCE_LABEL[source] || source;
}

function AggTags({ rows, emptyText }: { rows: MspAuditAggRow[]; emptyText: string }) {
  if (!rows || rows.length === 0) {
    return <Text type='secondary'>{emptyText}</Text>;
  }
  return (
    <Space size={[8, 8]} wrap>
      {rows.map(row => (
        <Tag key={row.key}>
          {row.label || sourceLabel(row.key)} · {row.count}
        </Tag>
      ))}
    </Space>
  );
}

const denialColumns: TableColumnsType<MspAuditDenialRow> = [
  {
    title: '时间',
    dataIndex: 'createdAt',
    width: 170,
    render: (value: string) => (value ? new Date(value).toLocaleString() : '—'),
  },
  {
    title: '事件',
    dataIndex: 'action',
    width: 160,
    render: (value: string) => <Tag color='volcano'>{actionLabel(value)}</Tag>,
  },
  {
    title: '来源',
    dataIndex: 'source',
    width: 110,
    render: (value: string) => sourceLabel(value),
  },
  {
    title: '目标客户',
    dataIndex: 'targetName',
    width: 160,
    render: (_: unknown, record) => record.targetName || `#${record.targetTenantId}`,
  },
  { title: '账号', dataIndex: 'actorAccount', width: 140, render: (v: string) => v || '—' },
  {
    title: '状态码',
    dataIndex: 'statusCode',
    width: 90,
    render: (value: number) => <Tag color={STATUS_COLOR[value] || 'default'}>{value}</Tag>,
  },
  { title: '原因码', dataIndex: 'reasonCode', render: (v: string) => v || '—' },
];

const targetColumns: TableColumnsType<MspAuditAggRow> = [
  {
    title: '客户租户',
    dataIndex: 'label',
    render: (_: unknown, row) => (row.label ? `${row.label}（#${row.key}）` : `#${row.key}`),
  },
  { title: '事件数', dataIndex: 'count', width: 100 },
];

const MSPAuditBoardPage: React.FC = () => {
  const [days, setDays] = useState(30);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [summary, setSummary] = useState<MspAuditSummary | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setSummary(await getMspAuditSummary(days));
    } catch (err) {
      setError(err instanceof Error ? err.message : '审计聚合加载失败');
    } finally {
      setLoading(false);
    }
  }, [days]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className='p-6 space-y-4'>
      <div className='flex items-start justify-between gap-4'>
        <div>
          <Title level={3} className='!mb-1'>
            <ShieldAlert size={22} className='inline-block mr-2 -mt-1 text-orange-500' />
            审计看板
          </Title>
          <Text type='secondary'>
            跨租户审计聚合与越权尝试/冲突告警（provider 治理视角；窗口内数据）
          </Text>
        </div>
        <Space>
          <Select
            aria-label='统计窗口'
            value={days}
            options={AUDIT_WINDOW_OPTIONS}
            onChange={setDays}
            style={{ width: 130 }}
          />
          <Button icon={<RefreshCw size={16} />} onClick={() => void load()} loading={loading}>
            刷新
          </Button>
        </Space>
      </div>

      {error && <Alert type='error' showIcon message={error} />}

      <div className='grid grid-cols-1 md:grid-cols-3 gap-4'>
        <Card data-testid='audit-card-denied' loading={loading && !summary}>
          <Statistic title='拒绝事件（越权/冲突）' value={summary?.deniedEvents ?? 0} valueStyle={{ color: '#cf1322' }} />
        </Card>
        <Card data-testid='audit-card-cross' loading={loading && !summary}>
          <Statistic title='跨租户审计事件' value={summary?.crossTenantEvents ?? 0} />
        </Card>
        <Card data-testid='audit-card-total' loading={loading && !summary}>
          <Statistic title='窗口内审计总数' value={summary?.totalEvents ?? 0} />
        </Card>
      </div>

      <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
        <Card title='按来源分布' loading={loading && !summary} size='small'>
          <AggTags rows={summary?.bySource ?? []} emptyText='窗口内暂无审计事件' />
        </Card>
        <Card title='按事件分布' loading={loading && !summary} size='small'>
          <AggTags
            rows={(summary?.byAction ?? []).map(row => ({ ...row, label: actionLabel(row.key) }))}
            emptyText='窗口内暂无审计事件'
          />
        </Card>
      </div>

      <Card title='客户分布（目标租户）' loading={loading && !summary} size='small'>
        <Table<MspAuditAggRow>
          rowKey='key'
          size='small'
          pagination={false}
          columns={targetColumns}
          dataSource={summary?.byTargetTenant ?? []}
          locale={{ emptyText: <Empty description='窗口内暂无跨租户事件' /> }}
        />
      </Card>

      <Card
        title='越权尝试 / 冲突告警'
        loading={loading && !summary}
        size='small'
        extra={
          // 后端在无告警时下发 recentDenials: null（E2E 实测崩溃点），此处按空数组兜底。
          <Text type='secondary'>最近 {summary?.recentDenials?.length ?? 0} 条</Text>
        }
      >
        <Table<MspAuditDenialRow>
          rowKey='id'
          size='small'
          columns={denialColumns}
          dataSource={summary?.recentDenials ?? []}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description='窗口内暂无未分配客户访问或冲突记录' /> }}
        />
      </Card>
    </div>
  );
};

export default MSPAuditBoardPage;
