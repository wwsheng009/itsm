/**
 * O2-02 诊断摘要纯逻辑单测：最近运行提取、步骤排序、报障引用与耗时可读化。
 */
import {
  buildDiagnosticsSummary,
  diagnosticsReference,
  formatStepDuration,
  formatTokenUsage,
} from '../diagnostics-model';
import type { ChatMessage } from '@/components/ai/chat-core/types';

const messages: ChatMessage[] = [
  { id: 'u1', role: 'user', content: '帮我处理这张工单', createdAt: '2026-10-18T10:00:00Z' },
  {
    id: 'a1',
    role: 'assistant',
    content: '好的',
    createdAt: '2026-10-18T10:00:01Z',
    run: { runId: 3, status: 'done', stepCount: 1 },
    steps: [{ runId: 3, stepIndex: 0, type: 'llm', durationMs: 500 }],
  },
  { id: 'u2', role: 'user', content: '再帮我分析影响', createdAt: '2026-10-18T10:01:00Z' },
  {
    id: 'a2',
    role: 'assistant',
    content: '分析完成',
    createdAt: '2026-10-18T10:01:02Z',
    run: {
      runId: 7,
      status: 'done',
      stepCount: 2,
      lastDurationMs: 1200,
      providerLabel: 'OpenAI · gpt-4o',
      budget: { stepsUsed: 2, stepsLimit: 20, toolCallsUsed: 1, toolCallsLimit: 8 },
    },
    steps: [
      { runId: 7, stepIndex: 1, type: 'tool', payloadRef: 'tool_invocation:42', durationMs: 700 },
      { runId: 7, stepIndex: 0, type: 'llm', durationMs: 500 },
    ],
  },
];

describe('diagnostics-model（O2-02）', () => {
  it('提取最近一次运行（最后一条带 run 的助手消息）与排序后的步骤', () => {
    const summary = buildDiagnosticsSummary(messages);
    expect(summary.messageCount).toBe(4);
    expect(summary.assistantMessageCount).toBe(2);
    expect(summary.latestRun?.runId).toBe(7);
    expect(summary.runMessageId).toBe('a2');
    expect(summary.steps.map(step => step.stepIndex)).toEqual([0, 1]);
    expect(summary.steps[1].payloadRef).toBe('tool_invocation:42');
  });

  it('无运行/空会话 → 无 latestRun，不报错', () => {
    expect(buildDiagnosticsSummary([]).latestRun).toBeUndefined();
    expect(buildDiagnosticsSummary(undefined).messageCount).toBe(0);
    const noRun = buildDiagnosticsSummary([
      { id: 'u1', role: 'user', content: 'hi', createdAt: '2026-10-18T10:00:00Z' },
      { id: 'a1', role: 'assistant', content: 'hello', createdAt: '2026-10-18T10:00:01Z' },
    ]);
    expect(noRun.latestRun).toBeUndefined();
    expect(noRun.steps).toEqual([]);
  });

  it('报障引用：轨迹 ID 优先，其次会话 ID，皆无为空串', () => {
    const summary = buildDiagnosticsSummary(messages);
    expect(diagnosticsReference(summary, 9)).toBe('run#7');
    expect(diagnosticsReference(buildDiagnosticsSummary([]), 9)).toBe('conversation#9');
    expect(diagnosticsReference(buildDiagnosticsSummary([]), undefined)).toBe('');
  });

  it('步骤耗时可读化', () => {
    expect(formatStepDuration(980)).toBe('980 ms');
    expect(formatStepDuration(2500)).toBe('2.5 s');
    expect(formatStepDuration(0)).toBe('0 ms');
    expect(formatStepDuration(undefined)).toBe('—');
    expect(formatStepDuration(-1)).toBe('—');
  });

  it('用量估算：取最近一次带 usage 的助手消息（独立于 run）', () => {
    const withUsage = buildDiagnosticsSummary([
      ...messages,
      {
        id: 'a3',
        role: 'assistant',
        content: '纯问答',
        createdAt: '2026-10-18T10:02:00Z',
        usage: { tokenIn: 120, tokenOut: 48, estimated: true, scope: 'turn' },
      },
    ]);
    expect(withUsage.latestUsage).toEqual({ tokenIn: 120, tokenOut: 48, estimated: true, scope: 'turn' });
    expect(withUsage.usageMessageId).toBe('a3');

    // 老会话（无 usage 字段）→ undefined，不显示用量段（诚实降级）。
    expect(buildDiagnosticsSummary(messages).latestUsage).toBeUndefined();
    expect(buildDiagnosticsSummary([]).latestUsage).toBeUndefined();
  });

  it('用量文案：恒带估算符号（≈），缺省为 —', () => {
    expect(formatTokenUsage({ tokenIn: 120, tokenOut: 48, estimated: true })).toBe(
      '输入 ≈ 120 · 输出 ≈ 48 · 合计 ≈ 168'
    );
    expect(formatTokenUsage({ tokenIn: 0, tokenOut: 0 })).toBe('输入 ≈ 0 · 输出 ≈ 0 · 合计 ≈ 0');
    expect(formatTokenUsage(undefined)).toBe('—');
  });

  it('provider 实测（estimated=false）不带估算符号并显式标注（B4-02）', () => {
    expect(formatTokenUsage({ tokenIn: 33, tokenOut: 7, estimated: false })).toBe(
      '输入 33 · 输出 7 · 合计 40（provider 实测）'
    );
  });
});
