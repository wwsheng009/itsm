/**
 * V0-02 一句话建单度量接线测试：生成 → 修改分类 → 提交成功
 * （task_started / ai_create_field_corrected / task_completed）。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { A2UIFormRenderer } from '../A2UIFormRenderer';
import { A2UIApi } from '@/lib/api/a2ui-api';
import { aiInterpretTicketIntent, type TicketIntentInterpretation } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useAuthStore } from '@/lib/store/auth-store';

jest.setTimeout(300000);

jest.mock('@/lib/api/a2ui-api', () => ({
  __esModule: true,
  A2UIApi: {
    generateTicketForm: jest.fn(),
    handleTicketAction: jest.fn(),
  },
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiInterpretTicketIntent: jest.fn(),
}));

const mockedApi = A2UIApi as unknown as {
  generateTicketForm: jest.Mock;
  handleTicketAction: jest.Mock;
};
const mockedTrack = trackAssistantEvent as jest.Mock;
const mockedInterpret = aiInterpretTicketIntent as jest.Mock;

const surfaceMessages = [
  JSON.stringify({
    surfaceUpdate: {
      surfaceId: 's1',
      components: [
        { id: 'root', component: { Column: { children: { explicitList: ['type_field', 'submit_btn'] } } } },
        {
          id: 'type_field',
          component: { TextField: { label: { literalString: '类型' }, text: { path: '/ticket/type' } } },
        },
        { id: 'submit_btn', component: { Button: { child: 'submit_text', action: { name: 'submit', context: [] } } } },
        { id: 'submit_text', component: { Text: { text: { literalString: '提交工单' } } } },
      ],
    },
  }),
  JSON.stringify({
    dataModelUpdate: {
      path: 'ticket',
      contents: [
        { key: 'type', valueString: 'hardware' },
        { key: 'priority', valueString: 'medium' },
      ],
    },
  }),
];

const createdMessages = [
  JSON.stringify({
    dataModelUpdate: { path: 'ui', contents: [{ key: 'status', valueString: 'created' }] },
  }),
];

const interpretationFixture: TicketIntentInterpretation = {
  summary: '硬件故障（中）',
  matched: true,
  needsConfirmation: true,
  confidence: 0.8,
  terms: [
    { field: 'type', surface: '笔记本', term: '硬件故障', value: 'hardware', confidence: 0.85 },
  ],
  fields: {
    title: '申请笔记本',
    type: 'hardware',
    priority: 'medium',
    description: '申请笔记本',
  },
};

beforeEach(() => {
  useAuthStore.setState({ isAuthenticated: true } as never);
  mockedApi.generateTicketForm.mockReset();
  mockedApi.handleTicketAction.mockReset();
  mockedTrack.mockReset();
  mockedInterpret.mockReset();
});

describe('A2UIFormRenderer（V1-03 理解确认 + V0-02 建单度量接线）', () => {
  it('理解→确认→生成→修改分类→提交成功：task_started、字段纠正（误分类率代理）、task_completed（时长）', async () => {
    mockedInterpret.mockResolvedValue(interpretationFixture);
    mockedApi.generateTicketForm.mockResolvedValue({ code: 0, message: 'ok', messages: surfaceMessages });
    mockedApi.handleTicketAction.mockResolvedValue({ code: 0, message: 'ok', messages: createdMessages });

    render(
      <App>
        <A2UIFormRenderer pageKey="ticket.aiCreate" />
      </App>
    );

    fireEvent.change(screen.getByPlaceholderText(/ThinkPad/), { target: { value: '申请笔记本' } });
    fireEvent.click(screen.getByRole('button', { name: /发\s*送/ }));

    // 阶段一：先出「我理解成 …」确认卡；未确认前不得生成表单。
    expect(await screen.findByText('硬件故障（中）')).toBeInTheDocument();
    expect(mockedApi.generateTicketForm).not.toHaveBeenCalled();
    expect(mockedInterpret).toHaveBeenCalledWith('申请笔记本');

    // 阶段二：确认后生成表单，用户确认的字段作为初值。
    fireEvent.click(screen.getByRole('button', { name: /确\s*认/ }));
    await waitFor(() => expect(mockedApi.generateTicketForm).toHaveBeenCalledWith('申请笔记本', null));
    expect(mockedTrack).toHaveBeenCalledWith(
      'task_started',
      expect.objectContaining({ pageKey: 'ticket.aiCreate', taskCardId: 'ai_create_ticket' })
    );
    expect(await screen.findByLabelText('类型')).toHaveValue('hardware');

    // AI 建议 hardware → 用户改为 software（字段纠正 = 误分类率代理）。
    const typeInput = screen.getByLabelText('类型');
    fireEvent.change(typeInput, { target: { value: 'software' } });

    fireEvent.click(screen.getByRole('button', { name: '提交工单' }));
    await waitFor(() => expect(mockedApi.handleTicketAction).toHaveBeenCalled());

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'ai_create_field_corrected',
        expect.objectContaining({ field: 'type', taskCardId: 'ai_create_ticket' })
      )
    );
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'task_completed',
        expect.objectContaining({
          taskCardId: 'ai_create_ticket',
          durationMs: expect.any(Number),
          latencyMs: expect.any(Number),
        })
      )
    );
    // 提交成功后渲染成功态（不重复提交）。
    expect(await screen.findByText('工单创建成功')).toBeInTheDocument();
  });

  it('生成失败：task_failed（generate_failed），不产生 task_completed', async () => {
    mockedInterpret.mockResolvedValue(interpretationFixture);
    mockedApi.generateTicketForm.mockRejectedValue(new Error('boom'));

    render(
      <App>
        <A2UIFormRenderer pageKey="ticket.aiCreate" />
      </App>
    );
    fireEvent.change(screen.getByPlaceholderText(/ThinkPad/), { target: { value: '任何需求' } });
    fireEvent.click(screen.getByRole('button', { name: /发\s*送/ }));
    fireEvent.click(await screen.findByRole('button', { name: /确\s*认/ }));

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'task_failed',
        expect.objectContaining({ errorCode: 'generate_failed', taskCardId: 'ai_create_ticket' })
      )
    );
    const names = mockedTrack.mock.calls.map(call => call[0]);
    expect(names).not.toContain('task_completed');
  });

  it('理解失败降级：跳过确认卡直接生成（不阻塞建单）', async () => {
    mockedInterpret.mockRejectedValue(new Error('interpret down'));
    mockedApi.generateTicketForm.mockResolvedValue({ code: 0, message: 'ok', messages: surfaceMessages });

    render(
      <App>
        <A2UIFormRenderer pageKey="ticket.aiCreate" />
      </App>
    );
    fireEvent.change(screen.getByPlaceholderText(/ThinkPad/), { target: { value: '打印机坏了' } });
    fireEvent.click(screen.getByRole('button', { name: /发\s*送/ }));

    await waitFor(() => expect(mockedApi.generateTicketForm).toHaveBeenCalledWith('打印机坏了', null));
    expect(screen.queryByText('我理解成：')).not.toBeInTheDocument();
    expect(await screen.findByLabelText('类型')).toBeInTheDocument();
  });

  it('修改理解：纠正后的术语作为表单初值，纠正只计一次（不与提交基线重复计数）', async () => {
    mockedInterpret.mockResolvedValue(interpretationFixture);
    mockedApi.generateTicketForm.mockResolvedValue({ code: 0, message: 'ok', messages: surfaceMessages });
    mockedApi.handleTicketAction.mockResolvedValue({ code: 0, message: 'ok', messages: createdMessages });

    render(
      <App>
        <A2UIFormRenderer pageKey="ticket.aiCreate" />
      </App>
    );
    fireEvent.change(screen.getByPlaceholderText(/ThinkPad/), { target: { value: '申请笔记本' } });
    fireEvent.click(screen.getByRole('button', { name: /发\s*送/ }));

    fireEvent.click(await screen.findByRole('button', { name: /修\s*改/ }));
    fireEvent.mouseDown(screen.getByLabelText('分类'));
    fireEvent.click(
      await screen.findByText('软件问题', { selector: '.ant-select-item-option-content' })
    );
    fireEvent.click(screen.getByRole('button', { name: /继\s*续/ }));

    await waitFor(() => expect(mockedApi.generateTicketForm).toHaveBeenCalledWith('申请笔记本', null));
    expect(await screen.findByLabelText('类型')).toHaveValue('software');

    const correctionCalls = () =>
      mockedTrack.mock.calls.filter(call => call[0] === 'ai_create_field_corrected');
    expect(correctionCalls()).toHaveLength(1);

    // 提交（表单内不再修改）→ 基线已按用户确认值记录，不得重复计入纠正。
    fireEvent.click(screen.getByRole('button', { name: '提交工单' }));
    await waitFor(() => expect(mockedApi.handleTicketAction).toHaveBeenCalled());
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'task_completed',
        expect.objectContaining({ taskCardId: 'ai_create_ticket' })
      )
    );
    expect(correctionCalls()).toHaveLength(1);
  });
});
