/**
 * V1-01 列表页 AI 批量助手用例：
 * 权限门禁（无 ai:read 不渲染）、批量分诊建议渲染与逐行/全部采纳写回、
 * 按建议分派、批量草稿复制（只读不自动评论）、超限拦截、失败埋点。
 */

import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Ticket } from '@/lib/api/types';

const mockUseCan = jest.fn((_spec?: unknown) => true);

jest.mock('@/components/auth/PermissionGate', () => ({
  useCan: (spec: unknown) => mockUseCan(spec),
}));

jest.mock('@/lib/ai/telemetry', () => ({ trackAssistantEvent: jest.fn() }));

jest.mock('@/lib/api/ticket-api', () => ({
  TicketApi: { updateTicket: jest.fn(), assignTicket: jest.fn() },
}));

jest.mock('@/lib/api/ai-api', () => {
  const actual = jest.requireActual('@/lib/api/ai-api');
  return {
    ...actual,
    aiBatchTriage: jest.fn(),
    aiBatchReplyDrafts: jest.fn(),
  };
});

import { TicketApi } from '@/lib/api/ticket-api';
import { aiBatchReplyDrafts, aiBatchTriage } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { TicketBatchAIPanel } from '../TicketBatchAIPanel';

const makeTicket = (id: number): Ticket => ({
  id,
  ticketNumber: `T-${id}`,
  title: `工单 ${id}`,
  description: '描述',
  priority: 'high',
  status: 'open',
  requesterId: 1,
  createdAt: '2026-10-18T00:00:00Z',
  updatedAt: '2026-10-18T00:00:00Z',
});

const tickets = [makeTicket(101), makeTicket(102)];

const clipboardWriteText = jest.fn().mockResolvedValue(undefined);

beforeEach(() => {
  jest.clearAllMocks();
  mockUseCan.mockReturnValue(true);
  Object.defineProperty(window.navigator, 'clipboard', {
    value: { writeText: clipboardWriteText },
    configurable: true,
  });
});

