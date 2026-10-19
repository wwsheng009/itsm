/**
 * O0-01 AI Copilot 前端埋点 SDK。
 *
 * 契约：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.2（事件字典）/ §13.1（原则）。
 *
 * 设计约束：
 * - 元数据优先：不采集对话正文；自由文本一律哈希（FNV-1a，仅用于分组统计，非加密）；
 * - 不阻塞交互：批量发送 + 采样 + 全链路 try/catch，埋点异常绝不冒泡到业务链路；
 * - 失败静默：transport 抛错只丢弃当前批次并回调 onError（默认吞掉），不做重试队列；
 * - 合规三档：`on`（个性化）/ `anonymous`（匿名聚合，剥离可关联字段）/ `off`（完全关闭，偏好持久化）。
 *
 * 注：本模块位于 `src/lib/ai/**`，不在 eslint「禁止直接 fetch」的 glob 范围内
 * （该规则覆盖 pages / components / lib/services）。埋点需要 `keepalive` 且不能触发
 * httpClient 的 401 刷新链路（best-effort 语义），故直接使用 fetch——与 `lib/api`
 * 的 SSE 实现采用同一取舍，理由记录于此。
 */
import { security } from '@/lib/security';

/** 事件名（与方案 §13.2 事件字典逐项对齐；新增事件必须同步更新契约与单测）。 */
export const ASSISTANT_EVENT_NAMES = [
  'assistant_opened',
  'assistant_closed',
  'context_loaded',
  'context_degraded',
  'suggestion_shown',
  'suggestion_clicked',
  'suggestion_ignored',
  'task_started',
  'task_completed',
  'task_failed',
  'message_sent',
  'first_token',
  'stream_end',
  // T5-06a 质量层：回答级反馈（helpful/unhelpful）。
  'message_feedback',
  'tool_call',
  'confirmation_shown',
  'confirmation_accepted',
  'confirmation_rejected',
  'approval_pending',
  'inline_preview',
  'inline_applied',
  'inline_undone',
  'help_opened',
  'help_gap',
  'doc_draft_created',
  // P2 起草采纳率（草稿→成文）：AI 预填草稿在新建页创建成功后才上报，是同一漏斗的终点。
  'doc_draft_published',
  // P2 文档闭环：用户显式反馈（extra.helpful=true/false）→ 反馈解决率运营报表。
  'doc_feedback',
  'error_shown',
  'error_recovered',
  'handoff',
  'proactive_shown',
  'proactive_clicked',
  'proactive_dismissed',
  'self_service_answered',
  'self_service_resolved',
  'self_service_ticket_created',
  'ai_create_field_corrected',
  // V2-01 个性化记忆：保存/删除常用语与模板、最近处理记录、用户开关（extra.op + 计数）。
  'memory_updated',
  'telemetry_disabled',
] as const;

export type AssistantEventName = (typeof ASSISTANT_EVENT_NAMES)[number];

/** 埋点开关三档（合规：用户可关闭个性化埋点）。 */
export type TelemetryMode = 'on' | 'anonymous' | 'off';

/** 脱敏后的标量允许进入 extra（复杂结构哈希为字符串）。 */
export type TelemetryScalar = string | number | boolean | null;

/**
 * 事件载荷（全字段可选，按事件语义携带；与 §13.2「关键字段」一一对应）。
 *
 * 注意：`extra` 是唯一允许承载自由文本的入口，写入前统一脱敏；
 * 顶层字段一律是结构化短值（pageKey/route/tool 名等），不做哈希。
 */
