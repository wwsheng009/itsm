/**
 * MSP 跨客户工作台页测试（IP-P0-8 / WB-A2、WB-A5）。
 *
 * 覆盖：
 * - 列表渲染客户列与跨客户条目；
 * - 行内操作严格按每条 allowedActions[] 渲染（allowed=true 可点，allowed=false 禁用）；
 * - allowedActions 为空 → 只读态，不渲染操作按钮；
 * - nextCursor 分页（加载更多携带 cursor）。
 */
import React from 'react';
import { fireEvent } from '@testing-library/react';
import { render, screen, waitFor, within } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';

// 交互链路用例在慢环境（CI/本地冷启动/全量并跑+覆盖率）下单例可达 170s+，统一放宽到 300s 避免假红。
jest.setTimeout(300000);

const mockSetSearchParams = jest.fn();

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useNavigate: () => jest.fn(),
  useLocation: () => ({
    pathname: '/msp/workbench',
    search: '',
    hash: '',
    state: null,
    key: 'test',
  }),
  useSearchParams: () => [
    new URLSearchParams('customerTenantIds=all'),
    mockSetSearchParams,
  ],
}));

jest.mock('@/lib/api/msp-workbench-api', () => {
  const actual = jest.requireActual('@/lib/api/msp-workbench-api');
  return {
    ...actual,
    listWorkbenchTickets: jest.fn(),
    batchWorkbenchItems: jest.fn(),
    assignWorkbenchTicket: jest.fn(),
    // IP-P2-4a：默认按"服务端灰度未开启"处理，现有用例不受视图控件影响。
    listWorkbenchViews: jest.fn().mockResolvedValue({ views: [], total: 0, enabled: false }),
  };
});

import {
  assignWorkbenchTicket,
  batchWorkbenchItems,
  listWorkbenchTickets,
  type WorkbenchTicketItem,
} from '@/lib/api/msp-workbench-api';
import MSPWorkbenchPage from '../index';

const ticketWithActions: WorkbenchTicketItem = {
  id: 1,
  customerTenantId: 1,
  customerName: 'Acme Corp',
  ticketNumber: 'ACME-1',
  title: 'VPN 无法连接',
  status: 'open',
  priority: 'high',
  assigneeName: '张三',
  updatedAt: '2026-09-30T08:00:00Z',
  allowedActions: [
    { action: 'reply', allowed: true },
    {
      action: 'status',
      allowed: false,
      reasonCode: 'ACTION_NOT_ALLOWED',
      reasonText: '当前角色在该客户租户无对应权限',
    },
    { action: 'assign', allowed: false, reasonCode: 'ACTION_NOT_ALLOWED' },
  ],
};

const readOnlyTicket: WorkbenchTicketItem = {
  id: 2,
  customerTenantId: 2,
  customerName: 'Beta LLC',
  ticketNumber: 'BETA-2',
  title: '打印机离线',
  status: 'in_progress',
  priority: 'medium',
  updatedAt: '2026-09-30T07:00:00Z',
  allowedActions: [],
};

