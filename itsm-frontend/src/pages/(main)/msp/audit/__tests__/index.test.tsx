/**
 * MSP 审计看板页测试（IP-P1-8）。
 *
 * 覆盖：聚合渲染（拒绝/跨租户/总数、来源与动作分布、客户分布、拒绝明细）、
 * 空态、窗口切换重拉、加载失败提示。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@/lib/test-utils';

jest.mock('@/lib/api/msp-audit-api', () => ({
  ...jest.requireActual('@/lib/api/msp-audit-api'),
  getMspAuditSummary: jest.fn(),
}));

import { getMspAuditSummary, type MspAuditSummary } from '@/lib/api/msp-audit-api';
import MSPAuditBoardPage from '../index';

const mockedGet = getMspAuditSummary as jest.MockedFunction<typeof getMspAuditSummary>;

function summaryFixture(overrides: Partial<MspAuditSummary> = {}): MspAuditSummary {
  return {
    windowDays: 30,
    generatedAt: '2026-09-30T08:00:00Z',
    totalEvents: 5,
    crossTenantEvents: 4,
    deniedEvents: 3,
    bySource: [
      { key: 'header', count: 2 },
      { key: 'workbench', count: 2 },
      { key: 'login', count: 1 },
    ],
    byAction: [
      { key: 'tenant.scope_denied', count: 2 },
      { key: 'tenant.probe_denied', count: 1 },
      { key: 'workbench.action', count: 1 },
    ],
    byTargetTenant: [
      { key: '2', label: 'Alpha Corp', count: 2 },
      { key: '3', label: 'Beta LLC', count: 2 },
    ],
    byMembership: [{ key: '11', count: 2 }],
    recentDenials: [
      {
        id: 1,
        createdAt: '2026-09-30T07:00:00Z',
        action: 'tenant.probe_denied',
        source: 'header',
        targetTenantId: 3,
        targetName: 'Beta LLC',
        actorAccount: 'msp-user',
        statusCode: 401,
        reasonCode: 'TENANT_MISMATCH_REJECTED',
        path: '/api/v1/tickets',
      },
      {
        id: 2,
        createdAt: '2026-09-30T06:00:00Z',
        action: 'tenant.scope_denied',
        source: 'workbench',
        targetTenantId: 2,
        targetName: 'Alpha Corp',
        actorAccount: 'msp-user',
        statusCode: 403,
        reasonCode: 'MSP_ALLOCATION_REQUIRED',
        path: '/api/v1/msp/workbench/tickets',
      },
    ],
    ...overrides,
  };
}

describe('MSPAuditBoardPage', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedGet.mockResolvedValue(summaryFixture());
  });

  it('渲染聚合：统计卡、来源/动作分布、客户分布与拒绝明细', async () => {
    render(<MSPAuditBoardPage />);

    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith(30));

    expect(screen.getByTestId('audit-card-denied').textContent).toContain('3');
    expect(screen.getByTestId('audit-card-cross').textContent).toContain('4');
    expect(screen.getByTestId('audit-card-total').textContent).toContain('5');

    // 来源/动作分布（动作 → 中文标签）。
    expect(screen.getByText(/请求头通道 · 2/)).toBeInTheDocument();
    expect(screen.getByText(/未分配客户访问 · 2/)).toBeInTheDocument();
    expect(screen.getByText(/租户冲突\/探测 · 1/)).toBeInTheDocument();

    // 客户分布（名称 + id）。
    expect(screen.getByText('Alpha Corp（#2）')).toBeInTheDocument();

    // 拒绝明细：事件标签、账号、原因码。
    expect(screen.getByText('租户冲突/探测')).toBeInTheDocument();
    expect(screen.getAllByText('msp-user').length).toBeGreaterThan(0);
    expect(screen.getByText('TENANT_MISMATCH_REJECTED')).toBeInTheDocument();
    expect(screen.getByText('MSP_ALLOCATION_REQUIRED')).toBeInTheDocument();
    expect(screen.getByText('Beta LLC')).toBeInTheDocument();
  });

  it('空窗口：展示空态文案', async () => {
    mockedGet.mockResolvedValue(
      summaryFixture({
        totalEvents: 0,
        crossTenantEvents: 0,
        deniedEvents: 0,
        bySource: [],
        byAction: [],
        byTargetTenant: [],
        byMembership: [],
        recentDenials: [],
      })
    );

    render(<MSPAuditBoardPage />);

    expect(await screen.findByText('窗口内暂无跨租户事件')).toBeInTheDocument();
    expect(screen.getByText('窗口内暂无未分配客户访问或冲突记录')).toBeInTheDocument();
    expect(screen.getAllByText('窗口内暂无审计事件').length).toBe(2);
  });

  it('切换统计窗口（7 天）触发重拉', async () => {
    render(<MSPAuditBoardPage />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith(30));

    fireEvent.mouseDown(screen.getByLabelText('统计窗口'));
    fireEvent.click(await screen.findByText('近 7 天'));

    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith(7));
  });

  it('加载失败：展示错误提示', async () => {
    mockedGet.mockRejectedValue(new Error('审计聚合失败'));

    render(<MSPAuditBoardPage />);

    expect(await screen.findByText('审计聚合失败')).toBeInTheDocument();
  });
});
