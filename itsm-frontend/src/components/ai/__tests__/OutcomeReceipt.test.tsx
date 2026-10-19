/**
 * I0-04 回执卡测试：渲染落点（kind 标签/引用/描述）、打开（注入 SPA 导航）、复制引用、空列表不渲染。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { OutcomeReceipt } from '../OutcomeReceipt';
import type { AssistantOutcome } from '@/lib/ai/outcome';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

const outcomes: AssistantOutcome[] = [
  {
    kind: 'artifact',
    ref: 'artifact:31',
    label: '产物 #31',
    description: 'analyze_ci_impact_plan',
  },
  {
    kind: 'draft',
    ref: 'draft:/tickets/create',
    label: '去创建工单（字段草案）',
    route: '/tickets/create',
    applyTarget: 'ticket.create.form',
  },
];

beforeEach(() => mockedTrack.mockReset());

describe('OutcomeReceipt（I0-04）', () => {
  it('渲染落点：kind 标签、引用、仅可跳转项显示「打开」', () => {
    render(<OutcomeReceipt outcomes={outcomes} onOpen={jest.fn()} />);

    expect(screen.getByTestId('outcome-receipt')).toBeInTheDocument();
    expect(screen.getByTestId('outcome-artifact-artifact:31')).toHaveTextContent('产物 #31');
    expect(screen.getByTestId('outcome-draft-draft:/tickets/create')).toHaveTextContent(
      '去创建工单（字段草案）'
    );
    // 产物无 route → 不显示「打开」，但可复制引用。
    expect(screen.queryByTestId('outcome-open-artifact-artifact:31')).not.toBeInTheDocument();
    expect(screen.getByTestId('outcome-copy-artifact-artifact:31')).toBeInTheDocument();
  });

  it('打开：调用注入的 SPA 导航并记录 inline_applied(open)', () => {
    const onOpen = jest.fn();
    render(<OutcomeReceipt outcomes={outcomes} onOpen={onOpen} />);

    fireEvent.click(screen.getByTestId('outcome-open-draft-draft:/tickets/create'));

    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ route: '/tickets/create' }));
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({
        extra: expect.objectContaining({ panel: 'outcome_receipt', action: 'open' }),
      })
    );
  });

  it('复制引用并记录 inline_applied(copy)', async () => {
    render(<OutcomeReceipt outcomes={outcomes} />);

    fireEvent.click(screen.getByTestId('outcome-copy-artifact-artifact:31'));

    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({
        extra: expect.objectContaining({ panel: 'outcome_receipt', action: 'copy', ref: 'artifact:31' }),
      })
    );
  });

  it('无落点不渲染', () => {
    const { container } = render(<OutcomeReceipt outcomes={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});