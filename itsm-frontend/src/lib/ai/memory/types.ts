/**
 * V2-01 个性化记忆：数据模型 + 纯函数核心（前后端同口径护栏）。
 *
 * 口径（方案 §7 V2-01 + §13.1）：
 * - **元数据优先**：绝不采集对话正文；`recent` 只存动作键（结构化短值）+ 时间；
 * - **显式保存**：常用语/模板由用户主动保存（而非静默采集），可随时删除；
 * - **上限护栏**：phrases ≤20×200 字；templates ≤10（名 ≤40 / 体 ≤1000）；recent ≤20×120 键；
 *   服务端 `validateAssistantMemory` 同口径校验，非法结构整体拒绝（前端先裁剪，拒绝即契约破坏信号）；
 * - **合规三档**：`on` → 服务端同步；`anonymous` → 仅本机；`off` → 全禁用（零读写，见 store）。
 */

export const ASSISTANT_MEMORY_VERSION = 1;
export const MEMORY_MAX_PHRASES = 20;
export const MEMORY_MAX_PHRASE_CHARS = 200;
export const MEMORY_MAX_TEMPLATES = 10;
export const MEMORY_MAX_TEMPLATE_NAME_CHARS = 40;
export const MEMORY_MAX_TEMPLATE_BODY_CHARS = 1000;
export const MEMORY_MAX_RECENT_ACTIONS = 20;
export const MEMORY_MAX_RECENT_KEY_CHARS = 120;

/** recent 动作键的字面量字符集（与服务端 recentActionKeyPattern 对齐）。 */
export const MEMORY_RECENT_KEY_PATTERN = /^[a-z0-9_.:-]+$/;

export interface AssistantMemoryTemplate {
  name: string;
  body: string;
}

/** 最近处理方式：只存动作键 + 时间（元数据；无任何自由文本）。 */
export interface AssistantMemoryRecentAction {
  key: string;
  at: string;
}

export interface AssistantMemory {
  version: number;
  phrases: string[];
  templates: AssistantMemoryTemplate[];
  recent: AssistantMemoryRecentAction[];
}

export function emptyAssistantMemory(): AssistantMemory {
  return { version: ASSISTANT_MEMORY_VERSION, phrases: [], templates: [], recent: [] };
}

/** 按字符（非 UTF-16 单元）截断，避免切坏多字节字符。 */
function clampChars(text: string, max: number): string {
  const chars = Array.from(text);
  return chars.length <= max ? text : chars.slice(0, max).join('');
}

/** 解析/裁剪任意来源（服务端偏好 / 本地缓存）的记忆载荷；未知结构安全降级为空记忆。 */
export function normalizeAssistantMemory(raw: unknown): AssistantMemory {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return emptyAssistantMemory();
  const source = raw as Record<string, unknown>;
  const memory = emptyAssistantMemory();

  if (Array.isArray(source.phrases)) {
    const seen = new Set<string>();
    for (const item of source.phrases) {
      if (typeof item !== 'string') continue;
      const phrase = clampChars(item.trim(), MEMORY_MAX_PHRASE_CHARS);
      if (!phrase || seen.has(phrase)) continue;
      seen.add(phrase);
      memory.phrases.push(phrase);
      if (memory.phrases.length >= MEMORY_MAX_PHRASES) break;
    }
  }

  if (Array.isArray(source.templates)) {
    const seen = new Set<string>();
    for (const item of source.templates) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) continue;
      const tpl = item as Record<string, unknown>;
      const name = clampChars(String(tpl.name ?? '').trim(), MEMORY_MAX_TEMPLATE_NAME_CHARS);
      if (!name || seen.has(name)) continue;
      const body = clampChars(String(tpl.body ?? '').trim(), MEMORY_MAX_TEMPLATE_BODY_CHARS);
      seen.add(name);
      memory.templates.push({ name, body });
      if (memory.templates.length >= MEMORY_MAX_TEMPLATES) break;
    }
  }

  if (Array.isArray(source.recent)) {
    const seen = new Set<string>();
    for (const item of source.recent) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) continue;
      const entry = item as Record<string, unknown>;
      const key = String(entry.key ?? '').trim();
      const at = String(entry.at ?? '').trim();
      if (!MEMORY_RECENT_KEY_PATTERN.test(key) || key.length > MEMORY_MAX_RECENT_KEY_CHARS) continue;
      if (!at || Number.isNaN(Date.parse(at)) || seen.has(key)) continue;
      seen.add(key);
      memory.recent.push({ key, at });
      if (memory.recent.length >= MEMORY_MAX_RECENT_ACTIONS) break;
    }
  }

  return memory;
}

