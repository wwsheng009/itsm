/**
 * V0-02 一句话建单度量单测：字段纠正 diff 与流程收口事件（时长/误分类率代理）。
 */
import { AICreateFlowTracker, diffTicketFields } from '../ai-create-metrics';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

beforeEach(() => {
  mockedTrack.mockReset();
});

describe('diffTicketFields（误分类率代理口径）', () => {
  it('仅统计 type/priority 的变化；相同/缺省不计', () => {
    expect(diffTicketFields({ type: 'hardware', priority: 'medium' }, { type: 'software', priority: 'high' })).toEqual(['type', 'priority']);
    expect(diffTicketFields({ type: 'hardware' }, { type: 'hardware', priority: 'high' })).toEqual([]);
    expect(diffTicketFields({}, { type: 'software' })).toEqual([]);
    expect(diffTicketFields({ type: 'hardware' }, {})).toEqual([]);
  });
});

describe('AICreateFlowTracker（建单时长 + 漏斗）', () => {
  it('start → captureSuggested → finishSuccess：先报字段纠正再报任务完成（latencyMs=durationMs）', () => {
    let nowMs = 1_000;
    const tracker = new AICreateFlowTracker(() => nowMs);

    tracker.start('ticket.aiCreate');
    expect(tracker.isRunning()).toBe(true);
    expect(mockedTrack).toHaveBeenNthCalledWith(
      1,
      'task_started',
      expect.objectContaining({ pageKey: 'ticket.aiCreate', taskCardId: 'ai_create_ticket' })
    );

    tracker.captureSuggested({ type: 'hardware', priority: 'medium' });
    nowMs = 4_500;
    tracker.finishSuccess({ type: 'software', priority: 'medium' }, 'ticket.aiCreate');

    expect(mockedTrack).toHaveBeenNthCalledWith(
      2,
      'ai_create_field_corrected',
      expect.objectContaining({ field: 'type', taskCardId: 'ai_create_ticket' })
    );
    expect(mockedTrack).toHaveBeenNthCalledWith(
      3,
      'task_completed',
      expect.objectContaining({ durationMs: 3500, latencyMs: 3500, taskCardId: 'ai_create_ticket' })
    );
    expect(tracker.isRunning()).toBe(false);
  });

  it('未纠正字段时只报任务完成', () => {
    const tracker = new AICreateFlowTracker(() => 0);
    tracker.start();
    tracker.captureSuggested({ type: 'hardware' });
    tracker.finishSuccess({ type: 'hardware' });

    const names = mockedTrack.mock.calls.map(call => call[0]);
    expect(names).toEqual(['task_started', 'task_completed']);
  });

  it('失败收口：task_failed 带 errorCode+durationMs；二次收口不重复上报', () => {
    let nowMs = 10;
    const tracker = new AICreateFlowTracker(() => nowMs);
    tracker.start();
    nowMs = 510;
    tracker.finishFailure('submit_failed');
    tracker.finishFailure('submit_failed');

    expect(mockedTrack).toHaveBeenCalledTimes(2);
    expect(mockedTrack).toHaveBeenLastCalledWith(
      'task_failed',
      expect.objectContaining({ errorCode: 'submit_failed', durationMs: 500 })
    );
  });

  it('未 start 直接收口 → 零事件（不产生悬挂样本）', () => {
    const tracker = new AICreateFlowTracker(() => 0);
    tracker.finishSuccess({ type: 'software' });
    tracker.finishFailure('boom');
    expect(mockedTrack).not.toHaveBeenCalled();
  });
});