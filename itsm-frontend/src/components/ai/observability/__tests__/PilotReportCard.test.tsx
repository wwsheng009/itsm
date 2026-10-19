/**
 * V0-04 试点对照报告卡测试：门槛标签/指标表/负向表/导出 Markdown/降级。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import PilotReportCard from '../PilotReportCard';
import { aiGetPilotReport, type PilotReport } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { downloadTextFile } from '@/lib/utils/download-text-file';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetPilotReport: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
jest.mock('@/lib/utils/download-text-file', () => ({
  __esModule: true,
  downloadTextFile: jest.fn(() => true),
}));

const mockedGet = aiGetPilotReport as jest.MockedFunction<typeof aiGetPilotReport>;
const mockedTrack = trackAssistantEvent as jest.Mock;
const mockedDownload = downloadTextFile as jest.MockedFunction<typeof downloadTextFile>;

const metric = (overrides: Partial<PilotReport['sections'][0]['metrics'][0]> = {}) => ({
  key: 'inline_adoption_rate',
  title: 'Inline 采纳率',
  unit: 'rate' as const,
  direction: 'up' as const,
  kind: 'primary' as const,
  baseline: 0.5,
  current: 0.8,
  delta: 0.3,
  improvement: 0.6,
  comparable: true,
  improved: true,
  sample: 100,
  ...overrides,
});

const report: PilotReport = {
  tenantId: 1,
  current: { days: 7, since: '2026-10-11T12:00:00Z', until: '2026-10-18T12:00:00Z' },
  baseline: { days: 7, since: '2026-10-04T12:00:00Z', until: '2026-10-11T12:00:00Z' },
  generatedAt: '2026-10-18T12:00:00Z',
  sections: [
    {
      key: 'ticket_inline',
      pilot: 'V0-01',
      title: '工单 Inline 处理',
      met: true,
      reasons: ['达标：主指标相对基线改善 ≥30%'],
      metrics: [metric()],
    },
    {
      key: 'self_service_create',
      pilot: 'V0-02',
      title: '自助问答 + 一句话建单',
      met: true,
      reasons: ['达标'],
      metrics: [
        metric({
          key: 'self_service_resolve_rate',
          title: '自助解决率',
          baseline: 0.3,
          current: 0.6,
          delta: 0.3,
          improvement: 1,
        }),
      ],
    },
    {
      key: 'change_impact_plan',
      pilot: 'V0-03',
      title: '变更影响分析',
      met: false,
      reasons: ['计划采纳率：样本不足或基线为 0，不可比（本期样本 0）'],
      metrics: [
        metric({
          key: 'plan_adoption_rate',
          title: '计划采纳率',
          baseline: 0,
          current: 0,
          comparable: false,
          improved: false,
          sample: 0,
        }),
      ],
    },
  ],
  negative: [
    metric({
      key: 'suggestion_ignore_rate',
      title: '建议忽略率',
      direction: 'down',
      kind: 'negative',
      baseline: 0.3,
      current: 0.2,
      improvement: 0.333,
    }),
  ],
  gate: {
    passed: true,
    pilotsMet: 2,
    required: 2,
    reasons: ['达标：≥2 项试点改善 ≥30% 且负向指标未恶化，可按 §7 顺序放量 Dock'],
    thresholds: ['§11.5：≥2 项试点相对基线改善 ≥30%', '§11.5：负向指标不恶化'],
  },
  notes: [],
};

beforeEach(() => {
  mockedGet.mockReset();
  mockedTrack.mockReset();
  mockedDownload.mockReset();
  mockedDownload.mockReturnValue(true);
});

describe('PilotReportCard（V0-04）', () => {
  it('渲染门槛结论、三项试点标签、指标对照表与负向指标表', async () => {
    mockedGet.mockResolvedValue(report);

    render(<PilotReportCard days={7} />);

    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ currentDays: 7, baselineDays: 7 }));
    expect(await screen.findByTestId('pilot-report-gate')).toHaveTextContent('达标（可放量）');
    expect(screen.getByTestId('pilot-report-gate')).toHaveTextContent('2/2');

    expect(screen.getByTestId('pilot-section-ticket_inline')).toHaveTextContent('V0-01');
    expect(screen.getByTestId('pilot-section-change_impact_plan')).toHaveTextContent('未达标');

    const table = screen.getByTestId('pilot-report-table');
    expect(table).toHaveTextContent('Inline 采纳率');
    expect(table).toHaveTextContent('50.0%');
    expect(table).toHaveTextContent('80.0%');
    expect(table).toHaveTextContent('+60.0%');
    // 不可比指标 → 样本不足。
    expect(table).toHaveTextContent('样本不足');

    expect(screen.getByTestId('pilot-report-negative')).toHaveTextContent('建议忽略率');
    expect(screen.getByTestId('pilot-report-gate-reasons')).toHaveTextContent('可按 §7 顺序放量 Dock');
    expect(screen.getByTestId('pilot-report-thresholds')).toHaveTextContent('≥2 项试点相对基线改善 ≥30%');
  });

  it('导出：下载 Markdown 报告并记录 handoff 埋点', async () => {
    mockedGet.mockResolvedValue(report);

    render(<PilotReportCard days={7} />);
    await screen.findByTestId('pilot-report-gate');

    fireEvent.click(screen.getByTestId('pilot-report-export'));

    await waitFor(() =>
      expect(mockedDownload).toHaveBeenCalledWith('ai-pilot-report-7d.md', expect.stringContaining('# 试点对照报告'))
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ source: 'pilot_report_export', pageKey: 'ai.observability' })
    );
  });

  it('窗口联动：days 变化按新窗口重取', async () => {
    mockedGet.mockResolvedValue({ ...report, current: { ...report.current, days: 30 } });

    const { rerender } = render(<PilotReportCard days={7} />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ currentDays: 7, baselineDays: 7 }));

    rerender(<PilotReportCard days={30} />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ currentDays: 30, baselineDays: 30 }));
  });

  it('埋点服务未启用（503）：提示未启用且导出禁用（不抛错）', async () => {
    mockedGet.mockRejectedValue({ response: { status: 503 } });

    render(<PilotReportCard days={7} />);

    expect(await screen.findByTestId('pilot-report-disabled')).toBeInTheDocument();
    expect(screen.getByTestId('pilot-report-export')).toBeDisabled();
    expect(screen.queryByTestId('pilot-report-gate')).not.toBeInTheDocument();
  });
});