/**
 * V2-01 个性化记忆：模式解析 + 数据面（服务端同步 / 本机 / 禁用三档）。
 *
 * 模式矩阵（`resolveAssistantMemoryMode` 纯函数）：
 * - `off`    ← 原位素（租户策略关闭）/ 用户显式禁用 / 埋点档为 off —— 零读写、零记忆数据面请求；
 * - `local`  ← 埋点档 anonymous（匿名使用：记忆只落本机，绝不上传）；
 * - `server` ← 默认：走 `users/me/preferences` 的 `assistantMemory` 键（跨设备，仅本人可读写）。
 *
 * 远侧契约：
 * - 后端 `validateAssistantMemory`（结构白名单 + 上限）与本地 `normalizeAssistantMemory` 同口径；
 * - 读取顺序：telemetry off / 用户禁用 → **不发起任何请求**；否则以 `GET /api/v1/capabilities`
 *   的 `assistant.memoryEnabled` 判定租户策略（缺省/失败按产品默认开启，fail-safe 不阻断）。
 *
 * 边界：`recordAction` 只在记忆已就绪时写入（绝不为「上次处理方式」隐式触发加载请求）。
 */
import { create } from 'zustand';

import { getCapabilities } from '@/lib/api/capability-api';
import { getUserPreferences, updateUserPreferences } from '@/lib/api/user-preferences-api';
import { getTelemetryMode, trackAssistantEvent, type TelemetryMode } from '@/lib/ai/telemetry';

import {
  addMemoryPhrase,
  emptyAssistantMemory,
  normalizeAssistantMemory,
  recordRecentAction,
  removeMemoryPhrase,
  removeMemoryTemplate,
  upsertMemoryTemplate,
  type AssistantMemory,
} from './types';

/** 本机镜像存储键（local/anonymous 档的主存储；server 档的兜底缓存）。 */
export const ASSISTANT_MEMORY_LOCAL_KEY = 'assistant.memory.v1';
/** 用户级禁用标记（合规：用户可随时关闭个性化记忆；不删除服务端数据，重新开启即恢复）。 */
export const ASSISTANT_MEMORY_USER_DISABLED_KEY = 'assistant.memory.user_disabled.v1';

export type AssistantMemoryMode = 'server' | 'local' | 'off';

export type AssistantMemoryOp =
  | 'phrase_saved'
  | 'phrase_deleted'
  | 'template_saved'
  | 'template_deleted'
  | 'action_recorded'
  | 'cleared'
  | 'disabled'
  | 'enabled';

export interface AssistantMemoryModeInput {
  telemetryMode: TelemetryMode;
  /** 租户策略：false = 租户关闭；true/undefined/null = 开启（未知按默认开启）。 */
  tenantEnabled?: boolean | null;
  userDisabled: boolean;
}

/** 解析记忆模式（纯函数，便于单测覆盖三档与边界）。 */
export function resolveAssistantMemoryMode(input: AssistantMemoryModeInput): AssistantMemoryMode {
  if (input.userDisabled || input.telemetryMode === 'off' || input.tenantEnabled === false) {
    return 'off';
  }
  if (input.telemetryMode === 'anonymous') return 'local';
  return 'server';
}

function safeStorage(): Storage | null {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return null;
    return window.localStorage;
  } catch {
    return null;
  }
}

function readLocalMemory(): AssistantMemory | null {
  const storage = safeStorage();
  if (!storage) return null;
  try {
    const raw = storage.getItem(ASSISTANT_MEMORY_LOCAL_KEY);
    if (!raw) return null;
    return normalizeAssistantMemory(JSON.parse(raw));
  } catch {
    return null;
  }
}

function writeLocalMemory(memory: AssistantMemory): void {
  const storage = safeStorage();
  if (!storage) return;
  try {
    storage.setItem(ASSISTANT_MEMORY_LOCAL_KEY, JSON.stringify(memory));
  } catch {
    // 存储不可用（配额/隐私模式）：静默降级——绝不阻断交互。
  }
}

function readUserDisabled(): boolean {
  const storage = safeStorage();
  if (!storage) return false;
  try {
    return storage.getItem(ASSISTANT_MEMORY_USER_DISABLED_KEY) === '1';
  } catch {
    return false;
  }
}

function writeUserDisabled(disabled: boolean): void {
  const storage = safeStorage();
  if (!storage) return;
  try {
    if (disabled) storage.setItem(ASSISTANT_MEMORY_USER_DISABLED_KEY, '1');
    else storage.removeItem(ASSISTANT_MEMORY_USER_DISABLED_KEY);
  } catch {
    // 同上：静默降级。
  }
}

export interface AssistantMemoryState {
  /** null = 尚未解析（首次 load 前）。 */
  mode: AssistantMemoryMode | null;
  status: 'idle' | 'loading' | 'ready' | 'disabled';
  memory: AssistantMemory;
  /** 'sync_failed' = 服务端读取失败已回落本机缓存；'save_failed' = 服务端保存失败（本机已兜底）。 */
  error: 'sync_failed' | 'save_failed' | null;
  /** 解析模式并按需加载数据（幂等；mode 已解析时可重复调用刷新）。 */
  load: () => Promise<void>;
  /** 写入记忆（乐观更新 + 埋点 + 按模式持久化）。 */
  save: (next: AssistantMemory, op: AssistantMemoryOp) => Promise<void>;
  /** 记录「上次处理方式」（仅在已就绪时写入；未加载/禁用时静默跳过）。 */
  recordAction: (key: string) => void;
  /** 用户级开关（合规）：关闭 = 立即停用且零请求；开启 = 重新加载。 */
  setUserEnabled: (enabled: boolean) => Promise<void>;
  /** 用户清空全部记忆（云端删除键 + 本机清空）。 */
  clearAll: () => Promise<void>;
  /** 会话/登出重置（测试亦用）。 */
  reset: () => void;
}

