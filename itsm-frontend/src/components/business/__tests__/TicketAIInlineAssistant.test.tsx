/**
 * V0-01 Inline 三件套组件测试：草稿生成/失败/填入、知识推荐与引用、埋点。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { TicketAIInlineAssistant } from '../TicketAIInlineAssistant';
import { aiCreateReplyDraft, aiSearchKB } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiCreateReplyDraft: jest.fn(),
  aiSearchKB: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedCreate = aiCreateReplyDraft as jest.MockedFunction<typeof aiCreateReplyDraft>;
const mockedSearchKB = aiSearchKB as jest.MockedFunction<typeof aiSearchKB>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const kbItem = {
  objectType: 'kb',
  id: 11,
  title: 'SSO 登录排查',
  snippet: '检查账号状态与权限组绑定。',
  score: 0.9,
};

beforeEach(() => {
  mockedCreate.mockReset();
  mockedSearchKB.mockReset();
  mockedTrack.mockReset();
  mockedSearchKB.mockResolvedValue({ answers: [kbItem] });
  mockedCreate.mockResolvedValue({
    ticketId: 42,
    draft: '您好，请先检查权限组绑定，并提供登录时间。',
    citations: [{ title: 'SSO 登录排查', source: 'kb#11' }],
    degraded: false,
  });
});

const renderPanel = (onApplyReply?: (text: string) => void) =>
  render(
    <TicketAIInlineAssistant
      ticketId={42}
      title="无法登录"
      description="SSO 登录后提示权限不足"
      onApplyReply={onApplyReply}
    />
  );

describe('TicketAIInlineAssistant（V0-01 Inline 三件套）', () => {
  it('知识推荐自动检索并渲染，展示 inline_preview 埋点', async () => {
    renderPanel();

    expect(await screen.findByTestId('inline-kb-item-11')).toHaveTextContent('SSO 登录排查');
    expect(mockedSearchKB).toHaveBeenCalledWith('无法登录 SSO 登录后提示权限不足', 3);
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'inline_preview',
        expect.objectContaining({
          targetId: 42,
          extra: expect.objectContaining({ panel: 'kb' }),
        })
      )
    );
  });

  it('生成草稿成功：可编辑草稿 + task_completed/latency 埋点', async () => {
    renderPanel();
    fireEvent.click(screen.getByTestId('inline-generate-draft'));

    await waitFor(() =>
      expect((screen.getByTestId('inline-draft-input') as HTMLTextAreaElement).value).toContain(
        '请先检查权限组绑定'
      )
    );
    expect(mockedCreate).toHaveBeenCalledWith(42);
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'task_completed',
        expect.objectContaining({
          taskCardId: 'reply_draft',
          durationMs: expect.any(Number),
          extra: expect.objectContaining({ task: 'reply_draft' }),
        })
      )
    );
  });

  it('生成失败：错误提示 + task_failed/error_shown 埋点', async () => {
    mockedCreate.mockRejectedValue(new Error('AI 草稿生成失败，请稍后重试'));
    renderPanel();

    fireEvent.click(screen.getByTestId('inline-generate-draft'));

    expect(await screen.findByTestId('inline-draft-error')).toHaveTextContent('AI 草稿生成失败');
    await waitFor(() => expect(mockedTrack).toHaveBeenCalledWith('task_failed', expect.anything()));
    expect(mockedTrack).toHaveBeenCalledWith(
      'error_shown',
      expect.objectContaining({ errorCode: 'reply_draft_failed' })
    );
  });

  it('填入回复框：只回调文本，不提交（inline_applied fill 埋点）', async () => {
    const onApplyReply = jest.fn();
    renderPanel(onApplyReply);

    fireEvent.click(screen.getByTestId('inline-generate-draft'));
    await waitFor(() =>
      expect((screen.getByTestId('inline-draft-input') as HTMLTextAreaElement).value).not.toBe('')
    );

    fireEvent.click(screen.getByTestId('inline-apply-reply'));
    expect(onApplyReply).toHaveBeenCalledWith('您好，请先检查权限组绑定，并提供登录时间。');
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'inline_applied',
        expect.objectContaining({
          extra: expect.objectContaining({ panel: 'reply', action: 'fill' }),
        })
      )
    );
  });

  it('引用知识到草稿：追加来源片段并记录 inline_applied quote', async () => {
    renderPanel();
    await screen.findByTestId('inline-kb-item-11');

    fireEvent.click(screen.getByTestId('inline-quote-11'));

    await waitFor(() =>
      expect((screen.getByTestId('inline-draft-input') as HTMLTextAreaElement).value).toContain(
        '（引用：SSO 登录排查 kb#11）'
      )
    );
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'inline_applied',
        expect.objectContaining({
          extra: expect.objectContaining({ panel: 'kb', action: 'quote' }),
        })
      )
    );
  });
});