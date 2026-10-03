/**
 * TenantUsageModal 测试（IP-P2-6 收尾）。
 *
 * 覆盖：
 * - 有配额：三行「已用 / 上限」文本与进度条渲染；
 * - 不限：limit <= 0 仅展示用量文本、不渲染进度条；
 * - 失败：错误提示（可注入 fetch 替身）；
 * - 重开：关闭后再次打开重新拉取（不复用旧数据）。
 */
import React from 'react';
import { render, screen, waitFor } from '@/lib/test-utils';
import TenantUsageModal, { formatBytes } from '../TenantUsageModal';
import type { TenantQuotaUsageResponse } from '@/lib/api/api-config';

jest.setTimeout(120000);

const fullUsage: TenantQuotaUsageResponse = {
  tenantId: 1,
  limits: { maxUsers: 5, maxTicketsPerMonth: 100, maxStorageMB: 1 },
  used: { users: 3, ticketsThisMonth: 12, storageBytes: 512 * 1024 },
};

describe('TenantUsageModal', () => {
  it('渲染「已用 / 上限」与进度条', async () => {
    const fetchUsage = jest.fn().mockResolvedValue(fullUsage);
    render(
      <TenantUsageModal open tenantId={1} tenantName="Acme" onClose={jest.fn()} fetchUsage={fetchUsage} />
    );

    expect(await screen.findByText('租户用量 · Acme')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('tenant-usage-users')).toHaveTextContent('3 / 5'));
    expect(screen.getByTestId('tenant-usage-tickets')).toHaveTextContent('12 / 100');
    expect(screen.getByTestId('tenant-usage-storage')).toHaveTextContent('512 KB / 1.0 MB');
    // 三条进度条（antd Progress → role=progressbar）
    expect(screen.getAllByRole('progressbar')).toHaveLength(3);
    expect(fetchUsage).toHaveBeenCalledWith(1);
  });

  it('limit 缺省/0 = 不限：仅文本、无进度条', async () => {
    const fetchUsage = jest.fn().mockResolvedValue({
      tenantId: 2,
      limits: {},
      used: { users: 7, ticketsThisMonth: 3, storageBytes: 2048 },
    });
    render(<TenantUsageModal open tenantId={2} onClose={jest.fn()} fetchUsage={fetchUsage} />);

    await waitFor(() => expect(screen.getByTestId('tenant-usage-users')).toHaveTextContent('7 · 不限'));
    expect(screen.getByTestId('tenant-usage-tickets')).toHaveTextContent('3 · 不限');
    expect(screen.getByTestId('tenant-usage-storage')).toHaveTextContent('2.0 KB · 不限');
    expect(screen.queryAllByRole('progressbar')).toHaveLength(0);
  });

  it('拉取失败：渲染错误提示', async () => {
    const fetchUsage = jest.fn().mockRejectedValue(new Error('用量接口 500'));
    render(<TenantUsageModal open tenantId={3} onClose={jest.fn()} fetchUsage={fetchUsage} />);

    expect(await screen.findByTestId('tenant-usage-error')).toHaveTextContent('用量接口 500');
  });

  it('关闭后重开重新拉取（不复用旧数据）', async () => {
    const fetchUsage = jest.fn().mockResolvedValue(fullUsage);
    const { rerender } = render(
      <TenantUsageModal open tenantId={1} onClose={jest.fn()} fetchUsage={fetchUsage} />
    );
    await waitFor(() => expect(fetchUsage).toHaveBeenCalledTimes(1));

    rerender(<TenantUsageModal open={false} tenantId={1} onClose={jest.fn()} fetchUsage={fetchUsage} />);
    rerender(<TenantUsageModal open tenantId={1} onClose={jest.fn()} fetchUsage={fetchUsage} />);
    await waitFor(() => expect(fetchUsage).toHaveBeenCalledTimes(2));
  });

  it('formatBytes 边界', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(2048)).toBe('2.0 KB');
    expect(formatBytes(15 * 1024 * 1024)).toBe('15 MB');
  });
});
