/**
 * P2 文档命中率/解决率运营报表卡测试：比率/缺口表/覆盖率渲染、无分母「—」、埋点降级、导出 Markdown。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import DocMetricsCard from '../DocMetricsCard';
import { aiGetDocMetrics, type DocMetricsReport } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { downloadTextFile } from '@/lib/utils/download-text-file';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetDocMetrics: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
jest.mock('@/lib/utils/download-text-file', () => ({
  __esModule: true,
  downloadTextFile: jest.fn(() => true),
}));

const mockedGet = aiGetDocMetrics as jest.MockedFunction<typeof aiGetDocMetrics>;
const mockedTrack = trackAssistantEvent as jest.Mock;
const mockedDownload = downloadTextFile as jest.MockedFunction<typeof downloadTextFile>;

const report: DocMetricsReport = {
  days: 7,
  totals: {
    opened: 8,
    gaps: 1,
    drafts: 2,
    published: 1,
    feedbackHelpful: 5,
    feedbackUnhelpful: 1,
    handoffs: 1,
    openedSessions: 8,
    gapSessions: 1,
    handoffSessions: 1,
    events: 17,
  },
  rates: { hitRate: 0.875, resolveRate: 0.875, feedbackResolveRate: 5 / 6, draftPublishRate: 0.5 },
  byPage: [
    { pageKey: 'dashboard', opened: 4, gaps: 1, drafts: 1, published: 1, helpful: 3, unhelpful: 1 },
    { pageKey: 'ticket.list', opened: 4, gaps: 0, drafts: 1, published: 0, helpful: 2, unhelpful: 0 },
  ],
  trend: [{ day: '2026-10-18', opened: 8, gaps: 1, drafts: 2, published: 1, helpful: 5, unhelpful: 1 }],
  coverage: { coverage: 0.81, totals: { pages: 21, ok: 17, missing: 3, stale: 1, orphan: 0 } },
  telemetry: { available: true },
  generatedAt: '2026-10-18T12:00:00Z',
};

beforeEach(() => {
  mockedGet.mockReset();
  mockedTrack.mockReset();
  mockedDownload.mockReset();
});

describe('DocMetricsCard（P2 文档命中率/解决率运营报表）', () => {
  it('渲染比率、缺口页 Top 与账本覆盖率（按 days 请求）', async () => {
    mockedGet.mockResolvedValue(report);

    render(<DocMetricsCard days={7} />);

    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ days: 7 }));
    expect(await screen.findByTestId('doc-metrics-hit-rate')).toHaveTextContent('87.5%');
    expect(screen.getByTestId('doc-metrics-resolve-rate')).toHaveTextContent('87.5%');
    expect(screen.getByTestId('doc-metrics-feedback-rate')).toHaveTextContent('83.3%');
    expect(screen.getByTestId('doc-metrics-opened')).toHaveTextContent('8');
    expect(screen.getByTestId('doc-metrics-gaps')).toHaveTextContent('1');
    expect(screen.getByTestId('doc-metrics-feedback')).toHaveTextContent('5 / 1');
    expect(screen.getByTestId('doc-metrics-coverage')).toHaveTextContent('81.0%');
    expect(screen.getByTestId('doc-metrics-draft-publish-rate')).toHaveTextContent('50.0%');
    expect(screen.getByTestId('doc-metrics-published')).toHaveTextContent('1');
    expect(screen.getByTestId('doc-metrics-by-page')).toHaveTextContent('dashboard');
    expect(screen.getByTestId('doc-metrics-by-page')).toHaveTextContent('ticket.list');
  });

  it('无有效分母：比率显示「—」，不虚报 0%', async () => {
    mockedGet.mockResolvedValue({
      ...report,
      totals: {
        opened: 0,
        gaps: 0,
        drafts: 0,
        published: 0,
        feedbackHelpful: 0,
        feedbackUnhelpful: 0,
        handoffs: 0,
        openedSessions: 0,
        gapSessions: 0,
        handoffSessions: 0,
        events: 0,
      },
      rates: {},
      byPage: [],
      trend: [],
    });

    render(<DocMetricsCard days={30} />);

    expect(await screen.findByTestId('doc-metrics-hit-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('doc-metrics-resolve-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('doc-metrics-feedback-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('doc-metrics-draft-publish-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('doc-metrics-empty')).toBeInTheDocument();
  });

  it('埋点未启用：提示仅覆盖率，但覆盖率照常渲染', async () => {
    mockedGet.mockResolvedValue({
      ...report,
      rates: {},
      telemetry: { available: false },
    });

    render(<DocMetricsCard days={30} />);

    expect(await screen.findByTestId('doc-metrics-telemetry-disabled')).toBeInTheDocument();
    expect(screen.getByTestId('doc-metrics-coverage')).toHaveTextContent('81.0%');
    expect(screen.getByTestId('doc-metrics-hit-rate')).toHaveTextContent('—');
  });

  it('导出 Markdown：下载快照 + handoff 埋点（doc_metrics_export）', async () => {
    mockedGet.mockResolvedValue(report);

    render(<DocMetricsCard days={7} pageKey="ai.observability" />);

    fireEvent.click(await screen.findByTestId('doc-metrics-export'));

    await waitFor(() =>
      expect(mockedDownload).toHaveBeenCalledWith(
        'ai-doc-metrics-7d.md',
        expect.stringContaining('# 帮助文档命中率 / 解决率运营报表')
      )
    );
    expect(mockedDownload.mock.calls[0][1]).toContain('| 文档命中率（未产生缺口的会话占比） | 87.5% |');
    expect(mockedDownload.mock.calls[0][1]).toContain('| 起草落地率（成文 / 起草动作） | 50.0% |');
    expect(mockedDownload.mock.calls[0][1]).toContain('| dashboard | 4 | 1 | 1 | 1 | 3 | 1 |');
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ source: 'doc_metrics_export', pageKey: 'ai.observability' })
    );
  });

  it('加载失败：展示错误提示（不抛出）', async () => {
    mockedGet.mockRejectedValue(new Error('boom'));

    render(<DocMetricsCard days={7} />);

    expect(await screen.findByTestId('doc-metrics-error')).toHaveTextContent('文档运营报表加载失败');
  });
});