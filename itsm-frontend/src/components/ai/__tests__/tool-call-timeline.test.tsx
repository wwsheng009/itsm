/**
 * 工具调用时间线测试（M1-04，jsdom + @testing-library/react）。
 *
 * 覆盖任务卡要求的四条：正常序列 / 失败序列 / 无事件降级 / 超长输出折叠与截断标记，
 * 另补两条边界：同名工具多次调用不互相覆盖、事件丢失时终态事件自建条目（不丢过程）。
 *
 * 安全断言：工具输出按纯文本渲染——`<img onerror>` 之类的载荷只能以文本出现，不得进入 DOM。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import ToolCallTimeline, { TRUNCATED_MARKER, mergeToolEvents } from '../tool-call-timeline';
import type { AIToolStreamEvent } from '@/lib/api/ai-api';

const mcpList = (status: string, extra: Partial<AIToolStreamEvent> = {}): AIToolStreamEvent => ({
  tool: 'mcp__mock__list_issues',
  provider: 'mcp',
  server: 'mock',
  phase: 'read',
  status,
  ...extra,
});

describe('ToolCallTimeline', () => {
  it('正常序列：started → done 合并为一条，展示来源徽标/工具名/状态/耗时与脱敏摘要', () => {
    render(
      <ToolCallTimeline
        events={[
          mcpList('started'),
          mcpList('done', { summary: '{"issues":[]}', durationMs: 12 }),
        ]}
      />
    );

    // 标题计数 = 1 条调用（started 与 done 已配对，不产生两行）。
    expect(screen.getByText('工具调用 · 1')).toBeTruthy();
    expect(screen.getByText('MCP · mock')).toBeTruthy();
    expect(screen.getByText('mcp__mock__list_issues')).toBeTruthy();
    expect(screen.getByText('已完成')).toBeTruthy();
    expect(screen.getByText('12ms')).toBeTruthy();

    // 摘要默认折叠：展开后才可见（长输出不占用回答区）。
    expect(screen.queryByText('{"issues":[]}')).toBeNull();
    fireEvent.click(screen.getByText('mcp__mock__list_issues'));
    expect(screen.getByText('{"issues":[]}')).toBeTruthy();
  });

  it('失败序列：展示错误码与失败状态', () => {
    render(
      <ToolCallTimeline
        events={[
          { tool: 'delete_ci', provider: 'builtin', phase: 'write', status: 'started' },
          { tool: 'delete_ci', provider: 'builtin', phase: 'write', status: 'failed', errorCode: 'tool_permission_denied' },
        ]}
      />
    );

    expect(screen.getByText('失败')).toBeTruthy();
    expect(screen.getByText('写操作')).toBeTruthy();
    fireEvent.click(screen.getByText('delete_ci'));
    expect(screen.getByText('错误码：tool_permission_denied')).toBeTruthy();
  });

  it('待审批：pending 事件展示 invocation 与跳转提示', () => {
    render(
      <ToolCallTimeline
        events={[
          { tool: 'create_ticket', provider: 'builtin', phase: 'write', status: 'started' },
          { tool: 'create_ticket', provider: 'builtin', phase: 'write', status: 'pending', id: 9 },
        ]}
      />
    );

    expect(screen.getByText('待审批')).toBeTruthy();
    fireEvent.click(screen.getByText('create_ticket'));
    expect(screen.getByText(/已提交人工审批（#9）/)).toBeTruthy();
  });

  it('无事件降级：不渲染任何容器（不出现空白块）', () => {
    const { container } = render(<ToolCallTimeline events={[]} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByTestId('tool-call-timeline')).toBeNull();

    // undefined（旧调用方完全不传）同样安全。
    const second = render(<ToolCallTimeline />);
    expect(second.container.firstChild).toBeNull();
  });

  it('超长输出：截断标记展示「已截断」，展开后仍可见摘要（滚动承载）', () => {
    const longSummary = `{"items":["${'x'.repeat(200)}"]}${TRUNCATED_MARKER}`;
    render(<ToolCallTimeline events={[mcpList('done', { summary: longSummary, durationMs: 1500 })]} />);

    // 事件丢失降级：只有终态事件也能渲染。
    expect(screen.getByText('工具调用 · 1')).toBeTruthy();
    fireEvent.click(screen.getByText('mcp__mock__list_issues'));
    expect(screen.getByText('已截断')).toBeTruthy();
    expect(screen.getByText(longSummary)).toBeTruthy();
    expect(screen.getByText('1.50s')).toBeTruthy();
  });

  it('同名工具多次调用：按序配对，互不覆盖', () => {
    const entries = mergeToolEvents([
      mcpList('started'),
      mcpList('done', { summary: 'first', durationMs: 5 }),
      mcpList('started'),
      mcpList('failed', { errorCode: 'server_error' }),
    ]);

    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({ status: 'done', summary: 'first', durationMs: 5 });
    expect(entries[1]).toMatchObject({ status: 'failed', errorCode: 'server_error' });
  });

  it('事件丢失：只有终态事件时自建条目（不丢过程）', () => {
    const entries = mergeToolEvents([mcpList('done', { summary: 'ok', durationMs: 3 })]);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ status: 'done', provider: 'mcp', server: 'mock' });
  });

  it('工具输出按纯文本渲染：HTML/脚本载荷只以文本出现，不进入 DOM', () => {
    const payload = '<img src=x onerror="alert(1)"><script>alert(2)</script>';
    const { container } = render(<ToolCallTimeline events={[mcpList('done', { summary: payload })]} />);

    fireEvent.click(screen.getByText('mcp__mock__list_issues'));
    expect(screen.getByText(payload)).toBeTruthy();
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
  });
});
