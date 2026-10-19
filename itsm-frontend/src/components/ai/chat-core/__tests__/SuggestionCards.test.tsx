/**
 * D1-01 建议卡数据源测试：服务端过滤优先、失败/未知页面回退静态基线、
 * 空结果视为权威（不下发）、会话级缓存不重复请求。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import SuggestionCards from '../SuggestionCards';
import { aiGetAssistantContext, aiGetDynamicSuggestions } from '@/lib/api/ai-api';
import {
  clearAssistantActions,
  registerAssistantAction,
} from '@/lib/ai/assistant-actions';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { clearAssistantSuggestionCache } from '@/lib/ai/page-context/use-assistant-suggestions';
import { clearDynamicSuggestionCache } from '@/lib/ai/page-context/use-dynamic-suggestions';

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetAssistantContext: jest.fn(),
  aiGetDynamicSuggestions: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
// jest.setup.js 全局 mock 了 useLocation（固定 `/`）；建议卡依赖真实路由解析，故还原真实实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedContext = aiGetAssistantContext as jest.MockedFunction<typeof aiGetAssistantContext>;
const mockedDynamic = aiGetDynamicSuggestions as jest.MockedFunction<
  typeof aiGetDynamicSuggestions
>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const renderCards = (route = '/tickets/42') =>
  render(
    <MemoryRouter initialEntries={[route]}>
      <SuggestionCards onSend={jest.fn()} compact />
    </MemoryRouter>
  );

beforeEach(() => {
  clearAssistantSuggestionCache();
  clearDynamicSuggestionCache();
  clearAssistantActions();
  mockedContext.mockReset();
  mockedDynamic.mockReset();
  mockedDynamic.mockResolvedValue({
    pageKey: 'dashboard',
    known: true,
    generated: false,
    cached: false,
    reason: 'disabled',
    suggestions: [],
  });
  mockedTrack.mockReset();
});

describe('SuggestionCards（D1-01 服务端建议过滤）', () => {
  it('服务端过滤结果优先：仅渲染下发建议，埋点标记 feed=server', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      filtering: 'enabled',
      scene: { slug: 's1-ticket-assistant', eligible: true, reason: '' },
      suggestions: [
        {
          id: 'reply_draft',
          kind: 'task',
          label: '生成答复草稿',
          prompt: '请生成答复草稿',
          readOnly: true,
          rank: 1,
        },
      ],
      filtered: [{ id: 'triage_plan', reason: 'rbac_denied' }],
    });

    renderCards();

    // 静态基线先行（零延迟），服务端结果到达后替换。
    expect(await screen.findByTestId('suggestion-card-reply_draft')).toHaveTextContent(
      '生成答复草稿'
    );
    await waitFor(() =>
      expect(screen.queryByTestId('suggestion-card-triage_plan')).not.toBeInTheDocument()
    );
    expect(mockedContext).toHaveBeenCalledWith('ticket.detail', '/tickets/42');

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_shown',
        expect.objectContaining({
          pageKey: 'ticket.detail',
          cardId: 'reply_draft',
          extra: { feed: 'server' },
        })
      )
    );
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'context_loaded',
        expect.objectContaining({ pageKey: 'ticket.detail' })
      )
    );
  });

  it('请求失败回退静态基线并上报 context_degraded', async () => {
    mockedContext.mockRejectedValue(new Error('network'));

    renderCards();

    expect(await screen.findByTestId('suggestion-card-triage_plan')).toBeInTheDocument();
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'context_degraded',
        expect.objectContaining({ pageKey: 'ticket.detail', reasonCode: 'context_fetch_failed' })
      )
    );
  });

  it('known=false（服务端不认识页面）回退静态基线', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: false,
      suggestions: [],
    });

    renderCards();

    expect(await screen.findByTestId('suggestion-card-triage_plan')).toBeInTheDocument();
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'context_degraded',
        expect.objectContaining({ pageKey: 'ticket.detail', reasonCode: 'context_unknown_page' })
      )
    );
  });

  it('服务端返回空建议为权威结果（全部被过滤时不下发）', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      filtering: 'enabled',
      scene: { slug: 's1-ticket-assistant', eligible: false, reason: 'template_missing' },
      suggestions: [],
      filtered: [
        { id: 'triage_plan', reason: 'template_missing' },
        { id: 'reply_draft', reason: 'template_missing' },
        { id: 'kb_lookup', reason: 'template_missing' },
      ],
    });

    renderCards();

    expect(await screen.findByTestId('suggestion-cards')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByTestId('suggestion-card-triage_plan')).not.toBeInTheDocument()
    );
    expect(mockedTrack).not.toHaveBeenCalledWith('context_degraded', expect.anything());
  });

  it('会话级缓存：同一页面二次渲染不重复请求', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      suggestions: [
        {
          id: 'reply_draft',
          kind: 'task',
          label: '生成答复草稿',
          prompt: '请生成答复草稿',
          readOnly: true,
          rank: 0,
        },
      ],
    });

    const first = renderCards();
    await waitFor(() => expect(mockedContext).toHaveBeenCalledTimes(1));
    first.unmount();

    renderCards();
    expect(await screen.findByTestId('suggestion-card-reply_draft')).toBeInTheDocument();
    expect(mockedContext).toHaveBeenCalledTimes(1);
  });

  it('D1-02 动作桥：已注册 handler 优先，不再发送提示词', async () => {
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    const handler = jest.fn();
    const unregister = registerAssistantAction('triage_plan', handler);
    try {
      const onSend = jest.fn();
      render(
        <MemoryRouter initialEntries={['/tickets/42']}>
          <SuggestionCards onSend={onSend} compact />
        </MemoryRouter>
      );

      const card = await screen.findByTestId('suggestion-card-triage_plan');
      fireEvent.click(card);

      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({ actionId: 'triage_plan' })
      );
      expect(onSend).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(mockedTrack).toHaveBeenCalledWith(
          'suggestion_clicked',
          expect.objectContaining({
            cardId: 'triage_plan',
            extra: expect.objectContaining({ dispatch: 'handler' }),
          })
        )
      );
    } finally {
      unregister();
    }
  });

  it('D1-02 动作桥：未注册 handler 回退发送提示词（dispatch=prompt）', async () => {
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    const onSend = jest.fn();
    render(
      <MemoryRouter initialEntries={['/tickets/42']}>
        <SuggestionCards onSend={onSend} compact />
      </MemoryRouter>
    );

    const card = await screen.findByTestId('suggestion-card-triage_plan');
    fireEvent.click(card);

    expect(onSend).toHaveBeenCalledWith(expect.stringContaining('分类与优先级建议'));
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_clicked',
        expect.objectContaining({
          cardId: 'triage_plan',
          extra: expect.objectContaining({ dispatch: 'prompt' }),
        })
      )
    );
  });

  it('I0-02 分组：注册页渲染任务卡/问题卡两组，任务在前', async () => {
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    renderCards();

    const taskGroup = await screen.findByTestId('suggestion-group-tasks');
    const questionGroup = screen.getByTestId('suggestion-group-questions');
    expect(taskGroup).toHaveTextContent('任务卡（零输入可执行；只读）');
    expect(taskGroup).toHaveTextContent('分诊建议（分类与优先级）');
    expect(questionGroup).toHaveTextContent('问题卡（了解）');
    expect(questionGroup).toHaveTextContent('知识库有没有相关方案？');

    // 任务组在问题组之前（DOM 顺序 = 服务端 rank 顺序）。
    const groups = screen.getAllByTestId(/suggestion-group-/);
    expect(groups[0]).toHaveAttribute('data-testid', 'suggestion-group-tasks');
  });

  it('P2 LLM 动态建议：置于任务组首位，点击发送其提示词并标记 feed=llm', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      filtering: 'enabled',
      suggestions: [
        {
          id: 'triage_plan',
          kind: 'task',
          label: '分诊建议（分类与优先级）',
          prompt: '请给出分类与优先级建议',
          readOnly: true,
          rank: 0,
        },
      ],
      filtered: [],
    });
    mockedDynamic.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      generated: true,
      cached: false,
      model: 'gpt-4o',
      suggestions: [
        {
          id: 'dyn:0',
          kind: 'task',
          label: '先看 SLA 风险',
          prompt: '看看这张单临近 SLA 的风险',
          readOnly: true,
          source: 'llm',
        },
      ],
    });

    const onSend = jest.fn();
    render(
      <MemoryRouter initialEntries={['/tickets/42']}>
        <SuggestionCards onSend={onSend} compact />
      </MemoryRouter>
    );

    const dynamicCard = await screen.findByTestId('suggestion-card-dyn:0');
    expect(dynamicCard).toHaveTextContent('先看 SLA 风险');

    // 动态任务卡在静态任务卡之前（组内首位；分组结构不变）。
    const taskGroup = screen.getByTestId('suggestion-group-tasks');
    const buttons = taskGroup.querySelectorAll('button');
    expect(buttons[0]).toHaveAttribute('data-testid', 'suggestion-card-dyn:0');
    expect(screen.getByTestId('suggestion-card-triage_plan')).toBeInTheDocument();

    // 展示埋点标记 feed=llm（可聚合 LLM 建议的曝/点/忽略）。
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_shown',
        expect.objectContaining({
          cardId: 'dyn:0',
          extra: expect.objectContaining({ feed: 'llm' }),
        })
      )
    );

    fireEvent.click(dynamicCard);
    expect(onSend).toHaveBeenCalledWith('看看这张单临近 SLA 的风险');
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_clicked',
        expect.objectContaining({
          cardId: 'dyn:0',
          extra: expect.objectContaining({ feed: 'llm' }),
        })
      )
    );
  });

  it('D1-02 忽略采集：展示后未点击的卡在卸载时结算 ignored（含 kind/feed）', async () => {
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    const view = renderCards();
    await screen.findByTestId('suggestion-card-triage_plan');
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_shown',
        expect.objectContaining({ cardId: 'triage_plan' })
      )
    );

    view.unmount();

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_ignored',
        expect.objectContaining({
          pageKey: 'ticket.detail',
          cardId: 'triage_plan',
          cardKind: 'task',
          rank: expect.any(Number),
          extra: { feed: 'static' },
        })
      )
    );
  });

  it('D1-02 忽略采集：点击过的卡豁免，其余卡仍结算 ignored', async () => {
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    const view = renderCards();
    const card = await screen.findByTestId('suggestion-card-triage_plan');
    fireEvent.click(card);
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_clicked',
        expect.objectContaining({ cardId: 'triage_plan' })
      )
    );

    view.unmount();

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_ignored',
        expect.objectContaining({ cardId: 'kb_lookup' })
      )
    );
    expect(mockedTrack).not.toHaveBeenCalledWith(
      'suggestion_ignored',
      expect.objectContaining({ cardId: 'triage_plan' })
    );
  });

  it('D1-02 忽略采集：服务端结果替换基线时只结算消失的卡（保留卡不误报）', async () => {
    mockedContext.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      filtering: 'enabled',
      suggestions: [
        {
          id: 'reply_draft',
          kind: 'task',
          label: '生成答复草稿',
          prompt: '请生成答复草稿',
          readOnly: true,
          rank: 0,
        },
      ],
      filtered: [],
    });

    const view = renderCards();

    // 静态基线先展示；服务端结果到达后 triage_plan/kb_lookup 消失 → 各自结算一次 ignored。
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_ignored',
        expect.objectContaining({ cardId: 'triage_plan' })
      )
    );
    expect(mockedTrack).not.toHaveBeenCalledWith(
      'suggestion_ignored',
      expect.objectContaining({ cardId: 'reply_draft' })
    );

    // 面板关闭（卸载）→ 仍在展示且未点击的 reply_draft 也结算一次；消失的卡不重复。
    view.unmount();
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_ignored',
        expect.objectContaining({ cardId: 'reply_draft', extra: { feed: 'server' } })
      )
    );
    const triageIgnores = mockedTrack.mock.calls.filter(
      ([name, payload]) =>
        name === 'suggestion_ignored' &&
        (payload as { cardId?: string } | undefined)?.cardId === 'triage_plan'
    );
    expect(triageIgnores).toHaveLength(1);
  });

  it('D1-02 忽略采集：StrictMode 双挂载不产生虚假 ignored（卸载后仍结算）', async () => {
    // 先清空此前用例遗留的 0ms 结算宏任务（卸载结算延迟一拍），再做负向断言。
    await new Promise(resolve => setTimeout(resolve, 0));
    mockedTrack.mockClear();
    mockedContext.mockResolvedValue({ pageKey: 'ticket.detail', known: false, suggestions: [] });
    const view = render(
      <React.StrictMode>
        <MemoryRouter initialEntries={['/tickets/42']}>
          <SuggestionCards onSend={jest.fn()} compact />
        </MemoryRouter>
      </React.StrictMode>
    );
    await screen.findByTestId('suggestion-card-triage_plan');
    await new Promise(resolve => setTimeout(resolve, 20));
    expect(mockedTrack).not.toHaveBeenCalledWith('suggestion_ignored', expect.anything());

    view.unmount();
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'suggestion_ignored',
        expect.objectContaining({ cardId: 'triage_plan' })
      )
    );
  });
});
