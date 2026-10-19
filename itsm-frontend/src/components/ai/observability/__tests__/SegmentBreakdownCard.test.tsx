/**
 * O2-01 分群对比卡测试：指标渲染、维度切换、窗口联动与降级。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import SegmentBreakdownCard from '../SegmentBreakdownCard';
import { aiGetAssistantSegments, type AssistantSegmentsReport } from '@/lib/api/ai-api';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetAssistantSegments: jest.fn(),
}));

const mockedGet = aiGetAssistantSegments as jest.MockedFunction<typeof aiGetAssistantSegments>;

const report: AssistantSegmentsReport = {
  tenantId: 1,
  days: 7,
  dimension: 'role',
  generatedAt: '2026-10-18T12:00:00Z',
  segments: [
    {
      key: 'agent',
      events: 200,
      sessions: 45,
      suggestionShown: 40,
      suggestionClicked: 12,
      taskStarted: 10,
      taskCompleted: 6,
      inlinePreview: 20,
      inlineApplied: 9,
      errors: 4,
      suggestionCtr: 0.3,
      taskCompletionRate: 0.6,
      inlineAdoptionRate: 0.45,
      errorRate: 0.02,
    },
  ],
};

beforeEach(() => {
  mockedGet.mockReset();
  mockedGet.mockResolvedValue(report);
});

describe('SegmentBreakdownCard（O2-01 分群对比）', () => {
  it('渲染分组行与子集指标（比率百分比化）', async () => {
    render(<SegmentBreakdownCard days={7} />);

    await waitFor(() =>
      expect(mockedGet).toHaveBeenCalledWith({ dimension: 'role', days: 7 })
    );
    const table = await screen.findByTestId('segment-table');
    expect(table).toHaveTextContent('agent');
    expect(table).toHaveTextContent('200');
    expect(table).toHaveTextContent('45');
    expect(table).toHaveTextContent('30.0%');
    expect(table).toHaveTextContent('60.0%');
    expect(table).toHaveTextContent('45.0%');
    expect(table).toHaveTextContent('2.0%');
  });

  it('维度切换：按页面 → 以 pageKey 重新请求', async () => {
    render(<SegmentBreakdownCard days={7} />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByText('按页面'));

    await waitFor(() =>
      expect(mockedGet).toHaveBeenLastCalledWith({ dimension: 'pageKey', days: 7 })
    );
  });

  it('窗口联动：days 变化按新窗口重取', async () => {
    const { rerender } = render(<SegmentBreakdownCard days={7} />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledWith({ dimension: 'role', days: 7 }));

    rerender(<SegmentBreakdownCard days={30} />);
    await waitFor(() =>
      expect(mockedGet).toHaveBeenLastCalledWith({ dimension: 'role', days: 30 })
    );
  });

  it('埋点未启用（503）：提示未启用；空窗口 → 空态提示', async () => {
    mockedGet.mockRejectedValueOnce({ response: { status: 503 } });
    render(<SegmentBreakdownCard days={7} />);
    expect(await screen.findByTestId('segment-disabled')).toBeInTheDocument();

    mockedGet.mockResolvedValue({ ...report, segments: [] });
    fireEvent.click(screen.getByText('按页面'));
    expect(await screen.findByTestId('segment-empty')).toBeInTheDocument();
  });
});