/**
 * D0-03 页面助手上下文解析（L1）。
 *
 * - `resolvePageAssistantContext(pathname)`：纯函数，按注册表顺序匹配（具体静态路由优先，
 *   由注册表顺序保证，见 registry.ts 头注释与一致性测试）；
 * - `usePageAssistantContext()`：路由联动封装，并把当前路由记入
 *   `assistant-store.lastRoute`（Dock 常驻挂载时记录，供后续上下文感知/诊断使用）。
 */
import { useEffect, useMemo } from 'react';
import { matchPath, useLocation } from 'react-router';

import { useAssistantStore } from '@/lib/store/assistant-store';
import { PAGE_ASSISTANT_REGISTRY } from './registry';
import type { PageAssistantDeclaration } from './types';

export interface PageAssistantResolution {
  declaration: PageAssistantDeclaration | null;
  pageKey: string;
  route: string;
  params: Record<string, string | undefined>;
}

export function resolvePageAssistantContext(pathname: string): PageAssistantResolution {
  for (const declaration of PAGE_ASSISTANT_REGISTRY) {
    const match = matchPath({ path: declaration.pattern, end: true }, pathname);
    if (match) {
      return {
        declaration,
        pageKey: declaration.pageKey,
        route: pathname,
        params: (match.params ?? {}) as Record<string, string | undefined>,
      };
    }
  }
  return { declaration: null, pageKey: 'unknown', route: pathname, params: {} };
}

export function usePageAssistantContext(): PageAssistantResolution {
  const { pathname } = useLocation();
  const setLastRoute = useAssistantStore(state => state.setLastRoute);
  const resolution = useMemo(() => resolvePageAssistantContext(pathname), [pathname]);

  useEffect(() => {
    setLastRoute(pathname);
  }, [pathname, setLastRoute]);

  return resolution;
}