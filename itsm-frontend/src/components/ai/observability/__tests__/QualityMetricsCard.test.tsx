/**
 * T5-06b 质量层指标卡测试：三率/分母样本/空分母降级。
 */
import React from 'react';
import { render, screen } from '@testing-library/react';

import QualityMetricsCard from '../QualityMetricsCard';

jest.setTimeout(300000);

describe('QualityMetricsCard（T5-06b）', () => {
  it('渲染引用覆盖率/无引用回答率/点踩率与分母样本', () => {
    render(
      <QualityMetricsCard
        quality={{
          answerCount: 5,
          citationAnswerCount: 3,
          feedbackCount: 8,
          downvoteCount: 2,
          citationCoverage: 0.6,
          noCitationRate: 0.4,
          downvoteRate: 0.25,
        }}
      />
    );

    expect(screen.getByTestId('quality-measurable')).toHaveTextContent('可测');
    expect(screen.getByTestId('quality-citation-coverage')).toHaveTextContent('60.0%');
    expect(screen.getByTestId('quality-no-citation-rate')).toHaveTextContent('40.0%');
    expect(screen.getByTestId('quality-downvote-rate')).toHaveTextContent('25.0%');
    expect(screen.getByTestId('quality-citation-sample')).toHaveTextContent('有引用 3 / 可统计 5 回答');
    expect(screen.getByTestId('quality-no-citation-sample')).toHaveTextContent('无引用 2 / 可统计 5 回答');
    expect(screen.getByTestId('quality-downvote-sample')).toHaveTextContent('点踩 2 / 反馈 8');
    expect(screen.queryByTestId('quality-empty')).not.toBeInTheDocument();
  });

  it('无分母：比率显示「—」，不伪造 0%，并给出空样本说明', () => {
    render(
      <QualityMetricsCard
        quality={{
          answerCount: 0,
          citationAnswerCount: 0,
          feedbackCount: 0,
          downvoteCount: 0,
        }}
      />
    );

    expect(screen.getByTestId('quality-citation-coverage')).toHaveTextContent('—');
    expect(screen.getByTestId('quality-no-citation-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('quality-downvote-rate')).toHaveTextContent('—');
    expect(screen.getByTestId('quality-empty')).toBeInTheDocument();
  });

  it('真实 0 值保留为 0.0%（与空分母区分）', () => {
    render(
      <QualityMetricsCard
        quality={{
          answerCount: 4,
          citationAnswerCount: 4,
          feedbackCount: 4,
          downvoteCount: 0,
          citationCoverage: 1,
          noCitationRate: 0,
          downvoteRate: 0,
        }}
      />
    );

    expect(screen.getByTestId('quality-citation-coverage')).toHaveTextContent('100.0%');
    expect(screen.getByTestId('quality-no-citation-rate')).toHaveTextContent('0.0%');
    expect(screen.getByTestId('quality-downvote-rate')).toHaveTextContent('0.0%');
  });
});
