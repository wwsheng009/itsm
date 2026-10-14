/**
 * MSP 服务台仪表盘页测试（/msp/dashboard）。
 *
 * 覆盖：
 * - KPI 汇总（客户数/未关闭/SLA 超期/临近/未指派 = 客户维度求和）；
 * - 全部客户概览表：每客户一行计数徽标，点击行 = 本页 URL 收窄 customerTenantIds；
 * - 客户范围过滤：Segmented 切换 全部/单客户（URL query 同步）；
 * - 最近更新的跨客户工单（fetchTickets 注入）；
 * - 空态（无已分配客户）与加载失败（错误 + 重试按钮）。
 */
import React from 'react';
import { fireEvent } from '@testing-library/react';
import { render, screen, waitFor } from '@/lib/test-utils';

const mockSetSearchParams = jest.fn();
let mockSearchParams = '';

jest.mock('react-router', () => {
  const actual = jest.requireActual('react-router');
  return {
    ...actual,
    useNavigate: () => jest.fn(),
    useLocation: () => ({
      pathname: '/msp/dashboard',
      search: '',
      hash: '',
      state: null,
      key: 'test',
    }),
    useSearchParams: () => [
      new URLSearchParams(mockSearchParams),
      mockSetSearchParams,
    ],
  };
});

jest.mock('@/lib/api/msp-workbench-api', () => {
  const actual = jest.requireActual('@/lib/api/msp-workbench-api');
  return {
    ...actual,
    getWorkbenchSummary: jest.fn(),
    listWorkbenchTickets: jest.fn(),
  };
});

// 门禁消费：MspProviderGate = 服务商租户上下文 + msp_ticket:read（双条件 fail-closed）。
jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';
import {
  getWorkbenchSummary,
  listWorkbenchTickets,
  type WorkbenchSummary,
  type WorkbenchTicketItem,
} from '@/lib/api/msp-workbench-api';
import MspDashboardPage, {
  summarizeCustomers,
  topRiskCustomers,
} from '../index';

/** 与门禁消费方式对齐：selector 接收含 user.permissions 与 currentTenant.type 的状态。 */
function setAuth(permissions: string[], tenantType: string | null = 'msp_provider') {
  (useAuthStore as unknown as jest.Mock).mockImplementation(
    (
      selector: (state: {
        user: { permissions: string[] };
        currentTenant: { type?: string } | null;
      }) => unknown
    ) => selector({ user: { permissions }, currentTenant: tenantType ? { type: tenantType } : null })
  );
}

const summaryFixture: WorkbenchSummary = {
  generatedAt: '2026-10-14T08:00:00Z',
  ttlSeconds: 30,
  slaDueSoonWindowHours: 24,
  customers: [
    {
      customerTenantId: 1,
      customerName: 'Acme Corp',
      open: 5,
      slaRisk: 2,
      slaDueSoon: 1,
      unassigned: 1,
      members: 10,
      ticketsCreated30d: 12,
    },
    {
      customerTenantId: 2,
      customerName: 'Beta LLC',
      open: 3,
      slaRisk: 0,
      slaDueSoon: 2,
      unassigned: 0,
      members: 4,
      ticketsCreated30d: 5,
    },
  ],
};

const recentTicketsFixture: WorkbenchTicketItem[] = [
  {
    id: 11,
    customerTenantId: 1,
    customerName: 'Acme Corp',
    ticketNumber: 'ACME-1',
    title: 'VPN 无法连接',
    status: 'open',
    priority: 'high',
    assigneeName: '张三',
    updatedAt: '2026-10-14T07:30:00Z',
    allowedActions: [],
  },
  {
    id: 12,
    customerTenantId: 2,
    customerName: 'Beta LLC',
    ticketNumber: 'BETA-2',
    title: '打印机离线',
    status: 'in_progress',
    priority: 'medium',
    updatedAt: '2026-10-14T06:00:00Z',
    allowedActions: [],
  },
];

describe('MspDashboardPage', () => {
  beforeEach(() => {
    mockSearchParams = '';
    setAuth(['msp_ticket:read']);
    (getWorkbenchSummary as jest.Mock).mockResolvedValue(summaryFixture);
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: recentTicketsFixture,
      total: recentTicketsFixture.length,
    });
  });

  it('渲染 KPI 汇总（客户维度求和）', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-kpi-customers')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-open')).toHaveTextContent('8');
      expect(screen.getByTestId('msp-dash-kpi-risk')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-due-soon')).toHaveTextContent('3');
      expect(screen.getByTestId('msp-dash-kpi-unassigned')).toHaveTextContent('1');
    });
  });

  it('渲染全部客户概览表并展示每客户徽标', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-customer-table')).toBeInTheDocument();
    });
    // Acme Corp 同时出现在 Segmented 过滤器与客户表，用 getAllByText 断言出现。
    expect(screen.getAllByText('Acme Corp').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Beta LLC').length).toBeGreaterThanOrEqual(1);
  });

  it('点击客户行 → 本页 URL 收窄 customerTenantIds（不跳转工作台）', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-customer-row-1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('msp-dash-customer-row-1'));

    await waitFor(() => {
      expect(mockSetSearchParams).toHaveBeenCalledTimes(1);
      const params = mockSetSearchParams.mock.calls[0][0] as URLSearchParams;
      expect(params.get('customerTenantIds')).toBe('1');
    });
  });

  it('渲染最近更新的跨客户工单（客户 + 工单号 + 状态）', async () => {
    render(
      <MspDashboardPage
        fetchSummary={() => Promise.resolve(summaryFixture)}
        fetchTickets={() => Promise.resolve({ items: recentTicketsFixture, total: 2 })}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-recent')).toBeInTheDocument();
      expect(screen.getByText('ACME-1')).toBeInTheDocument();
      expect(screen.getByText('BETA-2')).toBeInTheDocument();
      expect(screen.getByText('VPN 无法连接')).toBeInTheDocument();
    });
  });

  it('summary 失败 → 错误提示 + 重试按钮', async () => {
    (getWorkbenchSummary as jest.Mock).mockRejectedValue(new Error('boom'));
    render(<MspDashboardPage fetchTickets={() => Promise.resolve({ items: [], total: 0 })} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-summary-error')).toHaveTextContent('boom');
      expect(screen.getByTestId('msp-dash-summary-retry')).toBeInTheDocument();
    });
  });

  it('无已分配客户 → 空态', async () => {
    (getWorkbenchSummary as jest.Mock).mockResolvedValue({
      generatedAt: '2026-10-14T08:00:00Z',
      ttlSeconds: 30,
      customers: [],
    });
    render(<MspDashboardPage fetchTickets={() => Promise.resolve({ items: [], total: 0 })} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-empty')).toBeInTheDocument();
    });
  });
});

describe('summarizeCustomers / topRiskCustomers', () => {
  it('汇总口径 = 客户维度计数之和', () => {
    const totals = summarizeCustomers(summaryFixture.customers);
    expect(totals).toEqual({
      customers: 2,
      open: 8,
      slaRisk: 2,
      slaDueSoon: 3,
      unassigned: 1,
    });
  });

  it('风险排序：超期 desc → 临近 desc → open desc', () => {
    const sorted = topRiskCustomers(summaryFixture.customers, 5);
    expect(sorted[0].customerName).toBe('Acme Corp');
    expect(sorted).toHaveLength(2);
  });
});
