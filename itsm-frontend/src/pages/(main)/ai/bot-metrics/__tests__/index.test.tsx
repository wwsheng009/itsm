/**
 * Bot 运行指标看板测试（B4-02）。
 *
 * 覆盖：KPI 渲染（成功率/确认率/回读失败率/工具错误率）、成本代理提示（token 未接线）、
 * 入口/Bot 分解表、`bot.enabled=false`（503）→「能力未启用」、其他错误 → 错误提示。
 */
import { render, screen, waitFor } from '@testing-library/react';

import BotMetricsPage from '..';
import { aiGetBotMetrics, type BotMetrics } from '@/lib/api/ai-api';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetBotMetrics: jest.fn(),
}));

const mockedGet = aiGetBotMetrics as jest.MockedFunction<typeof aiGetBotMetrics>;

const fixture: BotMetrics = {
  windowDays: 7,
  since: '2026-09-20T00:00:00Z',
  generatedAt: '2026-09-27T12:00:00Z',
  runs: { total: 5, completed: 2, failed: 1, running: 1, cancelled: 1, successRate: 0.667, avgDurationMs: 20000 },
  steps: { total: 11, llmSteps: 5, toolSteps: 5, confirmSteps: 1, avgPerRun: 2.2, avgDurationMs: 168.18 },
  tools: { total: 3, errors: 1, errorRate: 0.333, avgDurationMs: 366.67, topTools: [{ key: 'list_tickets', count: 1 }] },
  verify: { verified: 1, failed: 1, skipped: 1, pending: 1, failRate: 0.5 },
  confirmations: {
    approved: 1,
    rejected: 1,
    expired: 1,
    pending: 1,
    approvalRate: 0.333,
    rejectRate: 0.333,
    expireRate: 0.333,
    avgDecisionMs: 150000,
  },
  cost: { llmCalls: 5, toolCalls: 3, steps: 11, avgStepsPerRun: 2.2, avgToolCallsPerRun: 0.6 },
  byEntrypoint: [
    { key: 'chat', runs: 3, failed: 1, successRate: 0.667, avgDurationMs: 20000, toolCalls: 2, toolErrors: 0 },
    { key: 'ticket_detail', runs: 1, failed: 0, successRate: 1, avgDurationMs: 20000, toolCalls: 0, toolErrors: 0 },
  ],
  byBot: [
    { key: 'bot#7', runs: 2, failed: 1, successRate: 0.5, avgDurationMs: 15000, toolCalls: 3, toolErrors: 1 },
    { key: 'compat-default', runs: 2, failed: 0, successRate: 1, avgDurationMs: 25000, toolCalls: 0, toolErrors: 0 },
  ],
  tokensRecorded: false,
  notes: ['token 计量未接线（B1-02 遗留）：成本维度以步数/工具调用数/时延为代理'],
};

describe('BotMetricsPage（B4-02 看板）', () => {
  beforeEach(() => {
    mockedGet.mockReset();
  });

  it('渲染 KPI、成本代理提示与分解表', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<BotMetricsPage />);

    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ days: 7, entrypoint: undefined }));
    expect(await screen.findByTestId('kpi-run-success')).toHaveTextContent('66.7%');
    expect(screen.getByTestId('kpi-approval-rate')).toHaveTextContent('33.3%');
    expect(screen.getByTestId('kpi-verify-fail')).toHaveTextContent('50.0%');
    expect(screen.getByTestId('kpi-tool-error')).toHaveTextContent('33.3%');

    // token 未接线提示 + 成本代理标签。
    expect(screen.getByTestId('bot-metrics-token-note')).toBeInTheDocument();
    expect(screen.getByTestId('cost-proxy-tag')).toBeInTheDocument();

    // 分解表：入口 chat=3、Bot bot#7=2。
    const entryTable = screen.getByTestId('bot-metrics-by-entrypoint');
    expect(entryTable).toHaveTextContent('chat');
    expect(entryTable).toHaveTextContent('66.7%');
    expect(entryTable).toHaveTextContent('100.0%'); // ticket_detail：1 运行 0 失败
    expect(screen.getByTestId('bot-metrics-by-bot')).toHaveTextContent('bot#7');
    expect(screen.getByTestId('bot-metrics-by-bot')).toHaveTextContent('compat-default');
  });

  it('bot.enabled=false（503）时展示能力未启用提示，不报错', async () => {
    mockedGet.mockRejectedValue({ response: { status: 503 } });

    render(<BotMetricsPage />);

    expect(await screen.findByTestId('bot-metrics-disabled')).toHaveTextContent('Bot 能力未启用');
    expect(screen.queryByTestId('kpi-run-success')).not.toBeInTheDocument();
  });

  it('其他错误展示错误提示并可重试', async () => {
    mockedGet.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(fixture);

    render(<BotMetricsPage />);

    expect(await screen.findByTestId('bot-metrics-error')).toHaveTextContent('boom');
  });
});
