/**
 * T5-08：流式内容 `aria-live` 行为验收（不限于 axe 静态扫描）。
 *
 * 口径（方案 §14.2 T5-08；§3.7「流式内容 aria-live」；评审 §12-12 缺口：消息区无 role="log"/aria-live）：
 * - 消息区为持久 live region：`role="log"` + `aria-live="polite"`（新消息加入即被礼貌播报）；
 * - 流式期间 `aria-busy="true"`（抑制逐字/逐块播报，避免刷屏）；生成结束复位为 `false`，
 *   由读屏一次性读出完整回答；
 * - 双表面一致：工作区（`AIChat`）与 Dock 面板（`AssistantChatPanel`）同语义。
 *
 * 说明：会话内核「流式期间 streaming 置位/结束后复位」由 use-ai-chat-session 内核测试覆盖；
 * 本文件以会话桩驱动组件，断言 DOM live region 的状态机行为（busy 抑制 → 复位可读）。
 */
import React from 'react';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AIChat from '@/components/ai/AIChat';
import AssistantChatPanel from '@/components/ai/dock/AssistantChatPanel';
import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { aiGetAssistantContext } from '@/lib/api/ai-api';
import { useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';
import { useAssistantMemoryStore } from '@/lib/ai/memory/assistant-memory-store';
import type { ChatMessage } from '@/components/ai/chat-core/types';

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

const userMsg: ChatMessage = {
  id: 'u1',
  role: 'user',
  content: 'VPN 连不上怎么办？',
  createdAt: '2026-10-19T02:00:00Z',
  toolEvents: [],
};
const streamingMsg: ChatMessage = {
  id: 'a1',
  role: 'assistant',
  content: '第一段（生成中）',
  createdAt: '2026-10-19T02:00:01Z',
  streaming: true,
  toolEvents: [],
};
const doneMsg: ChatMessage = {
  id: 'a1',
  role: 'assistant',
  content: '最终回答：先检查网络，再重试。',
  createdAt: '2026-10-19T02:00:02Z',
  streaming: false,
  toolEvents: [],
};

/** 工作区会话桩（字段面同 AIChat.diagnostics.test.tsx；消息/流式可控）。 */
function workspaceSession(over: Record<string, unknown> = {}) {
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
function panelSession(over: Record<string, unknown> = {}) {
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
  mockedPermissions.mockReturnValue({
    hasPermission: () => true,
  } as unknown as ReturnType<typeof usePermissions>);
  (aiGetAssistantContext as jest.Mock).mockResolvedValue({
    pageKey: 'ai.chat',
    known: false,
    suggestions: [],
  });
});

describe('T5-08 流式内容 aria-live 行为', () => {
  it('工作区：空会话也持久持有一个 log（live region 不随消息出现/消失）', () => {
    mockedSession.mockReturnValue(
      workspaceSession() as unknown as ReturnType<typeof useAIChatSession>
    );
    renderWorkspace();

    const log = screen.getByRole('log');
    expect(log).toHaveAttribute('aria-live', 'polite');
    expect(log).toHaveAttribute('aria-busy', 'false');
  });

  it('工作区：流式期间 aria-busy=true，完成后复位 false 且回答在 log 内', () => {
    mockedSession.mockReturnValue(
      workspaceSession({
        messages: [userMsg, streamingMsg],
        streaming: true,
        isEmpty: false,
      }) as unknown as ReturnType<typeof useAIChatSession>
    );
    const view = renderWorkspace();

    const streamingLog = screen.getByRole('log');
    expect(streamingLog).toHaveAttribute('aria-busy', 'true');
    expect(within(streamingLog).getByText('第一段（生成中）')).toBeInTheDocument();

    mockedSession.mockReturnValue(
      workspaceSession({
        messages: [userMsg, doneMsg],
        streaming: false,
        isEmpty: false,
      }) as unknown as ReturnType<typeof useAIChatSession>
    );
    view.rerender(
      <MemoryRouter initialEntries={['/ai/chat']}>
        <AIChat />
      </MemoryRouter>
    );

    const settledLog = screen.getByRole('log');
    expect(settledLog).toHaveAttribute('aria-busy', 'false');
    expect(within(settledLog).getByText('最终回答：先检查网络，再重试。')).toBeInTheDocument();
  });

  it('Dock 面板：同语义（流式 busy → 完成复位，回答在 log 内）', () => {
    mockedSession.mockReturnValue(
      panelSession({
        messages: [userMsg, streamingMsg],
        streaming: true,
        isEmpty: false,
      }) as unknown as ReturnType<typeof useAIChatSession>
    );
    const view = renderPanel();

    const streamingLog = screen.getByRole('log');
    expect(streamingLog).toHaveAttribute('aria-live', 'polite');
    expect(streamingLog).toHaveAttribute('aria-busy', 'true');
    expect(within(streamingLog).getByText('第一段（生成中）')).toBeInTheDocument();

    mockedSession.mockReturnValue(
      panelSession({
        messages: [userMsg, doneMsg],
        streaming: false,
        isEmpty: false,
      }) as unknown as ReturnType<typeof useAIChatSession>
    );
    view.rerender(
      <MemoryRouter>
        <AssistantChatPanel />
      </MemoryRouter>
    );

    const settledLog = screen.getByRole('log');
    expect(settledLog).toHaveAttribute('aria-busy', 'false');
    expect(within(settledLog).getByText('最终回答：先检查网络，再重试。')).toBeInTheDocument();
  });
});
