/**
 * O2-02 补全：`/ai/chat` 工作区诊断入口测试。
 *
 * 口径：
 * - 入口与 Dock 面板共用 `AssistantDiagnosticsEntry`（同一实现，独立 testid 避免同页歧义）；
 * - 会话数据（messages/convId/streaming）由工作区会话内核透传：本用例以 mock 会话验证「入口打开
 *   抽屉、抽屉读到工作区会话 ID、空会话不发轨迹请求」；
 * - 关闭态零请求：抽屉懒加载轨迹。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AIChat from '@/components/ai/AIChat';
import { AI_CHAT_DIAGNOSTICS_OPEN_TESTID } from '@/components/ai/diagnostics/AssistantDiagnosticsEntry';
import { DIAGNOSTICS_DRAWER_TESTID } from '@/components/ai/diagnostics/DiagnosticsDrawer';
import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import { aiGetAssistantContext, aiGetConversationTrace, aiGetRunTrace } from '@/lib/api/ai-api';

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
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedSession = useAIChatSession as jest.MockedFunction<typeof useAIChatSession>;
const mockedTrace = aiGetRunTrace as jest.Mock;
const mockedConversationTrace = aiGetConversationTrace as jest.Mock;

/** 工作区会话内核桩：空会话 + 已选会话 ID（覆盖入口透传的关键字段）。 */
function sessionFixture(over: Record<string, unknown> = {}) {
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
  mockedTrace.mockResolvedValue({
    run: {
      id: 7,
      conversationId: 9,
      botId: 0,
      entrypoint: 'chat',
      status: 'completed',
      startedAt: '2026-10-18T10:00:00Z',
      finishedAt: '2026-10-18T10:00:02Z',
    },
    steps: [],
    events: [],
    toolInvocations: [],
    artifacts: [],
  });
  mockedConversationTrace.mockResolvedValue({
    conversation: { id: 9, userId: 7, botId: 0, title: '', createdAt: '2026-10-18T10:00:00Z' },
    messages: [],
    messageCount: 0,
    messagesTruncated: false,
    runs: [],
    toolInvocations: [],
    artifacts: [],
  });
});

describe('AIChat 工作区诊断入口（O2-02 补全）', () => {
  it('工具条渲染诊断入口；点击打开抽屉并读到工作区会话 ID；关闭态零轨迹请求', async () => {
    renderPage();

    const entry = await screen.findByTestId(AI_CHAT_DIAGNOSTICS_OPEN_TESTID);
    expect(entry).toHaveAttribute('aria-label', '会话诊断');
    // 关闭态：抽屉未展开、零轨迹请求。
    expect(screen.queryByTestId(DIAGNOSTICS_DRAWER_TESTID)).toBeNull();
    expect(mockedTrace).not.toHaveBeenCalled();

    fireEvent.click(entry);
    expect(await screen.findByTestId(DIAGNOSTICS_DRAWER_TESTID)).toBeInTheDocument();
    // 抽屉读到工作区会话数据（本桩为空会话 + convId=9 → 无运行记录、回退会话维度轨迹）。
    const summary = await screen.findByTestId('diagnostics-summary');
    expect(summary).toHaveTextContent('会话');
    expect(summary).toHaveTextContent('#9');
    // O1-01 条件项：纯问答（无 run）改拉会话维度轨迹；run 维度请求保持零。
    expect(mockedTrace).not.toHaveBeenCalled();
    await waitFor(() => expect(mockedConversationTrace).toHaveBeenCalledWith(9));
    expect(await screen.findByTestId('conversation-trace-view')).toBeInTheDocument();
  });

  it('非空会话透传 messages（有最近运行才按需加载轨迹）', async () => {
    mockedSession.mockReturnValue(
      sessionFixture({
        isEmpty: false,
        messages: [
          {
            id: 'm1',
            role: 'assistant',
            content: '完成',
            createdAt: '2026-10-18T10:00:02Z',
            run: { runId: 7, status: 'done', stepCount: 1 },
            steps: [{ runId: 7, stepIndex: 0, type: 'llm', durationMs: 500 }],
            usage: { tokenIn: 120, tokenOut: 80, estimated: true },
          },
        ],
      }) as unknown as ReturnType<typeof useAIChatSession>
    );

    renderPage();
    fireEvent.click(await screen.findByTestId(AI_CHAT_DIAGNOSTICS_OPEN_TESTID));

    expect(await screen.findByTestId(DIAGNOSTICS_DRAWER_TESTID)).toBeInTheDocument();
    // 有最近运行 → 打开即按需拉取轨迹（O1-01 复用）。
    await waitFor(() => expect(mockedTrace).toHaveBeenCalledWith(7));
    // token 段与工作区消息同源（O2-02 token 可见）。
    expect(await screen.findByTestId('diagnostics-usage')).toHaveTextContent('120');
  });
});