export interface AssistantEventInput {
  /** 事件时间（ms epoch）；缺省取 now()，供批处理/测试注入。 */
  ts?: number;
  pageKey?: string;
  route?: string;
  entrypoint?: string;
  /** 入口来源：fab | shortcut | launcher | page | notification 等。 */
  source?: string;
  targetType?: string;
  targetId?: number;
  cardId?: string;
  cardKind?: 'task' | 'question';
  rank?: number;
  taskCardId?: string;
  /** 结果落点类型（Outcome Contract）。 */
  outcomeType?: 'entity' | 'artifact' | 'draft' | 'navigation';
  conversationId?: number;
  runId?: string;
  toolName?: string;
  risk?: string;
  reasonCode?: string;
  model?: string;
  providerSource?: string;
  latencyMs?: number;
  durationMs?: number;
  tokenIn?: number;
  tokenOut?: number;
  /** T5-06a：本回合引用条数（0 = 无引用回答；旧后端缺省不落字段）。 */
  sourcesCount?: number;
  /** T5-06a：引用覆盖率的分母/分子便捷标记（sourcesCount > 0）。 */
  hasSources?: boolean;
  /** T5-06a：问题文本哈希（仅分组关联，不采集原文）。 */
  queryHash?: string;
  /** T5-06a：回答级反馈。 */
  feedback?: 'helpful' | 'unhelpful';
  errorCode?: string;
  recoveryAction?: string;
  field?: string;
  coverageStatus?: string;
  /** 自由文本/附加信息：写入前按 key 与类型脱敏（敏感 key 哈希，其余截断）。 */
  extra?: Record<string, unknown>;
}

/** 上报信封：事件名 + 时间 + 会话标识 + 载荷。 */
export interface AssistantEventEnvelope extends Omit<AssistantEventInput, 'extra'> {
  name: AssistantEventName;
  ts: number;
  /** 匿名会话 id（非用户 id；anonymous/off 模式下不携带）。 */
  sessionId?: string;
  extra?: Record<string, TelemetryScalar>;
}

/** 传输层抽象：默认走 `POST /api/v1/ai/telemetry/events`；测试注入假实现。 */
export interface TelemetryTransport {
  send(events: AssistantEventEnvelope[]): Promise<void>;
}

/** 最小存储抽象（默认 localStorage；SSR/隐私模式返回 null）。 */
export interface TelemetryStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface TelemetryClientOptions {
  transport?: TelemetryTransport;
  /** 达到该条数立即发送（默认 20）。 */
  batchSize?: number;
  /** 定时发送间隔（默认 5000ms）。 */
  flushIntervalMs?: number;
  /** 采样率 0~1（默认 1，全量）。 */
  sampleRate?: number;
  /** 缓冲上限，超出丢弃最旧（默认 200）。 */
  maxQueueSize?: number;
  /** 可注入随机源（测试采样用）。 */
  random?: () => number;
  /** 可注入时钟（测试用）。 */
  now?: () => number;
  /** 失败回调（默认吞掉；仅用于测试/诊断）。 */
  onError?: (error: unknown) => void;
  storage?: TelemetryStorage | null;
  sessionIdFactory?: () => string;
  /** 初始模式覆盖（缺省读持久化，再缺省 on）。 */
  mode?: TelemetryMode;
}

export const TELEMETRY_MODE_STORAGE_KEY = 'assistant.telemetry.mode';
export const TELEMETRY_SESSION_STORAGE_KEY = 'assistant.telemetry.sid';
export const TELEMETRY_ENDPOINT = '/api/v1/ai/telemetry/events';

/** 匿名模式下剥离的可关联字段（匿名聚合 = 不可回指到用户/会话/对象）。 */
const ANONYMOUS_STRIPPED_FIELDS: readonly string[] = [
  'sessionId',
  'conversationId',
  'runId',
  'targetId',
];

/** 可透传的顶层字段白名单（显式复制，防止任意对象被整体上报）。 */
const COPYABLE_FIELDS = [
  'pageKey',
  'route',
  'entrypoint',
  'source',
  'targetType',
  'targetId',
  'cardId',
  'cardKind',
  'rank',
  'taskCardId',
  'outcomeType',
  'conversationId',
  'runId',
  'toolName',
  'risk',
  'reasonCode',
  'model',
  'providerSource',
  'latencyMs',
  'durationMs',
  'tokenIn',
  'tokenOut',
  'sourcesCount',
  'hasSources',
  'queryHash',
  'feedback',
  'errorCode',
  'recoveryAction',
  'field',
  'coverageStatus',
] as const;

/** 敏感 key 特征：命中即整体哈希，不落原文。 */
const SENSITIVE_KEY_PATTERN =
  /prompt|content|text|query|question|message|answer|body|title|summary|description|remark|note|error|detail/i;

