/**
 * CustomerUsageBoard 每客户用量看板（IP-P2-4c；P2 收尾项）。
 *
 * 数据源定案（2026-09-30 实测，见实施方案 §5.0-D 与工作台方案 P2 行）：
 * - 系统**无租户硬配额模型**：`tenants` 无 `quota/settings` 列，`dto.TenantDTO.Quota`
 *   为未赋值遗留字段，附件配额（6106）仅有错误码无校验——故先交付 **usage-only** 口径，
 *   硬配额（limits）登记为后续「平台租户管理」批次遗留；
 * - 用量指标全部真实可算（GET /api/v1/msp/workbench/summary，30s 服务端缓存）：
 *   `members`（active membership 单源）/ `open`（未关闭工单）/ `ticketsCreated30d`（窗口新增）。
 *
 * 展示：按窗口新增 desc → 成员 desc → 名称排序；条长相对全体客户最大值；
 * 交互：点击行 → onSelectCustomer（父页面写 customerTenantIds 过滤）；刷新/空态/错误态。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Card, Empty, Space, Tag, Tooltip, Typography } from 'antd';
import { RefreshCw } from 'lucide-react';
import { getWorkbenchSummary, type WorkbenchSummaryCustomer } from '@/lib/api/msp-workbench-api';

const DEFAULT_USAGE_WINDOW_DAYS = 30;

interface CustomerUsageBoardData {
  usageWindowDays?: number;
  customers: WorkbenchSummaryCustomer[];
}

export interface CustomerUsageBoardProps {
  onSelectCustomer?: (customerTenantId: number) => void;
  /** 测试注入用；缺省请求服务端 summary。 */
  fetchSummary?: () => Promise<CustomerUsageBoardData>;
}

const USAGE_HINT =
  '系统当前无硬配额（limits）数据源，此处为真实用量口径：成员=active 成员身份数（membership 单源）；未关闭=未关闭工单数；窗口新增=窗口内新建工单数。';

export default function CustomerUsageBoard({ onSelectCustomer, fetchSummary }: CustomerUsageBoardProps) {
  const [customers, setCustomers] = useState<WorkbenchSummaryCustomer[]>([]);
  const [windowDays, setWindowDays] = useState(DEFAULT_USAGE_WINDOW_DAYS);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await (fetchSummary ? fetchSummary() : getWorkbenchSummary());
      setCustomers(response?.customers ?? []);
      setWindowDays(response?.usageWindowDays ?? DEFAULT_USAGE_WINDOW_DAYS);
    } catch (err) {
      setCustomers([]);
      setError(err instanceof Error && err.message ? err.message : '加载用量数据失败');
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
          y.ticketsCreated30d - x.ticketsCreated30d ||
          y.members - x.members ||
          x.customerName.localeCompare(y.customerName)
      ),
    [customers]
  );

  const maxCreated = useMemo(
    () => Math.max(1, ...sorted.map(customer => customer.ticketsCreated30d)),
    [sorted]
  );

  return (
    <Card
      size="small"
      style={{ marginBottom: 16 }}
      data-testid="customer-usage-board"
      title={
        <Space size={8}>
          <Tooltip title={USAGE_HINT}>
            <span style={{ borderBottom: '1px dashed #bfbfbf' }}>客户用量</span>
          </Tooltip>
          <Tag color="blue" data-testid="usage-window">
            窗口 {windowDays}d
          </Tag>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            用量口径（无硬配额数据源）
          </Typography.Text>
        </Space>
      }
      extra={
        <Button
          size="small"
          icon={<RefreshCw size={14} />}
          loading={loading}
          onClick={() => void load()}
          data-testid="usage-refresh"
        >
          刷新
        </Button>
      }
    >
      {error ? (
        <Typography.Text type="danger" data-testid="usage-error">
          {error}
        </Typography.Text>
      ) : !loading && sorted.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无可用客户" />
      ) : (
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          {sorted.map(customer => (
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
              data-testid={`usage-row-${customer.customerTenantId}`}
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
              <span
                data-testid={`usage-members-${customer.customerTenantId}`}
                style={{ width: 88, fontSize: 12 }}
              >
                成员 {customer.members}
              </span>
              <span
                data-testid={`usage-open-${customer.customerTenantId}`}
                style={{ width: 96, fontSize: 12 }}
              >
                未关闭 {customer.open}
              </span>
              <span
                data-testid={`usage-created-${customer.customerTenantId}`}
                style={{ width: 104, fontSize: 12 }}
              >
                新增 {customer.ticketsCreated30d}
              </span>
              <div
                data-testid={`usage-bar-${customer.customerTenantId}`}
                style={{
                  flex: 1,
                  minWidth: 100,
                  height: 10,
                  background: '#f5f5f5',
                  borderRadius: 5,
                  overflow: 'hidden',
                }}
              >
                <div
                  style={{
                    width: `${(customer.ticketsCreated30d / maxCreated) * 100}%`,
                    background: '#1677ff',
                    height: '100%',
                  }}
                />
              </div>
            </div>
          ))}
        </Space>
      )}
    </Card>
  );
}
