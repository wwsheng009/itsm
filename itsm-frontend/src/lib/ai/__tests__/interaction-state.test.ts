/**
 * I0-01：交互状态机 + 错误字典（§3.2）。
 *
 * 覆盖验收要求：状态判定优先级、10 个状态、字典命中/兜底、错误归类、恢复动作。
 */
import {
  ASSISTANT_ERROR_DICTIONARY,
  ASSISTANT_ERROR_FALLBACK,
  classifyAssistantError,
  deriveInteractionState,
  resolveAssistantErrorCopy,
  type AssistantInteractionSignals,
} from '../interaction-state';

const base: AssistantInteractionSignals = {
  panelOpen: true,
  contextReady: true,
  hasMessages: false,
  streaming: false,
};

describe('deriveInteractionState（§3.2 状态机）', () => {
  it('closed：面板收起 → FAB 态', () => {
    expect(deriveInteractionState({ ...base, panelOpen: false })).toBe('closed');
  });

  it('opening：已展开但上下文未就绪 → 骨架屏（即使有历史消息）', () => {
    expect(deriveInteractionState({ ...base, contextReady: false, hasMessages: true })).toBe('opening');
  });

  it('ready：就绪且未开始', () => {
    expect(deriveInteractionState(base)).toBe('ready');
  });

  it('streaming：正在流式生成', () => {
    expect(deriveInteractionState({ ...base, streaming: true })).toBe('streaming');
  });

  it('tool_running：工具优先于流式（工具执行期也算生成中）', () => {
    expect(deriveInteractionState({ ...base, streaming: true, runningTool: 'list_tickets' })).toBe(
      'tool_running'
    );
  });

  it('awaiting_confirmation：待确认优先于工具/流式', () => {
    expect(
      deriveInteractionState({ ...base, streaming: true, runningTool: 'create_ticket', pendingConfirmation: true })
    ).toBe('awaiting_confirmation');
  });

  it('awaiting_approval：待审批优先于工具/流式', () => {
    expect(deriveInteractionState({ ...base, runningTool: 'create_ticket', pendingApprovalCount: 2 })).toBe(
      'awaiting_approval'
    );
  });

  it('done：有消息且已产出落点', () => {
    expect(deriveInteractionState({ ...base, hasMessages: true, outcomeCount: 1 })).toBe('done');
    expect(deriveInteractionState({ ...base, hasMessages: true, runStatus: 'completed' })).toBe('done');
  });

  it('done 不被"只有消息、无产物"误触发', () => {
    expect(deriveInteractionState({ ...base, hasMessages: true })).toBe('ready');
  });

  it('handoff：转人工', () => {
    expect(deriveInteractionState({ ...base, hasMessages: true, handoff: true })).toBe('handoff');
  });

  it('degraded：错误码 / 可读错误 / 运行失败 三者任一 → 降级（最高优先级）', () => {
    expect(deriveInteractionState({ ...base, errorCode: 'rate_limited', streaming: true })).toBe('degraded');
    expect(deriveInteractionState({ ...base, hasError: true })).toBe('degraded');
    expect(deriveInteractionState({ ...base, runStatus: 'failed', pendingApprovalCount: 1 })).toBe('degraded');
  });
});

describe('错误字典（原因码 → 文案 → 动作）', () => {
  it('覆盖真实 Provider 错误码与运行时错误码', () => {
    for (const code of [
      'AI_PROVIDER_DISABLED',
      'AI_PROVIDER_KEY_MISSING',
      'AI_PROVIDER_UNAVAILABLE',
      'AI_PROTOCOL_NOT_IMPLEMENTED',
      'budget_exceeded',
      'rate_limited',
      'offline',
      'stream_failed',
      'approval_expired',
      'aborted',
    ]) {
      expect(ASSISTANT_ERROR_DICTIONARY[code]).toBeDefined();
      expect(ASSISTANT_ERROR_DICTIONARY[code].title.length).toBeGreaterThan(0);
      expect(ASSISTANT_ERROR_DICTIONARY[code].actionLabel.length).toBeGreaterThan(0);
    }
  });

  it('每个条目都自带可执行恢复动作（不留死路）', () => {
    for (const copy of Object.values(ASSISTANT_ERROR_DICTIONARY)) {
      expect(['retry', 'open_settings', 'open_approval', 'switch_provider', 'dismiss']).toContain(copy.action);
    }
  });

  it('命中原因码返回对应动作（配置缺失 → 去配置；预算 → 拆分后重试）', () => {
    expect(resolveAssistantErrorCopy('AI_PROVIDER_KEY_MISSING').action).toBe('open_settings');
    expect(resolveAssistantErrorCopy('budget_exceeded').action).toBe('retry');
    expect(resolveAssistantErrorCopy('approval_expired').action).toBe('open_approval');
  });

  it('未命中原因码但有后端文案：保留文案，动作回退为重试', () => {
    const copy = resolveAssistantErrorCopy('some_new_code', '后端补充说明');
    expect(copy.code).toBe(ASSISTANT_ERROR_FALLBACK.code);
    expect(copy.description).toBe('后端补充说明');
    expect(copy.action).toBe('retry');
  });

  it('原因码与文案都缺失：返回兜底且不抛错', () => {
    expect(resolveAssistantErrorCopy(undefined, undefined)).toEqual(ASSISTANT_ERROR_FALLBACK);
    expect(resolveAssistantErrorCopy(null, '   ')).toEqual(ASSISTANT_ERROR_FALLBACK);
  });
});

describe('classifyAssistantError（错误归类）', () => {
  it('AbortError → aborted', () => {
    expect(classifyAssistantError(Object.assign(new Error('x'), { name: 'AbortError' }))).toBe('aborted');
  });

  it('已知 errorCode 原样保留', () => {
    expect(classifyAssistantError({ errorCode: 'AI_PROVIDER_DISABLED' })).toBe('AI_PROVIDER_DISABLED');
  });

  it('429 → rate_limited；5xx → stream_failed', () => {
    expect(classifyAssistantError({ httpStatus: 429 })).toBe('rate_limited');
    expect(classifyAssistantError({ httpStatus: 502 })).toBe('stream_failed');
  });

  it('未知错误 → unknown（不编造原因）', () => {
    expect(classifyAssistantError({})).toBe('unknown');
    expect(classifyAssistantError(null)).toBe('unknown');
  });
});
