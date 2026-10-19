/**
 * P2 LLM 动态建议数据源（增强层，失败静默回退静态基线）。
 *
 * 口径：
 *   - 仅在页面已注册（declaration 存在且 pageKey 有效）时请求；
 *   - 服务端 `generated=false`（LLM 未启用/生成或解析失败）或请求异常 → `items=[]`，消费方保持基线；
 *   - 会话级缓存（5 分钟，键=pageKey|route）+ 并发去重；测试可显式清空。
 */
import { useEffect, useMemo, useState } from 'react';

import { aiGetDynamicSuggestions } from '@/lib/api/ai-api';
import type { PageAssistantDeclaration } from './types';

export interface DynamicSuggestionItem {
  id: string;
  kind: 'task' | 'question';
  label: string;
  prompt: string;
}

export interface DynamicSuggestionFeed {
  items: DynamicSuggestionItem[];
  /** 是否来自 LLM（消费方据此标记 feed=llm 埋点）。 */
  source: 'llm' | 'none';
}

const CACHE_TTL_MS = 5 * 60_000;

const cache = new Map<string, { at: number; items: DynamicSuggestionItem[] }>();
const inflight = new Map<string, Promise<DynamicSuggestionItem[]>>();

/** 清空会话级缓存（测试/登出场景）。 */
export function clearDynamicSuggestionCache(): void {
  cache.clear();
  inflight.clear();
}

function cacheKey(pageKey: string, route: string): string {
  return `${pageKey}|${route}`;
}

async function fetchDynamic(pageKey: string, route: string): Promise<DynamicSuggestionItem[]> {
  const key = cacheKey(pageKey, route);
  const running = inflight.get(key);
  if (running) return running;
  const promise = (async () => {
    try {
      const result = await aiGetDynamicSuggestions({ pageKey, route });
      if (!result || !result.generated || !Array.isArray(result.suggestions)) return [];
      return result.suggestions
        .filter(item => item && item.label && item.prompt)
        .map(item => ({
          id: item.id,
          kind: item.kind === 'question' ? ('question' as const) : ('task' as const),
          label: item.label,
          prompt: item.prompt,
        }));
    } catch {
      // 失败静默：动态建议只是增强，回退静态基线。
      return [];
    }
  })();
  inflight.set(key, promise);
  try {
    const items = await promise;
    cache.set(key, { at: Date.now(), items });
    return items;
  } finally {
    inflight.delete(key);
  }
}

export function useDynamicSuggestions(
  declaration: PageAssistantDeclaration | null,
  pageKey: string,
  route: string
): DynamicSuggestionFeed {
  const [items, setItems] = useState<DynamicSuggestionItem[]>(() => {
    const cached = pageKey ? cache.get(cacheKey(pageKey, route)) : undefined;
    return cached && Date.now() - cached.at <= CACHE_TTL_MS ? cached.items : [];
  });

  useEffect(() => {
    if (!declaration || !pageKey || pageKey === 'unknown') {
      setItems([]);
      return;
    }
    const cached = cache.get(cacheKey(pageKey, route));
    if (cached && Date.now() - cached.at <= CACHE_TTL_MS) {
      setItems(cached.items);
      return;
    }
    let alive = true;
    fetchDynamic(pageKey, route).then(result => {
      if (alive) setItems(result);
    });
    return () => {
      alive = false;
    };
  }, [declaration, pageKey, route]);

  const source = useMemo<'llm' | 'none'>(() => (items.length > 0 ? 'llm' : 'none'), [items]);
  return { items, source };
}