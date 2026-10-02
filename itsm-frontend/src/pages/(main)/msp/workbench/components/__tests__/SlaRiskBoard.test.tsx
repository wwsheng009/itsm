/**
 * SlaRiskBoard 测试（IP-P2-4b）。
 *
 * 覆盖：
 * - 风险排序：超期 desc → 临近 desc → open desc；
 * - 徽标与合计（超期/临近）+ 服务端窗口回显；
 * - 点击客户行 → onSelectCustomer（父页面写 customerTenantIds）；
 * - 刷新重新拉取；空数据与错误态。
 */
import React from 'react';
import { render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';
import SlaRiskBoard, { type SlaRiskBoardProps } from '../SlaRiskBoard';
import type { WorkbenchSummaryCustomer } from '@/lib/api/msp-workbench-api';

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

function setup(overrides: Partial<SlaRiskBoardProps> = {}) {
  const fetchSummary = jest
    .fn()
    .mockResolvedValue({ slaDueSoonWindowHours: 24, customers: [beta, gamma, acme] });
  const props: SlaRiskBoardProps = { fetchSummary, ...overrides };
  render(<SlaRiskBoard {...props} />);
  return { fetchSummary, props };
}

describe('SlaRiskBoard', () => {
  it('按风险排序并展示徽标/合计/窗口', async () => {
    setup();

    await waitFor(() => expect(screen.getByTestId('sla-risk-row-1')).toBeInTheDocument());
    const rows = screen.getAllByTestId(/^sla-risk-row-/);
    expect(rows.map(row => row.getAttribute('data-testid'))).toEqual([
      'sla-risk-row-1', // 超期 2，排最前
      'sla-risk-row-2', // 临近 2
      'sla-risk-row-3', // 无风险
    ]);

    expect(screen.getByTestId('sla-breach-1')).toHaveTextContent('超期 2');
    expect(screen.getByTestId('sla-due-soon-2')).toHaveTextContent('临近 2');
    expect(screen.queryByTestId('sla-breach-3')).not.toBeInTheDocument();
    expect(screen.getByTestId('sla-total-risk')).toHaveTextContent('超期 2');
    expect(screen.getByTestId('sla-total-due')).toHaveTextContent('临近 2');
    expect(screen.getByTestId('sla-risk-board')).toHaveTextContent('窗口 24h');
  });

  it('点击客户行回调 onSelectCustomer（驱动 customerTenantIds 过滤）', async () => {
    const onSelectCustomer = jest.fn();
    setup({ onSelectCustomer });

    await waitFor(() => expect(screen.getByTestId('sla-risk-row-2')).toBeInTheDocument());
    await userEvent.click(screen.getByTestId('sla-risk-row-2'));
    expect(onSelectCustomer).toHaveBeenCalledWith(2);
  });

  it('刷新按钮重新拉取 summary', async () => {
    const { fetchSummary } = setup();
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByTestId('sla-risk-refresh'));
    await waitFor(() => expect(fetchSummary).toHaveBeenCalledTimes(2));
  });

  it('空数据渲染空态', async () => {
    setup({ fetchSummary: jest.fn().mockResolvedValue({ customers: [] }) });
    expect(await screen.findByText('无可用客户')).toBeInTheDocument();
  });

  it('请求失败渲染错误态', async () => {
    setup({ fetchSummary: jest.fn().mockRejectedValue(new Error('boom')) });
    expect(await screen.findByTestId('sla-risk-error')).toHaveTextContent('boom');
  });
});
