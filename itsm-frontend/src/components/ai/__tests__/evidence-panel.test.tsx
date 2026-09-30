/**
 * B1-08 EvidencePanel / RunStatusBar 组件测试。
 *
 * 覆盖：空态、工具行（来源/状态/耗时/错误码/摘要）、目标跳转与依据（detailsByInvocation）、
 * 步骤渲染、错误态；状态条的 running/done/中断、预算段（有则显示）。
 */
import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';

import EvidencePanel, { targetUrl } from '../evidence-panel';
import RunStatusBar, { formatDuration, formatElapsed } from '../run-status-bar';
import type { AIRunStepEvent, AIToolStreamEvent } from '@/lib/api/ai-api';

const toolEvents: AIToolStreamEvent[] = [
  { tool: 'mcp__mock__list_issues', provider: 'mcp', server: 'mock', phase: 'read', status: 'started' },
  {
    tool: 'mcp__mock__list_issues',
    provider: 'mcp',
    server: 'mock',
    phase: 'read',
    status: 'done',
    summary: '共 3 条',
    durationMs: 1200,
  },
  {
    tool: 'mcp__mock__create_issue',
    provider: 'mcp',
    server: 'mock',
    phase: 'write',
    status: 'failed',
    errorCode: 'tool_permission_denied',
  },
];

const steps: AIRunStepEvent[] = [
  { runId: 7, stepIndex: 2, type: 'tool', payloadRef: 'tool_invocation:42', durationMs: 1200 },
  { runId: 7, stepIndex: 1, type: 'llm', durationMs: 800 },
];

describe('EvidencePanel (B1-08)', () => {
  it('renders an explicit empty state when there is no evidence', () => {
    render(<EvidencePanel />);
    expect(screen.getByTestId('evidence-empty')).toBeInTheDocument();
    expect(screen.getByText(/没有可展示的过程证据/)).toBeInTheDocument();
  });

  it('pairs tool events and shows source, status, duration and summary', () => {
    render(<EvidencePanel toolEvents={toolEvents} />);

    // 配对后剩两行：完成的读调用 + 失败的写调用（started 帧被合并）。
    expect(screen.getByTestId('evidence-tool-0')).toHaveTextContent('mcp__mock__list_issues');
    expect(screen.getByTestId('evidence-tool-0')).toHaveTextContent('MCP · mock');
    expect(screen.getByTestId('evidence-tool-0')).toHaveTextContent('完成');
    expect(screen.getByTestId('evidence-tool-0')).toHaveTextContent('1.2s');
    expect(screen.getByTestId('evidence-tool-0')).toHaveTextContent('共 3 条');
    expect(screen.getByTestId('evidence-tool-1')).toHaveTextContent('tool_permission_denied');
  });

  it('shows target jump and support_ref when invocation details are supplied', () => {
    const onOpenTarget = jest.fn();
    render(
      <EvidencePanel
        toolEvents={[
          { id: 42, tool: 'create_ticket', provider: 'builtin', phase: 'write', status: 'done' },
        ]}
        detailsByInvocation={{ 42: { targetType: 'ticket', targetId: '1001', supportRef: 'kb:runbook/42' } }}
        onOpenTarget={onOpenTarget}
      />
    );

    // 事件带 invocation id 且调用方提供了详情 → 渲染跳转与依据。
    expect(screen.getByText('依据 kb:runbook/42')).toBeInTheDocument();
    fireEvent.click(screen.getByText(/查看目标/));
    expect(onOpenTarget).toHaveBeenCalledWith('ticket', '1001');
  });

  it('does not fabricate targets for events without an invocation id', () => {
    render(
      <EvidencePanel
        toolEvents={toolEvents}
        detailsByInvocation={{ 42: { targetType: 'ticket', targetId: '1001', supportRef: 'kb:runbook/42' } }}
        onOpenTarget={jest.fn()}
      />
    );

    expect(screen.queryByText(/查看目标/)).not.toBeInTheDocument();
    expect(screen.queryByText('依据 kb:runbook/42')).not.toBeInTheDocument();
  });

  it('renders target as plain text when no jump handler is provided (no fabricated links)', () => {
    render(
      <EvidencePanel
        toolEvents={[{ tool: 'create_ticket', provider: 'builtin', phase: 'write', status: 'pending', id: 42 }]}
        detailsByInvocation={{ 42: { targetType: 'ticket', targetId: '1001' } }}
      />
    );

    expect(screen.getByText('目标 ticket#1001')).toBeInTheDocument();
    expect(screen.queryByText(/查看目标/)).not.toBeInTheDocument();
  });

  it('orders steps by stepIndex and shows payload refs', () => {
    render(<EvidencePanel steps={steps} />);
    const stepOne = screen.getByTestId('evidence-step-1');
    const stepTwo = screen.getByTestId('evidence-step-2');
    expect(stepOne).toHaveTextContent('模型');
    expect(stepOne).toHaveTextContent('0.8s');
    expect(stepTwo).toHaveTextContent('工具');
    expect(stepTwo).toHaveTextContent('tool_invocation:42');
  });

  it('surfaces a read-only error without hiding the empty hint contract', () => {
    render(<EvidencePanel error="run 档案读取失败" />);
    expect(screen.getByTestId('evidence-error')).toHaveTextContent('run 档案读取失败');
  });
});

