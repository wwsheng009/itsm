/**
 * TicketCommentTimeline 回归：只读评论渲染件（工作台 P2 首个共享件）。
 * 锁定：作者侧标签、内容、空态、loading、error 降级；不含任何写入口。
 */
import React from 'react';
import { render, screen, within } from '@/lib/test-utils';

import TicketCommentTimeline from '../TicketCommentTimeline';

describe('TicketCommentTimeline', () => {
  it('渲染作者/侧标签/时间/内容（customer 与 provider）', () => {
    render(
      <TicketCommentTimeline
        testIdPrefix="t"
        items={[
          {
            id: 1,
            userId: 3,
            authorName: '李四',
            authorSide: 'customer',
            content: '问题依旧存在',
            createdAt: '2026-09-30T07:00:00Z',
          },
          {
            id: 2,
            userId: 9,
            authorName: '张三',
            authorSide: 'provider',
            content: '已安排远程排查',
            createdAt: '2026-09-30T07:30:00Z',
          },
        ]}
      />
    );

    const section = screen.getByTestId('t');
    expect(within(section).getByText('李四')).toBeInTheDocument();
    expect(within(section).getByText('问题依旧存在')).toBeInTheDocument();
    expect(within(section).getByText('客户')).toBeInTheDocument();
    expect(within(section).getByText('张三')).toBeInTheDocument();
    expect(within(section).getByText('已安排远程排查')).toBeInTheDocument();
    expect(within(section).getByText('服务商')).toBeInTheDocument();
    expect(within(section).queryByRole('textbox')).toBeNull();
  });

  it('空态与 loading / error 降级', () => {
    const { rerender } = render(<TicketCommentTimeline testIdPrefix="t" items={[]} />);
    expect(screen.getByText('暂无评论')).toBeInTheDocument();

    rerender(<TicketCommentTimeline testIdPrefix="t" items={[]} loading />);
    expect(screen.getByTestId('t-loading')).toBeInTheDocument();

    rerender(<TicketCommentTimeline testIdPrefix="t" items={[]} error="加载评论失败" />);
    expect(screen.getByText('加载评论失败')).toBeInTheDocument();
  });

  it('缺省 authorSide 时不渲染侧标签（客户侧复用场景）', () => {
    render(
      <TicketCommentTimeline
        testIdPrefix="t"
        items={[
          {
            id: 1,
            authorName: '王五',
            content: '内部处理中',
            createdAt: '2026-09-30T08:00:00Z',
          },
        ]}
      />
    );
    const section = screen.getByTestId('t');
    expect(within(section).getByText('王五')).toBeInTheDocument();
    expect(within(section).queryByText('客户')).toBeNull();
    expect(within(section).queryByText('服务商')).toBeNull();
  });
});
