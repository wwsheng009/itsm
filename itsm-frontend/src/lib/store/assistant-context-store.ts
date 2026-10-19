/**
 * D1-02 L2 运行时页面数据 store（非持久化）。
 *
 * 口径（方案 §4.1 L2）：
 * - 仅由页面**显式**注册（详情对象 id/摘要），禁止自动采集；
 * - 不落 localStorage（按会话生命周期存在；服务端仍以对象预检为事实源）；
 * - owner 标记保证「后挂载页面优先」：旧页面卸载回调不得清掉新页面的数据。
 */
import { create } from 'zustand';

import type { AskAITargetType } from '@/lib/ai/ask-ai-scope';

export interface AssistantPageData {
  targetType: AskAITargetType;
  targetId: number;
  /** 用户可见摘要（如工单标题）；上限与入口协议一致（300 字符）。 */
  summary?: string;
}

interface AssistantContextState {
  pageData: AssistantPageData | null;
  owner: symbol | null;
  registerPageData: (owner: symbol, data: AssistantPageData) => void;
  clearPageData: (owner: symbol) => void;
}

export const useAssistantContextStore = create<AssistantContextState>()((set, get) => ({
  pageData: null,
  owner: null,
  registerPageData: (owner, data) => set({ owner, pageData: data }),
  clearPageData: owner => {
    // 仅当前 owner 可清理（页面切换时序：新页面先注册、旧页面后卸载）。
    if (get().owner !== owner) return;
    set({ owner: null, pageData: null });
  },
}));

/** 读取当前页面数据（无注册时返回 null）。 */
export function useAssistantPageData(): AssistantPageData | null {
  return useAssistantContextStore(state => state.pageData);
}