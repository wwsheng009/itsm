/**
 * D0-01 chat-core 会话内核冒烟测试。
 *
 * 目的：抽取后行为等价——挂载加载会话列表；发送消息走 SSE 回调增量更新；
 * done 回填会话 id；流式期间 streaming 置位并在结束后复位；清空对话复位状态。
 */
import React from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import { useAIChatSession } from '../use-ai-chat-session';
import {
  AIApi,
  aiChatStream,
  exportConversation,
  getConversationMessages,
  listConversations,
} from '@/lib/api/ai-api';
import { useLLMProviderFeature } from '@/lib/hooks/use-llm-provider-feature';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { downloadTextFile } from '@/lib/utils/download-text-file';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  AIApi: { listVisibleBotsWithCapabilities: jest.fn() },
  aiChatStream: jest.fn(),
  getConversationMessages: jest.fn(),
  listConversations: jest.fn(),
  deleteConversation: jest.fn(),
  exportConversation: jest.fn(),
}));
jest.mock('@/lib/hooks/use-llm-provider-feature', () => ({
  __esModule: true,
  useLLMProviderFeature: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
// 埋点单例会尝试真实网络（best-effort）；测试只关心内核行为，整体替换为 noop。
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  hashTelemetryText: (text: string) => `h-${text.length}`,
  isAssistantEventName: () => true,
  getTelemetryMode: () => 'off',
  setTelemetryMode: jest.fn(),
}));
// I2-02：下载链路（Blob/anchor）由 utils 单测覆盖；内核测试只断言「导出→落盘」调用。
jest.mock('@/lib/utils/download-text-file', () => ({
  __esModule: true,
  downloadTextFile: jest.fn(() => true),
}));

// D0-04：需要真实路由上下文验证 pageContext；jest.setup.js 全局 mock 了 useLocation → 还原真实实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedBots = AIApi as unknown as { listVisibleBotsWithCapabilities: jest.Mock };
const mockedStream = aiChatStream as jest.MockedFunction<typeof aiChatStream>;
const mockedGetMessages = getConversationMessages as jest.MockedFunction<typeof getConversationMessages>;
const mockedListConversations = listConversations as jest.MockedFunction<typeof listConversations>;
const mockedExport = exportConversation as jest.MockedFunction<typeof exportConversation>;
const mockedProviderFeature = useLLMProviderFeature as jest.MockedFunction<
  typeof useLLMProviderFeature
>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <MemoryRouter>{children}</MemoryRouter>
);

beforeEach(() => {
  mockedBots.listVisibleBotsWithCapabilities.mockResolvedValue({ items: [], capabilities: null });
  mockedListConversations.mockResolvedValue([]);
  mockedProviderFeature.mockReturnValue({
    enabled: false,
    ready: true,
    providers: [],
    preference: null,
    refresh: jest.fn(),
    setPreference: jest.fn(),
    loading: false,
    error: null,
  } as unknown as ReturnType<typeof useLLMProviderFeature>);
  mockedPermissions.mockReturnValue({
    hasPermission: () => false,
  } as unknown as ReturnType<typeof usePermissions>);
  mockedStream.mockReset();
  mockedTrack.mockReset();
  mockedGetMessages.mockReset();
  useAssistantStore.setState({ handoffConversationId: undefined });
});

afterEach(() => {
  // D1-02：L2 store 为模块级状态，用例间隔离。
  useAssistantContextStore.setState({ pageData: null, owner: null });
});

