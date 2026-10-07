/**
 * TicketListShell 回归（§13.1 复用收敛 #2）：
 * - 表格渲染/空态文案/分页透传；
 * - 错误态 Alert + 重试；
 * - 行点击守卫（交互元素不触发行点击）；
 * - 游标「加载更多」显示/隐藏与回调。
 */
import React from 'react';
import { Button } from 'antd';
import { fireEvent, render, screen } from '@/lib/test-utils';

import TicketListShell from '../TicketListShell';

interface Row {
  id: number;
  title: string;
}

const columns = [
  { title: '标题', dataIndex: 'title' },
  {
    title: '操作',
    key: 'action',
    render: () => (
      <Button size="small" data-testid="row-action">
        操作
      </Button>
    ),
  },
];

const rows: Row[] = [
  { id: 1, title: '工单一' },
  { id: 2, title: '工单二' },
];

describe('TicketListShell', () => {
  it('渲染数据行与空态文案', () => {
    const { unmount } = render(
      <TicketListShell<Row> columns={columns} dataSource={rows} emptyText="暂无工单" />
    );
    expect(screen.getByText('工单一')).toBeInTheDocument();
    expect(screen.getByText('工单二')).toBeInTheDocument();
    unmount();

    render(<TicketListShell<Row> columns={columns} dataSource={[]} emptyText="暂无工单" />);
    expect(screen.getByText('暂无工单')).toBeInTheDocument();
  });

  it('错误态渲染 Alert 并支持重试', () => {
    const onRetry = jest.fn();
    render(
      <TicketListShell<Row>
        columns={columns}
        dataSource={[]}
        error="加载失败"
        onRetry={onRetry}
      />
    );
    expect(screen.getByText('加载失败')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('行点击守卫：点击单元格触发行点击，点击行内按钮不触发', () => {
    const onRowClick = jest.fn();
    render(
      <TicketListShell<Row> columns={columns} dataSource={rows} onRowClick={onRowClick} />
    );

    fireEvent.click(screen.getByText('工单一'));
    expect(onRowClick).toHaveBeenCalledWith(rows[0]);

    onRowClick.mockClear();
    fireEvent.click(screen.getAllByTestId('row-action')[1]);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it('游标模式：hasMore 时渲染加载更多并回调；隐藏时不渲染', () => {
    const onLoadMore = jest.fn();
    const { unmount } = render(
      <TicketListShell<Row>
        columns={columns}
        dataSource={rows}
        cursor={{ hasMore: true, onLoadMore, testId: 'shell-load-more' }}
      />
    );
    fireEvent.click(screen.getByTestId('shell-load-more'));
    expect(onLoadMore).toHaveBeenCalledTimes(1);
    unmount();

    render(
      <TicketListShell<Row>
        columns={columns}
        dataSource={rows}
        cursor={{ hasMore: false, onLoadMore, testId: 'shell-load-more' }}
      />
    );
    expect(screen.queryByTestId('shell-load-more')).not.toBeInTheDocument();
  });
});
