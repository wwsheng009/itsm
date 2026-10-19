/**
 * O1-01 条件项测试：会话维度轨迹视图（纯问答无 run 可回放）。
 *
 * 口径：消息角色/内容/截断标记、运行跳转链接、工具确认状态、产物、
 * 业务对象深链、复制会话 ID（报障导出埋点）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import ConversationTraceView from '../ConversationTraceView';
import type { AIConversationTrace } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.setTimeout(300000);

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

const fixture: AIConversationTrace = {
  conversation: { id: 9, userId: 7, botId: 3, title: '连不上网', createdAt: '2026-10-18T10:00:00Z' },
  messages: [
    { id: 1, role: 'user', content: '我连不上网了', createdAt: '2026-10-18T10:00:00Z' },
    {
      id: 2,
      role: 'assistant',
      content: '请先确认范围',
      truncated: true,
      createdAt: '2026-10-18T10:00:01Z',
    },
  ],
  messageCount: 2,
  messagesTruncated: false,
  runs: [
    {
      id: 7,
      status: 'completed',
      entrypoint: 'chat',
      model: 'gpt-4o',
      startedAt: '2026-10-18T10:00:00Z',
      finishedAt: '2026-10-18T10:00:03Z',
    },
  ],
  toolInvocations: [
    {
      id: 11,
      toolName: 'create_ticket',
      status: 'success',
      approvalState: 'approved',
      confirmationState: 'confirmed',
      needsApproval: true,
      risk: 'act_low',
      argsRedacted: '{"title":"***"}',
      durationMs: 120,
      targetType: 'ticket',
      targetId: '42',
      stepId: 3,
      userId: 7,
      createdAt: '2026-10-18T10:00:01Z',
    },
  ],
  artifacts: [
    {
      id: 31,
      kind: 'draft',
      toolName: 'draft_reply',
      title: '答复草稿',
      createdAt: '2026-10-18T10:00:02Z',
    },
  ],
};

const pureQA: AIConversationTrace = {
  conversation: { id: 12, userId: 7, botId: 0, title: '', createdAt: '2026-10-18T10:00:00Z' },
  messages: [
    { id: 1, role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' },
    { id: 2, role: 'assistant', content: 'hello', createdAt: '2026-10-18T10:00:01Z' },
  ],
  messageCount: 202,
  messagesTruncated: true,
  runs: [],
  toolInvocations: [],
  artifacts: [],
};

describe('ConversationTraceView（O1-01 条件项：会话维度回放）', () => {
  beforeEach(() => {
    mockedTrack.mockReset();
  });

  it('渲染会话维度链路：会话/消息/运行/工具确认/产物/业务对象', () => {
    render(<ConversationTraceView trace={fixture} pageKey="ai.observability" />);

    expect(screen.getByTestId('conversation-trace-view')).toBeInTheDocument();
    expect(screen.getByText('会话轨迹 #9')).toBeInTheDocument();
    expect(screen.getByText('连不上网')).toBeInTheDocument();

    // 消息回合：角色 + 内容 + 截断显式标记。
    const messages = screen.getByTestId('conversation-trace-messages');
    expect(messages).toHaveTextContent('我连不上网了');
    expect(messages).toHaveTextContent('请先确认范围');
    expect(screen.getByTestId('conversation-trace-message-2-truncated')).toHaveTextContent('已截断');

    // 运行跳转：run 维度轨迹链接。
    const runLink = screen.getByText('运行 #7') as HTMLAnchorElement;
    expect(runLink.getAttribute('href')).toBe('/ai/observability?runId=7');

    // 工具确认状态与脱敏参数。
    expect(screen.getByTestId('conversation-trace-tool-11-confirmation')).toHaveTextContent('已确认');
    expect(screen.getByTestId('conversation-trace-tools')).toHaveTextContent('{"title":"***"}');

    // 产物与业务对象深链。
    expect(screen.getByTestId('conversation-trace-artifacts')).toHaveTextContent('答复草稿');
    const target = screen.getByText('ticket #42') as HTMLAnchorElement;
    expect(target.getAttribute('href')).toBe('/tickets/42');
  });

  it('纯问答（无运行）：显示纯问答说明与窗口截断标注', () => {
    render(<ConversationTraceView trace={pureQA} />);

    expect(screen.getByTestId('conversation-trace-pure-qa')).toHaveTextContent('纯问答');
    expect(screen.getByTestId('conversation-trace-messages-truncated')).toHaveTextContent(
      '仅显示前 2 条（共 202 条）'
    );
    expect(screen.queryByTestId('conversation-trace-run-7')).not.toBeInTheDocument();
  });

  it('复制会话 ID：写入剪贴板并记录 handoff 埋点（报障导出）', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

    render(<ConversationTraceView trace={fixture} pageKey="ai.observability" />);
    fireEvent.click(screen.getByTestId('conversation-trace-copy'));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('9'));
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({
        pageKey: 'ai.observability',
        source: 'trace_copy',
        extra: { reference: 'conversation' },
      })
    );
  });
});