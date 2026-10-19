/**
 * CrossTenantOverview 测试（跨租户缺口治理）。
 *
 * 覆盖：
 * - 只读汇总口径（客户数 / 未关闭 / 超期 / 临近 / 未指派）与 Top 风险排序；
 * - 下钻深链：客户行携带 customerTenantIds、board 入口（tasks/records/workbench）；
 * - 刷新、空态、错误态（错误只降级本分区）；
 * - 权限门控：非 MSP 权限不渲染且不发请求（fetchSummary 零调用）。
 */
import React from 'react';
import { MemoryRouter } from 'react-router';
import { render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';
import CrossTenantOverview from '../CrossTenantOverview';
import { MspProviderGate } from '@/components/auth/MspProviderGate';
import type { WorkbenchSummaryCustomer } from '@/lib/api/msp-workbench-api';

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';

jest.setTimeout(120000);

const acme: WorkbenchSummaryCustomer = {
  customerTenantId: 1,
  customerName: 'Acme Corp',
  open: 4,
  slaRisk: 2,
  slaDueSoon: 0,
  unassigned: 1,
  members: 5,
  ticketsCreated30d: 8,
};

const beta: WorkbenchSummaryCustomer = {
  customerTenantId: 2,
  customerName: 'Beta LLC',
  open: 3,
  slaRisk: 0,
  slaDueSoon: 2,
  unassigned: 0,
  members: 3,
  ticketsCreated30d: 4,
};

const gamma: WorkbenchSummaryCustomer = {
  customerTenantId: 3,
  customerName: 'Gamma Inc',
  open: 1,
  slaRisk: 0,
  slaDueSoon: 0,
  unassigned: 0,
  members: 1,
  ticketsCreated30d: 1,
};

  /** 与门禁消费方式对齐：canUseMspWorkbench 需 userId + 租户类型 + msp_role + 操作码（2026-10-18 起）。 */
  function setAuth(
    permissions: string[],
    tenantType: string | null = 'msp_provider',
    mspRole: string | null = 'provider_admin'
  ) {
    (useAuthStore as unknown as jest.Mock).mockImplementation(
      (
        selector: (state: {
          user: { id: number; permissions: string[]; mspRole?: string | null };
          currentTenant: { type?: string } | null;
          hasPermission: (permission: string) => boolean;
        }) => unknown
      ) =>
        selector({
          user: { id: 1, permissions, mspRole },
          currentTenant: tenantType ? { type: tenantType } : null,
          hasPermission: (permission: string) =>
            permissions.includes(permission) || permissions.includes('*'),
        })
    );
  }

function setup(overrides: { customers?: WorkbenchSummaryCustomer[] } = {}) {
  setAuth(['msp_ticket:read']);
  const fetchSummary = jest.fn().mockResolvedValue({
    generatedAt: '2026-10-13T08:00:00Z',
    ttlSeconds: 30,
    slaDueSoonWindowHours: 24,
    usageWindowDays: 30,
    customers: overrides.customers ?? [beta, gamma, acme],
  });
  const onNavigate = jest.fn();
  render(
    <MemoryRouter>
      <CrossTenantOverview fetchSummary={fetchSummary} onNavigate={onNavigate} />
    </MemoryRouter>
  );
  return { fetchSummary, onNavigate };
}

describe('CrossTenantOverview', () => {
  it('渲染只读汇总口径与 Top 风险排序', async () => {
    setup();

    await waitFor(() => expect(screen.getByTestId('cross-tenant-customer-1')).toBeInTheDocument());

    // 汇总：客户 3 / 未关闭 8 / 超期 2 / 临近 2 / 未指派 1
    expect(screen.getByTestId('cross-tenant-stat-customers')).toHaveTextContent('3');
    expect(screen.getByTestId('cross-tenant-stat-open')).toHaveTextContent('8');
    expect(screen.getByTestId('cross-tenant-stat-risk')).toHaveTextContent('2');
    expect(screen.getByTestId('cross-tenant-stat-due-soon')).toHaveTextContent('临近到期（24h）');
    expect(screen.getByTestId('cross-tenant-stat-due-soon')).toHaveTextContent('2');
    expect(screen.getByTestId('cross-tenant-stat-unassigned')).toHaveTextContent('1');

    // 风险排序：超期 2 → 临近 2 → 无风险
    const rows = screen.getAllByTestId(/^cross-tenant-customer-/);
    expect(rows.map(row => row.getAttribute('data-testid'))).toEqual([
      'cross-tenant-customer-1',
      'cross-tenant-customer-2',
      'cross-tenant-customer-3',
    ]);
  });

  it('点击客户行下钻：携带 customerTenantIds 进入工作台', async () => {
    const { onNavigate } = setup();
    await waitFor(() => expect(screen.getByTestId('cross-tenant-customer-2')).toBeInTheDocument());

    await userEvent.click(screen.getByTestId('cross-tenant-customer-2'));
    expect(onNavigate).toHaveBeenCalledWith('/msp/workbench?customerTenantIds=2');
  });

  it('board 深链：待办任务 / 跨租户单据 / 工作台', async () => {
    const { onNavigate } = setup();
    await waitFor(() => expect(screen.getByTestId('cross-tenant-goto-workbench')).toBeInTheDocument());

    await userEvent.click(screen.getByTestId('cross-tenant-goto-tasks'));
    expect(onNavigate).toHaveBeenCalledWith('/msp/workbench?board=tasks');

    await userEvent.click(screen.getByTestId('cross-tenant-goto-records'));
    expect(onNavigate).toHaveBeenCalledWith('/msp/workbench?board=records');

    await userEvent.click(screen.getByTestId('cross-tenant-goto-workbench'));
    expect(onNavigate).toHaveBeenCalledWith('/msp/workbench');
  });

  it('刷新按钮重新拉取 summary', async () => {
    const { fetchSummary } = setup();
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(1));

    await userEvent.click(screen.getByTestId('cross-tenant-refresh'));
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(2));
  });

  it('空数据渲染空态', async () => {
    setup({ customers: [] });
    expect(await screen.findByText('暂无已分配的客户')).toBeInTheDocument();
  });

  it('请求失败只降级本分区（错误态 + 重试入口）', async () => {
    setAuth(['msp_ticket:read']);
    const fetchSummary = jest.fn().mockRejectedValue(new Error('boom'));
    render(
      <MemoryRouter>
        <CrossTenantOverview fetchSummary={fetchSummary} onNavigate={jest.fn()} />
      </MemoryRouter>
    );

    const errorBox = await screen.findByTestId('cross-tenant-error');
    expect(errorBox).toHaveTextContent('boom');
    expect(screen.queryByTestId('cross-tenant-customer-1')).not.toBeInTheDocument();
  });
});

