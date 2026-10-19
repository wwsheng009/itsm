/**
 * V1-03 口语↔术语确认卡单测：
 * 展示「我理解成：网络故障（高）」+ 证据片段；确认 → 生成；修改 → 纠正字段后继续。
 * 埋点断言复用既有事件字典（confirmation_* / ai_create_field_corrected），零新增。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen } from '@testing-library/react';

import {
  IntentUnderstandingCard,
  INTENT_INTERPRETATION_TASK_ID,
} from '../IntentUnderstandingCard';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import type { TicketIntentInterpretation } from '@/lib/api/ai-api';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

const interpretation: TicketIntentInterpretation = {
  summary: '网络故障（高）',
  matched: true,
  needsConfirmation: true,
  confidence: 0.915,
  terms: [
    { field: 'type', surface: '连不上网', term: '网络故障', value: 'network', confidence: 0.93 },
    { field: 'priority', surface: '很急', term: '高', value: 'high', confidence: 0.9 },
  ],
  fields: {
    title: '我的电脑连不上网了，很急',
    type: 'network',
    priority: 'high',
    description: '我的电脑连不上网了，很急',
  },
};

beforeEach(() => {
  mockedTrack.mockReset();
});

describe('IntentUnderstandingCard（V1-03 我理解成 → 确认 / 修改）', () => {
  it('展示归一化术语与口语证据片段，并上报曝光事件', () => {
    render(
      <App>
        <IntentUnderstandingCard
          interpretation={interpretation}
          onConfirm={jest.fn()}
          onModify={jest.fn()}
          pageKey="ticket.aiCreate"
        />
      </App>
    );

    expect(screen.getByText(/我理解成：/)).toBeInTheDocument();
    expect(screen.getByText('网络故障（高）')).toBeInTheDocument();
    expect(screen.getByText('「连不上网」→ 网络故障')).toBeInTheDocument();
    expect(screen.getByText('「很急」→ 高')).toBeInTheDocument();

    expect(mockedTrack).toHaveBeenCalledWith(
      'confirmation_shown',
      expect.objectContaining({
        pageKey: 'ticket.aiCreate',
        taskCardId: INTENT_INTERPRETATION_TASK_ID,
      })
    );
  });

  it('未识别术语时按默认值提示（不假装识别成功）', () => {
    render(
      <App>
        <IntentUnderstandingCard
          interpretation={{
            ...interpretation,
            matched: false,
            confidence: 0.35,
            summary: '其他（中）',
            terms: [],
            fields: { ...interpretation.fields, type: 'other', priority: 'medium' },
          }}
          onConfirm={jest.fn()}
          pageKey="ticket.aiCreate"
        />
      </App>
    );

    expect(screen.getByText(/没识别到明确术语，先按默认理解：/)).toBeInTheDocument();
    expect(screen.getByText('其他（中）')).toBeInTheDocument();
    expect(mockedTrack).toHaveBeenCalledWith(
      'confirmation_shown',
      expect.objectContaining({ extra: expect.objectContaining({ matched: false }) })
    );
  });

  it('确认原样理解：onConfirm + confirmation_accepted', () => {
    const onConfirm = jest.fn();
    render(
      <App>
        <IntentUnderstandingCard
          interpretation={interpretation}
          onConfirm={onConfirm}
          onModify={jest.fn()}
          pageKey="ticket.aiCreate"
        />
      </App>
    );

    fireEvent.click(screen.getByRole('button', { name: /确\s*认/ }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(mockedTrack).toHaveBeenCalledWith(
      'confirmation_accepted',
      expect.objectContaining({ taskCardId: INTENT_INTERPRETATION_TASK_ID })
    );
  });

  it('修改：选择新优先级 → 继续，仅回传变化字段并上报纠正（不重复计数）', async () => {
    const onConfirm = jest.fn();
    const onModify = jest.fn();
    render(
      <App>
        <IntentUnderstandingCard
          interpretation={interpretation}
          onConfirm={onConfirm}
          onModify={onModify}
          pageKey="ticket.aiCreate"
        />
      </App>
    );

    fireEvent.click(screen.getByRole('button', { name: /修\s*改/ }));
    expect(mockedTrack).toHaveBeenCalledWith(
      'confirmation_rejected',
      expect.objectContaining({ reasonCode: 'modify' })
    );

    // 优先级 高 → 中（antd Select：mouseDown 展开 + 点击选项）。
    fireEvent.mouseDown(screen.getByLabelText('优先级'));
    fireEvent.click(
      await screen.findByText('中', { selector: '.ant-select-item-option-content' })
    );
    fireEvent.click(screen.getByRole('button', { name: /继\s*续/ }));

    expect(onModify).toHaveBeenCalledWith({ priority: 'medium' });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(mockedTrack).toHaveBeenCalledWith(
      'ai_create_field_corrected',
      expect.objectContaining({
        field: 'priority',
        taskCardId: INTENT_INTERPRETATION_TASK_ID,
        extra: expect.objectContaining({ from: 'high', to: 'medium' }),
      })
    );
    // 只纠正了优先级：不得多发 type 的纠正事件。
    const correctedCalls = mockedTrack.mock.calls.filter(call => call[0] === 'ai_create_field_corrected');
    expect(correctedCalls).toHaveLength(1);
  });

  it('修改后未改动任何字段：继续等价确认（不产生纠正事件）', () => {
    const onConfirm = jest.fn();
    const onModify = jest.fn();
    render(
      <App>
        <IntentUnderstandingCard
          interpretation={interpretation}
          onConfirm={onConfirm}
          onModify={onModify}
          pageKey="ticket.aiCreate"
        />
      </App>
    );

    fireEvent.click(screen.getByRole('button', { name: /修\s*改/ }));
    fireEvent.click(screen.getByRole('button', { name: /继\s*续/ }));

    expect(onModify).not.toHaveBeenCalled();
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(
      mockedTrack.mock.calls.filter(call => call[0] === 'ai_create_field_corrected')
    ).toHaveLength(0);
  });
});