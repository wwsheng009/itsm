/**
 * V1-02 邮件场景测试：AI 理解邮件（只读）+ 采纳回填 + 未命中降级 + 失败不阻断 + 权限门禁。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import EmailIntentSuggest, {
  EMAIL_INTENT_SUGGEST_ADOPT_TESTID,
  EMAIL_INTENT_SUGGEST_RESULT_TESTID,
  EMAIL_INTENT_SUGGEST_RUN_TESTID,
  EMAIL_INTENT_SUGGEST_TESTID,
} from '../EmailIntentSuggest';
import { aiInterpretTicketIntent, type TicketIntentInterpretation } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useCan } from '@/components/auth/PermissionGate';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiInterpretTicketIntent: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
jest.mock('@/components/auth/PermissionGate', () => ({
  __esModule: true,
  useCan: jest.fn(() => true),
}));

const mockedInterpret = aiInterpretTicketIntent as jest.MockedFunction<
  typeof aiInterpretTicketIntent
>;
const mockedTrack = trackAssistantEvent as jest.Mock;
const mockedUseCan = useCan as jest.Mock;

const interpretation = (
  overrides: Partial<TicketIntentInterpretation> = {}
): TicketIntentInterpretation => ({
  summary: '网络故障（高）',
  matched: true,
  needsConfirmation: true,
  confidence: 0.8,
  terms: [
    { field: 'type', surface: '连不上网', term: '网络故障', value: 'network', confidence: 0.8 },
    { field: 'priority', surface: '很急', term: '高', value: 'high', confidence: 0.7 },
  ],
  fields: { title: '网络故障', type: 'network', priority: 'high', description: '连不上网，很急' },
  ...overrides,
});

const renderSuggest = (text = '邮件标题：连不上网\n正文：办公区很急，帮忙处理') => {
  const onAdopt = jest.fn();
  render(<EmailIntentSuggest text={text} onAdopt={onAdopt} pageKey="email.intake" />);
  return { onAdopt };
};

const run = () => fireEvent.click(screen.getByTestId(EMAIL_INTENT_SUGGEST_RUN_TESTID));

beforeEach(() => {
  jest.clearAllMocks();
  mockedUseCan.mockImplementation(() => true);
});

describe('EmailIntentSuggest（V1-02 邮件场景）', () => {
  it('无 ai:read ∪ ticket:read 权限不渲染（入口级门禁）', () => {
    mockedUseCan.mockImplementation((codes: string[]) => !codes.includes('ai:read'));
    renderSuggest();

    expect(screen.queryByTestId(EMAIL_INTENT_SUGGEST_TESTID)).not.toBeInTheDocument();
    expect(mockedInterpret).not.toHaveBeenCalled();
  });

  it('运行理解：展示「我理解成」+ 证据片段 + 置信度；采纳只回填、不保存', async () => {
    mockedInterpret.mockResolvedValue(interpretation());
    const { onAdopt } = renderSuggest();

    run();

    expect(await screen.findByTestId(EMAIL_INTENT_SUGGEST_RESULT_TESTID)).toHaveTextContent(
      '我理解成：网络故障（高）'
    );
    expect(screen.getByTestId(EMAIL_INTENT_SUGGEST_RESULT_TESTID)).toHaveTextContent('连不上网 → 网络故障');
    expect(screen.getByTestId(EMAIL_INTENT_SUGGEST_RESULT_TESTID)).toHaveTextContent('置信度 80%');
    // 送理解的文本先裁到 500 字内（邮件正文可能超长）
    expect(mockedInterpret).toHaveBeenCalledWith(expect.stringContaining('连不上网'));
    expect((mockedInterpret.mock.calls[0][0] as string).length).toBeLessThanOrEqual(500);
    expect(mockedTrack).toHaveBeenCalledWith(
      'suggestion_shown',
      expect.objectContaining({
        pageKey: 'email.intake',
        source: 'email_intent',
        extra: expect.objectContaining({ card_id: 'email_intent', matched: true }),
      })
    );

    fireEvent.click(screen.getByTestId(EMAIL_INTENT_SUGGEST_ADOPT_TESTID));

    expect(onAdopt).toHaveBeenCalledWith(
      expect.objectContaining({ title: '网络故障', description: '连不上网，很急' })
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'suggestion_clicked',
      expect.objectContaining({ source: 'email_intent' })
    );
  });

  it('未命中（matched=false）：如实标注兜底口径，仍可采纳或人工修正', async () => {
    mockedInterpret.mockResolvedValue(
      interpretation({
        summary: '其他（中）',
        matched: false,
        confidence: 0.35,
        terms: [],
        fields: { title: '其他', type: 'other', priority: 'medium', description: '原文' },
      })
    );
    const { onAdopt } = renderSuggest('随便聊聊');

    run();

    expect(await screen.findByText(/未识别到明确术语/)).toBeInTheDocument();
    expect(screen.getByTestId(EMAIL_INTENT_SUGGEST_RESULT_TESTID)).toHaveTextContent('置信度 35%');
    fireEvent.click(screen.getByTestId(EMAIL_INTENT_SUGGEST_ADOPT_TESTID));
    expect(onAdopt).toHaveBeenCalled();
  });

  it('理解失败：不展示建议、不阻断复核（error_shown；可重试）', async () => {
    mockedInterpret.mockRejectedValue(new Error('503 service unavailable'));
    const { onAdopt } = renderSuggest();

    run();

    expect(await screen.findByText(/理解失败/)).toBeInTheDocument();
    expect(screen.queryByTestId(EMAIL_INTENT_SUGGEST_RESULT_TESTID)).not.toBeInTheDocument();
    expect(screen.queryByTestId(EMAIL_INTENT_SUGGEST_ADOPT_TESTID)).not.toBeInTheDocument();
    expect(onAdopt).not.toHaveBeenCalled();
    expect(mockedTrack).toHaveBeenCalledWith(
      'error_shown',
      expect.objectContaining({ source: 'email_intent', errorCode: 'interpret_failed' })
    );
    // 可重试：按钮仍在
    expect(screen.getByTestId(EMAIL_INTENT_SUGGEST_RUN_TESTID)).toBeInTheDocument();
    await waitFor(() => expect(mockedInterpret).toHaveBeenCalledTimes(1));
  });

  it('无可理解文本（空白）时不渲染（边界）', () => {
    renderSuggest('   ');

    expect(screen.queryByTestId(EMAIL_INTENT_SUGGEST_TESTID)).not.toBeInTheDocument();
  });
});