/**
 * `/ai/chat` 工作区响应式布局测试（2026-10-20 布局优化）。
 *
 * 锁定的产品契约：
 * 1. 窄屏（<1100px）侧栏默认收起，不挤压消息区；
 * 2. 窄屏下侧栏展开时浮在主区之上（absolute），主区宽度不因展开而缩小；
 * 3. 窄屏抽屉支持点击外部 / Esc 收起；
 * 4. 窄屏（<900px）头部 Bot/Provider 收进「模型与能力」弹层，工具条不溢出；
 * 5. 宽屏（≥1100px）行为不变：侧栏并排、选择器直接可见。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AIChat from '@/components/ai/AIChat';
import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import { aiGetAssistantContext } from '@/lib/api/ai-api';

jest.setTimeout(300000);

jest.mock('@/components/ai/chat-core/use-ai-chat-session', () => ({
  __esModule: true,
  useAIChatSession: jest.fn(),
}));
jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  AIApi: { listVisibleBotsWithCapabilities: jest.fn() },
  aiChatStream: jest.fn(),
  getConversationMessages: jest.fn(),
  listConversations: jest.fn(),
  deleteConversation: jest.fn(),
  exportConversation: jest.fn(),
  aiGetPageHelp: jest.fn(),
  aiSearchHelp: jest.fn(),
  aiGetAssistantContext: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(() => ({ hasPermission: () => true })),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  isAssistantEventName: () => true,
  hashTelemetryText: (v: string) => `h-${v.length}`,
  getTelemetryMode: () => 'off',
  setTelemetryMode: jest.fn(),
}));
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedSession = useAIChatSession as jest.MockedFunction<typeof useAIChatSession>;

function sessionFixture(over: Record<string, unknown> = {}) {
  return {
    messages: [],
    convId: null,
    streaming: false,
    query: '',
    setQuery: jest.fn(),
    providerNotice: null,
    setProviderNotice: jest.fn(),
    conversations: [],
    loadingConvs: false,
    sidebarOpen: true,
    setSidebarOpen: jest.fn(),
    activeScope: null,
    setScopeDismissed: jest.fn(),
    bots: [],
    botCapabilities: undefined,
    selectedBotId: null,
    setSelectedBotId: jest.fn(),
    feature: { enabled: false, providers: [], preference: null },
    selectedProvider: undefined,
    setSelectedProvider: jest.fn(),
    canSwitchProvider: false,
    canManageProviderPreference: false,
    providerLabel: (v: string) => v,
    handleSetPersonalDefault: jest.fn(),
    handleClearPersonalDefault: jest.fn(),
    canPromoteKnowledge: false,
    canDeleteConversation: false,
    scrollRef: { current: null },
    isEmpty: true,
    currentConvTitle: null,
    chatCss: '',
    formatTime: (v: string) => v,
    handleSend: jest.fn(),
    handleStop: jest.fn(),
    handleClear: jest.fn(),
    handleKeyDown: jest.fn(),
    handleScroll: jest.fn(),
    switchToConversation: jest.fn(),
    startNewConversation: jest.fn(),
    handleDeleteConversation: jest.fn(),
    handleExportConversation: jest.fn(),
    handlePromoteToArticle: jest.fn(),
    handleSaveConversationAsArticle: jest.fn(),
    handleOpenApproval: jest.fn(),
    handleRequestConfirm: jest.fn(),
    openOutcome: jest.fn(),
    ...over,
  };
}

/** 控制 matchMedia 的返回值（按查询串匹配）。 */
const installMatchMedia = (matches: Record<string, boolean>) => {
  window.matchMedia = jest.fn().mockImplementation((query: string) => ({
    matches: Boolean(matches[query]),
    media: query,
    onchange: null,
    addListener: jest.fn(),
    removeListener: jest.fn(),
    addEventListener: jest.fn(),
    removeEventListener: jest.fn(),
    dispatchEvent: jest.fn(),
  })) as unknown as typeof window.matchMedia;
};

const renderPage = () =>
  render(
    <MemoryRouter initialEntries={['/ai/chat']}>
      <AIChat />
    </MemoryRouter>
  );

beforeEach(() => {
  jest.clearAllMocks();
  mockedSession.mockReturnValue(
    sessionFixture() as unknown as ReturnType<typeof useAIChatSession>
  );
  (aiGetAssistantContext as jest.Mock).mockResolvedValue({
    pageKey: 'ai.chat',
    known: true,
    suggestions: [],
  });
});

