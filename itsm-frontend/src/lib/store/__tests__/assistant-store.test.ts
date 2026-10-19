/**
 * I0-03 助手 store 测试：预填提示词（set/consume/去空白）与持久化字段范围（pendingPrompt 不落 localStorage）。
 */
import { ASSISTANT_STORE_KEY, useAssistantStore } from '../assistant-store';

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