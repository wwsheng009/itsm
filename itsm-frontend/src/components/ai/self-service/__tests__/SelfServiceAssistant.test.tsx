/**
 * V0-02 员工侧自助问答测试：命中/未命中、已解决、转建单（含埋点与负向断言）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import SelfServiceAssistant from '../SelfServiceAssistant';
import { aiSearchKB, type RagAnswer } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiSearchKB: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedSearch = aiSearchKB as jest.MockedFunction<typeof aiSearchKB>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const answer = (overrides: Partial<RagAnswer> = {}): RagAnswer => ({
  objectType: 'kb',
  id: 11,
  title: 'VPN 无法连接排查',
  category: '网络',
  snippet: '先检查客户端版本，再重建隧道。',
  score: 0.87,
  ...overrides,
});

const renderAssistant = () => {
  const onProceed = jest.fn();
  render(<SelfServiceAssistant pageKey="ticket.aiCreate" onProceedToCreate={onProceed} />);
  return { onProceed };
};

const ask = (text = 'VPN 连不上怎么办？') => {
  fireEvent.change(screen.getByTestId('self-service-input'), { target: { value: text } });
  fireEvent.click(screen.getByTestId('self-service-ask'));
};

beforeEach(() => {
  mockedSearch.mockReset();
  mockedTrack.mockReset();
});

describe('SelfServiceAssistant（V0-02 员工侧自助问答）', () => {
  it('命中：渲染答案与匹配度并埋点 self_service_answered；不自动建单', async () => {
    mockedSearch.mockResolvedValue({ answers: [answer()] });
    const { onProceed } = renderAssistant();

    ask();
    expect(await screen.findByTestId('self-service-answer-11')).toHaveTextContent('VPN 无法连接排查');
    expect(screen.getByTestId('self-service-answer-11')).toHaveTextContent('先检查客户端版本');
    expect(screen.getByTestId('self-service-answer-11')).toHaveTextContent('匹配 0.87');
    expect(mockedSearch).toHaveBeenCalledWith('VPN 连不上怎么办？', 3);
    expect(mockedTrack).toHaveBeenCalledWith(
      'self_service_answered',
      expect.objectContaining({
        pageKey: 'ticket.aiCreate',
        source: 'self_service',
        extra: expect.objectContaining({ results: 1 }),
      })
    );
    // 负向：检索命中不触发建单（用户必须显式选择）。
    expect(onProceed).not.toHaveBeenCalled();
  });

  it('已解决：埋点 self_service_resolved 并显示不建单提示', async () => {
    mockedSearch.mockResolvedValue({ answers: [answer()] });
    const { onProceed } = renderAssistant();

    ask();
    fireEvent.click(await screen.findByTestId('self-service-resolved'));

    expect(await screen.findByTestId('self-service-resolved-hint')).toHaveTextContent(
      '已记录：本次自助解决（不建单）'
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'self_service_resolved',
      expect.objectContaining({ pageKey: 'ticket.aiCreate', source: 'self_service' })
    );
    expect(onProceed).not.toHaveBeenCalled();
  });

  it('还没解决：埋点 self_service_ticket_created 并把问题描述带回建单意图', async () => {
    mockedSearch.mockResolvedValue({ answers: [answer()] });
    const { onProceed } = renderAssistant();

    ask('打印机卡纸');
    fireEvent.click(await screen.findByTestId('self-service-proceed'));

    expect(mockedTrack).toHaveBeenCalledWith(
      'self_service_ticket_created',
      expect.objectContaining({ pageKey: 'ticket.aiCreate', source: 'self_service' })
    );
    expect(onProceed).toHaveBeenCalledWith('打印机卡纸');
  });

  it('未命中：提示可直接建单，点击「去建单」回传描述', async () => {
    mockedSearch.mockResolvedValue({ answers: [] });
    const { onProceed } = renderAssistant();

    ask('奇怪的报错');
    expect(await screen.findByTestId('self-service-empty')).toHaveTextContent('知识库暂未命中');

    fireEvent.click(screen.getByTestId('self-service-proceed-empty'));
    expect(onProceed).toHaveBeenCalledWith('奇怪的报错');
  });

  it('检索失败：按未命中降级（不抛错、仍可建单）', async () => {
    mockedSearch.mockRejectedValue(new Error('503'));
    const { onProceed } = renderAssistant();

    ask('服务异常');
    expect(await screen.findByTestId('self-service-empty')).toBeInTheDocument();
    expect(mockedTrack).toHaveBeenCalledWith(
      'self_service_answered',
      expect.objectContaining({ errorCode: 'search_failed' })
    );

    fireEvent.click(screen.getByTestId('self-service-proceed-empty'));
    expect(onProceed).toHaveBeenCalledWith('服务异常');
  });

  it('空输入：按钮禁用且不发起检索', () => {
    mockedSearch.mockResolvedValue({ answers: [] });
    renderAssistant();

    expect(screen.getByTestId('self-service-ask')).toBeDisabled();
    fireEvent.click(screen.getByTestId('self-service-ask'));
    expect(mockedSearch).not.toHaveBeenCalled();
  });
});