/**
 * D1-01 页面建议数据源（服务端过滤 + 静态基线回退）。
 *
 * 两段式（方案 §4.1 L1 / P1-D1-01）：
 *   1) 静态注册表基线（D0-03/D0-05）立即渲染，保持零延迟；
 *   2) `GET /ai/assistant/context` 返回后替换为服务端过滤结果（可见 Bot ∩ 工具授权 ∩ RBAC）；
 *      请求失败 / known=false → 保持静态基线，并按观测性口径上报 `context_degraded`。
 *
 * 缓存：按 pageKey 会话级缓存 5 分钟（开关面板/页签往返不重复请求）；测试可显式清空。
 */
import { useEffect, useMemo, useState } from 'react';

import { aiGetAssistantContext } from '@/lib/api/ai-api';
import type { AssistantContextResult } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

import { flattenPageActions, type PageAssistantDeclaration } from './types';

export interface AssistantSuggestionItem {
  id: string;
  kind: 'task' | 'question';
  label: string;
  prompt: string;
}

export interface AssistantSuggestionFeed {
  items: AssistantSuggestionItem[];
  /** server = 服务端过滤结果；static = 静态注册表基线（未注册页面由消费方走通用回退）。 */
  source: 'server' | 'static';
  loading: boolean;
}

const CACHE_TTL_MS = 5 * 60_000;

const cache = new Map<string, { at: number; result: AssistantContextResult }>();
const inflight = new Map<string, Promise<AssistantContextResult | null>>();

/** 清空会话级缓存（测试/登出场景）。 */
export function clearAssistantSuggestionCache(): void {
  cache.clear();
  inflight.clear();
}

function readCache(pageKey: string): AssistantContextResult | null {
  const hit = cache.get(pageKey);
  if (!hit) return null;
  if (Date.now() - hit.at > CACHE_TTL_MS) {
    cache.delete(pageKey);
    return null;
  }
  return hit.result;
}

/**
 * 拉取服务端上下文；并发去重。
 * 返回 null 表示「服务端不认识该页面」→ 消费方保持静态基线。
 */
async function fetchContext(pageKey: string, route: string): Promise<AssistantContextResult | null> {
  const running = inflight.get(pageKey);
  if (running) return running;
  const promise = (async () => {
    const result = await aiGetAssistantContext(pageKey, route);
    if (!result || !result.known || !Array.isArray(result.suggestions)) return null;
    cache.set(pageKey, { at: Date.now(), result });
    return result;
  })();
  inflight.set(pageKey, promise);
  try {
    return await promise;
  } finally {
    inflight.delete(pageKey);
  }
}

function staticItems(declaration: PageAssistantDeclaration | null): AssistantSuggestionItem[] {
  if (!declaration) return [];
  return flattenPageActions(declaration).map(action => ({
    id: action.id,
    kind: action.kind,
    label: action.label,
    prompt: action.prompt,
  }));
}

export function useAssistantSuggestions(
  declaration: PageAssistantDeclaration | null,
  pageKey: string,
  route: string
): AssistantSuggestionFeed {
  // serverState 记录「哪个页面的结果」，避免切页瞬间串页复用上一次的下发。
  const [serverState, setServerState] = useState<{ pageKey: string; result: AssistantContextResult } | null>(
    () => {
      const cached = readCache(pageKey);
      return cached ? { pageKey, result: cached } : null;
    }
  );
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!declaration || !pageKey || pageKey === 'unknown') return;
    const cached = readCache(pageKey);
    if (cached) {
      setServerState({ pageKey, result: cached });
      return;
    }
    let alive = true;
    setLoading(true);
    const startedAt = Date.now();
    fetchContext(pageKey, route)
      .then(result => {
        if (!alive) return;
        if (result) {
          setServerState({ pageKey, result });
          trackAssistantEvent('context_loaded', {
            pageKey,
            route,
            latencyMs: Date.now() - startedAt,
            extra: { suggestions: result.suggestions.length, filtering: result.filtering ?? 'none' },
          });
        } else {
          setServerState(null);
          trackAssistantEvent('context_degraded', {
            pageKey,
            route,
            reasonCode: 'context_unknown_page',
          });
        }
      })
      .catch(() => {
        if (!alive) return;
        setServerState(null);
        trackAssistantEvent('context_degraded', {
          pageKey,
          route,
          reasonCode: 'context_fetch_failed',
        });
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [declaration, pageKey, route]);

  const active = serverState && serverState.pageKey === pageKey ? serverState.result : null;

  const items = useMemo(() => {
    if (active) {
      return active.suggestions.map(suggestion => ({
        id: suggestion.id,
        kind: suggestion.kind,
        label: suggestion.label,
        prompt: suggestion.prompt,
      }));
    }
    return staticItems(declaration);
  }, [active, declaration]);

  return { items, source: active ? 'server' : 'static', loading };
}