describe('CrossTenantOverview 门控（服务商上下文 + 操作码）', () => {
  function renderGated(permissions: string[], tenantType: string | null = 'msp_provider') {
    setAuth(permissions, tenantType);
    const fetchSummary = jest.fn().mockResolvedValue({
      generatedAt: '2026-10-13T08:00:00Z',
      ttlSeconds: 30,
      customers: [acme],
    });
    render(
      <MemoryRouter>
        <MspProviderGate>
          <CrossTenantOverview fetchSummary={fetchSummary} onNavigate={jest.fn()} />
        </MspProviderGate>
      </MemoryRouter>
    );
    return { fetchSummary };
  }

  it('客户租户 + 角色残留 msp_ticket:read（回归：过度授权）→ 不渲染且不发请求', () => {
    const { fetchSummary } = renderGated(['msp_ticket:read', 'ticket:read'], 'msp_customer');

    expect(screen.queryByTestId('cross-tenant-overview')).not.toBeInTheDocument();
    expect(fetchSummary).not.toHaveBeenCalled();
  });

  it('客户租户 + 仅区域码 msp:read：不渲染且不发请求', () => {
    const { fetchSummary } = renderGated(['msp:read', 'ticket:read'], 'msp_customer');

    expect(screen.queryByTestId('cross-tenant-overview')).not.toBeInTheDocument();
    expect(fetchSummary).not.toHaveBeenCalled();
  });

  it('服务商租户 + 无操作码（仅 msp:read）：不渲染且不发请求', () => {
    const { fetchSummary } = renderGated(['msp:read']);

    expect(screen.queryByTestId('cross-tenant-overview')).not.toBeInTheDocument();
    expect(fetchSummary).not.toHaveBeenCalled();
  });

  it('服务商租户 + msp_ticket:read：渲染分区并发起请求', async () => {
    const { fetchSummary } = renderGated(['msp_ticket:read']);

    expect(await screen.findByTestId('cross-tenant-overview')).toBeInTheDocument();
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(1));
  });
});
