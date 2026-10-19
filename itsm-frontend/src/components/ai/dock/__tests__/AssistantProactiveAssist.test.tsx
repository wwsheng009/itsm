/**
 * I1-03 主动协助集成测试：FAB 徽标 / 内联建议条 / 通知订阅 / 静默开关 / 「不再提示」记忆。
 *
 * 数据面全部 mock（只读聚合契约由 `lib/ai/proactive/__tests__/signals.test.ts` 覆盖），
 * 本文件验证容器接线与用户可见行为（含负向断言：总开关关闭 → 零请求、无打扰）。
 */
import React from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AssistantDock from '../AssistantDock';
import { aiGetToolApprovals, type ToolApproval } from '@/lib/api/ai-api';
import {
  TicketNotificationApi,
  type UserNotification,
} from '@/lib/api/ticket-notification-api';
import { notificationWS } from '@/lib/services/notification-ws';
import { useAssistantProactiveStore } from '@/lib/store/assistant-proactive-store';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { usePermissions } from '@/lib/hooks/use-permissions';

// 需要真实路由上下文（页面感知 + MemoryRouter）。
jest.mock('react-router', () => jest.requireActual('react-router'));
jest.setTimeout(300000);

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetToolApprovals: jest.fn(),
}));
jest.mock('@/lib/api/ticket-notification-api', () => ({
  __esModule: true,
  TicketNotificationApi: {
    getUserNotifications: jest.fn(),
  },
}));
jest.mock('@/lib/services/notification-ws', () => ({
  __esModule: true,
  notificationWS: {
    onNotification: jest.fn(),
  },
}));
jest.mock('../AssistantChatPanel', () => ({
  __esModule: true,
  default: () => <div data-testid="assistant-chat-panel-stub" />,
}));

const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedApprovals = aiGetToolApprovals as jest.MockedFunction<typeof aiGetToolApprovals>;
const mockedNotifications = TicketNotificationApi.getUserNotifications as jest.MockedFunction<
  typeof TicketNotificationApi.getUserNotifications
>;
const wsOnNotification = notificationWS.onNotification as jest.Mock;

/** 当前挂载中被捕获的订阅回调（用于模拟服务端推送）。 */
let wsSubscribers: Array<(payload: UserNotification) => void> = [];

const emitNotification = (payload: UserNotification) => {
  act(() => {
    wsSubscribers.forEach(callback => callback(payload));
  });
};

const grantPermission = (allowed: boolean) => {
  mockedPermissions.mockReturnValue({
    hasPermission: () => allowed,
  } as unknown as ReturnType<typeof usePermissions>);
};

const renderDock = (route = '/tickets/42') =>
  render(
    <MemoryRouter initialEntries={[route]}>
      <AssistantDock />
    </MemoryRouter>
  );

const pendingApproval = (id: number): ToolApproval => ({
  id,
  toolName: 'create_ticket',
  argsRedacted: '{}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  conversationId: 1,
  userId: 7,
  createdAt: '2026-10-18T00:00:00Z',
});

const userNotification = (over: Partial<UserNotification> = {}): UserNotification => ({
  id: 99,
  title: 'SLA 临期',
  message: '工单 #42 即将超时',
  type: 'sla_warning',
  read: false,
  userId: 7,
  tenantId: 1,
  createdAt: '2026-10-18T00:00:00Z',
  updatedAt: '2026-10-18T00:00:00Z',
  ...over,
});

beforeEach(() => {
  window.localStorage.clear();
  useAssistantStore.setState({ open: false, pendingPrompt: undefined });
  useAssistantProactiveStore.setState({ enabled: true, silent: true, dismissedScopes: [] });
  grantPermission(true);
  mockedApprovals.mockReset();
  mockedApprovals.mockResolvedValue({ items: [], state: 'pending' });
  mockedNotifications.mockReset();
  mockedNotifications.mockResolvedValue({ notifications: [], total: 0, page: 1, size: 20 });
  wsSubscribers = [];
  wsOnNotification.mockReset();
  wsOnNotification.mockImplementation((callback: (payload: UserNotification) => void) => {
    wsSubscribers.push(callback);
    return () => {
      wsSubscribers = wsSubscribers.filter(item => item !== callback);
    };
  });
});

