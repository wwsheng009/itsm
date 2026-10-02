/**
 * SlaRiskBoard SLA 风险看板（IP-P2-4b）。
 *
 * 数据（GET /api/v1/msp/workbench/summary；服务端 30s 缓存）：
 * - `slaRisk`：已超期未关闭；
 * - `slaDueSoon`：窗口（服务端下发，默认 24h）内临近到期且未超期。
 *
 * 展示：按风险排序（超期 desc → 临近 desc → open desc → 名称）；
 * 每客户一行：计数徽标 + 该客户 open 工单的风险占比条（红=超期 / 橙=临近 / 灰=其余）。
 * 交互：点击行 → onSelectCustomer（父页面写 customerTenantIds 过滤）；刷新按钮。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Card, Empty, Space, Tag, Typography } from 'antd';
import { RefreshCw } from 'lucide-react';
import { getWorkbenchSummary, type WorkbenchSummaryCustomer } from '@/lib/api/msp-workbench-api';

const DEFAULT_WINDOW_HOURS = 24;

interface SlaRiskBoardData {
  slaDueSoonWindowHours?: number;
  customers: WorkbenchSummaryCustomer[];
}

export interface SlaRiskBoardProps {
  onSelectCustomer?: (customerTenantId: number) => void;
  /** 测试注入用；缺省请求服务端 summary。 */
  fetchSummary?: () => Promise<SlaRiskBoardData>;
}

export default function SlaRiskBoard({ onSelectCustomer, fetchSummary }: SlaRiskBoardProps) {
  const [customers, setCustomers] = useState<WorkbenchSummaryCustomer[]>([]);
  const [windowHours, setWindowHours] = useState(DEFAULT_WINDOW_HOURS);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await (fetchSummary ? fetchSummary() : getWorkbenchSummary());
      setCustomers(response?.customers ?? []);
      setWindowHours(response?.slaDueSoonWindowHours ?? DEFAULT_WINDOW_HOURS);
    } catch (err) {
      setCustomers([]);
      setError(err instanceof Error && err.message ? err.message : '加载 SLA 数据失败');
    } finally {
      setLoading(false);
    }
  }, [fetchSummary]);

  useEffect(() => {
    void load();
  }, [load]);

  const sorted = useMemo(
    () =>
      [...customers].sort(
        (x, y) =>
          y.slaRisk - x.slaRisk ||
          y.slaDueSoon - x.slaDueSoon ||
          y.open - x.open ||
          x.customerName.localeCompare(y.customerName)
      ),
    [customers]
  );

  const totals = useMemo(
    () =>
      sorted.reduce(
        (acc, customer) => ({
          risk: acc.risk + customer.slaRisk,
          dueSoon: acc.dueSoon + customer.slaDueSoon,
        }),
        { risk: 0, dueSoon: 0 }
      ),
    [sorted]
  );

  return (
    <Card
      size="small"
      style={{ marginBottom: 16 }}
      data-testid="sla-risk-board"
      title={
        <Space size={8}>
          <span>SLA 风险</span>
          <Tag color={totals.risk > 0 ? 'red' : 'default'} data-testid="sla-total-risk">
            超期 {totals.risk}
          </Tag>
          <Tag color={totals.dueSoon > 0 ? 'orange' : 'default'} data-testid="sla-total-due">
            临近 {totals.dueSoon}
          </Tag>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            窗口 {windowHours}h
          </Typography.Text>
        </Space>
      }
      extra={
        <Button
          size="small"
          icon={<RefreshCw size={14} />}
          loading={loading}
          onClick={() => void load()}
          data-testid="sla-risk-refresh"
        >
          刷新
        </Button>
      }
    >
      {error ? (
        <Typography.Text type="danger" data-testid="sla-risk-error">
          {error}
        </Typography.Text>
      ) : !loading && sorted.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无可用客户" />
      ) : (
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          {sorted.map(customer => {
            const open = Math.max(customer.open, customer.slaRisk + customer.slaDueSoon, 1);
            const riskPct = (customer.slaRisk / open) * 100;
            const duePct = (customer.slaDueSoon / open) * 100;
            const restPct = Math.max(0, 100 - riskPct - duePct);
            return (
              <div
                key={customer.customerTenantId}
                role="button"
                tabIndex={0}
                onClick={() => onSelectCustomer?.(customer.customerTenantId)}
                onKeyDown={event => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    onSelectCustomer?.(customer.customerTenantId);
                  }
                }}
                data-testid={`sla-risk-row-${customer.customerTenantId}`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  cursor: onSelectCustomer ? 'pointer' : 'default',
                }}
              >
                <span
                  title={customer.customerName}
                  style={{
                    width: 140,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {customer.customerName}
                </span>
                {customer.slaRisk > 0 && (
                  <Tag
                    color="red"
                    title="已超期未关闭"
                    data-testid={`sla-breach-${customer.customerTenantId}`}
                  >
                    超期 {customer.slaRisk}
                  </Tag>
                )}
                {customer.slaDueSoon > 0 && (
                  <Tag
                    color="orange"
                    title={`${windowHours}h 内临近到期`}
                    data-testid={`sla-due-soon-${customer.customerTenantId}`}
                  >
                    临近 {customer.slaDueSoon}
                  </Tag>
                )}
                <div
                  data-testid={`sla-bar-${customer.customerTenantId}`}
                  style={{
                    flex: 1,
                    minWidth: 120,
                    height: 10,
                    background: '#f5f5f5',
                    borderRadius: 5,
                    overflow: 'hidden',
                    display: 'flex',
                  }}
                >
                  <div style={{ width: `${riskPct}%`, background: '#cf1322', height: '100%' }} />
                  <div style={{ width: `${duePct}%`, background: '#fa8c16', height: '100%' }} />
                  <div style={{ width: `${restPct}%`, background: '#d9d9d9', height: '100%' }} />
                </div>
              </div>
            );
          })}
        </Space>
      )}
    </Card>
  );
}
