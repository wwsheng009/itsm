/**
 * CustomerFilter 组件测试（IP-P0-8 / WB-A2、WB-A6）。
 *
 * 覆盖：
 * - 渲染：全部客户标签、计数徽标、下拉面板客户列表与每客户计数；
 * - 选择：勾选客户 → URL query customerTenantIds 同步（工作台页内）；
 * - URL 初值：customerTenantIds=2 时刷新/分享状态可恢复；
 * - 非工作台页选择 → 导航到 /msp/workbench 并携带 query。
 */
import React from 'react';
import { render, screen, waitFor, within } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';

let mockSearchParamsString = '';
let mockPathname = '/msp/workbench';
const mockSetSearchParams = jest.fn();
const mockNavigate = jest.fn();

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useNavigate: () => mockNavigate,
  useLocation: () => ({
    pathname: mockPathname,
    search: '',
    hash: '',
    state: null,
    key: 'test',
  }),
  useSearchParams: () => [new URLSearchParams(mockSearchParamsString), mockSetSearchParams],
}));

jest.mock('@/lib/api/msp-workbench-api', () => {
  const actual = jest.requireActual('@/lib/api/msp-workbench-api');
  return {
    ...actual,
    listMspCustomers: jest.fn(),
    getWorkbenchSummary: jest.fn(),
    switchTenantScope: jest.fn(),
  };
});

jest.mock('@/lib/api/user-preferences-api', () => ({
  getUserPreferences: jest.fn(),
  updateUserPreferences: jest.fn(),
}));

import {
  getWorkbenchSummary,
  listMspCustomers,
  type MspCustomersResponse,
  type WorkbenchSummary,
} from '@/lib/api/msp-workbench-api';
import { getUserPreferences, updateUserPreferences } from '@/lib/api/user-preferences-api';
import { useAuthStore } from '@/lib/store/auth-store';
import { CustomerFilter } from '../CustomerFilter';

const mockCustomersResponse: MspCustomersResponse = {
  customers: [
    { id: 1, code: 'ACME', name: 'Acme Corp' },
    { id: 2, code: 'BETA', name: 'Beta LLC' },
  ],
  total: 2,
};

const mockSummary: WorkbenchSummary = {
  generatedAt: '2026-09-30T08:00:00Z',
  ttlSeconds: 30,
  slaDueSoonWindowHours: 24,
  customers: [
    { customerTenantId: 1, customerName: 'Acme Corp', open: 3, slaRisk: 1, slaDueSoon: 1, unassigned: 2, members: 12, ticketsCreated30d: 30 },
    { customerTenantId: 2, customerName: 'Beta LLC', open: 5, slaRisk: 0, slaDueSoon: 0, unassigned: 1, members: 3, ticketsCreated30d: 5 },
  ],
};

