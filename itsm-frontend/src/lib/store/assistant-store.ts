/**
 * D0-02 常驻 AI 助手（AssistantDock）状态：展开态 / 面板宽度 / 最近页面路由。
 *
 * 口径：
 * - 持久化按用户维度（localStorage），刷新后保持展开态与宽度；
 * - 仅存 UI 偏好，不存会话内容（会话归属由服务端 conversations 管理）。
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const ASSISTANT_STORE_KEY = 'assistant.dock.v1';

interface AssistantState {
  /** 面板是否展开（FAB 收起态 = false）。 */
  open: boolean;
  /** 桌面端面板宽度（拖拽调整后续接入；先持久化默认值）。 */
  width: number;
  /** 最近一次打开/切换的页面路由（诊断与后续页面上下文预留）。 */
  lastRoute?: string;
  /**
   * I0-03：待预填的提示词（来自全局搜索「问 AI」/后续划词等入口）。
   * 面板消费后清空；**不持久化**（partialize 排除）。
   */
  pendingPrompt?: string;
  openPanel: () => void;
  closePanel: () => void;
  togglePanel: () => void;
  setWidth: (width: number) => void;
  setLastRoute: (route: string) => void;
  /** 设置待预填提示词（空白值按清除处理）。 */
  setPendingPrompt: (prompt?: string) => void;
  /** 面板消费待预填提示词（返回并清空，只填不发送）。 */
  consumePendingPrompt: () => string | undefined;
}

/** 持久化字段（仅 UI 偏好；pendingPrompt 等会话态不入 localStorage）。 */
export type AssistantPersistedState = Pick<AssistantState, 'open' | 'width' | 'lastRoute'>;

export const useAssistantStore = create<AssistantState>()(
  persist<AssistantState, [], [], AssistantPersistedState>(
    (set, get) => ({
      open: false,
      width: 400,
      lastRoute: undefined,
      pendingPrompt: undefined,
      openPanel: () => set({ open: true }),
      closePanel: () => set({ open: false }),
      togglePanel: () => set(state => ({ open: !state.open })),
      setWidth: width => set({ width }),
      setLastRoute: lastRoute => set({ lastRoute }),
      setPendingPrompt: prompt =>
        set({ pendingPrompt: prompt && prompt.trim() ? prompt : undefined }),
      consumePendingPrompt: () => {
        const current = get().pendingPrompt;
        if (current) set({ pendingPrompt: undefined });
        return current;
      },
    }),
    {
      name: ASSISTANT_STORE_KEY,
      partialize: state => ({ open: state.open, width: state.width, lastRoute: state.lastRoute }),
    }
  )
);
