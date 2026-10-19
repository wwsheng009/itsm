import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';

import HandoffActionCard from '../HandoffActionCard';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import type { ChatMessage } from '@/components/ai/chat-core/types';
import { TicketApi } from '@/lib/api/ticket-api';
import { UserApi } from '@/lib/api/user-api';

// jest.setup.js 全局 mock 了 useLocation；本套件需要真实路由状态。
jest.mock('react-router', () => jest.requireActual('react-router'));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
jest.mock('@/lib/api/ticket-api', () => ({
  __esModule: true,
  TicketApi: { assignTicket: jest.fn() },
}));
jest.mock('@/lib/api/incident-api', () => ({
  __esModule: true,
  IncidentAPI: { assignIncident: jest.fn() },
}));
jest.mock('@/lib/api/user-api', () => ({
  __esModule: true,
  UserApi: { getUsers: jest.fn() },
}));

const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedTrack = trackAssistantEvent as jest.Mock;
const mockedAssign = TicketApi.assignTicket as jest.Mock;
const mockedUsers = UserApi.getUsers as jest.Mock;

const message: ChatMessage = {
  id: 'a-1',
  role: 'assistant',
  content: '建议先检查网络配置，再联系人工坐席。',
  createdAt: '2026-10-19T10:00:00Z',
  conversationId: 7,
  queryHash: 'hash-1',
};

const LocationProbe = () => {
  const location = useLocation();
  return (
    <div
      data-testid="handoff-location"
      data-path={location.pathname}
      data-state={JSON.stringify(location.state ?? null)}
    />
  );
};

const renderCard = () =>
  render(
    <MemoryRouter initialEntries={['/tickets/42']}>
      <App>
        <HandoffActionCard open message={message} onClose={jest.fn()} />
        <LocationProbe />
      </App>
    </MemoryRouter>
  );

const grant = (allowed: string[]) => {
  mockedPermissions.mockReturnValue({
    hasPermission: (resource: string, action: string) =>
      allowed.includes(`${resource}:${action}`),
  } as unknown as ReturnType<typeof usePermissions>);
};

describe('HandoffActionCard（T5-04a/b）', () => {
  beforeEach(() => {
    mockedTrack.mockReset();
    mockedAssign.mockReset();
    mockedAssign.mockResolvedValue({});
    mockedUsers.mockReset();
    mockedUsers.mockResolvedValue({
      users: [
        { id: 7, name: '张三', username: 'zhangsan', department: '服务台', active: true },
      ],
    });
    useAssistantContextStore.setState({
      owner: Symbol('test'),
      pageData: { targetType: 'ticket', targetId: 42, summary: '打印机无法连接' },
    });
    grant(['ticket:read', 'ticket:create', 'ticket:assign', 'incident:read', 'incident:write']);
  });

  afterEach(() => {
    useAssistantContextStore.setState({ owner: null, pageData: null });
  });

  it('打开当前对象并记录 handoff 埋点', async () => {
    renderCard();

    fireEvent.click(screen.getByTestId('handoff-action-open'));

    await waitFor(() =>
      expect(screen.getByTestId('handoff-location')).toHaveAttribute('data-path', '/tickets/42')
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({
        conversationId: 7,
        extra: expect.objectContaining({ action: 'open', targetType: 'ticket', targetId: 42 }),
      })
    );
  });

  it('预填新建工单：携带一次性 state，且不提交', async () => {
    renderCard();

    fireEvent.click(screen.getByTestId('handoff-action-prefill-ticket'));

    await waitFor(() =>
      expect(screen.getByTestId('handoff-location')).toHaveAttribute('data-path', '/tickets/create')
    );
    const state = JSON.parse(
      screen.getByTestId('handoff-location').getAttribute('data-state') ?? 'null'
    );
    expect(state).toEqual({
      source: 'assistant_handoff',
      targetType: 'ticket',
      prefill: {
        title: '打印机无法连接',
        description: '建议先检查网络配置，再联系人工坐席。',
      },
    });
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({
        extra: expect.objectContaining({ action: 'prefill-ticket', targetType: 'ticket' }),
      })
    );
  });

  it('权限裁剪：无 ticket:create / incident:write 时只保留打开动作', () => {
    grant(['ticket:read']);
    renderCard();

    expect(screen.getByTestId('handoff-action-open')).toBeInTheDocument();
    expect(screen.queryByTestId('handoff-action-prefill-ticket')).not.toBeInTheDocument();
    expect(screen.queryByTestId('handoff-action-prefill-incident')).not.toBeInTheDocument();
    expect(screen.queryByTestId('handoff-action-assign')).not.toBeInTheDocument();
  });

  it('无任何权限时展示空态', () => {
    grant([]);
    renderCard();

    expect(screen.getByTestId('handoff-empty')).toBeInTheDocument();
  });

  it('指派：加载候选人，二次确认后调用工单 assign', async () => {
    renderCard();

    fireEvent.click(screen.getByTestId('handoff-action-assign'));
    await waitFor(() =>
      expect(mockedUsers).toHaveBeenCalledWith(
        expect.objectContaining({ page: 1, pageSize: 200, status: 'active' })
      )
    );

    fireEvent.mouseDown(screen.getByTestId('handoff-assignee-select'));
    fireEvent.click(await screen.findByText('张三 · 服务台'));
    fireEvent.click(screen.getByTestId('handoff-assign-confirm'));

    await waitFor(() => expect(mockedAssign).toHaveBeenCalledWith(42, { assigneeId: 7 }));
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({
        extra: expect.objectContaining({
          action: 'assign',
          targetType: 'ticket',
          targetId: 42,
          assigneeId: 7,
          success: true,
        }),
      })
    );
  });

  it('指派失败：保留动作卡并显示后端原因', async () => {
    mockedAssign.mockRejectedValue({ message: '目标工单已关闭' });
    renderCard();

    fireEvent.click(screen.getByTestId('handoff-action-assign'));
    await waitFor(() => expect(mockedUsers).toHaveBeenCalled());
    fireEvent.mouseDown(screen.getByTestId('handoff-assignee-select'));
    fireEvent.click(await screen.findByText('张三 · 服务台'));
    fireEvent.click(screen.getByTestId('handoff-assign-confirm'));

    expect(await screen.findByTestId('handoff-assign-error')).toHaveTextContent('目标工单已关闭');
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({
        extra: expect.objectContaining({ action: 'assign', success: false }),
      })
    );
  });
});
