/**
 * D1-02 动作桥单测：handler 优先 / route 跳转 / 提示词回退 / 注销与后注册者优先。
 */
import {
  clearAssistantActions,
  executeAssistantAction,
  registerAssistantAction,
} from '../assistant-actions';

const makeCtx = () => ({
  pageData: null,
  navigate: jest.fn(),
  sendPrompt: jest.fn(),
});

beforeEach(clearAssistantActions);
afterEach(clearAssistantActions);

describe('assistant-actions（D1-02 动作桥）', () => {
  it('已注册 handler 优先执行，不触发跳转/提示词', () => {
    const handler = jest.fn();
    registerAssistantAction('triage_plan', handler);
    const ctx = makeCtx();

    const dispatch = executeAssistantAction(
      { actionId: 'triage_plan', prompt: '分诊建议' },
      ctx
    );

    expect(dispatch).toBe('handler');
    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({ actionId: 'triage_plan', prompt: '分诊建议' })
    );
    expect(ctx.navigate).not.toHaveBeenCalled();
    expect(ctx.sendPrompt).not.toHaveBeenCalled();
  });

  it('未注册 handler 但声明 route → 路由跳转', () => {
    const ctx = makeCtx();

    const dispatch = executeAssistantAction(
      { actionId: 'create_ticket_entry', prompt: '新建工单', route: '/tickets/create' },
      ctx
    );

    expect(dispatch).toBe('navigated');
    expect(ctx.navigate).toHaveBeenCalledWith('/tickets/create');
    expect(ctx.sendPrompt).not.toHaveBeenCalled();
  });

  it('无 handler 无 route → 发送提示词（D0-05 默认行为）', () => {
    const ctx = makeCtx();

    const dispatch = executeAssistantAction(
      { actionId: 'summary', prompt: '汇总一下' },
      ctx
    );

    expect(dispatch).toBe('prompt');
    expect(ctx.sendPrompt).toHaveBeenCalledWith('汇总一下');
    expect(ctx.navigate).not.toHaveBeenCalled();
  });

  it('handler 抛错不阻塞（仍返回 handler，后续动作正常）', () => {
    registerAssistantAction('broken', () => {
      throw new Error('boom');
    });
    const ctx = makeCtx();

    const dispatch = executeAssistantAction({ actionId: 'broken', prompt: 'x' }, ctx);

    expect(dispatch).toBe('handler');
    // 抛错被桥吸收：既不跳转也不回落提示词（避免重复下发）。
    expect(ctx.sendPrompt).not.toHaveBeenCalled();

    // 桥仍可继续服务其它动作。
    expect(executeAssistantAction({ actionId: 'other', prompt: 'y' }, ctx)).toBe('prompt');
  });

  it('注销后回退；同一 actionId 后注册者优先、旧注销不误删', () => {
    const first = jest.fn();
    const second = jest.fn();
    const unregisterFirst = registerAssistantAction('a1', first);
    const unregisterSecond = registerAssistantAction('a1', second);

    executeAssistantAction({ actionId: 'a1', prompt: 'p' }, makeCtx());
    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();

    // 旧注销（first 的注销器）不得移除 second。
    unregisterFirst();
    executeAssistantAction({ actionId: 'a1', prompt: 'p' }, makeCtx());
    expect(second).toHaveBeenCalledTimes(2);

    unregisterSecond();
    const ctx = makeCtx();
    expect(executeAssistantAction({ actionId: 'a1', prompt: 'p' }, ctx)).toBe('prompt');
    expect(ctx.sendPrompt).toHaveBeenCalledWith('p');
  });
});