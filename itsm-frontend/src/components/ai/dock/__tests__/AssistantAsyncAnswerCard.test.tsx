/**
 * V2-02 异步精算回执卡测试：先给可用答案、完成后展示精算结果、诚实降级、面板打开即已读、移除。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import AssistantAsyncAnswerCard, {
  ASSISTANT_ASYNC_CARD_TESTID,
  ASSISTANT_ASYNC_DISMISS_TESTID,
  ASSISTANT_ASYNC_FAST_TESTID,
  ASSISTANT_ASYNC_REFINED_TESTID,
  ASSISTANT_ASYNC_SEEN_TESTID,
  ASSISTANT_ASYNC_STATUS_TESTID,
  asyncReasonText,
} from '../AssistantAsyncAnswerCard';
import { aiGetAsyncAnswer, type AIAsyncAnswerTask } from '@/lib/api/ai-api';
import { mergeServerTask, useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiStartAsyncAnswer: jest.fn(),
  aiGetAsyncAnswer: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedGet = aiGetAsyncAnswer as jest.MockedFunction<typeof aiGetAsyncAnswer>;

const serverTask = (over: Partial<AIAsyncAnswerTask> = {}): AIAsyncAnswerTask => ({
  taskId: 'aa_1',
  status: 'running',
  prompt: '连不上网怎么办',
  pageKey: 'ticket.detail',
  createdAt: '2026-10-18T10:00:00.000Z',
  fast: { source: 'help_index', items: [{ title: '网络故障排查', snippet: '检查 DNS 与网关' }] },
  ...over,
});

const renderCard = () =>
  render(
    <App>
      <AssistantAsyncAnswerCard />
    </App>
  );

beforeEach(() => {
  jest.clearAllMocks();
  useAssistantAsyncStore.getState().reset();
  mockedGet.mockResolvedValue(serverTask());
});

describe('AssistantAsyncAnswerCard（V2-02 异步回执卡）', () => {
  it('无任务 → 不渲染', () => {
    const { container } = renderCard();
    expect(container.querySelector(`[data-testid="${ASSISTANT_ASYNC_CARD_TESTID}"]`)).toBeNull();
  });

  it('running：先给可用答案（快速参考）+ 后台精算轮询完成后展示精算结果并转已读', async () => {
    useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask()));
    // 挂载即轮询到精算完成 → 展示 refined（先给可用答案 → 后台精算产物）。
    mockedGet.mockResolvedValue(
      serverTask({ status: 'done', finishedAt: '2026-10-18T10:00:30.000Z', refined: { model: 'gpt-4o', answer: '完整回答：先检查网关…' } })
    );

    renderCard();
    expect(screen.getByTestId(ASSISTANT_ASYNC_STATUS_TESTID)).toHaveTextContent('精算中');
    expect(screen.getByTestId(ASSISTANT_ASYNC_FAST_TESTID)).toHaveTextContent('网络故障排查');
    expect(screen.getByTestId(ASSISTANT_ASYNC_FAST_TESTID)).toHaveTextContent('检查 DNS 与网关');

    // 完成后未读转已读（面板打开即视为已读）。
    await waitFor(() => expect(screen.getByTestId(ASSISTANT_ASYNC_REFINED_TESTID)).toHaveTextContent('完整回答：先检查网关…'));
    expect(screen.getByTestId(ASSISTANT_ASYNC_STATUS_TESTID)).toHaveTextContent('已完成');
    await waitFor(() => expect(useAssistantAsyncStore.getState().tasks[0].seen).toBe(true));
  });

  it('degraded（未配置模型）：如实展示"仅快速参考"与原因，不假装精算', async () => {
    useAssistantAsyncStore.getState().start(
      mergeServerTask(undefined, serverTask({ status: 'degraded', reason: 'generator_unavailable', finishedAt: '2026-10-18T10:00:01.000Z' }))
    );
    mockedGet.mockResolvedValue(serverTask({ status: 'degraded', reason: 'generator_unavailable', finishedAt: '2026-10-18T10:00:01.000Z' }));

    renderCard();
    expect(screen.getByTestId(ASSISTANT_ASYNC_STATUS_TESTID)).toHaveTextContent('仅快速参考');
    expect(screen.getByTestId(ASSISTANT_ASYNC_SEEN_TESTID)).toHaveTextContent('当前未配置模型，仅提供快速参考');
    expect(screen.getByTestId(ASSISTANT_ASYNC_FAST_TESTID)).toHaveTextContent('网络故障排查');
    expect(screen.queryByTestId(ASSISTANT_ASYNC_REFINED_TESTID)).toBeNull();
  });

  it('移除回执：从任务表删除且卡片消失', async () => {
    useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask()));
    renderCard();

    fireEvent.click(screen.getByTestId(ASSISTANT_ASYNC_DISMISS_TESTID));
    await waitFor(() => expect(useAssistantAsyncStore.getState().tasks).toHaveLength(0));
    expect(screen.queryByTestId(ASSISTANT_ASYNC_CARD_TESTID)).toBeNull();
  });

  it('asyncReasonText：原因码 → 可读文案（未知原因按状态兜底）', () => {
    expect(asyncReasonText('timeout', 'failed')).toBe('精算超时，快速参考仍可用');
    expect(asyncReasonText('provider_error', 'failed')).toBe('模型调用失败，快速参考仍可用');
    expect(asyncReasonText('generator_unavailable', 'degraded')).toBe('当前未配置模型，仅提供快速参考');
    expect(asyncReasonText(undefined, 'failed')).toBe('精算未完成，快速参考仍可用');
    expect(asyncReasonText(undefined, 'running')).toBe('');
  });
});