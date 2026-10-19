/**
 * T5-02 交互测试：输入坞 `/` 快捷指令 + `@` 当前页对象引用（双表面）。
 *
 * 口径（方案 §3.1「输入坞（/ 快捷指令、@ 引用当前页对象）」；T5-02 验收「解析 + 交互测试」；
 * 解析纯函数单测见 chat-core/__tests__/input-dock-commands.test.ts）：
 * - `/` 打开指令菜单：过滤、↑/↓ 高亮、Enter 选择（只填不发，不触发会话 Enter 发送路径）；
 * - `@` 列出 L2 当前页对象（store 注册），点击插入 `@类型#编号`；无对象时给出提示（非静默）；
 * - Esc 关闭后按键交还会话内核（Enter 恢复发送路径）；双表面（工作区 / Dock 面板）同语义。
 *
 * 说明：会话桩里 `query/setQuery` 走真实 React state（否则输入变化不可回放）；
 * `handleKeyDown` 为会话发送路径的哨兵（真实内核中 Enter 由此触发发送）。
 */
import React, { act } from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AIChat from '@/components/ai/AIChat';
import AssistantChatPanel from '@/components/ai/dock/AssistantChatPanel';
import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import {
  INPUT_DOCK_ASSIST_HINT_TESTID,
  INPUT_DOCK_ASSIST_ITEM_TESTID,
  INPUT_DOCK_ASSIST_MENU_TESTID,
} from '@/components/ai/chat-core/InputDockAssistMenu';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { aiGetAssistantContext } from '@/lib/api/ai-api';
import { useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';
import { useAssistantMemoryStore } from '@/lib/ai/memory/assistant-memory-store';
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';

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
  aiGetRunTrace: jest.fn(),
  aiGetConversationTrace: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(() => ({ hasPermission: () => true })),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  isAssistantEventName: () => true,
  getTelemetryMode: () => 'off',
  setTelemetryMode: jest.fn(),
}));
// D0-04/D0-05：路由上下文走真实实现（jest.setup.js 全局 mock 了 useLocation）。
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedSession = useAIChatSession as jest.MockedFunction<typeof useAIChatSession>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;

/** 会话发送路径哨兵（菜单开启时不应被调用）。 */
let sessionKeyDown: jest.Mock;