describe('CustomerFilter', () => {
  beforeEach(() => {
    mockSearchParamsString = '';
    mockPathname = '/msp/workbench';
    (listMspCustomers as jest.Mock).mockResolvedValue(mockCustomersResponse);
    (getWorkbenchSummary as jest.Mock).mockResolvedValue(mockSummary);
    (getUserPreferences as jest.Mock).mockResolvedValue({ preferences: {} });
    (updateUserPreferences as jest.Mock).mockResolvedValue({ preferences: {} });
  });

  it('渲染全部客户、计数徽标与每客户计数（WB-A2）', async () => {
    render(<CustomerFilter />);
    await waitFor(() => expect(listMspCustomers).toHaveBeenCalledTimes(1));

    const trigger = screen.getByTestId('customer-filter-trigger');
    expect(trigger).toHaveTextContent('全部客户');
    // open 合计 3 + 5 = 8
    await waitFor(() => expect(trigger).toHaveTextContent('8'));

    await userEvent.click(trigger);
    const panel = await screen.findByTestId('customer-filter-panel');
    expect(within(panel).getByText('Acme Corp')).toBeInTheDocument();
    expect(within(panel).getByText('Beta LLC')).toBeInTheDocument();
    expect(within(panel).getByText('共 2 个可访问客户')).toBeInTheDocument();
    expect(screen.getByTestId('customer-counts-1')).toHaveTextContent('待处理 3');
    expect(screen.getByTestId('customer-counts-2')).toHaveTextContent('待处理 5');
    // 全部客户默认勾选
    const allRow = screen.getByTestId('customer-filter-all').closest('label')!;
    expect(within(allRow).getByRole('checkbox')).toBeChecked();
  });

  it('勾选客户后同步 URL query customerTenantIds（工作台页内不跳转）', async () => {
    mockSearchParamsString = 'customerTenantIds=1';
    (listMspCustomers as jest.Mock).mockResolvedValue(mockCustomersResponse);
    render(<CustomerFilter />);

    const trigger = screen.getByTestId('customer-filter-trigger');
    expect(trigger).toHaveTextContent('1 个客户');
    await userEvent.click(trigger);

    const row2 = await screen.findByTestId('customer-row-2');
    await userEvent.click(within(row2).getByRole('checkbox'));

    expect(mockSetSearchParams).toHaveBeenCalledTimes(1);
    const params = mockSetSearchParams.mock.calls[0][0] as URLSearchParams;
    expect(params.get('customerTenantIds')).toBe('1,2');
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('URL 初值 customerTenantIds=2 可恢复选择（刷新/分享保持）', async () => {
    mockSearchParamsString = 'customerTenantIds=2';
    render(<CustomerFilter />);

    const trigger = screen.getByTestId('customer-filter-trigger');
    expect(trigger).toHaveTextContent('1 个客户');
    await userEvent.click(trigger);

    const row2 = await screen.findByTestId('customer-row-2');
    await waitFor(() => expect(within(row2).getByRole('checkbox')).toBeChecked());
    const row1 = screen.getByTestId('customer-row-1');
    expect(within(row1).getByRole('checkbox')).not.toBeChecked();
  });

  it('非工作台页选择客户时导航到工作台并携带 query', async () => {
    mockPathname = '/dashboard';
    render(<CustomerFilter />);

    await userEvent.click(screen.getByTestId('customer-filter-trigger'));
    const row1 = await screen.findByTestId('customer-row-1');
    await userEvent.click(within(row1).getByRole('checkbox'));

    expect(mockNavigate).toHaveBeenCalledWith('/msp/workbench?customerTenantIds=1');
    expect(mockSetSearchParams).not.toHaveBeenCalled();
  });

  it('无 URL 参数时应用服务端偏好（IP-P1-6c 水合）', async () => {
    (getUserPreferences as jest.Mock).mockResolvedValue({
      preferences: { workbenchFilter: { mode: 'subset', customerTenantIds: [2] } },
    });
    render(<CustomerFilter />);

    await waitFor(() => expect(getUserPreferences).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(mockSetSearchParams).toHaveBeenCalled());
    const calls = mockSetSearchParams.mock.calls;
    const params = calls[calls.length - 1][0] as URLSearchParams;
    expect(params.get('customerTenantIds')).toBe('2');
  });

  it('选择客户后节流保存服务端偏好（IP-P1-6c 持久化）', async () => {
    render(<CustomerFilter />);
    await userEvent.click(screen.getByTestId('customer-filter-trigger'));
    const row1 = await screen.findByTestId('customer-row-1');
    await userEvent.click(within(row1).getByRole('checkbox'));

    await waitFor(() =>
      expect(updateUserPreferences).toHaveBeenCalledWith({
        workbenchFilter: { mode: 'subset', customerTenantIds: [1] },
      })
    );
  });

  it('深度态："返回工作台"切回 provider 作用域并导航（MSW-4 / Q4=A）', async () => {
    const switchTenant = jest.fn().mockResolvedValue(undefined);
    useAuthStore.setState({
      currentTenant: { id: 2, name: 'Beta LLC', code: 'BETA', type: 'msp_customer', status: 'active', createdAt: '', updatedAt: '' },
      user: { id: 1, username: 'msp', email: '', name: '', tenantId: 1, homeTenantId: 1, createdAt: '', updatedAt: '' },
      switchTenant,
    } as never);

    render(<CustomerFilter />);
    expect(screen.getByTestId('deep-tenant-indicator')).toHaveTextContent('当前客户：Beta LLC（深度操作中）');

    await userEvent.click(screen.getByTestId('back-to-workbench'));

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(1));
    expect(mockNavigate).toHaveBeenCalledWith('/msp/workbench');
    useAuthStore.setState({ currentTenant: null, user: null } as never);
  });

  it('深度态：切回失败时不导航、保留会话并提示（MSW-A5）', async () => {
    const switchTenant = jest.fn().mockRejectedValue(new Error('租户已被暂停'));
    useAuthStore.setState({
      currentTenant: { id: 2, name: 'Beta LLC', code: 'BETA', type: 'msp_customer', status: 'active', createdAt: '', updatedAt: '' },
      user: { id: 1, username: 'msp', email: '', name: '', tenantId: 1, homeTenantId: 1, createdAt: '', updatedAt: '' },
      switchTenant,
    } as never);

    render(<CustomerFilter />);
    await userEvent.click(screen.getByTestId('back-to-workbench'));

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(1));
    expect(mockNavigate).not.toHaveBeenCalled();
    expect(useAuthStore.getState().currentTenant?.id).toBe(2);
    expect(await screen.findByText('租户已被暂停')).toBeInTheDocument();
    useAuthStore.setState({ currentTenant: null, user: null } as never);
  });

  it('"进入客户"切换会话后落到登录后主页 /dashboard（不落公开落地页，MSW-A5 回归）', async () => {
    const switchTenant = jest.fn().mockResolvedValue(undefined);
    useAuthStore.setState({
      currentTenant: { id: 1, name: 'Acme Corp', code: 'ACME', type: 'msp_provider', status: 'active', createdAt: '', updatedAt: '' },
      user: { id: 1, username: 'msp', email: '', name: '', tenantId: 1, homeTenantId: 1, createdAt: '', updatedAt: '' },
      switchTenant,
    } as never);

    render(<CustomerFilter />);
    await userEvent.click(screen.getByTestId('customer-filter-trigger'));
    await userEvent.click(await screen.findByTestId('enter-customer-2'));

    const confirm = await screen.findByRole('button', { name: /^进\s*入$/ });
    await userEvent.click(confirm);

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(2));
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/dashboard'));
    useAuthStore.setState({ currentTenant: null, user: null } as never);
  });
});
