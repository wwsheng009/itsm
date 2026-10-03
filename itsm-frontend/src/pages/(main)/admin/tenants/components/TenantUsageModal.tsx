/**
 * TenantUsageModal（IP-P2-6 收尾）：平台治理页「配额 vs 用量」。
 *
 * - 数据源：`GET /api/v1/tenants/:id/usage`（上限与用量同源，口径 = 写入校验）；
 * - 单键 limit <= 0 / 缺省 = 不限：只展示用量文本，不画进度条；
 * - 加载中 / 失败（错误提示）/ 正常三态；关闭后重开重新拉取，避免陈旧数据；
 * - `fetchUsage` 可注入（测试/复用），默认走 TenantAPI。
 */
import { useEffect, useMemo, useState } from 'react';
import { Alert, Modal, Progress, Space, Typography } from 'antd';
import { TenantAPI } from '@/lib/api/tenant-api';
import type { TenantQuotaUsageResponse } from '@/lib/api/api-config';

const { Text } = Typography;

/** 字节 → 可读存储字符串（治理页展示口径）。 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx += 1;
  }
  return `${idx === 0 || value >= 10 ? Math.round(value) : value.toFixed(1)} ${units[idx]}`;
}

export type TenantUsageModalProps = {
  open: boolean;
  tenantId?: number;
  tenantName?: string;
  onClose: () => void;
  /** 可注入的用量拉取（测试替身；缺省 TenantAPI.getTenantUsage）。 */
  fetchUsage?: (id: number) => Promise<TenantQuotaUsageResponse>;
};

type UsageRow = {
  key: string;
  label: string;
  used: number;
  limit: number;
  fmt: (value: number) => string;
};

export default function TenantUsageModal({
  open,
  tenantId,
  tenantName,
  onClose,
  fetchUsage,
}: TenantUsageModalProps) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<TenantQuotaUsageResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !tenantId) return;
    let alive = true;
    setLoading(true);
    setError(null);
    setData(null);
    const fetcher = fetchUsage ?? TenantAPI.getTenantUsage;
    fetcher(tenantId)
      .then((res) => {
        if (alive) setData(res);
      })
      .catch((err: unknown) => {
        if (alive) setError(err instanceof Error && err.message ? err.message : '用量加载失败');
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [open, tenantId, fetchUsage]);

  const rows = useMemo<UsageRow[]>(() => {
    const limits = data?.limits ?? {};
    const used = data?.used;
    return [
      {
        key: 'users',
        label: '用户数',
        used: used?.users ?? 0,
        limit: limits.maxUsers ?? 0,
        fmt: (value) => `${value}`,
      },
      {
        key: 'tickets',
        label: '本月工单',
        used: used?.ticketsThisMonth ?? 0,
        limit: limits.maxTicketsPerMonth ?? 0,
        fmt: (value) => `${value}`,
      },
      {
        key: 'storage',
        label: '存储占用',
        used: used?.storageBytes ?? 0,
        limit: (limits.maxStorageMB ?? 0) * 1024 * 1024,
        fmt: formatBytes,
      },
    ];
  }, [data]);

  const active = open && !!tenantId;
  const busy = active && (loading || (!data && !error));
  const showBody = active && !loading && !error && !!data;

  return (
    <Modal
      open={open}
      title={`租户用量${tenantName ? ` · ${tenantName}` : ''}`}
      footer={null}
      onCancel={onClose}
      width={480}
    >
      {busy ? <Text type="secondary">加载中…</Text> : null}
      {active && error ? (
        <Alert type="error" showIcon message={error} data-testid="tenant-usage-error" />
      ) : null}
      {showBody ? (
        <Space
          direction="vertical"
          size={16}
          style={{ width: '100%' }}
          data-testid="tenant-usage-body"
        >
          {rows.map((row) => (
            <div key={row.key} data-testid={`tenant-usage-${row.key}`}>
              <div className="flex items-center justify-between">
                <Text>{row.label}</Text>
                <Text type="secondary">
                  {row.limit > 0
                    ? `${row.fmt(row.used)} / ${row.fmt(row.limit)}`
                    : `${row.fmt(row.used)} · 不限`}
                </Text>
              </div>
              {row.limit > 0 ? (
                <Progress
                  percent={Math.min(100, Math.round((row.used / row.limit) * 100))}
                  status={row.used >= row.limit ? 'exception' : 'active'}
                  showInfo={false}
                />
              ) : null}
            </div>
          ))}
        </Space>
      ) : null}
    </Modal>
  );
}
