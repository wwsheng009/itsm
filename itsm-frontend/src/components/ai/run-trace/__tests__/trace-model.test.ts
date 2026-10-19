/**
 * O1-01 轨迹回放纯逻辑单测：确认标签、业务对象落点、耗时与载荷摘要。
 */
import {
  collectTraceTargets,
  confirmationColor,
  confirmationLabel,
  formatDuration,
  runDurationMs,
  runStatusColor,
  summarizePayload,
  targetRoute,
} from '../trace-model';
import type { AIRunTrace } from '@/lib/api/ai-api';

describe('trace-model（O1-01 轨迹口径）', () => {
  it('确认状态标签与颜色：六态 + 未知回退', () => {
    expect(confirmationLabel('pending')).toBe('待确认');
    expect(confirmationLabel('confirmed')).toBe('已确认');
    expect(confirmationLabel('rejected')).toBe('已驳回');
    expect(confirmationLabel('expired')).toBe('已过期');
    expect(confirmationLabel('cancelled')).toBe('已取消');
    expect(confirmationLabel('unknown')).toBe('未知');
    expect(confirmationLabel(undefined)).toBe('未知');
    expect(confirmationLabel('weird')).toBe('weird');

    expect(confirmationColor('confirmed')).toBe('green');
    expect(confirmationColor('pending')).toBe('gold');
    expect(confirmationColor('rejected')).toBe('red');
    expect(confirmationColor('nope')).toBe('default');
    expect(runStatusColor('completed')).toBe('green');
    expect(runStatusColor('failed')).toBe('red');
    expect(runStatusColor('running')).toBe('blue');
    expect(runStatusColor('weird')).toBe('default');
  });

  it('业务对象落点：只映射注册表已确认的详情路由', () => {
    expect(targetRoute('ticket', 42)).toBe('/tickets/42');
    expect(targetRoute('ticket', '42')).toBe('/tickets/42');
    expect(targetRoute('incident', 7)).toBe('/incidents/7');
    expect(targetRoute('change', 8)).toBe('/changes/8');
    expect(targetRoute('ci', 9)).toBe('/cmdb/cis/9');
    // 未确认类型/非法 id → 不臆造链接。
    expect(targetRoute('knowledge_article', 3)).toBeUndefined();
    expect(targetRoute('ticket', 0)).toBeUndefined();
    expect(targetRoute('ticket', 'abc')).toBeUndefined();
    expect(targetRoute(undefined, 42)).toBeUndefined();
  });

  it('业务对象汇总：run + 工具 target 去重且保持顺序', () => {
    const trace = {
      run: { targetType: 'ticket', targetId: 42 },
      toolInvocations: [
        { targetType: 'ticket', targetId: '42' },
        { targetType: 'ci', targetId: '9' },
        { targetType: 'ci', targetId: '9' },
        { targetType: '', targetId: '' },
      ],
    } as unknown as AIRunTrace;

    const targets = collectTraceTargets(trace);
    expect(targets.map(item => item.key)).toEqual(['ticket#42', 'ci#9']);
    expect(targets[1].route).toBe('/cmdb/cis/9');
  });

  it('耗时：finished-started；未收口/倒挂 → undefined', () => {
    const base = { startedAt: '2026-10-18T10:00:00Z' };
    expect(runDurationMs({ ...base, finishedAt: '2026-10-18T10:00:02.500Z' } as never)).toBe(2500);
    expect(runDurationMs({ ...base } as never)).toBeUndefined();
    expect(runDurationMs({ startedAt: base.startedAt, finishedAt: '2026-10-18T09:59:00Z' } as never)).toBeUndefined();

    expect(formatDuration(980)).toBe('980 ms');
    expect(formatDuration(2500)).toBe('2.5 s');
    expect(formatDuration(undefined)).toBe('—');
  });

  it('事件载荷摘要：单行 JSON、空值占位、超长截断', () => {
    expect(summarizePayload(undefined)).toBe('—');
    expect(summarizePayload({})).toBe('—');
    expect(summarizePayload({ entrypoint: 'chat' })).toBe('{"entrypoint":"chat"}');

    const long = summarizePayload({ note: 'x'.repeat(300) });
    expect(long.endsWith('…')).toBe(true);
    expect(long.length).toBeLessThanOrEqual(161);
  });
});