describe('/ai/chat 响应式布局', () => {
  it('窄屏（<1100px）：挂载后自动收起侧栏，避免挤压消息区', async () => {
    installMatchMedia({
      '(max-width: 1099px)': true,
      '(max-width: 899px)': false,
    });
    const setSidebarOpen = jest.fn();
    mockedSession.mockReturnValue(
      sessionFixture({ sidebarOpen: true, setSidebarOpen }) as unknown as ReturnType<
        typeof useAIChatSession
      >
    );

    renderPage();
    await waitFor(() => expect(setSidebarOpen).toHaveBeenCalledWith(false));
  });

  it('宽屏（≥1100px）：不强制收起侧栏', async () => {
    installMatchMedia({
      '(max-width: 1099px)': false,
      '(max-width: 899px)': false,
    });
    const setSidebarOpen = jest.fn();
    mockedSession.mockReturnValue(
      sessionFixture({ sidebarOpen: false, setSidebarOpen }) as unknown as ReturnType<
        typeof useAIChatSession
      >
    );

    renderPage();
    await waitFor(() => expect(setSidebarOpen).toHaveBeenCalledWith(true));
  });

  it('窄屏且侧栏展开：侧栏浮在主区之上（absolute），主区宽度不受影响', async () => {
    installMatchMedia({
      '(max-width: 1099px)': true,
      '(max-width: 899px)': false,
    });
    mockedSession.mockReturnValue(
      sessionFixture({ sidebarOpen: true }) as unknown as ReturnType<typeof useAIChatSession>
    );

    const { container } = renderPage();
    const aside = container.querySelector('aside') as HTMLElement;
    expect(aside).toBeTruthy();
    expect(aside.style.position).toBe('absolute');
    expect(aside.style.zIndex).toBe('30');
  });

  it('窄屏抽屉：点击浮层外部收起', async () => {
    installMatchMedia({
      '(max-width: 1099px)': true,
      '(max-width: 899px)': false,
    });
    const setSidebarOpen = jest.fn();
    mockedSession.mockReturnValue(
      sessionFixture({ sidebarOpen: true, setSidebarOpen }) as unknown as ReturnType<
        typeof useAIChatSession
      >
    );

    renderPage();
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(setSidebarOpen).toHaveBeenCalledWith(false));
  });

  it('窄屏抽屉：Esc 收起', async () => {
    installMatchMedia({
      '(max-width: 1099px)': true,
      '(max-width: 899px)': false,
    });
    const setSidebarOpen = jest.fn();
    mockedSession.mockReturnValue(
      sessionFixture({ sidebarOpen: true, setSidebarOpen }) as unknown as ReturnType<
        typeof useAIChatSession
      >
    );

    renderPage();
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => expect(setSidebarOpen).toHaveBeenCalledWith(false));
  });

  it('窄屏（<900px）：Bot/Provider 收进「模型与能力」弹层，工具条不直接渲染选择器', async () => {
    installMatchMedia({
      '(max-width: 1099px)': true,
      '(max-width: 899px)': true,
    });
    mockedSession.mockReturnValue(
      sessionFixture({
        sidebarOpen: false,
        bots: [{ id: 1, name: '默认助手', enabled: true }],
        canSwitchProvider: true,
        feature: {
          enabled: true,
          providers: [{ key: 'p1', displayName: 'Provider 1', implemented: true }],
          preference: { effectiveProviderKey: 'p1' },
        },
      }) as unknown as ReturnType<typeof useAIChatSession>
    );

    renderPage();
    // 折叠入口存在
    expect(await screen.findByTestId('ai-chat-model-settings-open')).toBeInTheDocument();
    // 头部不再直接渲染 Bot 选择器 / Provider Select
    expect(screen.queryByTestId('bot-selector')).not.toBeInTheDocument();

    // 打开弹层后两者可访问
    fireEvent.click(screen.getByTestId('ai-chat-model-settings-open'));
    expect(await screen.findByTestId('ai-chat-model-settings')).toBeInTheDocument();
    expect(await screen.findByTestId('bot-selector')).toBeInTheDocument();
  });

  it('宽屏：Bot 选择器直接可见，无「模型与能力」折叠入口', async () => {
    installMatchMedia({
      '(max-width: 1099px)': false,
      '(max-width: 899px)': false,
    });
    mockedSession.mockReturnValue(
      sessionFixture({
        bots: [{ id: 1, name: '默认助手', enabled: true }],
      }) as unknown as ReturnType<typeof useAIChatSession>
    );

    renderPage();
    expect(await screen.findByTestId('bot-selector')).toBeInTheDocument();
    expect(screen.queryByTestId('ai-chat-model-settings-open')).not.toBeInTheDocument();
  });
});
