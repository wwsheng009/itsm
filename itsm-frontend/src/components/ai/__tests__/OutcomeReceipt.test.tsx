/**
 * I0-04 回执卡测试：可读标题、内部 ID 不外泄、三动作、标签去重、空证据不渲染空壳。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import {
  OutcomeReceipt,
  buildCitationText,
  resolveOutcomeTitle,
  shouldHideKindTag,
} from '../OutcomeReceipt';
import type { AssistantOutcome } from '@/lib/ai/outcome';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;
const writeText = jest.fn();

const outcomes: AssistantOutcome[] = [
  {
    kind: 'artifact',
    ref: 'artifact:2',
    title: '事件单字段草案',
    label: '事件单字段草案',
    route: '/ai/artifacts/2',
    description: 'draft_ticket_fields',
  },
  {
    kind: 'draft',
    ref: 'draft:/tickets/create',
    title: '去创建工单（字段草案）',
    label: '去创建工单（字段草案）',
    route: '/tickets/create',
    applyTarget: 'ticket.create.form',
  },
];

beforeEach(() => {
  mockedTrack.mockReset();
  writeText.mockReset();
  writeText.mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
    writable: true,
  });
});

describe('OutcomeReceipt（I0-04）', () => {
  it('产物条目具备可读标题与「打开」，且正文不出现内部 ID', () => {
    render(<OutcomeReceipt outcomes={outcomes} onOpen={jest.fn()} />);

    const receipt = screen.getByTestId('outcome-receipt');
    expect(receipt).toHaveTextContent('事件单字段草案');
    // 内部标识不外泄（不在正文文本节点中）。
    expect(receipt).not.toHaveTextContent('artifact:2');
    expect(receipt).not.toHaveTextContent('draft:/tickets/create');
    expect(screen.getByTestId('outcome-open-artifact-artifact:2')).toBeInTheDocument();
  });

  it('每条落点提供复制与引用；复制写标题、引用写稳定引用', async () => {
    render(<OutcomeReceipt outcomes={outcomes} onOpen={jest.fn()} />);

    fireEvent.click(screen.getByTestId('outcome-copy-artifact-artifact:2'));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('事件单字段草案'));

    writeText.mockClear();
    fireEvent.click(screen.getByTestId('outcome-cite-artifact-artifact:2'));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith('事件单字段草案（artifact:2，/ai/artifacts/2）')
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({ extra: expect.objectContaining({ action: 'cite' }) })
    );
  });

  it('打开：调用注入的 SPA 导航并记录 inline_applied(open)', () => {
    const onOpen = jest.fn();
    render(<OutcomeReceipt outcomes={outcomes} onOpen={onOpen} />);

    fireEvent.click(screen.getByTestId('outcome-open-artifact-artifact:2'));

    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ route: '/ai/artifacts/2' }));
    expect(mockedTrack).toHaveBeenCalledWith(
      'inline_applied',
      expect.objectContaining({
        extra: expect.objectContaining({ panel: 'outcome_receipt', action: 'open' }),
      })
    );
  });

  it('无 route 的落点不渲染「打开」，但仍可复制/引用', () => {
    render(
      <OutcomeReceipt
        outcomes={[{ kind: 'entity', ref: 'ticket:7', title: '工单 #7', label: '工单 #7' }]}
      />
    );
    expect(screen.queryByTestId('outcome-open-entity-ticket:7')).not.toBeInTheDocument();
    expect(screen.getByTestId('outcome-copy-entity-ticket:7')).toBeInTheDocument();
    expect(screen.getByTestId('outcome-cite-entity-ticket:7')).toBeInTheDocument();
  });

  it('标题回退：缺 title 时取摘要首句，且上报 title_fallback（可观测）', async () => {
    render(
      <OutcomeReceipt
        outcomes={[
          {
            kind: 'artifact',
            ref: 'artifact:9',
            label: '',
            title: '',
            description: 'CMDB 定位 CI。随后生成影响面。',
            route: '/ai/artifacts/9',
          },
        ]}
      />
    );

    expect(screen.getByTestId('outcome-artifact-artifact:9')).toHaveTextContent('CMDB 定位 CI');
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'inline_preview',
        expect.objectContaining({
          extra: expect.objectContaining({ action: 'title_fallback', ref: 'artifact:9' }),
        })
      )
    );
  });

  it('标签与标题相同时隐藏类型标签，避免「产物 产物」重复', () => {
    render(
      <OutcomeReceipt
        outcomes={[
          { kind: 'artifact', ref: 'artifact:2', title: '产物', label: '产物' },
          { kind: 'artifact', ref: 'artifact:3', title: '产物 #3', label: '产物 #3' },
          {
            kind: 'artifact',
            ref: 'artifact:4',
            title: '事件单字段草案',
            label: '事件单字段草案',
          },
        ]}
      />
    );

    // 同词/类型+序号：隐藏标签。
    expect(screen.getByTestId('outcome-artifact-artifact:2').querySelector('.ant-tag')).toBeNull();
    expect(screen.getByTestId('outcome-artifact-artifact:3').querySelector('.ant-tag')).toBeNull();
    // 不同词：保留类型标签。
    expect(screen.getByTestId('outcome-artifact-artifact:4').querySelector('.ant-tag')).not.toBeNull();
  });

  it('无落点不渲染', () => {
    const { container } = render(<OutcomeReceipt outcomes={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('OutcomeReceipt 纯函数', () => {
  it('resolveOutcomeTitle：title → name → 摘要首句', () => {
    expect(resolveOutcomeTitle({ kind: 'artifact', ref: 'a', label: '', title: '标题' })).toBe('标题');
    expect(resolveOutcomeTitle({ kind: 'artifact', ref: 'a', label: '', name: '别名' })).toBe('别名');
    expect(
      resolveOutcomeTitle({ kind: 'artifact', ref: 'a', label: '', description: '首句。次句' })
    ).toBe('首句');
  });

  it('shouldHideKindTag：同词/类型+序号时隐藏，不同词保留', () => {
    expect(shouldHideKindTag('产物', '产物')).toBe(true);
    expect(shouldHideKindTag('产物', '产物 #2')).toBe(true);
    expect(shouldHideKindTag('产物', '事件单字段草案')).toBe(false);
  });

  it('buildCitationText 含稳定引用与路由，便于核验', () => {
    expect(buildCitationText(outcomes[0])).toBe('事件单字段草案（artifact:2，/ai/artifacts/2）');
  });
});
