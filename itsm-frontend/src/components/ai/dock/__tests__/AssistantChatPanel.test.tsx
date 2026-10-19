/**
 * D0-02 面板聊天视图测试：复用会话内核（建议卡渲染、发送走 SSE、回答渲染）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AssistantChatPanel from '../AssistantChatPanel';
import {
  AIApi,
  aiChatStream,
  aiGetAsyncAnswer,
  aiGetAssistantContext,
  aiGetPageHelp,
  aiStartAsyncAnswer,
  listConversations,
} from '@/lib/api/ai-api';
import {
  ASSISTANT_ASYNC_CARD_TESTID,
  ASSISTANT_ASYNC_FAST_TESTID,
} from '../AssistantAsyncAnswerCard';
import { useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';
import { useLLMProviderFeature } from '@/lib/hooks/use-llm-provider-feature';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useAssistantStore } from '@/lib/store/assistant-store';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  AIApi: { listVisibleBotsWithCapabilities: jest.fn() },
  aiChatStream: jest.fn(),
  getConversationMessages: jest.fn(),
  listConversations: jest.fn(),
  deleteConversation: jest.fn(),
  aiGetPageHelp: jest.fn(),
  aiSearchHelp: jest.fn(),
  aiGetAssistantContext: jest.fn(),
  aiStartAsyncAnswer: jest.fn(),
  aiGetAsyncAnswer: jest.fn(),
}));
jest.mock('@/lib/hooks/use-llm-provider-feature', () => ({
  __esModule: true,
  useLLMProviderFeature: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  isAssistantEventName: () => true,
  getTelemetryMode: () => 'off',
  setTelemetryMode: jest.fn(),
}));

// D0-05：页面级建议卡依赖真实路由（注册表解析）；jest.setup.js 全局 mock 了 useLocation → 还原真实实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedStream = aiChatStream as jest.MockedFunction<typeof aiChatStream>;
const mockedListConversations = listConversations as jest.MockedFunction<typeof listConversations>;
const mockedBots = AIApi as unknown as { listVisibleBotsWithCapabilities: jest.Mock };
const mockedProviderFeature = useLLMProviderFeature as jest.MockedFunction<
  typeof useLLMProviderFeature
>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const renderPanel = () =>
  render(
    <MemoryRouter>
      <AssistantChatPanel />
    </MemoryRouter>
  );

beforeEach(() => {
  mockedBots.listVisibleBotsWithCapabilities.mockResolvedValue({ items: [], capabilities: null });
  mockedListConversations.mockResolvedValue([]);
  // D1-01：面板建议卡优先服务端过滤；本套用例聚焦面板链路，服务端按未知页面降级 → 静态基线。
  (aiGetAssistantContext as jest.Mock).mockResolvedValue({
    pageKey: 'dashboard',
    known: false,
    suggestions: [],
  });
  (aiGetPageHelp as jest.Mock).mockResolvedValue({
    pageKey: 'dashboard',
    status: 'ok',
    title: '仪表盘',
    pattern: '/dashboard',
    entrypoint: 'chat',
    docs: [],
    coverage: { coverage: 1, totals: { pages: 21, ok: 21, missing: 0, stale: 0, orphan: 0 } },
  });
  mockedProviderFeature.mockReturnValue({
    enabled: false,
    ready: true,
    providers: [],
    preference: null,
    refresh: jest.fn(),
    setPreference: jest.fn(),
  } as unknown as ReturnType<typeof useLLMProviderFeature>);
  mockedPermissions.mockReturnValue({
    hasPermission: () => true,
  } as unknown as ReturnType<typeof usePermissions>);
  mockedStream.mockReset();
  // V2-02：异步精算任务表与轮询 mock（无 running 任务时零请求）。
  useAssistantAsyncStore.getState().reset();
  (aiStartAsyncAnswer as jest.Mock).mockReset();
  (aiGetAsyncAnswer as jest.Mock).mockReset();
  // I0-03：预填通道为模块级状态，用例间隔离。
  useAssistantStore.setState({ pendingPrompt: undefined });
});

describe('AssistantChatPanel（D0-02 面板聊天视图）', () => {
  it('空态渲染建议卡（复用 chat-core 常量）', async () => {
    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());
    expect(await screen.findByTestId('assistant-panel-empty')).toBeInTheDocument();
    expect(screen.getByText('总结我最近处理过的工单')).toBeInTheDocument();
  });

  it('输入并发送：走同一 SSE 回调链并渲染回答', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('面板回答');
      callbacks?.onDone?.(5, { provider: 'p1' });
      return 5;
    });

    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    const input = screen.getByTestId('assistant-panel-input');
    fireEvent.change(input, { target: { value: '这个单怎么回事' } });
    fireEvent.click(screen.getByTestId('assistant-panel-send'));

    await waitFor(() =>
      expect(mockedStream).toHaveBeenCalledWith(
        expect.objectContaining({ query: '这个单怎么回事' }),
        expect.any(Object)
      )
    );
    expect(await screen.findByText('面板回答')).toBeInTheDocument();
  });

  it('点击建议卡直接发送', async () => {
    mockedStream.mockResolvedValue(6);

    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.click(screen.getByText('知识库里有没有相关方案？'));
    await waitFor(() =>
      expect(mockedStream).toHaveBeenCalledWith(
        expect.objectContaining({ query: '知识库里有没有相关方案？' }),
        expect.any(Object)
      )
    );
  });

  it('页面级建议卡：注册路由渲染声明卡片，点击发送并埋点（D0-05）', async () => {
    mockedStream.mockResolvedValue(21);
    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <AssistantChatPanel />
      </MemoryRouter>
    );
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    // 展示埋点：pageKey=dashboard 的声明卡片
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_shown',
        expect.objectContaining({ pageKey: 'dashboard', cardId: 'attention_today', cardKind: 'task', rank: 0 })
      )
    );

    const card = await screen.findByTestId('suggestion-card-attention_today');
    expect(card).toHaveTextContent('今天最需要关注什么？');

    mockedTrack.mockClear();
    fireEvent.click(card);

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_clicked',
        expect.objectContaining({ pageKey: 'dashboard', cardId: 'attention_today', cardKind: 'task', rank: 0 })
      )
    );
    await waitFor(() =>
      expect(mockedStream).toHaveBeenCalledWith(
        expect.objectContaining({ query: expect.stringContaining('今天最需要关注') }),
        expect.any(Object)
      )
    );
  });

  it('切换到「帮助」页签渲染面板帮助视图，切回「对话」恢复聊天（H0-04）', async () => {
    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <AssistantChatPanel />
      </MemoryRouter>
    );
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    // 默认对话页签
    expect(screen.getByTestId('assistant-panel-empty')).toBeInTheDocument();

    fireEvent.click(within(screen.getByTestId('assistant-panel-tabs')).getByText('帮助'));
    expect(await screen.findByTestId('assistant-help-panel')).toBeInTheDocument();
    await waitFor(() => expect(aiGetPageHelp).toHaveBeenCalledWith('dashboard'));
    expect(screen.queryByTestId('assistant-panel-empty')).not.toBeInTheDocument();

    fireEvent.click(within(screen.getByTestId('assistant-panel-tabs')).getByText('对话'));
    expect(await screen.findByTestId('assistant-panel-empty')).toBeInTheDocument();
  });

  it('I0-03：消费全局搜索预填（只填不发送，消费后清空）', async () => {
    useAssistantStore.getState().setPendingPrompt('来自全局搜索：SLA 风险怎么看？');
    renderPanel();

    await waitFor(() =>
      expect((screen.getByTestId('assistant-panel-input') as HTMLTextAreaElement).value).toBe(
        '来自全局搜索：SLA 风险怎么看？'
      )
    );
    expect(useAssistantStore.getState().pendingPrompt).toBeUndefined();
    expect(mockedStream).not.toHaveBeenCalled();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());
  });

  it('O2-02：会话诊断入口打开抽屉（空会话显示无运行记录，不发轨迹请求）', async () => {
    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId('assistant-diagnostics-open'));

    expect(await screen.findByTestId('assistant-diagnostics-drawer')).toBeInTheDocument();
    expect(screen.getByTestId('diagnostics-no-run')).toBeInTheDocument();
    expect(screen.getByTestId('diagnostics-copy-run')).toBeDisabled();
  });

  it('V2-02：输入坞「后台精算」只发起后台任务（先给可用答案卡），不发送对话', async () => {
    (aiStartAsyncAnswer as jest.Mock).mockResolvedValue({
      taskId: 'aa_panel',
      status: 'running',
      prompt: '连不上网怎么办',
      pageKey: 'dashboard',
      createdAt: '2026-10-18T10:00:00.000Z',
      fast: { source: 'help_index', items: [{ title: '网络故障排查', snippet: '检查 DNS 与网关' }] },
    });
    (aiGetAsyncAnswer as jest.Mock).mockResolvedValue({
      taskId: 'aa_panel',
      status: 'running',
      prompt: '连不上网怎么办',
      createdAt: '2026-10-18T10:00:00.000Z',
      fast: { source: 'help_index', items: [{ title: '网络故障排查', snippet: '检查 DNS 与网关' }] },
    });
    // 显式路由：后台精算携带页面上下文（pageKey=dashboard）。
    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <AssistantChatPanel />
      </MemoryRouter>
    );
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.change(screen.getByTestId('assistant-panel-input'), { target: { value: '连不上网怎么办' } });
    fireEvent.click(screen.getByTestId('assistant-async-start'));

    // 先给可用答案：快速参考立即可见（后台精算由轮询收口）。
    expect(await screen.findByTestId(ASSISTANT_ASYNC_CARD_TESTID)).toBeInTheDocument();
    expect(screen.getByTestId(ASSISTANT_ASYNC_FAST_TESTID)).toHaveTextContent('网络故障排查');
    expect(aiStartAsyncAnswer).toHaveBeenCalledWith(
      expect.objectContaining({ prompt: '连不上网怎么办', pageKey: 'dashboard' })
    );
    // 只发起后台任务：不发送对话、不提交任何写动作。
    expect(mockedStream).not.toHaveBeenCalled();
  });
});
