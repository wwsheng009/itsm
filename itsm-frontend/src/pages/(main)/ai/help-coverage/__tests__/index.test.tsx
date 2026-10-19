/**
 * H1-04 帮助覆盖率看板测试：统计/页面清单/待办队列/筛选/降级。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import HelpCoveragePage, {
  HELP_TODO_REASON_LABEL,
  HELP_TODO_STATE_META,
  formatHelpCoveragePercent,
} from '../index';
import {
  aiExecuteTool,
  aiGetHelpCoverage,
  aiGetHelpTodos,
  aiListHelpDrafts,
  aiSetHelpTodoState,
} from '@/lib/api/ai-api';

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetHelpCoverage: jest.fn(),
  aiGetHelpTodos: jest.fn(),
  aiListHelpDrafts: jest.fn(),
  aiExecuteTool: jest.fn(),
  aiSetHelpTodoState: jest.fn(),
}));

const mockedCoverage = aiGetHelpCoverage as jest.MockedFunction<typeof aiGetHelpCoverage>;
const mockedTodos = aiGetHelpTodos as jest.MockedFunction<typeof aiGetHelpTodos>;
const mockedDrafts = aiListHelpDrafts as jest.MockedFunction<typeof aiListHelpDrafts>;
const mockedExecuteTool = aiExecuteTool as jest.MockedFunction<typeof aiExecuteTool>;
const mockedSetState = aiSetHelpTodoState as jest.MockedFunction<typeof aiSetHelpTodoState>;

const coverageFixture = {
  generatedAt: '2026-10-18T02:00:00Z',
  coverage: 0.9524,
  totals: { pages: 21, ok: 20, missing: 1, stale: 0, orphan: 0 },
  pages: [
    {
      pageKey: 'ticket.detail',
      title: '工单详情',
      pattern: '/tickets/:id',
      entrypoint: 'ticket_detail',
      status: 'ok' as const,
      docs: ['docs/help/tickets/detail.md'],
    },
    {
      pageKey: 'dashboard',
      title: '仪表盘',
      pattern: '/dashboard',
      entrypoint: 'chat',
      status: 'missing' as const,
      docs: [],
    },
  ],
  orphanDocs: [] as Array<{ doc: string; unknownKeys: string[] }>,
};

const todosFixture = {
  days: 30,
  items: [
    {
      pageKey: 'dashboard',
      title: '仪表盘',
      docStatus: 'missing' as const,
      gapCount: 3,
      draftCount: 1,
      lastSeen: '2026-10-18T02:00:00Z',
      reason: 'doc_missing' as const,
      state: 'open' as const,
    },
  ],
  totals: { items: 1, missing: 1, stale: 0, claimed: 0, ignored: 0 },
  states: { available: true },
  telemetry: { available: true },
};

beforeEach(() => {
  mockedCoverage.mockReset();
  mockedTodos.mockReset();
  mockedDrafts.mockReset();
  mockedExecuteTool.mockReset();
  mockedSetState.mockReset();
  mockedCoverage.mockResolvedValue(coverageFixture);
  mockedTodos.mockResolvedValue(todosFixture);
  mockedDrafts.mockResolvedValue({ items: [], total: 0 });
  mockedSetState.mockResolvedValue({
    pageKey: 'dashboard',
    state: 'claimed',
    updatedBy: 9,
    updatedAt: '2026-10-18T03:00:00Z',
  });
});

describe('HelpCoveragePage（H1-04）', () => {
  it('渲染覆盖率统计、页面清单与待办队列', async () => {
    render(<HelpCoveragePage />);

    expect(await screen.findByText('95.2%')).toBeInTheDocument();
    const stats = screen.getByTestId('help-coverage-stats');
    expect(stats).toHaveTextContent('覆盖率');
    expect(stats).toHaveTextContent('页面总数');

    // 页面清单：页面键 + 关联文档路径。
    expect(await screen.findByText('ticket.detail')).toBeInTheDocument();
    expect(screen.getByText('docs/help/tickets/detail.md')).toBeInTheDocument();

    // 待办队列：原因中文 + 缺口数。
    const todoTable = screen.getByTestId('help-todos-table');
    expect(todoTable).toHaveTextContent('文档缺失');
    expect(todoTable).toHaveTextContent('2026-10-18T02:00:00Z');
  });

  it('状态筛选：缺失 → 仅缺失页面', async () => {
    render(<HelpCoveragePage />);
    await screen.findByText('ticket.detail');

    fireEvent.click(within(screen.getByTestId('help-status-filter')).getByText('缺失'));

    await waitFor(() => expect(screen.queryByText('ticket.detail')).not.toBeInTheDocument());
    expect(screen.getAllByText('dashboard').length).toBeGreaterThan(0);
  });

  it('待办接口失败 → 降级提示，账本仍渲染', async () => {
    mockedTodos.mockRejectedValue(new Error('503'));

    render(<HelpCoveragePage />);

    expect(await screen.findByTestId('help-todos-unavailable')).toBeInTheDocument();
    expect(screen.getByText('95.2%')).toBeInTheDocument();
  });

  it('埋点未装配（available=false）→ 提示仅账本缺口', async () => {
    mockedTodos.mockResolvedValue({ ...todosFixture, telemetry: { available: false } });

    render(<HelpCoveragePage />);

    expect(await screen.findByTestId('help-todos-telemetry-degraded')).toBeInTheDocument();
  });

  it('orphan 文档提示', async () => {
    mockedCoverage.mockResolvedValue({
      ...coverageFixture,
      orphanDocs: [{ doc: 'docs/help/x.md', unknownKeys: ['gone.page'] }],
    });

    render(<HelpCoveragePage />);

    const alert = await screen.findByTestId('help-orphan-alert');
    expect(alert).toHaveTextContent('gone.page');
  });

  it('辅助函数：百分比格式化与原因标签', () => {
    expect(formatHelpCoveragePercent(0.9524)).toBe('95.2%');
    expect(formatHelpCoveragePercent(undefined)).toBe('—');
    expect(HELP_TODO_REASON_LABEL.doc_missing).toBe('文档缺失');
    expect(HELP_TODO_REASON_LABEL.user_help_gaps).toBe('用户求助缺口');
    expect(HELP_TODO_STATE_META.claimed.label).toBe('已认领');
  });

  // H1-01/H1-03/H1-04 条件项闭环：认领/忽略/恢复 + 筛选 + 降级。

  it('认领待办：调用状态写面并渲染新状态', async () => {
    mockedTodos
      .mockResolvedValueOnce(todosFixture)
      .mockResolvedValueOnce({
        ...todosFixture,
        items: [
          {
            ...todosFixture.items[0],
            state: 'claimed' as const,
            stateUpdatedBy: 9,
            stateUpdatedAt: '2026-10-18T03:00:00Z',
          },
        ],
        totals: { items: 1, missing: 1, stale: 0, claimed: 1, ignored: 0 },
      });

    render(<HelpCoveragePage />);
    fireEvent.click(await screen.findByTestId('help-todo-claim-dashboard'));

    await waitFor(() => expect(mockedSetState).toHaveBeenCalledWith('dashboard', 'claimed'));
    expect(await screen.findByTestId('help-todo-state-claimed')).toBeInTheDocument();
    expect(screen.getByTestId('help-todo-reopen-dashboard')).toBeInTheDocument();
  });

  it('忽略待办后按状态筛选：仅显示已忽略项', async () => {
    mockedTodos.mockResolvedValue({
      ...todosFixture,
      items: [
        { ...todosFixture.items[0] },
        {
          ...todosFixture.items[0],
          pageKey: 'ghost.page',
          state: 'ignored' as const,
          reason: 'page_unregistered' as const,
        },
      ],
      totals: { items: 2, missing: 1, stale: 0, claimed: 0, ignored: 1 },
    });

    render(<HelpCoveragePage />);
    const todoTable = await screen.findByTestId('help-todos-table');
    await within(todoTable).findByText('ghost.page');

    fireEvent.click(within(screen.getByTestId('help-todo-state-filter')).getByText('已忽略'));

    await waitFor(() => expect(within(todoTable).queryByText('dashboard')).not.toBeInTheDocument());
    expect(within(todoTable).getByText('ghost.page')).toBeInTheDocument();
  });

  it('状态服务降级 → 提示且认领/忽略禁用', async () => {
    mockedTodos.mockResolvedValue({ ...todosFixture, states: { available: false } });

    render(<HelpCoveragePage />);

    expect(await screen.findByTestId('help-todos-states-degraded')).toBeInTheDocument();
    expect(screen.getByTestId('help-todo-claim-dashboard')).toBeDisabled();
    expect(screen.getByTestId('help-todo-ignore-dashboard')).toBeDisabled();
  });

  it('状态写入失败 → 错误提示且不伪装成功', async () => {
    mockedSetState.mockRejectedValue(new Error('500'));

    render(<HelpCoveragePage />);
    fireEvent.click(await screen.findByTestId('help-todo-ignore-dashboard'));

    expect(await screen.findByTestId('help-todos-action-error')).toBeInTheDocument();
    // 状态未被乐观改写：仍渲染「待处理」。
    expect(screen.getByTestId('help-todo-state-open')).toBeInTheDocument();
  });

  it('T5-09a：草稿提交审阅走工具入口并提示等待审批', async () => {
    mockedDrafts.mockResolvedValue({
      total: 1,
      items: [
        {
          artifactId: 31,
          ownerUserId: 9,
          pageKey: 'dashboard',
          title: '仪表盘帮助草稿',
          status: 'draft',
          revision: 1,
          markdown: '## 新内容',
          baselineMarkdown: '## 旧内容',
          diff: [
            { type: 'removed', content: '## 旧内容' },
            { type: 'added', content: '## 新内容' },
          ],
          createdAt: '2026-10-19T00:00:00Z',
        },
      ],
    });
    mockedExecuteTool.mockResolvedValue({ status: 'pending' });

    render(<HelpCoveragePage />);
    fireEvent.click(await screen.findByTestId('help-draft-submit-31'));

    await waitFor(() =>
      expect(mockedExecuteTool).toHaveBeenCalledWith('help_submit_review', { artifactId: 31 })
    );
    expect(await screen.findByTestId('help-draft-notice')).toHaveTextContent('已提交审批');
  });
});