/** 保存常用语（置顶；重复幂等；超限淘汰最旧）。 */
export function addMemoryPhrase(memory: AssistantMemory, phrase: string): AssistantMemory {
  const text = clampChars(phrase.trim(), MEMORY_MAX_PHRASE_CHARS);
  if (!text) return memory;
  if (memory.phrases.includes(text)) return memory;
  return {
    ...memory,
    phrases: [text, ...memory.phrases].slice(0, MEMORY_MAX_PHRASES),
  };
}

export function removeMemoryPhrase(memory: AssistantMemory, phrase: string): AssistantMemory {
  return { ...memory, phrases: memory.phrases.filter(item => item !== phrase) };
}

/** 保存个人模板（同名覆盖 body；置顶；超限淘汰最旧）。 */
export function upsertMemoryTemplate(
  memory: AssistantMemory,
  name: string,
  body: string
): AssistantMemory {
  const trimmedName = clampChars(name.trim(), MEMORY_MAX_TEMPLATE_NAME_CHARS);
  if (!trimmedName) return memory;
  const trimmedBody = clampChars(body.trim(), MEMORY_MAX_TEMPLATE_BODY_CHARS);
  const rest = memory.templates.filter(item => item.name !== trimmedName);
  return {
    ...memory,
    templates: [{ name: trimmedName, body: trimmedBody }, ...rest].slice(0, MEMORY_MAX_TEMPLATES),
  };
}

export function removeMemoryTemplate(memory: AssistantMemory, name: string): AssistantMemory {
  return { ...memory, templates: memory.templates.filter(item => item.name !== name) };
}

/**
 * 记录「上次处理方式」（元数据）：同一动作键去重并置顶。
 *
 * 仅在记忆已就绪时由调用方写入；非法键（含自由文本）静默忽略——绝不把正文写进记忆。
 */
export function recordRecentAction(
  memory: AssistantMemory,
  key: string,
  at: string = new Date().toISOString()
): AssistantMemory {
  const trimmed = key.trim();
  if (!MEMORY_RECENT_KEY_PATTERN.test(trimmed) || trimmed.length > MEMORY_MAX_RECENT_KEY_CHARS) {
    return memory;
  }
  if (typeof at !== 'string' || !at || Number.isNaN(Date.parse(at))) return memory;
  const rest = memory.recent.filter(item => item.key !== trimmed);
  return {
    ...memory,
    recent: [{ key: trimmed, at }, ...rest].slice(0, MEMORY_MAX_RECENT_ACTIONS),
  };
}

export function isMemoryEmpty(memory: AssistantMemory): boolean {
  return memory.phrases.length === 0 && memory.templates.length === 0 && memory.recent.length === 0;
}

/** recent 动作键 → 人类可读标签（未登记的键按后缀原样展示，不伪造语义）。 */
export function recentActionLabel(key: string): string {
  const known: Record<string, string> = {
    'inline_apply:triage_detail': '工单详情 · 应用字段建议',
    'inline_apply:triage_create': '工单创建 · 应用字段建议',
    'memory.phrase_used': '使用常用语',
    'memory.template_used': '使用个人模板',
  };
  if (known[key]) return known[key];
  if (key.startsWith('inline_apply:')) return `应用字段建议（${key.slice('inline_apply:'.length)}）`;
  return key;
}