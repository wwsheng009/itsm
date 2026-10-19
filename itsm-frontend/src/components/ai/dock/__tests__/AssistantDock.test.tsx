/**
 * D0-02 AssistantDock 容器测试：权限门禁、FAB 开合、Ctrl/Cmd+I、ESC、懒挂载。
 */
import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AssistantDock, { ASSISTANT_FAB_BADGE_TESTID, ASSISTANT_FAB_RUNNING_TESTID } from '../AssistantDock';
import { mergeServerTask, useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { useAssistantProactiveStore } from '@/lib/store/assistant-proactive-store';
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';
import { usePermissions } from '@/lib/hooks/use-permissions';

// 需要真实路由上下文（D0-03 页面感知 + MemoryRouter）；
// jest.setup.js 全局 mock 了 useLocation → 按 setup 注释约定还原真实实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

jest.setTimeout(300000);

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
// I1-03：主动协助信号为只读聚合（无后端即可断言徽标/建议条逻辑；契约由 proactive 测试覆盖）。
jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetToolApprovals: jest.fn(async () => ({ items: [], state: 'pending' })),
}));
jest.mock('@/lib/api/ticket-notification-api', () => ({
  __esModule: true,
  TicketNotificationApi: {
    getUserNotifications: jest.fn(async () => ({
      notifications: [],
      total: 0,
      page: 1,
      size: 20,
    })),
  },
}));
// 懒挂载断言：面板未打开时不得挂载聊天视图（此处用 stub 替换，接口由内核测试覆盖）。
jest.mock('../AssistantChatPanel', () => ({
  __esModule: true,
  default: () => <div data-testid="assistant-chat-panel-stub" />,
}));

const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;

const grantPermission = (allowed: boolean) => {
  mockedPermissions.mockReturnValue({
    hasPermission: () => allowed,
  } as unknown as ReturnType<typeof usePermissions>);
};

// D0-03：容器依赖路由上下文（页面感知），统一以 /dashboard 为初始路由渲染。
const renderDock = (route = '/dashboard') =>
  render(
    <MemoryRouter initialEntries={[route]}>
      <AssistantDock />
    </MemoryRouter>
  );

beforeEach(() => {
  window.localStorage.clear();
  useAssistantStore.setState({ open: false });
  // I1-03：默认静默（用例自行覆盖）。
  useAssistantProactiveStore.setState({ enabled: true, silent: true, dismissedScopes: [] });
  // D1-02：L2 store 为模块级状态，用例间隔离。
  useAssistantContextStore.setState({ pageData: null, owner: null });
  // V2-02：异步精算任务表用例间隔离。
  useAssistantAsyncStore.getState().reset();
  grantPermission(true);
});

