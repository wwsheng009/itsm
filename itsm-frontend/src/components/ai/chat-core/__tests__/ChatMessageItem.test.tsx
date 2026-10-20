/**
 * I0-04 消息级集成：工具事件/待审批 → 回执卡渲染 + 注入 SPA 打开动作（结果落点可跳转）。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { ChatMessageItem } from '../ChatMessageItem';
import type { ChatMessage } from '../types';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: () => ({ hasPermission: () => true }),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  hashTelemetryText: (text: string) => `h-${text.length}`,
}));

const message: ChatMessage = {
  id: 'm-outcome',
  role: 'assistant',
  content: '已生成字段草案。',
  createdAt: new Date().toISOString(),
  toolEvents: [
    {
      tool: 'draft_ticket_fields',
      provider: 'builtin',
      phase: 'read',
      status: 'done',
      artifactId: 31,
    },
  ],
  pendingApprovals: [9],
};

describe('ChatMessageItem × OutcomeReceipt（I0-04）', () => {
  it('渲染结果落点回执：产物引用 + 草稿跳转 + 待审批；打开走注入的 SPA 导航', () => {
    const onOpenOutcome = jest.fn();
    render(
      <ChatMessageItem
        message={message}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
        onOpenOutcome={onOpenOutcome}
      />
    );

    const receipt = screen.getByTestId('outcome-receipt');
    expect(receipt).toHaveTextContent('产物 #31');
    expect(receipt).toHaveTextContent('去创建工单（字段草案）');
    expect(receipt).toHaveTextContent('去审批中心（#9 待审批）');
    expect(receipt).toHaveTextContent('artifact:31');

    fireEvent.click(screen.getByTestId('outcome-open-draft-draft:/tickets/create'));
    expect(onOpenOutcome).toHaveBeenCalledWith(
      expect.objectContaining({ route: '/tickets/create', kind: 'draft' })
    );
  });

  it('无落点消息不渲染回执卡', () => {
    render(
      <ChatMessageItem
        message={{ ...message, toolEvents: [], pendingApprovals: [] }}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
      />
    );
    expect(screen.queryByTestId('outcome-receipt')).not.toBeInTheDocument();
  });

  it('回答级反馈：点击一次上报 message_feedback 并固化', () => {
    const mockedTrack = trackAssistantEvent as jest.Mock;
    mockedTrack.mockClear();
    render(
      <ChatMessageItem
        message={{ ...message, conversationId: 9, queryHash: 'q-1' }}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('answer-feedback-unhelpful'));
    expect(mockedTrack).toHaveBeenCalledWith(
      'message_feedback',
      expect.objectContaining({
        conversationId: 9,
        queryHash: 'q-1',
        feedback: 'unhelpful',
        extra: { helpful: false },
      })
    );
    expect(screen.getByTestId('answer-feedback-thanks')).toBeInTheDocument();

    // 单条消息只允许反馈一次，重复点击不重复计数。
    fireEvent.click(screen.getByTestId('answer-feedback-helpful'));
    expect(mockedTrack).toHaveBeenCalledTimes(1);
  });

  it('T5-04a：点击「转人工」把当前消息交给 handoff 动作卡', () => {
    const onHandoff = jest.fn();
    render(
      <ChatMessageItem
        message={message}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
        onHandoff={onHandoff}
      />
    );

    fireEvent.click(screen.getByTestId('answer-handoff'));
    expect(onHandoff).toHaveBeenCalledWith(expect.objectContaining({ id: 'm-outcome' }));
  });

  it('T5-03b：用户消息展示附件芯片', () => {
    render(
      <ChatMessageItem
        message={{
          id: 'u-1',
          role: 'user',
          content: '请看截图',
          createdAt: new Date().toISOString(),
          attachments: [
            {
              id: 11,
              fileName: '截图.png',
              fileSize: 1024,
              mimeType: 'image/png',
              fileUrl: '/api/v1/ai/attachments/11/content',
            },
          ],
        }}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
      />
    );

    expect(screen.getByTestId('user-message-attachments')).toHaveTextContent('截图.png');
  });

  it('预算降级：保留正文并以 warning 提示，而不是只显示错误', () => {
    render(
      <ChatMessageItem
        message={{
          ...message,
          content: '已保留的检索线索\n\n建议拆分问题。',
          errorCode: 'budget_exceeded',
          error: '工具调用预算已用尽，以上为当前保留结果',
          toolEvents: [],
          pendingApprovals: [],
        }}
        providerLabel={() => 'provider'}
        onCreateArticle={jest.fn()}
        onOpenApproval={jest.fn()}
      />
    );

    expect(screen.getByText('已保留的检索线索')).toBeInTheDocument();
    expect(screen.getByTestId('budget-degraded-alert')).toHaveTextContent('预算已用尽');
    expect(screen.getByTestId('budget-degraded-alert').closest('.ant-alert-warning')).not.toBeNull();
  });
});
