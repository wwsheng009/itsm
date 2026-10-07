/**
 * CommentItemBody 回归：评论条目通用渲染（§13.1 复用件 #1 的共享底层）。
 * 锁定：作者/侧标签/内部标签/时间/已编辑标记/正文与各槽位；缺省标签不渲染。
 */
import React from 'react';
import { render, screen } from '@/lib/test-utils';

import CommentItemBody from '../CommentItemBody';

describe('CommentItemBody', () => {
  it('渲染身份行、侧标签、内部标签、已编辑标记与槽位', () => {
    render(
      <CommentItemBody
        item={{
          id: 1,
          userId: 3,
          authorName: '李四',
          authorSide: 'provider',
          content: '已安排远程排查',
          isInternal: true,
          createdAt: '2026-09-30T07:00:00Z',
          updatedAt: '2026-09-30T07:30:00Z',
        }}
        avatar={<span data-testid="avatar" />}
        metaExtra={<span>@2</span>}
        footer={<span data-testid="footer">附件</span>}
        actions={<button type="button">编辑</button>}
        internalLabel="仅内部"
        editedLabel="已编辑"
      />
    );

    expect(screen.getByTestId('avatar')).toBeInTheDocument();
    expect(screen.getByText('李四')).toBeInTheDocument();
    expect(screen.getByText('服务商')).toBeInTheDocument();
    expect(screen.getByText('仅内部')).toBeInTheDocument();
    expect(screen.getByText('@2')).toBeInTheDocument();
    expect(screen.getByText('（已编辑）')).toBeInTheDocument();
    expect(screen.getByText('已安排远程排查')).toBeInTheDocument();
    expect(screen.getByTestId('footer')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '编辑' })).toBeInTheDocument();
  });

  it('缺省 authorSide 不渲染侧标签；未提供 editedLabel 不渲染已编辑标记', () => {
    render(
      <CommentItemBody
        item={{
          id: 2,
          userId: 9,
          content: '内部处理中',
          createdAt: '2026-09-30T08:00:00Z',
          updatedAt: '2026-09-30T09:00:00Z',
        }}
      />
    );

    expect(screen.queryByText('客户')).toBeNull();
    expect(screen.queryByText('服务商')).toBeNull();
    expect(screen.queryByText('（已编辑）')).toBeNull();
    expect(screen.getByText('#9')).toBeInTheDocument();
  });

  it('未知用户回退标签可定制', () => {
    render(
      <CommentItemBody
        item={{ id: 3, content: '无作者信息', createdAt: '2026-09-30T08:00:00Z' }}
        unknownUserLabel="未知用户"
      />
    );
    expect(screen.getByText('无作者信息')).toBeInTheDocument();
    expect(screen.getByText('未知用户')).toBeInTheDocument();
  });
});