describe('AssistantDock（D0-02）', () => {
  it('无 ai:read 权限时不渲染（入口级门禁）', () => {
    grantPermission(false);
    renderDock();

    expect(screen.queryByTestId('assistant-dock')).not.toBeInTheDocument();
    expect(screen.queryByTestId('assistant-fab')).not.toBeInTheDocument();

    // 即使面板状态被置为展开，无权限仍不渲染
    act(() => {
      useAssistantStore.setState({ open: true });
    });
    expect(screen.queryByTestId('assistant-panel')).not.toBeInTheDocument();
  });

  it('FAB 打开面板并懒挂载聊天视图；关闭按钮收回', () => {
    renderDock();

    // 未展开：只有 FAB，聊天视图未挂载（懒挂载 → 未展开时零会话请求的前提）
    expect(screen.getByTestId('assistant-fab')).toBeInTheDocument();
    // D0-03：页面感知键随路由解析
    expect(screen.getByTestId('assistant-dock')).toHaveAttribute('data-page-key', 'dashboard');
    expect(screen.queryByTestId('assistant-panel')).not.toBeInTheDocument();
    expect(screen.queryByTestId('assistant-chat-panel-stub')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('assistant-fab'));
    expect(screen.getByTestId('assistant-panel')).toBeInTheDocument();
    // D0-05 上下文条：面板打开时展示当前页面标题（注册表声明）
    expect(screen.getByTestId('assistant-context-chip')).toHaveTextContent('仪表盘');
    expect(screen.getByTestId('assistant-chat-panel-stub')).toBeInTheDocument();
    expect(screen.queryByTestId('assistant-fab')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('assistant-close'));
    expect(screen.queryByTestId('assistant-panel')).not.toBeInTheDocument();
    expect(screen.getByTestId('assistant-fab')).toBeInTheDocument();
  });

  it('V1-02：移动端（<768px）为底部全宽抽屉，拉宽后恢复桌面形态', () => {
    const originalInnerWidth = window.innerWidth;
    try {
      window.innerWidth = 375;
      renderDock();
      fireEvent.click(screen.getByTestId('assistant-fab'));

      const panel = screen.getByTestId('assistant-panel');
      expect(panel).toHaveAttribute('data-mobile', 'true');
      expect(panel).toHaveStyle({ width: '100%' });

      // resize → 回桌面：右侧面板（宽度取 store），移动端标记复位。
      act(() => {
        window.innerWidth = 1280;
        window.dispatchEvent(new Event('resize'));
      });
      const desktopPanel = screen.getByTestId('assistant-panel');
      expect(desktopPanel).toHaveAttribute('data-mobile', 'false');
      expect(desktopPanel).not.toHaveStyle({ width: '100%' });
    } finally {
      window.innerWidth = originalInnerWidth;
    }
  });

  it('D1-02：详情页 L2 数据在上下文条展示具体对象（用户可见）', () => {
    useAssistantContextStore.setState({
      pageData: { targetType: 'ticket', targetId: 42, summary: '无法登录' },
      owner: Symbol('test'),
    });
    renderDock('/tickets/42');

    act(() => {
      useAssistantStore.setState({ open: true });
    });

    const chip = screen.getByTestId('assistant-context-chip');
    expect(chip).toHaveTextContent('工单详情 · 工单 #42');
    expect(chip).toHaveAttribute('title', '无法登录');
  });

  it('Ctrl+I 唤起/收起，ESC 收起', () => {
    renderDock('/tickets/42');
    expect(screen.getByTestId('assistant-dock')).toHaveAttribute('data-page-key', 'ticket.detail');

    fireEvent.keyDown(document, { key: 'i', code: 'KeyI', ctrlKey: true });
    expect(screen.getByTestId('assistant-panel')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape', code: 'Escape' });
    expect(screen.queryByTestId('assistant-panel')).not.toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'i', code: 'KeyI', ctrlKey: true });
    expect(screen.getByTestId('assistant-panel')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'i', code: 'KeyI', ctrlKey: true });
    expect(screen.queryByTestId('assistant-panel')).not.toBeInTheDocument();
  });

  it('V2-02：后台精算运行中显示 FAB 状态点；收口未读并入徽标，已读后消除', () => {
    useAssistantAsyncStore
      .getState()
      .start(
        mergeServerTask(undefined, {
          taskId: 'aa_dock',
          status: 'running',
          prompt: '连不上网怎么办',
          createdAt: '2026-10-18T10:00:00.000Z',
          fast: { source: 'help_index', items: [{ title: '网络故障排查', snippet: '检查 DNS' }] },
        })
      );
    renderDock();
    // 运行中：FAB 呼吸点（未收口不计入徽标）。
    expect(screen.getByTestId(ASSISTANT_FAB_RUNNING_TESTID)).toBeInTheDocument();
    expect(screen.queryByTestId(ASSISTANT_FAB_BADGE_TESTID)).not.toBeInTheDocument();

    // 收口：运行点消失，未读回执并入 FAB 徽标（非打断，点击 FAB 查看）。
    act(() => {
      useAssistantAsyncStore.getState().settle({
        taskId: 'aa_dock',
        status: 'done',
        prompt: '连不上网怎么办',
        createdAt: '2026-10-18T10:00:00.000Z',
        finishedAt: '2026-10-18T10:00:30.000Z',
        fast: { source: 'help_index', items: [{ title: '网络故障排查', snippet: '检查 DNS' }] },
        refined: { model: 'gpt-4o', answer: '完整回答' },
      });
    });
    expect(screen.queryByTestId(ASSISTANT_FAB_RUNNING_TESTID)).not.toBeInTheDocument();
    expect(screen.getByTestId(ASSISTANT_FAB_BADGE_TESTID)).toHaveTextContent('1');

    // 面板打开阅读回执（真实面板卡片自动置已读；此处验证同一数据面驱动徽标清空）。
    fireEvent.click(screen.getByTestId('assistant-fab'));
    expect(screen.getByTestId('assistant-panel')).toBeInTheDocument();
    act(() => {
      useAssistantAsyncStore.getState().markAllSeen();
    });
    expect(screen.queryByTestId(ASSISTANT_FAB_BADGE_TESTID)).not.toBeInTheDocument();
  });
});
