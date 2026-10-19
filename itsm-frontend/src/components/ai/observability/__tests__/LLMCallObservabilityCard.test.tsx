/**
 * LLM 调用历史卡测试：汇总、错误分类、调用明细与筛选参数。
 */
import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';

import LLMCallObservabilityCard from '../LLMCallObservabilityCard';
import {
  aiGetLLMCallSummary,
  aiGetLLMCalls,
  type LLMCallList,
  type LLMCallSummary,
} from '@/lib/api/ai-api';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetLLMCallSummary: jest.fn(),
  aiGetLLMCalls: jest.fn(),
}));

const mockedSummary = aiGetLLMCallSummary as jest.MockedFunction<typeof aiGetLLMCallSummary>;
const mockedCalls = aiGetLLMCalls as jest.MockedFunction<typeof aiGetLLMCalls>;

const summaryFixture: LLMCallSummary = {
  windowDays: 7,
  since: '2026-10-12T00:00:00Z',
  generatedAt: '2026-10-19T00:00:00Z',
  total: 2,
  success: 1,
  failed: 1,
  successRate: 0.5,
  avgLatencyMs: 350,
  tokens: 300,
  tokensEstimated: true,
  byProvider: [{ key: 'p1', count: 2, success: 1, successRate: 0.5, avgLatencyMs: 350, tokens: 300 }],
  byTask: [{ key: 'chat', count: 2, success: 1, successRate: 0.5, avgLatencyMs: 350, tokens: 300 }],
  byModel: [{ key: 'gpt-4o', count: 1, success: 0, successRate: 0, avgLatencyMs: 500, tokens: 200 }],
  byErrorClass: [
    { key: 'rate_limited', count: 1, success: 0, successRate: 0, avgLatencyMs: 500, tokens: 200 },
  ],
};

const callsFixture: LLMCallList = {
  total: 2,
  page: 1,
  pageSize: 20,
  items: [
    {
      id: 2,
      createdAt: '2026-10-19T12:00:00Z',
      tenantId: 1,
      providerKey: 'p1',
      provider: 'openai_chat_completions',
      providerSource: 'tenant',
      model: 'gpt-4o',
      taskType: 'chat',
      tokens: 200,
      tokensEstimated: true,
      latencyMs: 500,
      success: false,
      errorClass: 'rate_limited',
      httpStatus: 429,
      errorMessage: 'upstream 429 rate limited',
    },
    {
      id: 1,
      createdAt: '2026-10-19T11:00:00Z',
      tenantId: 1,
      providerKey: 'p1',
      provider: 'openai_chat_completions',
      providerSource: 'tenant',
      model: 'gpt-4o-mini',
      taskType: 'chat',
      tokens: 100,
      tokensEstimated: true,
      latencyMs: 200,
      success: true,
      errorClass: '',
      errorMessage: '',
    },
  ],
};

describe('LLMCallObservabilityCard', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedSummary.mockResolvedValue(summaryFixture);
    mockedCalls.mockResolvedValue(callsFixture);
  });

  it('渲染调用汇总、错误分类与明细', async () => {
    render(<LLMCallObservabilityCard days={7} />);

    await waitFor(() => {
      expect(screen.getByTestId('llm-calls-total')).toHaveTextContent('2');
    });
    expect(screen.getByTestId('llm-calls-success-rate')).toHaveTextContent('50.0%');
    expect(screen.getByTestId('llm-calls-failed')).toHaveTextContent('1');
    expect(screen.getByTestId('llm-calls-avg-latency')).toHaveTextContent('350 ms');
    expect(screen.getByTestId('llm-calls-error-distribution')).toHaveTextContent('rate_limited');
    expect(screen.getByText('gpt-4o-mini')).toBeInTheDocument();
    expect(screen.getByText('gpt-4o')).toBeInTheDocument();
    expect(screen.getAllByTestId('llm-call-error-class')[0]).toHaveTextContent('rate_limited');
    expect(screen.getAllByTestId('llm-call-tokens')[0]).toHaveTextContent('~200');

    expect(mockedSummary).toHaveBeenCalledWith(
      expect.objectContaining({ days: 7, success: undefined })
    );
    expect(mockedCalls).toHaveBeenCalledWith(
      expect.objectContaining({ days: 7, page: 1, pageSize: 20 })
    );
  });

  it('空窗口显示空态', async () => {
    mockedSummary.mockResolvedValue({ ...summaryFixture, total: 0, success: 0, failed: 0, successRate: 0 });
    mockedCalls.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 });

    render(<LLMCallObservabilityCard days={7} />);

    await waitFor(() => {
      expect(screen.getByTestId('llm-calls-empty')).toBeInTheDocument();
    });
  });
});
