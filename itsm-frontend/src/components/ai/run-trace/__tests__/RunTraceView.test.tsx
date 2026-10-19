/**
 * O1-01 轨迹回放视图测试：链路六段渲染、确认/verify 标签、产物引用、业务对象链接、复制轨迹 ID。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import RunTraceView from '../RunTraceView';
import type { AIRunTrace } from '@/lib/api/ai-api';

jest.setTimeout(300000);

const fixture: AIRunTrace = {
  run: {
    id: 5,
    conversationId: 9,
    botId: 7,
    entrypoint: 'ticket_detail',
    status: 'completed',
    model: 'gpt-4o',
    targetType: 'ticket',
    targetId: 42,
    startedAt: '2026-10-18T10:00:00Z',
    finishedAt: '2026-10-18T10:00:03Z',
  },
  steps: [
    { id: 1, stepIndex: 0, type: 'llm', durationMs: 800, createdAt: '2026-10-18T10:00:00Z' },
    {
      id: 2,
      stepIndex: 1,
      type: 'tool',
      payloadRef: 'tool_invocation:1',
      durationMs: 120,
      createdAt: '2026-10-18T10:00:01Z',
    },
  ],
  events: [
    {
      id: 1,
      seq: 0,
      type: 'run_started',
      payload: { entrypoint: 'ticket_detail' },
      createdAt: '2026-10-18T10:00:00Z',
    },
  ],
  toolInvocations: [
    {
      id: 1,
      toolName: 'create_ticket',
      status: 'success',
      approvalState: 'approved',
      confirmationState: 'confirmed',
      needsApproval: true,
      risk: 'act_low',
      argsRedacted: '{"title":"***"}',
      verifyState: 'verified',
      durationMs: 120,
      targetType: 'ticket',
      targetId: '42',
      stepId: 2,
      userId: 7,
      createdAt: '2026-10-18T10:00:01Z',
    },
  ],
  artifacts: [
    {
      id: 31,
      kind: 'analysis',
      toolName: 'analyze_ci_impact_plan',
      title: '影响分析',
      contentJson: '{"checklist":["备份"]}',
      createdAt: '2026-10-18T10:00:02Z',
    },
  ],
};

describe('RunTraceView（O1-01 回放视图）', () => {
  it('渲染链路六段：会话 → 运行 → 步骤/事件 → 工具/确认 → 产物 → 业务对象', () => {
    render(<RunTraceView trace={fixture} pageKey="ai.observability" />);

    expect(screen.getByTestId('run-trace-view')).toBeInTheDocument();
    // 轨迹标识 + 状态
    expect(screen.getByText('轨迹 #5')).toBeInTheDocument();
    expect(screen.getByTestId('run-trace-status')).toHaveTextContent('completed');
    // 会话 / 运行
    expect(screen.getByTestId('run-trace-conversation')).toHaveTextContent('会话 #9');
    expect(screen.getByTestId('run-trace-run')).toHaveTextContent('ticket_detail');
    expect(screen.getByTestId('run-trace-run')).toHaveTextContent('3.0 s');
    // 步骤与事件
    expect(screen.getByTestId('run-trace-step-0')).toHaveTextContent('llm');
    expect(screen.getByTestId('run-trace-step-1')).toHaveTextContent('tool_invocation:1');
    expect(screen.getByTestId('run-trace-event-0')).toHaveTextContent('run_started');
    // 工具调用与确认（规范化标签 + verify + 脱敏参数）
    const tool = screen.getByTestId('run-trace-tool-1');
    expect(tool).toHaveTextContent('create_ticket');
    expect(screen.getByTestId('run-trace-tool-1-confirmation')).toHaveTextContent('已确认');
    expect(tool).toHaveTextContent('verify:verified');
    expect(tool).toHaveTextContent('{"title":"***"}');
    // 产物
    expect(screen.getByTestId('run-trace-artifact-31')).toHaveTextContent('artifact:31');
    expect(screen.getByTestId('run-trace-artifact-31')).toHaveTextContent('影响分析');
    // 业务对象（run + tool 去重后仅一条链接）
    const targets = screen.getByTestId('run-trace-targets');
    expect(targets).toHaveTextContent('ticket #42');
    expect(targets.querySelectorAll('a')).toHaveLength(1);
    expect(targets.querySelector('a')).toHaveAttribute('href', '/tickets/42');
  });

  it('空段降级：无步骤/工具/产物/业务对象时显示占位', () => {
    const empty: AIRunTrace = {
      run: {
        id: 6,
        conversationId: 0,
        botId: 0,
        entrypoint: 'chat',
        status: 'failed',
        startedAt: '2026-10-18T10:00:00Z',
        errorCode: 'provider_error',
      },
      steps: [],
      events: [],
      toolInvocations: [],
      artifacts: [],
    };
    render(<RunTraceView trace={empty} />);

    expect(screen.getAllByText('该运行没有此类记录').length).toBe(4);
    expect(screen.getByTestId('run-trace-status')).toHaveTextContent('failed');
    // 错误码同时出现在状态区与 Descriptions 明细（两处均为设计内展示）。
    expect(screen.getAllByText('provider_error').length).toBeGreaterThan(0);
  });

  it('复制轨迹 ID：写入剪贴板并显示已复制态（埋点不阻塞）', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });

    render(<RunTraceView trace={fixture} pageKey="ai.observability" />);
    fireEvent.click(screen.getByTestId('run-trace-copy'));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('5'));
    expect(screen.getByTestId('run-trace-copy')).toHaveTextContent('已复制');
  });
});