describe('TicketBatchAIPanel', () => {
  it('无 ai:read 时不渲染；权限具备时渲染入口', () => {
    mockUseCan.mockReturnValue(false);
    const { container } = render(<TicketBatchAIPanel selectedTickets={tickets} />);
    expect(container).toBeEmptyDOMElement();

    mockUseCan.mockReturnValue(true);
    render(<TicketBatchAIPanel selectedTickets={tickets} />);
    expect(screen.getByTestId('ticket-batch-ai-panel')).toBeInTheDocument();
    expect(screen.getByTestId('batch-ai-triage')).toBeInTheDocument();
    expect(screen.getByTestId('batch-ai-drafts')).toBeInTheDocument();
  });

  it('批量分诊：只读请求 + 建议渲染 + 逐行采纳写回既有 updateTicket + 埋点', async () => {
    (aiBatchTriage as jest.Mock).mockResolvedValue({
      items: [
        {
          ticketId: 101,
          status: 'ok',
          category: 'database',
          priority: 'high',
          assigneeId: 7,
          confidence: 0.8,
        },
        { ticketId: 102, status: 'not_found' },
      ],
      processed: 1,
      degraded: false,
    });
    (TicketApi.updateTicket as jest.Mock).mockResolvedValue({});
    (TicketApi.assignTicket as jest.Mock).mockResolvedValue({});

    render(<TicketBatchAIPanel selectedTickets={tickets} />);
    fireEvent.click(screen.getByTestId('batch-ai-triage'));

    await screen.findByTestId('batch-ai-triage-row-101');
    expect(aiBatchTriage).toHaveBeenCalledWith([101, 102]);
    expect(screen.getByText('数据库')).toBeInTheDocument();
    expect(screen.getByText(/不可用（工单不存在或无权访问）/)).toBeInTheDocument();
    expect(trackAssistantEvent).toHaveBeenCalledWith(
      'task_completed',
      expect.objectContaining({ taskCardId: 'batch_triage', outcomeType: 'entity' })
    );

    fireEvent.click(screen.getByTestId('batch-ai-accept-101'));
    await waitFor(() =>
      expect(TicketApi.updateTicket).toHaveBeenCalledWith(101, {
        category: 'database',
        priority: 'high',
      })
    );
    expect(trackAssistantEvent).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({ targetId: 101, taskCardId: 'batch_triage' })
    );

    fireEvent.click(screen.getByTestId('batch-ai-assign-101'));
    await waitFor(() =>
      expect(TicketApi.assignTicket).toHaveBeenCalledWith(101, { assigneeId: 7 })
    );
  });

  it('全部采纳 / 按建议分派：逐行串行写回所有可用行', async () => {
    (aiBatchTriage as jest.Mock).mockResolvedValue({
      items: [
        { ticketId: 101, status: 'ok', category: 'network', priority: 'medium', assigneeId: 7 },
        { ticketId: 102, status: 'ok', category: 'storage', priority: 'low', assigneeId: 8 },
      ],
      processed: 2,
      degraded: false,
    });
    (TicketApi.updateTicket as jest.Mock).mockResolvedValue({});
    (TicketApi.assignTicket as jest.Mock).mockResolvedValue({});

    const onApplied = jest.fn();
    render(<TicketBatchAIPanel selectedTickets={tickets} onApplied={onApplied} />);
    fireEvent.click(screen.getByTestId('batch-ai-triage'));
    await screen.findByTestId('batch-ai-accept-all');

    fireEvent.click(screen.getByTestId('batch-ai-accept-all'));
    await waitFor(() => expect(TicketApi.updateTicket).toHaveBeenCalledTimes(2));
    expect(onApplied).toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('batch-ai-assign-all'));
    await waitFor(() => expect(TicketApi.assignTicket).toHaveBeenCalledTimes(2));
  });

  it('批量草稿：只读生成、复制草稿并可读，不调用任何写端点', async () => {
    (aiBatchReplyDrafts as jest.Mock).mockResolvedValue({
      items: [
        { ticketId: 101, status: 'ok', draft: '您好，请提供报错截图，我们将尽快排查。' },
        { ticketId: 102, status: 'failed', reason: 'AI 草稿生成失败，请稍后重试' },
      ],
      succeeded: 1,
      degraded: true,
    });

    render(<TicketBatchAIPanel selectedTickets={tickets} />);
    fireEvent.click(screen.getByTestId('batch-ai-drafts'));

    await screen.findByTestId('batch-ai-draft-text-101');
    expect(aiBatchReplyDrafts).toHaveBeenCalledWith([101, 102]);
    expect(screen.getByText('您好，请提供报错截图，我们将尽快排查。')).toBeInTheDocument();
    expect(screen.getByText('AI 草稿生成失败，请稍后重试')).toBeInTheDocument();
    expect(TicketApi.updateTicket).not.toHaveBeenCalled();
    expect(TicketApi.assignTicket).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('batch-ai-copy-101'));
    await waitFor(() =>
      expect(clipboardWriteText).toHaveBeenCalledWith('您好，请提供报错截图，我们将尽快排查。')
    );
    expect(trackAssistantEvent).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({ targetId: 101, taskCardId: 'batch_draft' })
    );
  });

  it('超过分诊上限时拦截请求并提示', async () => {
    const many = Array.from({ length: 21 }, (_, index) => makeTicket(200 + index));
    render(<TicketBatchAIPanel selectedTickets={many} />);
    fireEvent.click(screen.getByTestId('batch-ai-triage'));

    await waitFor(() => expect(screen.getByText(/单次最多处理 20 张工单/)).toBeInTheDocument());
    expect(aiBatchTriage).not.toHaveBeenCalled();
  });

  it('批量草稿失败：不渲染结果并上报 task_failed', async () => {
    (aiBatchReplyDrafts as jest.Mock).mockRejectedValue(new Error('boom'));

    render(<TicketBatchAIPanel selectedTickets={tickets} />);
    fireEvent.click(screen.getByTestId('batch-ai-drafts'));

    await waitFor(() =>
      expect(trackAssistantEvent).toHaveBeenCalledWith(
        'task_failed',
        expect.objectContaining({ taskCardId: 'batch_draft', errorCode: 'batch_draft_failed' })
      )
    );
    expect(screen.queryByTestId('batch-ai-draft-result')).not.toBeInTheDocument();
  });
});