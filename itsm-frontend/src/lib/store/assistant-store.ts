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

/**
 * 面板宽度档位（P1）：窄 → 宽 → 超宽。
 *
 * 刻意用离散档位而非自由拖拽：
 * - 用户不需要在 437px 这类无意义状态上做决定，三档覆盖"扫一眼 / 读长回答 / 对照页面"；
 * - 档位可持久化、可测试、可用键盘循环，天然满足 WCAG 2.1.1（无纯指针操作）；
 * - 宽档位与布局推挤（P2）绑定，避免任意宽度导致页面遮挡面积不可控。
 */
export const ASSISTANT_WIDTH_TIERS = [400, 560, 720] as const;
export type AssistantWidthTier = (typeof ASSISTANT_WIDTH_TIERS)[number];

/** 默认宽度（= 档位一）。 */
export const ASSISTANT_DEFAULT_WIDTH: AssistantWidthTier = ASSISTANT_WIDTH_TIERS[0];

/** 触发"推挤主内容"的最小宽度：窄档保持悬浮覆盖，宽档起让出空间。 */
export const ASSISTANT_DOCK_PUSH_THRESHOLD = 560;

/**
 * 计算当前视口下允许的最大面板宽度。
 *
 * 约束：至少给主内容留 320px，因此超宽档在 1024px 视口下会自动收敛；
 * 用 `Math.min(720, viewport - 320)` 保证任何桌面尺寸都不会把页面遮死。
 */
export const maxAssistantWidthForViewport = (viewportWidth: number): number => {
  if (!Number.isFinite(viewportWidth) || viewportWidth <= 0) return ASSISTANT_WIDTH_TIERS[0];
  return Math.max(ASSISTANT_WIDTH_TIERS[0], Math.min(ASSISTANT_WIDTH_TIERS[2], viewportWidth - 320));
};

/**
 * 把任意宽度归一化到合法档位（持久化脏值/视口收敛都走这里，避免面板出现半档状态）。
 *
 * 语义是「吸附到最近档位」而不是「取最大可用档位」：
 * 437 应落到 400（更接近），而不是被放大到 720——后者会让用户在缩窄窗口时突然得到最宽面板。
 */
export const normalizeAssistantWidth = (width: number, viewportWidth: number): AssistantWidthTier => {
  const maxWidth = maxAssistantWidthForViewport(viewportWidth);
  const candidates = ASSISTANT_WIDTH_TIERS.filter(tier => tier <= maxWidth);
  if (candidates.length === 0) return ASSISTANT_DEFAULT_WIDTH;
  let nearest = candidates[0];
  for (const tier of candidates) {
    if (Math.abs(tier - width) < Math.abs(nearest - width)) nearest = tier;
  }
  return nearest;
};

/** 循环切换档位（窄 → 宽 → 超宽 → 窄），并跳过当前视口放不下的档位。 */
export const nextAssistantWidth = (current: number, viewportWidth: number): AssistantWidthTier => {
  const maxWidth = maxAssistantWidthForViewport(viewportWidth);
  const available = ASSISTANT_WIDTH_TIERS.filter(tier => tier <= maxWidth);
  if (available.length === 0) return ASSISTANT_DEFAULT_WIDTH;
  const index = available.findIndex(tier => tier >= current);
  if (index === -1) return available[0];
  return available[(index + 1) % available.length];
};

interface AssistantState {
  /** 面板是否展开（FAB 收起态 = false）。 */
  open: boolean;
  /** 桌面端面板宽度（三档：400 / 560 / 720；由标题栏「加宽」按钮循环切换）。 */
  width: number;
  /** 最近一次打开/切换的页面路由（诊断与后续页面上下文预留）。 */
  lastRoute?: string;
  /**
   * I0-03：待预填的提示词（来自全局搜索「问 AI」/后续划词等入口）。
   * 面板消费后清空；**不持久化**（partialize 排除）。
   */
  pendingPrompt?: string;
  /**
   * P3：Dock → `/ai/chat` 工作区的会话交接（一次性）。
   *
   * 背景：`convId` 是会话内核的组件内 state，直接 `navigate` 会让工作区挂载成空会话。
   * 约定：Dock 写入 id → 工作区挂载时消费并 `switchToConversation` → 立即清空。
   * **不持久化**：刷新页面后不应把用户强行拉回某个历史会话。
   */
  handoffConversationId?: number;
  openPanel: () => void;
  closePanel: () => void;
  togglePanel: () => void;
  setWidth: (width: number) => void;
  /** 循环切换到下一档（按当前视口收敛；返回生效宽度）。 */
  cycleWidth: (viewportWidth: number) => number;
  setLastRoute: (route: string) => void;
  /** 设置待预填提示词（空白值按清除处理）。 */
  setPendingPrompt: (prompt?: string) => void;
  /** 面板消费待预填提示词（返回并清空，只填不发送）。 */
  consumePendingPrompt: () => string | undefined;
  /** P3：登记待交接的会话 id（Dock → 工作区）。 */
  setHandoffConversationId: (id?: number) => void;
  /** P3：消费待交接的会话 id（返回并清空）。 */
  consumeHandoffConversationId: () => number | undefined;
}

/** 持久化字段（仅 UI 偏好；pendingPrompt 等会话态不入 localStorage）。 */
export type AssistantPersistedState = Pick<AssistantState, 'open' | 'width' | 'lastRoute'>;

export const useAssistantStore = create<AssistantState>()(
  persist<AssistantState, [], [], AssistantPersistedState>(
    (set, get) => ({
      open: false,
      width: ASSISTANT_DEFAULT_WIDTH,
      lastRoute: undefined,
      pendingPrompt: undefined,
      handoffConversationId: undefined,
      openPanel: () => set({ open: true }),
      closePanel: () => set({ open: false }),
      togglePanel: () => set(state => ({ open: !state.open })),
      setWidth: width => set({ width: normalizeAssistantWidth(width, typeof window === 'undefined' ? 1440 : window.innerWidth) }),
      cycleWidth: viewportWidth => {
        const next = nextAssistantWidth(get().width, viewportWidth);
        set({ width: next });
        return next;
      },
      setLastRoute: lastRoute => set({ lastRoute }),
      setPendingPrompt: prompt =>
        set({ pendingPrompt: prompt && prompt.trim() ? prompt : undefined }),
      consumePendingPrompt: () => {
        const current = get().pendingPrompt;
        if (current) set({ pendingPrompt: undefined });
        return current;
      },
      setHandoffConversationId: id =>
        set({ handoffConversationId: id && id > 0 ? id : undefined }),
      consumeHandoffConversationId: () => {
        const current = get().handoffConversationId;
        if (current) set({ handoffConversationId: undefined });
        return current;
      },
    }),
    {
      name: ASSISTANT_STORE_KEY,
      partialize: state => ({ open: state.open, width: state.width, lastRoute: state.lastRoute }),
    }
  )
);