describe('AssistantDock 主动协助（I1-03）', () => {
  it('徽标：待审批计数上球（默认静默不影响徽标，不出内联条）', async () => {
    mockedApprovals.mockResolvedValue({
      items: [pendingApproval(1), pendingApproval(2)],
      state: 'pending',
    });
    renderDock();

    const badge = await screen.findByTestId('assistant-fab-badge');
    expect(badge).toHaveTextContent('2');
    expect(badge).toHaveAttribute('aria-label', '2 项 AI 待处理');
    // 默认静默：页面建议不进内联条（只有徽标）
    expect(screen.queryByTestId('assistant-proactive-bar')).not.toBeInTheDocument();
  });

  it('默认静默不出条；关闭静默后出现页面建议；「不再提示」写入持久化记忆', async () => {
    renderDock();
    // 默认静默（store 默认值可复跑断言）
    expect(useAssistantProactiveStore.getState().silent).toBe(true);
    expect(screen.queryByTestId('assistant-proactive-bar')).not.toBeInTheDocument();

    act(() => {
      useAssistantProactiveStore.getState().setSilent(false);
    });

    const bar = await screen.findByTestId('assistant-proactive-bar');
    expect(bar).toHaveTextContent('建议：分诊建议（分类与优先级）');
    expect(bar).toHaveTextContent('工单详情');

    fireEvent.click(screen.getByRole('button', { name: '不再提示' }));
    await waitFor(() =>
      expect(screen.queryByTestId('assistant-proactive-bar')).not.toBeInTheDocument()
    );
    expect(useAssistantProactiveStore.getState().dismissedScopes).toContain('page:ticket.detail');
    const persisted = JSON.parse(
      window.localStorage.getItem('assistant.proactive.v1') || '{}'
    ) as { state?: { dismissedScopes?: string[] } };
    expect(persisted.state?.dismissedScopes).toContain('page:ticket.detail');
  });

  it('建议条「让 AI 看」：只预填 + 打开面板（不自动发送）', async () => {
    useAssistantProactiveStore.setState({ silent: false });
    renderDock();

    const bar = await screen.findByTestId('assistant-proactive-bar');
    fireEvent.click(within(bar).getByRole('button', { name: '让 AI 看' }));

    expect(useAssistantStore.getState().pendingPrompt).toBe(
      '请给出这张工单的分类与优先级建议，并说明判断依据。'
    );
    expect(useAssistantStore.getState().open).toBe(true);
    expect(screen.getByTestId('assistant-panel')).toBeInTheDocument();
    expect(screen.queryByTestId('assistant-proactive-bar')).not.toBeInTheDocument();
  });

  it('通知订阅：WS 高价值未读通知计入徽标；普通通知不计', async () => {
    renderDock();
    await waitFor(() => expect(mockedNotifications).toHaveBeenCalled());

    emitNotification(userNotification());
    const badge = await screen.findByTestId('assistant-fab-badge');
    expect(badge).toHaveTextContent('1');

    emitNotification(userNotification({ id: 100, type: 'commented' }));
    await waitFor(() => expect(badge).toHaveTextContent('1'));
  });

  it('总开关关闭：完全静默（无徽标、无建议条、零信号请求）', async () => {
    useAssistantProactiveStore.setState({ enabled: false, silent: false });
    mockedApprovals.mockResolvedValue({ items: [pendingApproval(1)], state: 'pending' });
    renderDock();

    expect(screen.queryByTestId('assistant-fab-badge')).not.toBeInTheDocument();
    expect(screen.queryByTestId('assistant-proactive-bar')).not.toBeInTheDocument();

    await act(async () => {
      await Promise.resolve();
    });
    expect(mockedApprovals).not.toHaveBeenCalled();
    expect(mockedNotifications).not.toHaveBeenCalled();
  });

  it('设置面板：静默开关可切换、「恢复已忽略提示」清空记忆', async () => {
    useAssistantProactiveStore.setState({ dismissedScopes: ['page:ticket.detail'] });
    useAssistantStore.setState({ open: true });
    renderDock();

    fireEvent.click(screen.getByTestId('assistant-proactive-settings'));
    const silentSwitch = await screen.findByTestId('assistant-proactive-silent');
    fireEvent.click(within(silentSwitch).getByRole('switch'));
    expect(useAssistantProactiveStore.getState().silent).toBe(false);

    fireEvent.click(screen.getByTestId('assistant-proactive-restore'));
    expect(useAssistantProactiveStore.getState().dismissedScopes).toEqual([]);
  });
});