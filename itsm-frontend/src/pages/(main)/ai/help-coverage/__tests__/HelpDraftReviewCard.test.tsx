import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import HelpDraftReviewCard from '../HelpDraftReviewCard';
import type { HelpDraftReviewItem } from '@/lib/api/ai-api';

const draft: HelpDraftReviewItem = {
  artifactId: 31,
  ownerUserId: 9,
  pageKey: 'ticket.detail',
  title: '工单详情帮助草稿',
  status: 'draft',
  revision: 2,
  markdown: '## 新内容\n- 新增步骤',
  baselineMarkdown: '## 旧内容',
  diff: [
    { type: 'removed', content: '## 旧内容' },
    { type: 'added', content: '## 新内容' },
    { type: 'added', content: '- 新增步骤' },
  ],
  sourceDocs: ['docs/help/tickets/detail.md'],
  createdAt: '2026-10-19T00:00:00Z',
};

describe('HelpDraftReviewCard（T5-09a）', () => {
  it('展示草稿并打开 diff；提交审阅走显式回调', () => {
    const onSubmitReview = jest.fn();
    render(
      <HelpDraftReviewCard
        items={[draft]}
        onSubmitReview={onSubmitReview}
        onPublish={jest.fn()}
      />
    );

    expect(screen.getByTestId('help-draft-table')).toHaveTextContent('工单详情帮助草稿');
    expect(screen.getByTestId('help-draft-status-draft')).toHaveTextContent('草稿');

    fireEvent.click(screen.getByTestId('help-draft-diff-31'));
    expect(screen.getByTestId('help-draft-diff-body')).toHaveTextContent('## 新内容');
    expect(screen.getAllByTestId('help-draft-diff-added')[0]).toHaveTextContent('+ ## 新内容');
    expect(screen.getByTestId('help-draft-diff-removed')).toHaveTextContent('- ## 旧内容');

    fireEvent.click(screen.getByTestId('help-draft-submit-31'));
    expect(onSubmitReview).toHaveBeenCalledWith(draft);
  });

  it('审阅中只能发布；已发布提供文章入口', () => {
    const onPublish = jest.fn();
    const inReview = { ...draft, status: 'in_review', knowledgeArticleId: 501 };
    const { rerender } = render(
      <HelpDraftReviewCard
        items={[inReview]}
        canPublish={false}
        onSubmitReview={jest.fn()}
        onPublish={onPublish}
      />
    );
    expect(screen.queryByTestId('help-draft-submit-31')).not.toBeInTheDocument();
    expect(screen.queryByTestId('help-draft-publish-31')).not.toBeInTheDocument();
    expect(screen.getByText('需知识库管理员发布')).toBeInTheDocument();

    rerender(
      <HelpDraftReviewCard
        items={[inReview]}
        canPublish
        onSubmitReview={jest.fn()}
        onPublish={onPublish}
      />
    );
    fireEvent.click(screen.getByTestId('help-draft-publish-31'));
    expect(onPublish).toHaveBeenCalledWith(inReview);

    rerender(
      <HelpDraftReviewCard
        items={[{ ...draft, status: 'published', knowledgeArticleId: 501 }]}
        onSubmitReview={jest.fn()}
        onPublish={onPublish}
      />
    );
    expect(screen.getByTestId('help-draft-open-31')).toHaveAttribute(
      'href',
      '/knowledge/articles/501'
    );
  });
});
