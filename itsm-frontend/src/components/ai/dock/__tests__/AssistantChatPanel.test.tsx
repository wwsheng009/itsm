/**
 * D0-02 面板聊天视图测试：复用会话内核（建议卡渲染、发送走 SSE、回答渲染）。
 */
import React from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';

import AssistantChatPanel from '../AssistantChatPanel';
import {
  AIApi,
  aiChatStream,
  aiGetAsyncAnswer,
  aiGetAssistantContext,
  aiGetPageHelp,
  aiStartAsyncAnswer,
  deleteConversation,
  exportConversation,
  getConversationMessages,
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
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';
import { downloadTextFile } from '@/lib/utils/download-text-file';

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
  exportConversation: jest.fn(),
}));
jest.mock('@/lib/utils/download-text-file', () => ({
  __esModule: true,
  downloadTextFile: jest.fn(() => true),
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

/** 工作区占位页：把导航 state 暴露到 DOM，便于断言上下文交接。 */
const WorkspaceProbe = () => {
  const location = useLocation();
  return (
    <div data-testid="workspace-page" data-state={JSON.stringify(location.state ?? null)} />
  );
};

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
  (deleteConversation as jest.Mock).mockReset();
  (exportConversation as jest.Mock).mockReset();
  (downloadTextFile as jest.Mock).mockClear();
  // V2-02：异步精算任务表与轮询 mock（无 running 任务时零请求）。
  useAssistantAsyncStore.getState().reset();
  (aiStartAsyncAnswer as jest.Mock).mockReset();
  (aiGetAsyncAnswer as jest.Mock).mockReset();
  // I0-03/P3：预填与交接通道为模块级状态，用例间隔离。
  useAssistantStore.setState({
    pendingPrompt: undefined,
    handoffConversationId: undefined,
    open: false,
  });
  useAssistantContextStore.setState({ pageData: null, owner: null });
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

  it('P3：有会话时「在工作区打开」写入交接、关闭面板并导航（无会话/生成中禁用）', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('回答');
      callbacks?.onDone?.(55, { provider: 'p1' });
      return 55;
    });

    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <Routes>
          <Route path="/dashboard" element={<AssistantChatPanel />} />
          <Route path="/ai/chat" element={<WorkspaceProbe />} />
        </Routes>
      </MemoryRouter>
    );
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    // 无会话：入口禁用（提示用户先发第一条消息）。
    const button = screen.getByTestId('assistant-open-workspace');
    expect(button).toBeDisabled();

    // 发送一轮 → done 回填 convId → 入口可用。
    fireEvent.change(screen.getByTestId('assistant-panel-input'), {
      target: { value: '继续这个话题' },
    });
    fireEvent.click(screen.getByTestId('assistant-panel-send'));
    await waitFor(() => expect(screen.getByTestId('assistant-open-workspace')).not.toBeDisabled());

    act(() => {
      useAssistantStore.setState({ open: true });
    });
    fireEvent.click(screen.getByTestId('assistant-open-workspace'));

    // 交接 id 已写入、面板已关闭、已导航到工作区。
    expect(useAssistantStore.getState().handoffConversationId).toBe(55);
    expect(useAssistantStore.getState().open).toBe(false);
    expect(await screen.findByTestId('workspace-page')).toBeInTheDocument();
    // /dashboard 没有具体目标：不带 scope state（保持请求体与现状一致）。
    expect(screen.getByTestId('workspace-page').dataset.state).toBe('null');
  });

  it('P3：交接把当前页面的 L2 目标上下文带到工作区（ticket#42）', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('回答');
      callbacks?.onDone?.(66, { provider: 'p1' });
      return 66;
    });
    useAssistantContextStore.setState({
      pageData: { targetType: 'ticket', targetId: 42, summary: '无法登录' },
      owner: Symbol('ticket-detail'),
    });

    render(
      <MemoryRouter initialEntries={['/tickets/42']}>
        <Routes>
          <Route path="/tickets/:id" element={<AssistantChatPanel />} />
          <Route path="/ai/chat" element={<WorkspaceProbe />} />
        </Routes>
      </MemoryRouter>
    );
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.change(screen.getByTestId('assistant-panel-input'), {
      target: { value: '这个工单怎么处理' },
    });
    fireEvent.click(screen.getByTestId('assistant-panel-send'));
    await waitFor(() => expect(screen.getByTestId('assistant-open-workspace')).not.toBeDisabled());
    fireEvent.click(screen.getByTestId('assistant-open-workspace'));

    const workspace = await screen.findByTestId('workspace-page');
    const state = JSON.parse(workspace.dataset.state as string);
    expect(state).toMatchObject({
      source: 'ask-ai-launcher',
      scope: {
        entrypoint: 'ticket_detail',
        targetType: 'ticket',
        targetId: 42,
        summary: '无法登录',
      },
    });
  });

  it('P4：会话历史下拉列出历史会话，点击切换并加载消息（与工作区共用内核）', async () => {
    mockedListConversations.mockResolvedValue([
      { id: 9, title: '连不上网', userId: 1, tenantId: 1, createdAt: '2026-10-19T01:00:00Z' },
      { id: 8, title: 'SLA 怎么算', userId: 1, tenantId: 1, createdAt: '2026-10-18T09:00:00Z' },
    ]);
    (getConversationMessages as jest.Mock).mockResolvedValue([
      {
        id: 1,
        conversationId: 9,
        role: 'user',
        content: '网络断了',
        createdAt: '2026-10-19T01:00:00Z',
      },
      {
        id: 2,
        conversationId: 9,
        role: 'assistant',
        content: JSON.stringify({ answer: '先检查网线', sources: [] }),
        createdAt: '2026-10-19T01:00:01Z',
      },
    ]);

    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    // 打开历史下拉：列出标题与相对时间。
    fireEvent.click(screen.getByTestId('assistant-history-open'));
    const item = await screen.findByTestId('assistant-history-item-9');
    expect(item).toHaveTextContent('连不上网');
    expect(screen.getByTestId('assistant-history-item-8')).toHaveTextContent('SLA 怎么算');

    // 点击切换：拉取该会话消息并渲染（assistant 信封解包）。
    fireEvent.click(item);
    await waitFor(() => expect(getConversationMessages).toHaveBeenCalledWith(9));
    expect(await screen.findByText('先检查网线')).toBeInTheDocument();
    expect(screen.getByText('网络断了')).toBeInTheDocument();
  });

  it('P4：会话历史「新建会话」清空当前消息', async () => {
    mockedStream.mockImplementation(async (_req, callbacks) => {
      callbacks?.onDelta?.('回答');
      callbacks?.onDone?.(77, { provider: 'p1' });
      return 77;
    });
    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.change(screen.getByTestId('assistant-panel-input'), { target: { value: '你好' } });
    fireEvent.click(screen.getByTestId('assistant-panel-send'));
    expect(await screen.findByText('回答')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('assistant-history-open'));
    fireEvent.click(await screen.findByTestId('assistant-history-new'));

    await waitFor(() => expect(screen.queryByText('回答')).not.toBeInTheDocument());
    expect(screen.getByTestId('assistant-panel-empty')).toBeInTheDocument();
  });

  it('P4：会话历史支持按标题搜索过滤', async () => {
    mockedListConversations.mockResolvedValue([
      { id: 9, title: '连不上网', userId: 1, tenantId: 1, createdAt: '2026-10-19T01:00:00Z' },
      { id: 8, title: 'SLA 怎么算', userId: 1, tenantId: 1, createdAt: '2026-10-18T09:00:00Z' },
    ]);
    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId('assistant-history-open'));
    expect(await screen.findByTestId('assistant-history-item-9')).toBeInTheDocument();
    expect(screen.getByTestId('assistant-history-item-8')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('assistant-history-search'), {
      target: { value: 'sla' },
    });
    expect(screen.queryByTestId('assistant-history-item-9')).not.toBeInTheDocument();
    expect(screen.getByTestId('assistant-history-item-8')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('assistant-history-search'), {
      target: { value: '不存在的标题' },
    });
    expect(screen.getByText('没有匹配的会话')).toBeInTheDocument();
  });

  it('P4：会话历史支持导出与删除（删除当前会话后回到空态）', async () => {
    mockedListConversations.mockResolvedValue([
      { id: 9, title: '连不上网', userId: 1, tenantId: 1, createdAt: '2026-10-19T01:00:00Z' },
    ]);
    (getConversationMessages as jest.Mock).mockResolvedValue([
      {
        id: 1,
        conversationId: 9,
        role: 'user',
        content: '网络断了',
        createdAt: '2026-10-19T01:00:00Z',
      },
    ]);
    (exportConversation as jest.Mock).mockResolvedValue({ markdown: '# 会话快照' });
    (deleteConversation as jest.Mock).mockResolvedValue(undefined);

    renderPanel();
    await waitFor(() => expect(mockedListConversations).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('assistant-history-open'));
    fireEvent.click(await screen.findByTestId('assistant-history-item-9'));
    await waitFor(() => expect(getConversationMessages).toHaveBeenCalledWith(9));
    // 等会话状态落定（convId 生效）再操作，否则删除的是"非当前会话"分支。
    expect(await screen.findByText('网络断了')).toBeInTheDocument();
    // 切换后下拉会关闭；真实流程下需重新打开历史再操作（也保证闭包拿到最新 convId）。
    fireEvent.click(screen.getByTestId('assistant-history-open'));

    // 导出：复用内核动作 + 本地下载。
    fireEvent.click(screen.getByTestId('assistant-history-export-9'));
    await waitFor(() => expect(exportConversation).toHaveBeenCalledWith(9));
    expect(downloadTextFile).toHaveBeenCalledWith('ai-conversation-9.md', '# 会话快照');

    // 删除：二次确认后调用 DELETE；删除的是当前会话 → 清空消息回到空态。
    fireEvent.click(screen.getByTestId('assistant-history-delete-9'));
    // 锚定精确匹配：图标按钮的 aria-label 是「删除会话」，不能误点。
    // antd 会给两字按钮自动插入空格（「删 除」），正则同时兼容。
    fireEvent.click(await screen.findByRole('button', { name: /^删\s*除$/ }));
    await waitFor(() => expect(deleteConversation).toHaveBeenCalledWith(9));
    await waitFor(() => expect(screen.getByTestId('assistant-panel-empty')).toBeInTheDocument());
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
