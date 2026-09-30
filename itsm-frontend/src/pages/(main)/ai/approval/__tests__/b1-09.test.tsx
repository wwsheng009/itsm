/**
 * 审批页 B1-09 增强测试：目标对象跳转、确认时限倒计时与过期禁用、dry-run 预览快照。
 *
 * 与 `index.test.tsx`（M1-06）分开组织：本文件聚焦 B1-09 新增面，
 * 沿用同一 mock 口径（ai-api / mcp-api / permissions / auth-store / react-router）。
 */
import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor } from '@testing-library/react';

import AIApprovalQueue from '..';
import { aiGetToolApprovals, aiGetToolInvocation } from '@/lib/api/ai-api';
import { mcpApi } from '@/lib/api/mcp-api';
import { usePermissions } from '@/lib/hooks/use-permissions';
import type { ToolApproval } from '@/lib/api/ai-api';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetToolApprovals: jest.fn(),
  aiApproveTool: jest.fn(),
  aiGetToolInvocation: jest.fn(),
}));

jest.mock('@/lib/api/mcp-api', () => ({
  __esModule: true,
  mcpApi: { listServers: jest.fn() },
}));

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));

jest.mock('@/lib/store/auth-store', () => ({
  __esModule: true,
  useAuthStoreHydration: () => undefined,
}));

const mockNavigate = jest.fn();
jest.mock('react-router', () => ({
  __esModule: true,
  useNavigate: () => mockNavigate,
}));

const mockedList = aiGetToolApprovals as jest.MockedFunction<typeof aiGetToolApprovals>;
const mockedDetail = aiGetToolInvocation as jest.MockedFunction<typeof aiGetToolInvocation>;
const mockedMcp = mcpApi as jest.Mocked<typeof mcpApi>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;

const btn = (label: string) => new RegExp(label.split('').join('\\s*'));

const base: ToolApproval = {
  id: 21,
  toolName: 'mcp__mock__create_issue',
  argsRedacted: '{"title":"打印机故障","token":"****"}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  createdAt: '2026-09-27T02:00:00Z',
  conversationId: 3,
  userId: 7,
  provider: 'mcp',
  serverName: 'mock',
  callableName: 'mcp__mock__create_issue',
  risk: 'act_high',
  permissionCheck: 'passed',
};

const renderPage = () =>
  render(
    <App>
      <AIApprovalQueue />
    </App>
  );

beforeEach(() => {
  jest.clearAllMocks();
  mockedMcp.listServers.mockResolvedValue({ items: [], summary: {} } as never);
  mockedPermissions.mockReturnValue({ hasPermission: () => false } as unknown as ReturnType<typeof usePermissions>);
});

describe('B1-09 审批页增强', () => {
  it('目标对象列：已核实路由可跳转，依据引用可见', async () => {
    mockedList.mockResolvedValue({
      items: [{ ...base, targetType: 'ticket', targetId: '1001', supportRef: 'kb:runbook/42' }],
      state: 'pending',
      provider: '',
      server: '',
    } as never);

    renderPage();
    const link = await screen.findByTestId('target-link-21');
    expect(link).toHaveTextContent('ticket#1001');
    expect(screen.getByText('依据 kb:runbook/42')).toBeTruthy();

    fireEvent.click(link);
    expect(mockNavigate).toHaveBeenCalledWith('/tickets/1001');
  });

  it('未核实路由类型只展示标识，不生成链接', async () => {
    mockedList.mockResolvedValue({
      items: [{ ...base, targetType: 'unknown_entity', targetId: 'X-9' }],
      state: 'pending',
      provider: '',
      server: '',
    } as never);

    renderPage();
    expect(await screen.findByText('unknown_entity#X-9')).toBeTruthy();
    expect(screen.queryByTestId('target-link-21')).toBeNull();
  });

  it('确认时限：未过期显示剩余时间；已过期禁用通过/驳回', async () => {
    const future = new Date(Date.now() + 90 * 60 * 1000).toISOString();
    mockedList.mockResolvedValue({
      items: [{ ...base, expiresAt: future }],
      state: 'pending',
      provider: '',
      server: '',
    } as never);

    const { unmount } = renderPage();
    expect(await screen.findByText(/剩余 1 小时/)).toBeTruthy();
    expect(screen.getByText(btn('通过')).closest('button')).toBeEnabled();
    unmount();

    const past = new Date(Date.now() - 60 * 1000).toISOString();
    mockedList.mockResolvedValue({
      items: [{ ...base, expiresAt: past }],
      state: 'pending',
      provider: '',
      server: '',
    } as never);

    renderPage();
    expect(await screen.findByText('已过期')).toBeTruthy();
    expect(screen.getByText(btn('通过')).closest('button')).toBeDisabled();
    expect(screen.getByText(btn('驳回')).closest('button')).toBeDisabled();
  });

  it('dry-run 预览：展开行按需拉取详情并展示快照（脱敏参数一并展示）', async () => {
    mockedList.mockResolvedValue({
      items: [{ ...base, dryRun: true, status: 'preview', approvalState: 'auto' }],
      state: 'auto',
      provider: '',
      server: '',
    } as never);
    mockedDetail.mockResolvedValue({
      ...base,
      dryRun: true,
      status: 'preview',
      approvalState: 'auto',
      result: '{"wouldUpdate":3}',
    } as never);

    const { container } = renderPage();
    expect(await screen.findByText('预览')).toBeTruthy();

    fireEvent.click(container.querySelector('.ant-table-row-expand-icon') as Element);
    await waitFor(() => expect(mockedDetail).toHaveBeenCalledWith(21));
    expect(await screen.findByTestId('dry-run-result')).toHaveTextContent('wouldUpdate');
    expect(screen.getByTestId('dry-run-args')).toHaveTextContent('"token": "****"');
  });
});