describe('RunStatusBar (B1-08)', () => {
  it('renders nothing without a run snapshot (old backend compatibility)', () => {
    const { container } = render(<RunStatusBar />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows running state, provider, step count and elapsed time', () => {
    render(
      <RunStatusBar
        info={{
          runId: 7,
          status: 'running',
          providerLabel: 'OpenAI · gpt-4o',
          stepCount: 3,
          lastStepType: 'tool',
          lastDurationMs: 1200,
          startedAt: 1_000_000,
        }}
        now={() => 1_000_000 + 65_000}
        tickMs={60_000}
      />
    );

    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('运行中');
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('run#7');
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('OpenAI · gpt-4o');
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('步骤 3 · 最近 tool');
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('已运行 1 分 5 秒');
  });

  it('shows done/failed states and omits budget when limits are unknown', () => {
    const { unmount } = render(<RunStatusBar info={{ runId: 8, status: 'done' }} />);
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('已完成');
    unmount();

    render(<RunStatusBar info={{ runId: 9, status: 'failed', budget: { stepsUsed: 3 } }} />);
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('已中断');
    expect(screen.queryByText(/预算：/)).not.toBeInTheDocument();
  });

  it('renders budget only when limits are supplied', () => {
    render(
      <RunStatusBar
        info={{ runId: 10, status: 'running', budget: { stepsUsed: 4, stepsLimit: 20, toolCallsUsed: 2, toolCallsLimit: 8 } }}
      />
    );
    expect(screen.getByTestId('run-status-bar')).toHaveTextContent('预算：步数 4/20 · 工具 2/8');
  });
});

describe('helpers (B1-08)', () => {
  it('maps only verified detail routes', () => {
    expect(targetUrl('ticket', '1')).toBe('/tickets/1');
    expect(targetUrl('incident', '2')).toBe('/incidents/2');
    expect(targetUrl('ci', '3')).toBe('/cmdb/cis/3');
    expect(targetUrl('unknown', '4')).toBeNull();
    expect(targetUrl('ticket', undefined)).toBeNull();
  });

  it('formats durations and elapsed time', () => {
    expect(formatDuration(1200)).toBe('1.2s');
    expect(formatDuration(0)).toBeNull();
    expect(formatElapsed(0, 1000)).toBe('');
    expect(formatElapsed(1_000_000, 1_000_000 + 5_000)).toBe('5 秒');
    expect(formatElapsed(1_000_000, 1_000_000 + 3_600_000)).toBe('60 分 0 秒');
  });
});