describe('useAIChatSession（D0-01 抽离后的会话内核）', () => {
  it('挂载即加载会话列表（空态、非流式）', async () => {
    const { result } = renderHook(() => useAIChatSession(), { wrapper });

    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());
    expect(result.current.messages).toEqual([]);
    expect(result.current.streaming).toBe(false);
    expect(result.current.convId).toBeUndefined();
  });

  it('发送消息：SSE delta 增量渲染、done 回填会话 id', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('wor');
      callbacks?.onDelta?.('ld');
      callbacks?.onDone?.(99, { provider: 'p1', providerSource: 'tenant' });
      return 99;
    });

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hello');
    });

    await waitFor(() => expect(result.current.messages).toHaveLength(2));
    expect(result.current.messages[0]).toMatchObject({ role: 'user', content: 'hello' });
    expect(result.current.messages[1]).toMatchObject({
      role: 'assistant',
      content: 'world',
      streaming: false,
    });
    expect(result.current.convId).toBe(99);
    expect(mockedStream).toHaveBeenCalledWith(
      expect.objectContaining({ query: 'hello' }),
      expect.any(Object)
    );
  });

  it('P3：工作区实例消费交接会话并加载历史（一次性）', async () => {
    mockedGetMessages.mockResolvedValue([
      {
        id: 1,
        conversationId: 77,
        role: 'user',
        content: '我之前问了什么？',
        createdAt: '2026-10-19T01:00:00Z',
      },
      {
        id: 2,
        conversationId: 77,
        role: 'assistant',
        content: JSON.stringify({ answer: '你问了会话连续性。', sources: [] }),
        createdAt: '2026-10-19T01:00:01Z',
      },
    ]);
    useAssistantStore.getState().setHandoffConversationId(77);

    const { result } = renderHook(
      () => useAIChatSession({ consumeWorkspaceHandoff: true }),
      { wrapper }
    );

    await waitFor(() => expect(result.current.convId).toBe(77));
    expect(result.current.messages).toHaveLength(2);
    expect(result.current.messages[0]).toMatchObject({ role: 'user', content: '我之前问了什么？' });
    // assistant 信封按既有规则解包成自然语言。
    expect(result.current.messages[1]).toMatchObject({
      role: 'assistant',
      content: '你问了会话连续性。',
      conversationId: 77,
      queryHash: 'h-8',
    });
    expect(mockedGetMessages).toHaveBeenCalledWith(77);
    // 一次性：消费后 store 立即清空。
    expect(useAssistantStore.getState().handoffConversationId).toBeUndefined();
  });

  it('P3：Dock 实例（默认）不消费交接，避免截胡工作区会话', async () => {
    useAssistantStore.getState().setHandoffConversationId(88);

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    expect(result.current.convId).toBeUndefined();
    expect(mockedGetMessages).not.toHaveBeenCalled();
    expect(useAssistantStore.getState().handoffConversationId).toBe(88);
  });

  it('流式期间 streaming=true，结束后复位', async () => {
    let release: (value: number) => void = () => undefined;
    mockedStream.mockImplementation(
      () =>
        new Promise<number>(resolve => {
          release = resolve;
        })
    );

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('q');
    });
    expect(result.current.streaming).toBe(true);

    await act(async () => {
      release(1);
      await Promise.resolve();
    });
    await waitFor(() => expect(result.current.streaming).toBe(false));
  });

  it('清空对话复位消息与会话 id', async () => {
    mockedStream.mockResolvedValue(7);

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hi');
    });
    await waitFor(() => expect(result.current.messages).toHaveLength(2));
    expect(result.current.convId).toBe(7);

    act(() => {
      result.current.handleClear();
    });
    expect(result.current.messages).toEqual([]);
    expect(result.current.convId).toBeUndefined();
  });

  it('D0-04：已注册路由携带 pageContext 提示（pageKey/route）', async () => {
    mockedStream.mockResolvedValue(11);
    const wrapperAt = ({ children }: { children: React.ReactNode }) => (
      <MemoryRouter initialEntries={['/tickets/42']}>{children}</MemoryRouter>
    );
    const { result } = renderHook(() => useAIChatSession(), { wrapper: wrapperAt });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hi');
    });

    await waitFor(() =>
      expect(mockedStream).toHaveBeenCalledWith(
        expect.objectContaining({
          pageContext: { pageKey: 'ticket.detail', route: '/tickets/42' },
        }),
        expect.any(Object)
      )
    );
  });

  it('D0-04：未注册路由不携带 pageContext（请求体兼容）', async () => {
    mockedStream.mockResolvedValue(12);
    const wrapperAt = ({ children }: { children: React.ReactNode }) => (
      <MemoryRouter initialEntries={['/no/such/page']}>{children}</MemoryRouter>
    );
    const { result } = renderHook(() => useAIChatSession(), { wrapper: wrapperAt });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hi');
    });

    await waitFor(() => expect(mockedStream).toHaveBeenCalled());
    const [requestArg] = mockedStream.mock.calls[0];
    expect(requestArg.pageContext).toBeUndefined();
  });

  it('D1-02：详情页 L2 数据派生入口上下文（entrypoint + target + summary）', async () => {
    mockedStream.mockResolvedValue(21);
    useAssistantContextStore
      .getState()
      .registerPageData(Symbol('test'), { targetType: 'ticket', targetId: 42, summary: '无法登录' });
    const wrapperAt = ({ children }: { children: React.ReactNode }) => (
      <MemoryRouter initialEntries={['/tickets/42']}>{children}</MemoryRouter>
    );
    const { result } = renderHook(() => useAIChatSession(), { wrapper: wrapperAt });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hi');
    });

    await waitFor(() =>
      expect(mockedStream).toHaveBeenCalledWith(
        expect.objectContaining({
          entrypoint: 'ticket_detail',
          targetType: 'ticket',
          targetId: 42,
          summary: '无法登录',
        }),
        expect.any(Object)
      )
    );
  });

  it('D1-02：列表页仅派生 entrypoint（无目标不携带 target 字段）', async () => {
    mockedStream.mockResolvedValue(22);
    const wrapperAt = ({ children }: { children: React.ReactNode }) => (
      <MemoryRouter initialEntries={['/tickets']}>{children}</MemoryRouter>
    );
    const { result } = renderHook(() => useAIChatSession(), { wrapper: wrapperAt });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hi');
    });

    await waitFor(() => expect(mockedStream).toHaveBeenCalled());
    const [requestArg] = mockedStream.mock.calls[0];
    expect(requestArg.entrypoint).toBe('ticket_list');
    expect(requestArg.targetType).toBeUndefined();
    expect(requestArg.targetId).toBeUndefined();
  });

  it('I2-02：导出会话调用导出端点并触发 Markdown 下载（revoke 释放）', async () => {
    mockedExport.mockResolvedValue({
      conversationId: 5,
      title: '打印机故障处理',
      exportedAt: '2026-10-18T10:00:00Z',
      messageCount: 2,
      markdown: '# 打印机故障处理',
    });
    const mockedDownload = downloadTextFile as jest.MockedFunction<typeof downloadTextFile>;
    mockedDownload.mockClear();

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      await result.current.handleExportConversation(
        { stopPropagation: jest.fn() } as unknown as React.MouseEvent,
        5
      );
    });

    expect(mockedExport).toHaveBeenCalledWith(5);
    expect(mockedDownload).toHaveBeenCalledWith('ai-conversation-5.md', '# 打印机故障处理');
  });

  it('I2-02：导出失败不抛错、不触发下载（会话状态不受影响）', async () => {
    mockedExport.mockRejectedValue(new Error('403'));
    const mockedDownload = downloadTextFile as jest.MockedFunction<typeof downloadTextFile>;
    mockedDownload.mockClear();

    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      await result.current.handleExportConversation(
        { stopPropagation: jest.fn() } as unknown as React.MouseEvent,
        6
      );
    });

    expect(mockedExport).toHaveBeenCalledWith(6);
    expect(mockedDownload).not.toHaveBeenCalled();
  });

  it('P2：「补充为知识文章」上报起草动作信号 doc_draft_created（单条回答）', async () => {
    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    act(() => {
      result.current.handlePromoteToArticle({
        id: 'm1',
        role: 'assistant',
        content: '# VPN 拨号失败排查\n\n正文',
        createdAt: '2026-10-18T10:00:00Z',
      });
    });

    expect(mockedTrack).toHaveBeenCalledWith(
      'doc_draft_created',
      expect.objectContaining({
        source: 'chat_promote_answer',
        outcomeType: 'draft',
        extra: { kind: 'answer' },
      })
    );
  });

  it('P2：「保存成文章」上报起草动作信号 doc_draft_created（整段会话，带会话 ID）', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('答');
      callbacks?.onDone?.(77, { provider: 'p1', providerSource: 'tenant' });
      return 77;
    });
    const { result } = renderHook(() => useAIChatSession(), { wrapper });
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    await act(async () => {
      result.current.handleSend('hello');
    });
    await waitFor(() => expect(result.current.convId).toBe(77));

    mockedTrack.mockClear();
    act(() => {
      result.current.handleSaveConversationAsArticle();
    });

    expect(mockedTrack).toHaveBeenCalledWith(
      'doc_draft_created',
      expect.objectContaining({
        source: 'chat_promote_conversation',
        conversationId: 77,
        extra: { kind: 'conversation' },
      })
    );
  });
});