describe('MSPWorkbenchPage', () => {
  beforeEach(() => {
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: [ticketWithActions, readOnlyTicket],
      total: 2,
    });
    (batchWorkbenchItems as jest.Mock).mockResolvedValue({
      batchId: 'batch-test',
      succeeded: 0,
      failed: 0,
      results: [],
    });
    (assignWorkbenchTicket as jest.Mock).mockResolvedValue(undefined);
  });

  it('渲染跨客户列表并带客户列', async () => {
    render(<MSPWorkbenchPage />);

    expect(await screen.findByText('VPN 无法连接')).toBeInTheDocument();
    expect(screen.getByText('打印机离线')).toBeInTheDocument();
    expect(screen.getByText('Acme Corp')).toBeInTheDocument();
    expect(screen.getByText('Beta LLC')).toBeInTheDocument();
    expect(screen.getByTestId('workbench-scope')).toHaveTextContent('全部客户');
    expect(listWorkbenchTickets).toHaveBeenCalledWith(
      expect.objectContaining({ customerTenantIds: 'all', sort: 'updated' })
    );
  });

  it('行内操作严格按 allowedActions 渲染（reply 可用 / status 禁用）', async () => {
    render(<MSPWorkbenchPage />);

    const replyButton = await screen.findByTestId('action-reply-1');
    expect(replyButton).toBeEnabled();
    expect(screen.getByTestId('action-status-1')).toBeDisabled();
  });

  it('allowedActions 为空 → 只读态且不渲染操作按钮（WB-A5）', async () => {
    render(<MSPWorkbenchPage />);

    await screen.findByText('打印机离线');
    expect(screen.getByTestId('ticket-readonly-2')).toHaveTextContent('只读');
    expect(screen.queryByTestId('action-reply-2')).not.toBeInTheDocument();
    expect(screen.queryByTestId('action-status-2')).not.toBeInTheDocument();
  });

  it('nextCursor 分页：加载更多携带游标并追加条目', async () => {
    (listWorkbenchTickets as jest.Mock)
      .mockResolvedValueOnce({ items: [ticketWithActions], nextCursor: 'cursor-1', total: 1 })
      .mockResolvedValueOnce({ items: [readOnlyTicket], total: 1 });

    render(<MSPWorkbenchPage />);
    await screen.findByText('VPN 无法连接');

    const loadMore = screen.getByTestId('workbench-load-more');
    await userEvent.click(loadMore);

    await waitFor(() => expect(screen.getByText('打印机离线')).toBeInTheDocument());
    const secondCall = (listWorkbenchTickets as jest.Mock).mock.calls[1][0];
    expect(secondCall.cursor).toBe('cursor-1');
    expect(screen.queryByTestId('workbench-load-more')).not.toBeInTheDocument();
  });

  it('CUSTOMER_INACTIVE → 只读态（暂停客户不渲染操作按钮）', async () => {
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: [
        {
          ...ticketWithActions,
          id: 3,
          allowedActions: [
            {
              action: 'reply',
              allowed: false,
              reasonCode: 'CUSTOMER_INACTIVE',
              reasonText: '客户租户已暂停或过期，仅可查看',
            },
          ],
        },
      ],
      total: 1,
    });

    render(<MSPWorkbenchPage />);

    expect(await screen.findByTestId('ticket-readonly-3')).toHaveTextContent('只读');
    expect(screen.queryByTestId('action-reply-3')).not.toBeInTheDocument();
  });

  it('批量回复：选择 → 客户分布确认 → 逐条结果（IP-P1-6b）', async () => {
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: [
        { ...ticketWithActions },
        { ...readOnlyTicket, allowedActions: [{ action: 'reply', allowed: true }] },
      ],
      total: 2,
    });
    (batchWorkbenchItems as jest.Mock).mockResolvedValue({
      batchId: 'BATCH-9',
      succeeded: 1,
      failed: 1,
      results: [
        { ticketId: 1, customerTenantId: 1, ok: true },
        {
          ticketId: 2,
          customerTenantId: 2,
          ok: false,
          reasonCode: 'ACTION_NOT_ALLOWED',
          message: '当前角色无权限',
        },
      ],
    });

    render(<MSPWorkbenchPage />);
    const row1 = (await screen.findByText('VPN 无法连接')).closest('tr') as HTMLElement;
    const row2 = screen.getByText('打印机离线').closest('tr') as HTMLElement;
    fireEvent.click(within(row1).getByRole('checkbox'));
    fireEvent.click(within(row2).getByRole('checkbox'));

    expect(screen.getByTestId('batch-selected-count')).toHaveTextContent('已选 2 条');
    fireEvent.click(screen.getByTestId('batch-reply'));
    fireEvent.change(screen.getByTestId('batch-reply-content'), {
      target: { value: '统一回复：正在处理' },
    });
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));

    const confirm = await screen.findByTestId('batch-confirm');
    expect(within(confirm).getByTestId('batch-dist-Acme Corp')).toHaveTextContent('1 条');
    expect(within(confirm).getByTestId('batch-dist-Beta LLC')).toHaveTextContent('1 条');
    fireEvent.click(screen.getByRole('button', { name: '提交（2 条）' }));

    await waitFor(() =>
      expect(batchWorkbenchItems).toHaveBeenCalledWith({
        action: 'reply',
        items: [
          { ticketId: 1, customerTenantId: 1 },
          { ticketId: 2, customerTenantId: 2 },
        ],
        payload: { content: '统一回复：正在处理' },
      })
    );

    const result = await screen.findByTestId('batch-result');
    expect(result).toHaveTextContent('成功 1');
    expect(result).toHaveTextContent('失败 1');
    expect(result).toHaveTextContent('ACTION_NOT_ALLOWED');
  });

  it('行内指派：assign 动作 allowed=true 时确认后指派给当前技术员', async () => {
    (listWorkbenchTickets as jest.Mock).mockResolvedValue({
      items: [{ ...ticketWithActions, allowedActions: [{ action: 'assign', allowed: true }] }],
      total: 1,
    });

    render(<MSPWorkbenchPage />);
    fireEvent.click(await screen.findByTestId('action-assign-1'));

    const confirm = await screen.findByTestId('assign-confirm');
    expect(confirm).toHaveTextContent('当前 MSP 技术员');
    fireEvent.click(screen.getByRole('button', { name: '确认指派' }));

    await waitFor(() =>
      expect(assignWorkbenchTicket).toHaveBeenCalledWith(1, { customerTenantId: 1 })
    );
  });

  it('分组视图：按客户分组展示，组内可勾选且保留行内操作（P1）', async () => {
    render(<MSPWorkbenchPage />);
    await screen.findByText('VPN 无法连接');

    fireEvent.click(screen.getByText('按客户分组'));

    expect(await screen.findByTestId('workbench-group-view')).toBeInTheDocument();
    expect(screen.getByTestId('group-count-1')).toHaveTextContent('1 条');
    expect(screen.getByTestId('group-count-2')).toHaveTextContent('1 条');

    const body1 = screen.getByTestId('group-body-1');
    expect(within(body1).getByText('ACME-1')).toBeInTheDocument();
    expect(within(body1).getByTestId('action-reply-1')).toBeInTheDocument();
    // 组内复选：只读行（Beta）禁用，可勾选行（Acme）驱动全局批量条。
    const row1 = within(body1).getByText('ACME-1').closest('tr') as HTMLElement;
    fireEvent.click(within(row1).getByRole('checkbox'));
    await waitFor(() =>
      expect(screen.getByTestId('batch-selected-count')).toHaveTextContent('已选 1 条')
    );
    const body2 = screen.getByTestId('group-body-2');
    const row2 = within(body2).getByText('BETA-2').closest('tr') as HTMLElement;
    expect(within(row2).getByRole('checkbox')).toBeDisabled();
  });
});
