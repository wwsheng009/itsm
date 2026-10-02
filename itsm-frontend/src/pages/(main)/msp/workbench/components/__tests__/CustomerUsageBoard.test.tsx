/**
 * CustomerUsageBoard 测试（IP-P2-4c）。
 *
 * 覆盖：
 * - 排序：窗口新增 desc → 成员 desc → 名称；
 * - 三类用量数字（成员/未关闭/窗口新增）与窗口回显；
 * - 点击客户行 → onSelectCustomer；刷新重新拉取；空态/错误态。
 */
import React from 'react';
import { render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';
import CustomerUsageBoard, { type CustomerUsageBoardProps } from '../CustomerUsageBoard';
import type { WorkbenchSummaryCustomer } from '@/lib/api/msp-workbench-api';

jest.setTimeout(120000);

const acme: WorkbenchSummaryCustomer = {
  customerTenantId: 1,
  customerName: 'Acme Corp',
  open: 4,
  slaRisk: 2,
  slaDueSoon: 0,
  unassigned: 1,
  members: 12,
  ticketsCreated30d: 30,
};

const beta: WorkbenchSummaryCustomer = {
  customerTenantId: 2,
  customerName: 'Beta LLC',
  open: 3,
  slaRisk: 0,
  slaDueSoon: 2,
  unassigned: 0,
  members: 3,
  ticketsCreated30d: 5,
};

const gamma: WorkbenchSummaryCustomer = {
  customerTenantId: 3,
  customerName: 'Gamma Inc',
  open: 1,
  slaRisk: 0,
  slaDueSoon: 0,
  unassigned: 0,
  members: 1,
  ticketsCreated30d: 0,
};

function setup(overrides: Partial<CustomerUsageBoardProps> = {}) {
  const fetchSummary = jest
    .fn()
    .mockResolvedValue({ usageWindowDays: 30, customers: [beta, gamma, acme] });
  const props: CustomerUsageBoardProps = { fetchSummary, ...overrides };
  render(<CustomerUsageBoard {...props} />);
  return { fetchSummary, props };
}

describe('CustomerUsageBoard', () => {
  it('按窗口新增排序并展示成员/未关闭/新增与窗口', async () => {
    setup();

    await waitFor(() => expect(screen.getByTestId('usage-row-1')).toBeInTheDocument());
    const rows = screen.getAllByTestId(/^usage-row-/);
    expect(rows.map(row => row.getAttribute('data-testid'))).toEqual([
      'usage-row-1', // 新增 30
      'usage-row-2', // 新增 5
      'usage-row-3', // 新增 0
    ]);

    expect(screen.getByTestId('usage-members-1')).toHaveTextContent('成员 12');
    expect(screen.getByTestId('usage-open-1')).toHaveTextContent('未关闭 4');
    expect(screen.getByTestId('usage-created-1')).toHaveTextContent('新增 30');
    expect(screen.getByTestId('usage-window')).toHaveTextContent('窗口 30d');
    expect(screen.getByTestId('customer-usage-board')).toHaveTextContent('用量口径（无硬配额数据源）');
  });

  it('点击客户行回调 onSelectCustomer（驱动 customerTenantIds 过滤）', async () => {
    const onSelectCustomer = jest.fn();
    setup({ onSelectCustomer });

    await waitFor(() => expect(screen.getByTestId('usage-row-3')).toBeInTheDocument());
    await userEvent.click(screen.getByTestId('usage-row-3'));
    expect(onSelectCustomer).toHaveBeenCalledWith(3);
  });

  it('刷新按钮重新拉取 summary', async () => {
    const { fetchSummary } = setup();
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByTestId('usage-refresh'));
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(2));
  });

  it('空数据渲染空态', async () => {
    setup({ fetchSummary: jest.fn().mockResolvedValue({ customers: [] }) });
    expect(await screen.findByText('无可用客户')).toBeInTheDocument();
  });

  it('请求失败渲染错误态', async () => {
    setup({ fetchSummary: jest.fn().mockRejectedValue(new Error('usage boom')) });
    expect(await screen.findByTestId('usage-error')).toHaveTextContent('usage boom');
  });
});
