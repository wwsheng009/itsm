/**
 * 布局状态管理 Store
 * 统一管理侧边栏折叠状态
 */

import { create } from 'zustand';

interface LayoutState {
  /** 侧边栏折叠状态 */
  collapsed: boolean;

  /** I0-03：全局搜索打开请求计数（Ctrl+K；Header 订阅后打开搜索弹窗）。 */
  searchRequestNonce: number;

  /** 切换折叠状态 */
  toggleCollapsed: () => void;

  /** 设置折叠状态 */
  setCollapsed: (collapsed: boolean) => void;

  /** I0-03：请求打开全局搜索（递增非零计数，Header 消费）。 */
  requestGlobalSearch: () => void;
}

export const useLayoutStore = create<LayoutState>((set) => ({
  collapsed: false,
  searchRequestNonce: 0,

  toggleCollapsed: () =>
    set((state) => ({
      collapsed: !state.collapsed,
    })),

  setCollapsed: (collapsed) => set({ collapsed }),

  requestGlobalSearch: () =>
    set((state) => ({
      searchRequestNonce: (state.searchRequestNonce ?? 0) + 1,
    })),
}));
