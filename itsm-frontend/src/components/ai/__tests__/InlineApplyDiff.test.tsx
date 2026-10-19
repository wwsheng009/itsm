/**
 * I1-01 InlineApplyDiff 弹窗测试：before→after 渲染、空值占位、确认/取消回调、不提交提示。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { InlineApplyDiff } from '../InlineApplyDiff';
import type { InlineChangeSet } from '../use-inline-apply';

const changes: InlineChangeSet[] = [
  { field: 'category', label: '分类', before: 'general', after: 'network' },
  { field: 'priority', label: '优先级', after: 'high' },
];

describe('InlineApplyDiff（I1-01）', () => {
  it('渲染字段差异（含空值占位）与「不自动提交」提示', () => {
    render(
      <InlineApplyDiff open changes={changes} onConfirm={jest.fn()} onCancel={jest.fn()} />
    );

    expect(screen.getByTestId('inline-apply-diff')).toBeInTheDocument();
    expect(screen.getByTestId('inline-apply-diff-row-category')).toHaveTextContent('general');
    expect(screen.getByTestId('inline-apply-diff-row-category')).toHaveTextContent('network');
    expect(screen.getByTestId('inline-apply-diff-row-priority')).toHaveTextContent('（空）');
    expect(screen.getByText(/保存\/提交仍由你在页面上完成/)).toBeInTheDocument();
  });

  it('确认/取消回调', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();
    render(
      <InlineApplyDiff open changes={changes} onConfirm={onConfirm} onCancel={onCancel} />
    );

    fireEvent.click(screen.getByText('确认应用'));
    expect(onConfirm).toHaveBeenCalledTimes(1);

    // antd 会在两个中文字符间自动插空格（取 消）→ 用可访问名匹配。
    fireEvent.click(screen.getByRole('button', { name: /取\s*消/ }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('自定义 testId 与标题', () => {
    render(
      <InlineApplyDiff
        open
        changes={changes}
        title="应用 AI 分类建议"
        testId="triage-create-apply-diff"
        onConfirm={jest.fn()}
        onCancel={jest.fn()}
      />
    );
    expect(screen.getByText('应用 AI 分类建议')).toBeInTheDocument();
    expect(screen.getByTestId('triage-create-apply-diff')).toBeInTheDocument();
  });
});