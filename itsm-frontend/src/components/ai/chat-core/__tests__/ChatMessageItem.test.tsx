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
});