export const useAssistantMemoryStore = create<AssistantMemoryState>()((set, get) => ({
  mode: null,
  status: 'idle',
  memory: emptyAssistantMemory(),
  error: null,

  load: async () => {
    set({ status: 'loading', error: null });
    const telemetryMode = getTelemetryMode();
    const userDisabled = readUserDisabled();

    // 零请求快速路径：埋点 off 或用户显式禁用 → 直接停用，不触达能力面/偏好面。
    if (telemetryMode === 'off' || userDisabled) {
      set({ mode: 'off', status: 'disabled', memory: emptyAssistantMemory(), error: null });
      return;
    }

    let tenantEnabled: boolean | null = null;
    try {
      const caps = await getCapabilities();
      tenantEnabled = typeof caps?.assistant?.memoryEnabled === 'boolean' ? caps.assistant.memoryEnabled : null;
    } catch {
      // fail-safe：策略读取失败按产品默认（开启）处理，绝不阻断。
      tenantEnabled = null;
    }

    const mode = resolveAssistantMemoryMode({ telemetryMode, tenantEnabled, userDisabled: false });
    if (mode === 'off') {
      // 租户策略关闭：零数据面读写。
      set({ mode: 'off', status: 'disabled', memory: emptyAssistantMemory(), error: null });
      return;
    }

    if (mode === 'local') {
      set({ mode, status: 'ready', memory: readLocalMemory() ?? emptyAssistantMemory(), error: null });
      return;
    }

    try {
      const res = await getUserPreferences();
      const memory = normalizeAssistantMemory(res?.preferences?.assistantMemory);
      writeLocalMemory(memory);
      set({ mode, status: 'ready', memory, error: null });
    } catch {
      const cached = readLocalMemory() ?? emptyAssistantMemory();
      set({ mode, status: 'ready', memory: cached, error: 'sync_failed' });
    }
  },

  save: async (next, op) => {
    const { mode } = get();
    if (mode === null || mode === 'off') return;
    const snapshot = normalizeAssistantMemory(next);
    set({ memory: snapshot });
    trackAssistantEvent('memory_updated', {
      extra: {
        op,
        phrases: snapshot.phrases.length,
        templates: snapshot.templates.length,
        recent: snapshot.recent.length,
      },
    });
    if (mode === 'local') {
      writeLocalMemory(snapshot);
      return;
    }
    try {
      await updateUserPreferences({ assistantMemory: snapshot });
      writeLocalMemory(snapshot);
      set({ error: null });
    } catch {
      // 服务端保存失败：本机兜底并如实置错（UI 提示），不静默丢数据。
      writeLocalMemory(snapshot);
      set({ error: 'save_failed' });
    }
  },

  recordAction: key => {
    const { mode, status, memory } = get();
    if (status !== 'ready' || mode === null || mode === 'off') return;
    const next = recordRecentAction(memory, key);
    if (next === memory) return;
    void get().save(next, 'action_recorded');
  },

  setUserEnabled: async enabled => {
    if (enabled) {
      writeUserDisabled(false);
      await get().load();
      trackAssistantEvent('memory_updated', { extra: { op: 'enabled' } });
      return;
    }
    writeUserDisabled(true);
    set({ mode: 'off', status: 'disabled', memory: emptyAssistantMemory(), error: null });
    trackAssistantEvent('memory_updated', { extra: { op: 'disabled' } });
  },

  clearAll: async () => {
    const { mode } = get();
    const empty = emptyAssistantMemory();
    writeLocalMemory(empty);
    if (mode === 'server') {
      try {
        await updateUserPreferences({ assistantMemory: null });
      } catch {
        set({ error: 'save_failed' });
      }
    }
    set({ memory: empty });
    trackAssistantEvent('memory_updated', { extra: { op: 'cleared' } });
  },

  reset: () => set({ mode: null, status: 'idle', memory: emptyAssistantMemory(), error: null }),
}));

/** 便捷动作（供非组件调用点复用；语义见 store）。 */
export function recordAssistantMemoryAction(key: string): void {
  useAssistantMemoryStore.getState().recordAction(key);
}

/** 便捷读写（组件外）：保存常用语/模板。 */
export function saveAssistantMemoryPhrase(phrase: string): Promise<void> {
  const state = useAssistantMemoryStore.getState();
  return state.save(addMemoryPhrase(state.memory, phrase), 'phrase_saved');
}

export function saveAssistantMemoryTemplate(name: string, body: string): Promise<void> {
  const state = useAssistantMemoryStore.getState();
  return state.save(upsertMemoryTemplate(state.memory, name, body), 'template_saved');
}

export function deleteAssistantMemoryPhrase(phrase: string): Promise<void> {
  const state = useAssistantMemoryStore.getState();
  return state.save(removeMemoryPhrase(state.memory, phrase), 'phrase_deleted');
}

export function deleteAssistantMemoryTemplate(name: string): Promise<void> {
  const state = useAssistantMemoryStore.getState();
  return state.save(removeMemoryTemplate(state.memory, name), 'template_deleted');
}

/** 用户级禁用标记（读取；供 UI 区分「用户关闭」与「租户策略关闭」）。 */
export function isAssistantMemoryUserDisabled(): boolean {
  return readUserDisabled();
}
