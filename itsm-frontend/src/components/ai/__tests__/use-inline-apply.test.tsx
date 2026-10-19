/**
 * I1-01 useInlineApplyFlow 测试：diff 预览 → 确认应用 → 撤销；负向断言「未确认不应用」；失败保留待确认。
 */
import { act, renderHook } from '@testing-library/react';

import { useInlineApplyFlow, type InlineChangeSet } from '../use-inline-apply';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

const changes: InlineChangeSet[] = [
  { field: 'category', label: '分类', before: 'general', after: 'network' },
  { field: 'priority', label: '优先级', before: 'medium', after: 'high' },
];

beforeEach(() => mockedTrack.mockReset());

describe('useInlineApplyFlow（I1-01 Inline Apply）', () => {
  it('负向：仅预览（未确认）不调用 onApply；命中 inline_preview 埋点', () => {
    const onApply = jest.fn();
    const { result } = renderHook(() =>
      useInlineApplyFlow({ scope: 'triage_detail', pageKey: 'ticket.detail', onApply })
    );

    act(() => result.current.preview(changes));

    expect(result.current.pending).toEqual(changes);
    expect(onApply).not.toHaveBeenCalled();
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_preview',
      expect.objectContaining({
        pageKey: 'ticket.detail',
        extra: { scope: 'triage_detail', fields: 'category,priority' },
      })
    );
  });

  it('确认应用：调用 onApply(changes) 并记录 inline_applied；可撤销写回', async () => {
    const onApply = jest.fn().mockResolvedValue(undefined);
    const onUndo = jest.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useInlineApplyFlow({ scope: 'triage_detail', pageKey: 'ticket.detail', onApply, onUndo })
    );

    act(() => result.current.preview(changes));
    await act(async () => {
      await result.current.confirm();
    });

    expect(onApply).toHaveBeenCalledWith(changes);
    expect(result.current.pending).toBeNull();
    expect(result.current.applied).toEqual(changes);
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({ extra: { scope: 'triage_detail', action: 'apply', fields: 'category,priority' } })
    );

    await act(async () => {
      await result.current.undo();
    });
    expect(onUndo).toHaveBeenCalledWith(changes);
    expect(result.current.applied).toBeNull();
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_undone',
      expect.objectContaining({ extra: { scope: 'triage_detail', fields: 'category,priority' } })
    );
  });

  it('取消：清空待确认，onApply 不被调用', () => {
    const onApply = jest.fn();
    const { result } = renderHook(() =>
      useInlineApplyFlow({ scope: 'triage_create', pageKey: 'ticket.create', onApply })
    );

    act(() => result.current.preview(changes));
    act(() => result.current.cancel());

    expect(result.current.pending).toBeNull();
    expect(onApply).not.toHaveBeenCalled();
  });

  it('应用失败：error_shown 且保留待确认（可重试），不标记已应用', async () => {
    const onApply = jest.fn().mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() =>
      useInlineApplyFlow({ scope: 'triage_detail', pageKey: 'ticket.detail', onApply })
    );

    act(() => result.current.preview(changes));
    await act(async () => {
      await result.current.confirm();
    });

    expect(result.current.pending).toEqual(changes);
    expect(result.current.applied).toBeNull();
    expect(mockedTrack).toHaveBeenCalledWith(
      'error_shown',
      expect.objectContaining({ errorCode: 'inline_apply_failed' })
    );
  });

  it('空变更集不进入预览（避免空 diff 弹窗）', () => {
    const onApply = jest.fn();
    const { result } = renderHook(() =>
      useInlineApplyFlow({ scope: 'triage_detail', pageKey: 'ticket.detail', onApply })
    );

    act(() => result.current.preview([]));
    expect(result.current.pending).toBeNull();
    expect(mockedTrack).not.toHaveBeenCalled();
  });
});