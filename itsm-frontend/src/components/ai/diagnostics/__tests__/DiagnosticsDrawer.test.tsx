/**
 * O2-02 诊断抽屉测试：会话/运行摘要、复制轨迹 ID（报障导出）、轨迹懒加载与降级。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import DiagnosticsDrawer from '../DiagnosticsDrawer';
import {
  aiGetConversationTrace,
  aiGetRunTrace,
  type AIConversationTrace,
  type AIRunTrace,
} from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import type { ChatMessage } from '@/components/ai/chat-core/types';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetRunTrace: jest.fn(),
  aiGetConversationTrace: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrace = aiGetRunTrace as jest.MockedFunction<typeof aiGetRunTrace>;
const mockedConversationTrace = aiGetConversationTrace as jest.MockedFunction<typeof aiGetConversationTrace>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const messages: ChatMessage[] = [
  { id: 'u1', role: 'user', content: '帮我处理这张工单', createdAt: '2026-10-18T10:00:00Z' },
  {
    id: 'a1',
    role: 'assistant',
    content: '完成',
    createdAt: '2026-10-18T10:00:03Z',
    run: {
      runId: 7,
      status: 'done',
      stepCount: 2,
      lastDurationMs: 1200,
      providerLabel: 'OpenAI · gpt-4o',
      budget: { stepsUsed: 2, stepsLimit: 20 },
    },
    steps: [
      { runId: 7, stepIndex: 0, type: 'llm', durationMs: 800 },
      { runId: 7, stepIndex: 1, type: 'tool', payloadRef: 'tool_invocation:42', durationMs: 120 },
    ],
  },
];

const traceFixture: AIRunTrace = {
  run: {
    id: 7,
    conversationId: 9,
    botId: 0,
    entrypoint: 'ticket_detail',
    status: 'completed',
    startedAt: '2026-10-18T10:00:00Z',
    finishedAt: '2026-10-18T10:00:03Z',
  },
  steps: [{ id: 1, stepIndex: 0, type: 'llm', durationMs: 800, createdAt: '2026-10-18T10:00:00Z' }],
  events: [],
  toolInvocations: [],
  artifacts: [],
};

const conversationTraceFixture: AIConversationTrace = {
  conversation: { id: 9, userId: 7, botId: 0, title: '连不上网', createdAt: '2026-10-18T10:00:00Z' },
  messages: [
    { id: 1, role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' },
    { id: 2, role: 'assistant', content: 'hello', createdAt: '2026-10-18T10:00:01Z' },
  ],
  messageCount: 2,
  messagesTruncated: false,
  runs: [],
  toolInvocations: [],
  artifacts: [],
};

const renderDrawer = (overrides: Partial<React.ComponentProps<typeof DiagnosticsDrawer>> = {}) =>
  render(
    <DiagnosticsDrawer
      open
      onClose={jest.fn()}
      messages={messages}
      conversationId={9}
      streaming={false}
      {...overrides}
    />
  );

beforeEach(() => {
  mockedTrace.mockReset();
  mockedTrace.mockResolvedValue(traceFixture);
  mockedConversationTrace.mockReset();
  mockedConversationTrace.mockResolvedValue(conversationTraceFixture);
  mockedTrack.mockReset();
});

describe('DiagnosticsDrawer（O2-02）', () => {
  it('展示会话/运行摘要与本地步骤，并加载深度轨迹', async () => {
    renderDrawer();

    const drawer = await screen.findByTestId('assistant-diagnostics-drawer');
    expect(drawer).toHaveTextContent('会话#9');
    expect(drawer).toHaveTextContent('消息数');
    expect(drawer).toHaveTextContent('run#7');
    // 本地运行快照（v2 事件）
    const runSummary = screen.getByTestId('diagnostics-run-summary');
    expect(runSummary).toHaveTextContent('已完成');
    expect(runSummary).toHaveTextContent('步骤 2');
    expect(runSummary).toHaveTextContent('OpenAI · gpt-4o');
    expect(runSummary).toHaveTextContent('预算：步数 2/20');
    // 本地步骤
    expect(screen.getByTestId('diagnostics-steps')).toHaveTextContent('tool_invocation:42');
    // 深度轨迹（O1-01 端点 + RunTraceView）
    await waitFor(() => expect(mockedTrace).toHaveBeenCalledWith(7));
    expect(await screen.findByTestId('run-trace-view')).toBeInTheDocument();
  });

  it('复制轨迹 ID：写入剪贴板并记录 handoff 埋点（报障导出）', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

    renderDrawer();
    fireEvent.click(await screen.findByTestId('diagnostics-copy-run'));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('7'));
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ source: 'diagnostics_copy', runId: '7' })
    );
  });

  it('纯问答（无运行记录）：回退会话维度轨迹（O1-01 条件项闭环）；复制会话 ID 可用', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

    renderDrawer({
      messages: [
        { id: 'u1', role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' },
        { id: 'a1', role: 'assistant', content: 'hello', createdAt: '2026-10-18T10:00:01Z' },
      ],
    });

    // run 维度无目标：不发 run 轨迹请求；改用会话维度轨迹回放。
    expect(mockedTrace).not.toHaveBeenCalled();
    await waitFor(() => expect(mockedConversationTrace).toHaveBeenCalledWith(9));
    expect(await screen.findByTestId('conversation-trace-view')).toBeInTheDocument();
    expect(screen.queryByTestId('diagnostics-no-run')).not.toBeInTheDocument();
    expect(screen.getByTestId('diagnostics-copy-run')).toBeDisabled();

    fireEvent.click(screen.getByTestId('diagnostics-copy-conversation'));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('9'));
  });

  it('无会话 ID 且无运行记录：显示降级说明（不发任何轨迹请求）', async () => {
    renderDrawer({
      conversationId: undefined,
      messages: [{ id: 'u1', role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' }],
    });

    expect(await screen.findByTestId('diagnostics-no-run')).toBeInTheDocument();
    expect(mockedTrace).not.toHaveBeenCalled();
    expect(mockedConversationTrace).not.toHaveBeenCalled();
  });

  it('会话维度轨迹加载失败：显示降级提示（可复制会话 ID 报障）', async () => {
    mockedConversationTrace.mockRejectedValue({ response: { status: 404 } });

    renderDrawer({
      messages: [{ id: 'u1', role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' }],
    });

    expect(await screen.findByTestId('diagnostics-conversation-trace-error')).toHaveTextContent(
      '会话轨迹加载失败（可复制会话 ID 报障）'
    );
  });

  it('轨迹加载失败：显示降级提示（可复制轨迹 ID 报障）', async () => {
    mockedTrace.mockRejectedValue({ response: { status: 404 } });

    renderDrawer();

    expect(await screen.findByTestId('diagnostics-trace-error')).toHaveTextContent(
      '轨迹加载失败（可复制轨迹 ID 报障）'
    );
  });

  it('展示回合 token 估算段（v2 usage；标注估算口径，非账单）', async () => {
    renderDrawer({
      messages: [
        ...messages,
        {
          id: 'a2',
          role: 'assistant',
          content: '纯问答',
          createdAt: '2026-10-18T10:02:00Z',
          usage: { tokenIn: 120, tokenOut: 48, estimated: true, scope: 'turn' },
        },
      ],
    });

    const usage = await screen.findByTestId('diagnostics-usage');
    expect(usage).toHaveTextContent('用量（估算）');
    expect(usage).toHaveTextContent('输入 ≈ 120 · 输出 ≈ 48 · 合计 ≈ 168');
    expect(usage).toHaveTextContent('与限流同源');
    expect(screen.getByTestId('assistant-diagnostics-drawer')).toHaveTextContent(
      'token 为估算口径（provider 未回包 usage 时）'
    );
  });

  it('展示 provider 实测用量段（estimated=false；B4-02）', async () => {
    renderDrawer({
      messages: [
        ...messages,
        {
          id: 'a3',
          role: 'assistant',
          content: '实测回合',
          createdAt: '2026-10-18T10:03:00Z',
          usage: { tokenIn: 33, tokenOut: 7, estimated: false, scope: 'turn' },
        },
      ],
    });

    const usage = await screen.findByTestId('diagnostics-usage');
    expect(usage).toHaveTextContent('用量（provider 实测）');
    expect(usage).toHaveTextContent('输入 33 · 输出 7 · 合计 40（provider 实测）');
    expect(usage).toHaveTextContent('provider 回包实测');
    expect(screen.getByTestId('assistant-diagnostics-drawer')).toHaveTextContent(
      'token 为 provider 回包实测（B4-02）'
    );
  });

  it('无用量数据：不渲染用量段，回退「无计量数据」说明', async () => {
    renderDrawer({
      messages: [
        { id: 'u1', role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' },
        { id: 'a1', role: 'assistant', content: 'hello', createdAt: '2026-10-18T10:00:01Z' },
      ],
    });

    await screen.findByTestId('conversation-trace-view');
    expect(screen.queryByTestId('diagnostics-usage')).not.toBeInTheDocument();
    expect(screen.getByTestId('assistant-diagnostics-drawer')).toHaveTextContent(
      '本会话没有可计量的回合用量'
    );
  });
});
