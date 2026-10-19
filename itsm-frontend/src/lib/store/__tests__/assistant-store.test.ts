/**
 * I0-03 助手 store 测试：预填提示词（set/consume/去空白）与持久化字段范围（pendingPrompt 不落 localStorage）。
 */
import {
  ASSISTANT_DEFAULT_WIDTH,
  ASSISTANT_STORE_KEY,
  ASSISTANT_WIDTH_TIERS,
  maxAssistantWidthForViewport,
  nextAssistantWidth,
  normalizeAssistantWidth,
  useAssistantStore,
} from '../assistant-store';

beforeEach(() => {
  window.localStorage.clear();
  useAssistantStore.setState({
    open: false,
    width: 400,
    lastRoute: undefined,
    pendingPrompt: undefined,
  });
});

describe('assistant-store（I0-03）', () => {
  it('setPendingPrompt：空白值按清除处理；consume 返回并清空', () => {
    const store = useAssistantStore.getState();
    store.setPendingPrompt('帮我查 SLA');
    expect(useAssistantStore.getState().pendingPrompt).toBe('帮我查 SLA');

    const consumed = useAssistantStore.getState().consumePendingPrompt();
    expect(consumed).toBe('帮我查 SLA');
    expect(useAssistantStore.getState().pendingPrompt).toBeUndefined();

    // 再次消费为空；空白输入直接清除。
    expect(useAssistantStore.getState().consumePendingPrompt()).toBeUndefined();
    useAssistantStore.getState().setPendingPrompt('   ');
    expect(useAssistantStore.getState().pendingPrompt).toBeUndefined();
  });

  it('持久化仅包含 UI 偏好：pendingPrompt 不写入 localStorage', () => {
    useAssistantStore.getState().setPendingPrompt('临时问题（不应持久化）');
    useAssistantStore.getState().setLastRoute('/tickets/42');

    const raw = window.localStorage.getItem(ASSISTANT_STORE_KEY);
    expect(raw).toBeTruthy();
    const persisted = JSON.parse(raw as string);
    expect(persisted.state.lastRoute).toBe('/tickets/42');
    expect(persisted.state.pendingPrompt).toBeUndefined();
    expect(Object.keys(persisted.state).sort()).toEqual(['lastRoute', 'open', 'width']);
  });
});

describe('assistant-store 宽度档位（P1 扩大面板）', () => {
  it('档位为 400/560/720，默认取窄档', () => {
    expect(ASSISTANT_WIDTH_TIERS).toEqual([400, 560, 720]);
    expect(ASSISTANT_DEFAULT_WIDTH).toBe(400);
    expect(useAssistantStore.getState().width).toBe(400);
  });

  it('cycleWidth：400 → 560 → 720 → 400（桌面视口）', () => {
    const store = useAssistantStore.getState();
    expect(store.cycleWidth(1440)).toBe(560);
    expect(useAssistantStore.getState().width).toBe(560);
    expect(useAssistantStore.getState().cycleWidth(1440)).toBe(720);
    expect(useAssistantStore.getState().cycleWidth(1440)).toBe(400);
  });

  it('cycleWidth：1024 视口跳过 720（至少给主内容留 320px）', () => {
    // 1024 - 320 = 704 → 720 不可用，档位集合收敛为 [400, 560]。
    expect(maxAssistantWidthForViewport(1024)).toBe(704);
    expect(nextAssistantWidth(400, 1024)).toBe(560);
    expect(nextAssistantWidth(560, 1024)).toBe(400);
    expect(normalizeAssistantWidth(720, 1024)).toBe(560);
  });

  it('normalizeAssistantWidth：脏值/窄视口收敛到合法档位', () => {
    expect(normalizeAssistantWidth(437, 1440)).toBe(400);
    expect(normalizeAssistantWidth(600, 1440)).toBe(560);
    expect(normalizeAssistantWidth(9999, 1440)).toBe(720);
    // 极窄视口：至少保留默认档（不会返回 0/NaN）。
    expect(normalizeAssistantWidth(720, 500)).toBe(400);
    expect(normalizeAssistantWidth(720, Number.NaN)).toBe(400);
  });

  it('setWidth：写入时按视口归一化，宽度始终落在档位上', () => {
    const originalInnerWidth = window.innerWidth;
    try {
      window.innerWidth = 1440;
      useAssistantStore.getState().setWidth(437);
      expect(useAssistantStore.getState().width).toBe(400);
      useAssistantStore.getState().setWidth(700);
      expect(useAssistantStore.getState().width).toBe(720);
    } finally {
      window.innerWidth = originalInnerWidth;
    }
  });

  it('宽度随 persist 持久化（刷新后保持用户档位）', () => {
    const originalInnerWidth = window.innerWidth;
    try {
      window.innerWidth = 1440;
      useAssistantStore.getState().setWidth(720);
      const raw = window.localStorage.getItem(ASSISTANT_STORE_KEY);
      expect(raw).toBeTruthy();
      expect(JSON.parse(raw as string).state.width).toBe(720);
    } finally {
      window.innerWidth = originalInnerWidth;
    }
  });
});

describe('assistant-store 会话交接（P3 Dock → 工作区）', () => {
  it('set/consume：一次性消费后清空；非法 id 视为未设置', () => {
    useAssistantStore.getState().setHandoffConversationId(42);
    expect(useAssistantStore.getState().handoffConversationId).toBe(42);

    expect(useAssistantStore.getState().consumeHandoffConversationId()).toBe(42);
    expect(useAssistantStore.getState().handoffConversationId).toBeUndefined();
    expect(useAssistantStore.getState().consumeHandoffConversationId()).toBeUndefined();

    useAssistantStore.getState().setHandoffConversationId(0);
    expect(useAssistantStore.getState().handoffConversationId).toBeUndefined();
    useAssistantStore.getState().setHandoffConversationId(-1);
    expect(useAssistantStore.getState().handoffConversationId).toBeUndefined();
  });

  it('交接 id 不持久化（刷新后不得把用户强行拉回历史会话）', () => {
    useAssistantStore.getState().setLastRoute('/dashboard');
    useAssistantStore.getState().setHandoffConversationId(42);
    const raw = window.localStorage.getItem(ASSISTANT_STORE_KEY);
    expect(raw).toBeTruthy();
    expect(JSON.parse(raw as string).state.handoffConversationId).toBeUndefined();
    expect(Object.keys(JSON.parse(raw as string).state).sort()).toEqual(['lastRoute', 'open', 'width']);
  });
});