/** 非敏感字符串的最大保留长度。 */
const STRING_FIELD_MAX_LENGTH = 200;
const EXTRA_STRING_MAX_LENGTH = 120;

export function isAssistantEventName(value: unknown): value is AssistantEventName {
  return typeof value === 'string' && (ASSISTANT_EVENT_NAMES as readonly string[]).includes(value);
}

/** FNV-1a 32 位哈希（十六进制）；用于分组关联，非加密安全。 */
export function hashTelemetryText(text: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16).padStart(8, '0');
}

/** 任意值 → 标量：敏感或复杂结构哈希，数字/布尔原样保留。 */
function redactValue(value: unknown): TelemetryScalar {
  if (value === null || value === undefined) return null;
  if (typeof value === 'number' || typeof value === 'boolean') return value;
  if (typeof value === 'string') {
    return `h:${hashTelemetryText(value)}`;
  }
  try {
    return `h:${hashTelemetryText(JSON.stringify(value))}`;
  } catch {
    return 'h:unserializable';
  }
}

/** extra 脱敏：敏感 key 哈希；其余字符串截断；复杂结构哈希。 */
function sanitizeExtra(extra: Record<string, unknown> | undefined): Record<string, TelemetryScalar> | undefined {
  if (!extra) return undefined;
  const out: Record<string, TelemetryScalar> = {};
  for (const [key, value] of Object.entries(extra)) {
    if (value === undefined) continue;
    if (SENSITIVE_KEY_PATTERN.test(key)) {
      out[key] = redactValue(value);
      continue;
    }
    if (value === null || typeof value === 'number' || typeof value === 'boolean') {
      out[key] = value;
      continue;
    }
    if (typeof value === 'string') {
      out[key] = value.length > EXTRA_STRING_MAX_LENGTH ? value.slice(0, EXTRA_STRING_MAX_LENGTH) : value;
      continue;
    }
    out[key] = redactValue(value);
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function truncate(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) : value;
}

function clampNumber(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min;
  return Math.min(max, Math.max(min, value));
}

function defaultSessionId(): string {
  try {
    const uuid = globalThis.crypto?.randomUUID?.();
    if (uuid) return uuid;
  } catch {
    /* 降级到随机串 */
  }
  return `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function defaultStorage(): TelemetryStorage | null {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return null;
    const probe = '__assistant_telemetry_probe__';
    window.localStorage.setItem(probe, '1');
    window.localStorage.removeItem(probe);
    return window.localStorage;
  } catch {
    return null;
  }
}/**
 * 埋点客户端：缓冲、批量、采样、脱敏、失败静默。
 *
 * 生命周期：`track()` 入队 → 达到 batchSize 或定时触发 `flush()` → transport 上报。
 * 所有公开方法都不抛异常（异常仅走 onError），保证埋点不阻塞业务交互。
 */
export class TelemetryClient {
  private readonly transport: TelemetryTransport;
  private readonly batchSize: number;
  private readonly flushIntervalMs: number;
  private readonly sampleRate: number;
  private readonly maxQueueSize: number;
  private readonly random: () => number;
  private readonly now: () => number;
  private readonly onError: (error: unknown) => void;
  private readonly storage: TelemetryStorage | null;
  private readonly sessionIdFactory: () => string;

  private buffer: AssistantEventEnvelope[] = [];
  private timer: ReturnType<typeof setTimeout> | null = null;
  private flushPromise: Promise<void> | null = null;
  private mode: TelemetryMode;
  private sessionId: string | null = null;
  private readonly visibilityHandler: (() => void) | null = null;
  private readonly pageHideHandler: (() => void) | null = null;

  constructor(options: TelemetryClientOptions = {}) {
    this.transport = options.transport ?? createHttpTransport();
    this.batchSize = Math.max(1, Math.floor(options.batchSize ?? 20));
    this.flushIntervalMs = Math.max(100, Math.floor(options.flushIntervalMs ?? 5000));
    this.sampleRate = clampNumber(options.sampleRate ?? 1, 0, 1);
    this.maxQueueSize = Math.max(1, Math.floor(options.maxQueueSize ?? 200));
    this.random = options.random ?? Math.random;
    this.now = options.now ?? (() => Date.now());
    this.onError = options.onError ?? (() => undefined);
    this.storage = options.storage === undefined ? defaultStorage() : options.storage;
    this.sessionIdFactory = options.sessionIdFactory ?? defaultSessionId;
    this.mode = options.mode ?? this.readStoredMode();

    // 页面隐藏/关闭时尽力冲刷（keepalive 由 transport 负责；失败静默）。
    if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
      this.visibilityHandler = () => {
        if (document.visibilityState === 'hidden') void this.flush();
      };
      document.addEventListener('visibilitychange', this.visibilityHandler);
    }
    if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
      this.pageHideHandler = () => {
        void this.flush();
      };
      window.addEventListener('pagehide', this.pageHideHandler);
    }
  }

  /** 当前模式（on / anonymous / off）。 */
  get currentMode(): TelemetryMode {
    return this.mode;
  }

  /** 待发送事件数（诊断/测试用）。 */
  get pendingCount(): number {
    return this.buffer.length;
  }

  /**
   * 记录一个事件（不抛异常；未知事件名与关闭态直接丢弃）。
   * 采样未命中时同样丢弃（采样是入口级，保证批量口径一致）。
   */
  track(name: AssistantEventName, input: AssistantEventInput = {}): void {
    try {
      if (!isAssistantEventName(name)) return;
      if (this.mode === 'off') return;
      if (this.sampleRate < 1 && this.random() >= this.sampleRate) return;

      const envelope = this.buildEnvelope(name, input);
      this.buffer.push(envelope);
      if (this.buffer.length > this.maxQueueSize) {
        this.buffer.splice(0, this.buffer.length - this.maxQueueSize);
      }

      if (this.buffer.length >= this.batchSize) {
        void this.flush();
      } else {
        this.schedule();
      }
    } catch (error) {
      this.onError(error);
    }
  }

  /**
   * 发送当前缓冲（并发调用共享同一在飞批次；永不 reject）。
   *
   * 失败静默：丢弃该批次（best-effort，不做重试队列，避免拖垮业务与内存）。
   */
  flush(): Promise<void> {
    if (this.flushPromise) return this.flushPromise;
    if (this.buffer.length === 0) return Promise.resolve();
    this.clearTimer();
    const batch = this.buffer.splice(0, this.batchSize);
    this.flushPromise = (async () => {
      try {
        await this.transport.send(batch);
      } catch (error) {
        this.onError(error);
      } finally {
        this.flushPromise = null;
        if (this.buffer.length > 0) this.schedule();
      }
    })();
    return this.flushPromise;
  }

  /**
   * 切换模式并持久化。
   * 关闭（off）时先补一条 `telemetry_disabled` 审计事件再冲刷（best-effort）。
   */
  setMode(mode: TelemetryMode): void {
    try {
      if (mode === this.mode) return;
      if (mode === 'off') {
        this.track('telemetry_disabled', { extra: { previousMode: this.mode } });
      }
      this.mode = mode;
      this.persistMode(mode);
      if (mode === 'off') {
        void this.flush();
      }
    } catch (error) {
      this.onError(error);
    }
  }

  /** 释放定时器与监听（测试/热更新用）。 */
  dispose(): void {
    this.clearTimer();
    if (this.visibilityHandler && typeof document !== 'undefined' && typeof document.removeEventListener === 'function') {
      document.removeEventListener('visibilitychange', this.visibilityHandler);
    }
    if (this.pageHideHandler && typeof window !== 'undefined' && typeof window.removeEventListener === 'function') {
      window.removeEventListener('pagehide', this.pageHideHandler);
    }
  }

  private buildEnvelope(name: AssistantEventName, input: AssistantEventInput): AssistantEventEnvelope {
    const envelope: AssistantEventEnvelope = {
      name,
      ts: typeof input.ts === 'number' && Number.isFinite(input.ts) ? input.ts : this.now(),
    };
    const mutable = envelope as unknown as Record<string, unknown>;

    if (this.mode === 'on') {
      const sessionId = this.ensureSessionId();
      if (sessionId) mutable.sessionId = sessionId;
    }

    for (const field of COPYABLE_FIELDS) {
      if (this.mode === 'anonymous' && ANONYMOUS_STRIPPED_FIELDS.includes(field)) continue;
      const value = input[field as keyof AssistantEventInput];
      if (value === undefined || value === null) continue;
      mutable[field] = typeof value === 'string' ? truncate(value, STRING_FIELD_MAX_LENGTH) : value;
    }

    const extra = sanitizeExtra(input.extra);
    if (extra) envelope.extra = extra;
    return envelope;
  }

  private ensureSessionId(): string | null {
    if (this.sessionId) return this.sessionId;
    try {
      const stored = this.storage?.getItem(TELEMETRY_SESSION_STORAGE_KEY) ?? null;
      if (stored) {
        this.sessionId = stored;
        return stored;
      }
      const created = this.sessionIdFactory();
      this.sessionId = created;
      this.storage?.setItem(TELEMETRY_SESSION_STORAGE_KEY, created);
      return created;
    } catch {
      // 存储不可用时退化为内存会话（仍可关联同一页面内的行为）。
      if (!this.sessionId) this.sessionId = this.sessionIdFactory();
      return this.sessionId;
    }
  }

  private readStoredMode(): TelemetryMode {
    try {
      const stored = this.storage?.getItem(TELEMETRY_MODE_STORAGE_KEY);
      if (stored === 'on' || stored === 'anonymous' || stored === 'off') return stored;
    } catch {
      /* 读取失败按默认 */
    }
    return 'on';
  }

  private persistMode(mode: TelemetryMode): void {
    try {
      this.storage?.setItem(TELEMETRY_MODE_STORAGE_KEY, mode);
    } catch {
      /* 持久化失败不影响当前会话 */
    }
  }

  private schedule(): void {
    if (this.timer !== null) return;
    try {
      this.timer = setTimeout(() => {
        this.timer = null;
        void this.flush();
      }, this.flushIntervalMs);
    } catch {
      this.timer = null;
    }
  }

  private clearTimer(): void {
    if (this.timer === null) return;
    try {
      clearTimeout(this.timer);
    } catch {
      /* 忽略 */
    }
    this.timer = null;
  }
}

/**
 * 默认传输：`POST /api/v1/ai/telemetry/events`（O0-02 后端契约）。
 *
 * - `credentials: 'include'`（浏览器走 HttpOnly Cookie）；CSRF 缺失时仍尝试发送
 *   （后端 O0-02 落地前该端点可能 404 → 视为失败，静默丢弃）；
 * - `keepalive: true` 支持页面隐藏/关闭时的最后一跳。
 */
export function createHttpTransport(
  options: {
    url?: string;
    csrfTokenProvider?: () => Promise<string | null>;
  } = {}
): TelemetryTransport {
  const url = options.url ?? TELEMETRY_ENDPOINT;
  const getCsrfToken = options.csrfTokenProvider ?? (() => security.csrf.getToken());
  return {
    async send(events: AssistantEventEnvelope[]): Promise<void> {
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        'X-Requested-With': 'XMLHttpRequest',
      };
      try {
        const token = await getCsrfToken();
        if (token) headers['X-CSRF-Token'] = token;
      } catch {
        /* CSRF 获取失败不阻断（后端按 403 拒绝时静默丢弃） */
      }
      const response = await fetch(url, {
        method: 'POST',
        credentials: 'include',
        keepalive: true,
        headers,
        body: JSON.stringify({ events }),
      });
      if (!response?.ok) {
        throw new Error(`telemetry transport: HTTP ${response?.status ?? 'unknown'}`);
      }
    },
  };
}

/** 应用级单例（App/MainLayout 或具体页面按需引用）。 */
export const assistantTelemetry = new TelemetryClient();

/** 便捷入口：`trackAssistantEvent('assistant_opened', { pageKey })`。 */
export function trackAssistantEvent(name: AssistantEventName, input?: AssistantEventInput): void {
  assistantTelemetry.track(name, input);
}

/** 读取当前埋点模式（on / anonymous / off）。 */
export function getTelemetryMode(): TelemetryMode {
  return assistantTelemetry.currentMode;
}

/** 设置埋点模式（持久化到 localStorage；off 时立即停止采集）。 */
export function setTelemetryMode(mode: TelemetryMode): void {
  assistantTelemetry.setMode(mode);
}