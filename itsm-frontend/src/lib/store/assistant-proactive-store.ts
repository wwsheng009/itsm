/**
 * I1-03 主动协助偏好与「不再提示」记忆（方案 §3.6 不打扰原则）。
 *
 * 口径：
 * - **默认静默**：`silent=true`（默认）→ 仅 FAB 徽标 + 通知中心投递，不出现页面内联建议条；
 * - **总开关**：`enabled=true`（默认）→ 关闭后无徽标、无内联条（完全静默）；
 * - **「不再提示」记忆**：按 `scopeKey`（页面/同类事件）持久化到 localStorage，仅 UI 偏好，
 *   不存会话内容；「恢复已忽略提示」清空。
 *
 * 与 assistant-store 分离：该 store 只承载主动协助开关，避免污染会话状态（partialize 边界）。
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const ASSISTANT_PROACTIVE_STORE_KEY = 'assistant.proactive.v1';

interface AssistantProactiveState {
  /** 主动协助总开关（默认开）。关闭 → 无徽标、无内联建议条。 */
  enabled: boolean;
  /** 静默模式（默认开）：仅徽标与通知中心，不出现页面内联建议条。 */
  silent: boolean;
  /** 已「不再提示」的 scopeKey 集合（持久化）。 */
  dismissedScopes: string[];
  setEnabled: (enabled: boolean) => void;
  setSilent: (silent: boolean) => void;
  /** 记忆「不再提示」（重复调用幂等）。 */
  dismissScope: (scopeKey: string) => void;
  /** 清空全部「不再提示」记忆。 */
  restoreDismissed: () => void;
}

/** 持久化字段（仅 UI 偏好）。 */
export type AssistantProactivePersistedState = Pick<
  AssistantProactiveState,
  'enabled' | 'silent' | 'dismissedScopes'
>;

/** 独立选择器：scopeKey 是否已被「不再提示」。 */
export function isScopeDismissed(state: Pick<AssistantProactiveState, 'dismissedScopes'>, scopeKey?: string): boolean {
  if (!scopeKey) return false;
  return state.dismissedScopes.includes(scopeKey);
}

export const useAssistantProactiveStore = create<AssistantProactiveState>()(
  persist<AssistantProactiveState, [], [], AssistantProactivePersistedState>(
    (set, get) => ({
      enabled: true,
      silent: true,
      dismissedScopes: [],
      setEnabled: enabled => set({ enabled }),
      setSilent: silent => set({ silent }),
      dismissScope: scopeKey => {
        if (!scopeKey) return;
        const current = get().dismissedScopes;
        if (current.includes(scopeKey)) return;
        set({ dismissedScopes: [...current, scopeKey] });
      },
      restoreDismissed: () => set({ dismissedScopes: [] }),
    }),
    {
      name: ASSISTANT_PROACTIVE_STORE_KEY,
      partialize: state => ({
        enabled: state.enabled,
        silent: state.silent,
        dismissedScopes: state.dismissedScopes,
      }),
    }
  )
);