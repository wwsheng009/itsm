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
    getWorkbenchRecordsSummary: jest.fn(),
  };
});

// 门禁消费：MspProviderGate = 服务商租户上下文 + msp_ticket:read（双条件 fail-closed）。
jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';
import {
  getWorkbenchSummary,
  getWorkbenchRecordsSummary,
  listWorkbenchTickets,
  type MspRecordsSummary,
  type WorkbenchSummary,
  type WorkbenchTicketItem,
} from '@/lib/api/msp-workbench-api';
import MspDashboardPage, {
  recordStatusLabel,
  summarizeCustomers,
  topRiskCustomers,
} from '../index';

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

const recordsSummaryFixture: MspRecordsSummary = {
  generatedAt: '2026-10-17T12:00:00Z',
  incident: { total: 7, open: 3, recent: [] },
  problem: {
    total: 4,
    open: 2,
    recent: [
      {
        recordType: 'problem',
        id: 21,
        customerTenantId: 1,
        customerName: 'Acme Corp',
        number: 'PRB-1',
        title: '数据库连接池耗尽',
        status: 'investigating',
        priority: 'high',
        updatedAt: '2026-10-17T11:00:00Z',
      },
    ],
  },
  knownError: { total: 2, open: 2, recent: [] },
  change: {
    total: 6,
    open: 1,
    recent: [
      {
        recordType: 'change',
        id: 31,
        customerTenantId: 2,
        customerName: 'Beta LLC',
        number: 'CHG-1',
        title: '核心交换机固件升级',
        status: 'scheduled',
        priority: 'medium',
        updatedAt: '2026-10-17T10:00:00Z',
      },
    ],
  },
  serviceRequest: { total: 5, open: 4, recent: [] },
};

const emptyRecordsSummaryFixture: MspRecordsSummary = {
  generatedAt: '2026-10-17T12:00:00Z',
  incident: { total: 0, open: 0, recent: [] },
  problem: { total: 0, open: 0, recent: [] },
  knownError: { total: 0, open: 0, recent: [] },
  change: { total: 0, open: 0, recent: [] },
  serviceRequest: { total: 0, open: 0, recent: [] },
};

describe('MspDashboardPage', () => {
  beforeEach(() => {
    mockSearchParams = '';
    setAuth(['msp_ticket:read']);
    (getWorkbenchSummary as jest.Mock).mockResolvedValue(summaryFixture);
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: recentTicketsFixture,
      total: recentTicketsFixture.length,
    });
    (getWorkbenchRecordsSummary as jest.Mock).mockResolvedValue(recordsSummaryFixture);
  });

  it('渲染 KPI 汇总（客户维度求和）', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-kpi-customers')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-open')).toHaveTextContent('8');
      expect(screen.getByTestId('msp-dash-kpi-risk')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-due-soon')).toHaveTextContent('3');
      expect(screen.getByTestId('msp-dash-kpi-unassigned')).toHaveTextContent('1');
    });
  });

  it('渲染全部客户概览表并展示每客户徽标', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-customer-table')).toBeInTheDocument();
    });
    // Acme Corp 同时出现在 Segmented 过滤器与客户表，用 getAllByText 断言出现。
    expect(screen.getAllByText('Acme Corp').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Beta LLC').length).toBeGreaterThanOrEqual(1);
  });

  it('点击客户行 → 本页 URL 收窄 customerTenantIds（不跳转工作台）', async () => {
    render(<MspDashboardPage fetchSummary={() => Promise.resolve(summaryFixture)} fetchTickets={() => Promise.resolve({ items: [], total: 0 })} fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)} />);

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
        fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)}
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
    render(<MspDashboardPage fetchTickets={() => Promise.resolve({ items: [], total: 0 })} fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)} />);

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
    render(<MspDashboardPage fetchTickets={() => Promise.resolve({ items: [], total: 0 })} fetchRecordsSummary={() => Promise.resolve(emptyRecordsSummaryFixture)} />);

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-empty')).toBeInTheDocument();
    });
  });

  it('渲染协助单据 KPI（事件/问题/已知错误/变更/服务请求 的未结数）', async () => {
    render(
      <MspDashboardPage
        fetchSummary={() => Promise.resolve(summaryFixture)}
        fetchTickets={() => Promise.resolve({ items: [], total: 0 })}
        fetchRecordsSummary={() => Promise.resolve(recordsSummaryFixture)}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-kpi-incident')).toHaveTextContent('3');
      expect(screen.getByTestId('msp-dash-kpi-problem')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-known-error')).toHaveTextContent('2');
      expect(screen.getByTestId('msp-dash-kpi-change')).toHaveTextContent('1');
      expect(screen.getByTestId('msp-dash-kpi-service-request')).toHaveTextContent('4');
    });
    // 卡片同时展示总量/已结说明。
    expect(screen.getByTestId('msp-dash-kpi-incident')).toHaveTextContent('总 7 · 已结 4');
  });

  it('渲染跨租户协助单据总览（最近条目按更新时间倒序）', async () => {
    render(
      <MspDashboardPage
        fetchSummary={() => Promise.resolve(summaryFixture)}
        fetchTickets={() => Promise.resolve({ items: [], total: 0 })}
        fetchRecordsSummary={() => Promise.resolve(recordsSummaryFixture)}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-records-overview')).toBeInTheDocument();
      expect(screen.getByText('PRB-1')).toBeInTheDocument();
      expect(screen.getByText('CHG-1')).toBeInTheDocument();
      expect(screen.getByText('数据库连接池耗尽')).toBeInTheDocument();
    });
    // 总览卡内每类计数徽标。
    expect(screen.getByTestId('msp-dash-record-stat-incident')).toHaveTextContent('3');
    expect(screen.getByTestId('msp-dash-record-stat-service_request')).toHaveTextContent('4');
  });

  it('协助单据总览失败 → 错误提示 + 重试按钮', async () => {
    render(
      <MspDashboardPage
        fetchSummary={() => Promise.resolve(summaryFixture)}
        fetchTickets={() => Promise.resolve({ items: [], total: 0 })}
        fetchRecordsSummary={() => Promise.reject(new Error('records boom'))}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('msp-dash-records-error')).toHaveTextContent('records boom');
      expect(screen.getByTestId('msp-dash-records-retry')).toBeInTheDocument();
    });
  });

  it('协助单据总览按当前客户范围请求（customerTenantIds 透传）', async () => {
    mockSearchParams = 'customerTenantIds=2';
    const fetchRecordsSummary = jest.fn().mockResolvedValue(recordsSummaryFixture);
    render(
      <MspDashboardPage
        fetchSummary={() => Promise.resolve(summaryFixture)}
        fetchTickets={() => Promise.resolve({ items: [], total: 0 })}
        fetchRecordsSummary={fetchRecordsSummary}
      />
    );

    await waitFor(() => {
      expect(fetchRecordsSummary).toHaveBeenCalledWith({ customerTenantIds: [2] });
    });
  });
});

describe('recordStatusLabel', () => {
  it('跨域状态标签并集 + 未知状态回落原值', () => {
    expect(recordStatusLabel('investigating')).toBe('调查中');
    expect(recordStatusLabel('scheduled')).toBe('已排期');
    expect(recordStatusLabel('provisioning')).toBe('开通中');
    expect(recordStatusLabel('weird_status')).toBe('weird_status');
    expect(recordStatusLabel(undefined)).toBe('—');
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
