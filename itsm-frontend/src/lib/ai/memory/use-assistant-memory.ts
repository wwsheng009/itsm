/**
 * V2-01：AI 助手个性化记忆的 React 接入（懒加载 + 便捷动作）。
 *
 * - 首次挂载（status=idle）自动 `load()` 解析模式并加载数据；
 * - 模式为 off（租户/用户/埋点任一关闭）时不渲染任何记忆 UI 的决策由调用方按 `isOff`/`userDisabled` 处理；
 * - 所有写动作走 store（乐观更新 + 埋点 + 按模式持久化）。
 */
import { useEffect } from 'react';

import {
  deleteAssistantMemoryPhrase,
  deleteAssistantMemoryTemplate,
  isAssistantMemoryUserDisabled,
  saveAssistantMemoryPhrase,
  saveAssistantMemoryTemplate,
  useAssistantMemoryStore,
  type AssistantMemoryMode,
} from './assistant-memory-store';
import type { AssistantMemory } from './types';

export interface UseAssistantMemoryResult {
  mode: AssistantMemoryMode | null;
  status: 'idle' | 'loading' | 'ready' | 'disabled';
  memory: AssistantMemory;
  error: 'sync_failed' | 'save_failed' | null;
  /** 记忆面可用（已加载且未禁用）。 */
  ready: boolean;
  /** 禁用（租户策略 / 用户关闭 / 埋点 off）。 */
  isOff: boolean;
  /** 用户显式关闭（区别于租户策略关闭；用于渲染「重新开启」入口）。 */
  userDisabled: boolean;
  savePhrase: (phrase: string) => Promise<void>;
  saveTemplate: (name: string, body: string) => Promise<void>;
  removePhrase: (phrase: string) => Promise<void>;
  removeTemplate: (name: string) => Promise<void>;
  clearAll: () => Promise<void>;
  setUserEnabled: (enabled: boolean) => Promise<void>;
}

export function useAssistantMemory(): UseAssistantMemoryResult {
  const mode = useAssistantMemoryStore(state => state.mode);
  const status = useAssistantMemoryStore(state => state.status);
  const memory = useAssistantMemoryStore(state => state.memory);
  const error = useAssistantMemoryStore(state => state.error);
  const load = useAssistantMemoryStore(state => state.load);
  const setUserEnabled = useAssistantMemoryStore(state => state.setUserEnabled);
  const clearAll = useAssistantMemoryStore(state => state.clearAll);

  useEffect(() => {
    if (status === 'idle') {
      void load();
    }
  }, [status, load]);

  return {
    mode,
    status,
    memory,
    error,
    ready: status === 'ready' && mode !== 'off',
    isOff: status === 'disabled' || mode === 'off',
    userDisabled: status === 'disabled' && isAssistantMemoryUserDisabled(),
    savePhrase: saveAssistantMemoryPhrase,
    saveTemplate: saveAssistantMemoryTemplate,
    removePhrase: deleteAssistantMemoryPhrase,
    removeTemplate: deleteAssistantMemoryTemplate,
    clearAll,
    setUserEnabled,
  };
}