/** 工作区会话桩（字段面同 streaming-a11y-behavior.test.tsx）。 */
function baseWorkspace(over: Record<string, unknown> = {}) {
  return {
    messages: [],
    convId: 9,
    streaming: false,
    query: '',
    setQuery: jest.fn(),
    providerNotice: null,
    setProviderNotice: jest.fn(),
    conversations: [],
    loadingConvs: false,
    sidebarOpen: false,
    setSidebarOpen: jest.fn(),
    activeScope: null,
    setScopeDismissed: jest.fn(),
    bots: [],
    botCapabilities: null,
    selectedBotId: null,
    setSelectedBotId: jest.fn(),
    feature: { enabled: false, ready: true, providers: [], preference: null },
    selectedProvider: undefined,
    setSelectedProvider: jest.fn(),
    canSwitchProvider: false,
    canManageProviderPreference: false,
    providerLabel: (key: string) => key,
    handleSetPersonalDefault: jest.fn(),
    handleClearPersonalDefault: jest.fn(),
    canPromoteKnowledge: false,
    canDeleteConversation: false,
    scrollRef: { current: null },
    isEmpty: true,
    currentConvTitle: null,
    chatCss: '',
    formatTime: (value: string) => value,
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

/** Dock 面板会话桩（字段面同 AssistantChatPanel 解构）。 */
function basePanel(over: Record<string, unknown> = {}) {
  return {
    messages: [],
    convId: undefined,
    streaming: false,
    conversations: [],
    loadingConvs: false,
    currentConvTitle: null,
    formatTime: (value: string) => value,
    switchToConversation: jest.fn(),
    startNewConversation: jest.fn(),
    canDeleteConversation: false,
    handleDeleteConversation: jest.fn(),
    handleExportConversation: jest.fn(),
    query: '',
    setQuery: jest.fn(),
    scrollRef: { current: null },
    isEmpty: true,
    chatCss: '',
    providerLabel: (key: string) => key,
    providerNotice: null,
    setProviderNotice: jest.fn(),
    handleSend: jest.fn(),
    handleStop: jest.fn(),
    handleKeyDown: jest.fn(),
    handlePromoteToArticle: jest.fn(),
    handleOpenApproval: jest.fn(),
    handleRequestConfirm: jest.fn(),
    openOutcome: jest.fn(),
    effectiveScope: null,
    ...over,
  };
}

/** 装配带真实输入状态的会话桩：query/setQuery 走 React.useState，保证输入交互可回放。 */
function installSession(base: () => Record<string, unknown>) {
  mockedSession.mockImplementation(() => {
    const [query, setQuery] = React.useState('');
    return {
      ...base(),
      query,
      setQuery,
      handleKeyDown: sessionKeyDown,
    } as unknown as ReturnType<typeof useAIChatSession>;
  });
}

const renderWorkspace = () =>
  render(
    <MemoryRouter initialEntries={['/ai/chat']}>
      <AIChat />
    </MemoryRouter>
  );

const renderPanel = () =>
  render(
    <MemoryRouter>
      <AssistantChatPanel />
    </MemoryRouter>
  );

beforeEach(() => {
  jest.clearAllMocks();
  window.localStorage.clear();
  useAssistantAsyncStore.getState().reset();
  useAssistantMemoryStore.getState().reset();
  useAssistantContextStore.setState({ pageData: null, owner: null });
  sessionKeyDown = jest.fn();
  mockedPermissions.mockReturnValue({
    hasPermission: () => true,
  } as unknown as ReturnType<typeof usePermissions>);
  (aiGetAssistantContext as jest.Mock).mockResolvedValue({
    pageKey: 'ai.chat',
    known: false,
    suggestions: [],
  });
});

describe('T5-02 输入坞 `/` 快捷指令 + `@` 引用（双表面交互）', () => {
  it('工作区：`/` 打开指令菜单，过滤后 Enter 选择只填不发', () => {
    installSession(baseWorkspace);
    renderWorkspace();
    const input = screen.getByTestId('assistant-chat-input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/' } });
    const menu = screen.getByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash);
    expect(within(menu).getAllByTestId(INPUT_DOCK_ASSIST_ITEM_TESTID)).toHaveLength(3);

    fireEvent.change(input, { target: { value: '/SL' } });
    const filtered = within(menu).getAllByTestId(INPUT_DOCK_ASSIST_ITEM_TESTID);
    expect(filtered).toHaveLength(1);
    expect(filtered[0]).toHaveTextContent('/SLA');

    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', keyCode: 13 });
    expect(input.value).toBe('当前 SLA 达成情况如何？');
    expect(screen.queryByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash)).not.toBeInTheDocument();
    // 菜单开启期间 Enter 被选择动作消费，未进入会话发送路径。
    expect(sessionKeyDown).not.toHaveBeenCalled();
  });

  it('工作区：↑/↓ 高亮移动、无匹配提示、Esc 关闭后 Enter 交还会话路径', () => {
    installSession(baseWorkspace);
    renderWorkspace();
    const input = screen.getByTestId('assistant-chat-input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/' } });
    const menu = screen.getByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash);
    expect(within(menu).getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(input, { key: 'ArrowDown', code: 'ArrowDown', keyCode: 40 });
    expect(within(menu).getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true');
    expect(sessionKeyDown).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: '/zzz' } });
    expect(screen.getByTestId(INPUT_DOCK_ASSIST_HINT_TESTID)).toHaveTextContent('没有匹配的指令');

    fireEvent.keyDown(input, { key: 'Escape', code: 'Escape', keyCode: 27 });
    expect(screen.queryByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash)).not.toBeInTheDocument();
    expect(sessionKeyDown).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', keyCode: 13 });
    expect(sessionKeyDown).toHaveBeenCalledTimes(1);
  });

  it('工作区：`@` 无对象给出提示；注册 L2 对象后可过滤并插入引用', () => {
    installSession(baseWorkspace);
    renderWorkspace();
    const input = screen.getByTestId('assistant-chat-input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '@' } });
    expect(screen.getByTestId(INPUT_DOCK_ASSIST_HINT_TESTID)).toHaveTextContent(
      '当前页面暂无可引用的对象'
    );
    expect(screen.queryAllByTestId(INPUT_DOCK_ASSIST_ITEM_TESTID)).toHaveLength(0);

    const owner = Symbol('t5-02-test');
    act(() => {
      useAssistantContextStore.getState().registerPageData(owner, {
        targetType: 'ticket',
        targetId: 42,
        summary: '打印机无法连接',
      });
    });

    fireEvent.change(input, { target: { value: '你好 @打' } });
    const menu = screen.getByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.reference);
    const items = within(menu).getAllByTestId(INPUT_DOCK_ASSIST_ITEM_TESTID);
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveTextContent('@工单#42');
    expect(items[0]).toHaveTextContent('打印机无法连接');

    fireEvent.click(items[0]);
    expect(input.value).toBe('你好 @工单#42 ');
    expect(screen.queryByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.reference)).not.toBeInTheDocument();

    act(() => {
      useAssistantContextStore.getState().clearPageData(owner);
    });
  });

  it('Dock 面板：`/` 点击插入脚手架（只填不发）；`@` 无对象提示', () => {
    installSession(basePanel);
    renderPanel();
    const input = screen.getByTestId('assistant-panel-input') as HTMLTextAreaElement;

    fireEvent.change(input, { target: { value: '/' } });
    const menu = screen.getByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash);
    const items = within(menu).getAllByTestId(INPUT_DOCK_ASSIST_ITEM_TESTID);
    expect(items).toHaveLength(3);
    fireEvent.click(items[2]);
    expect(input.value).toBe('帮我起草一段客户沟通话术');
    expect(screen.queryByTestId(INPUT_DOCK_ASSIST_MENU_TESTID.slash)).not.toBeInTheDocument();

    fireEvent.change(input, { target: { value: '@' } });
    expect(screen.getByTestId(INPUT_DOCK_ASSIST_HINT_TESTID)).toHaveTextContent(
      '当前页面暂无可引用的对象'
    );
  });
});
