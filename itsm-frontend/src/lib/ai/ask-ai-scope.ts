/**
 * B3-02 页面 launcher 的路由 state 契约（与后端 B3-01 入口协议一一对应）。
 *
 * 背景：工单/事件/CI 详情页的「问 AI」入口需要把**入口上下文**带到工作区：
 * `{ entrypoint, target_type, target_id, summary }`。该上下文经路由 state 传递，
 * 由工作区在每次对话请求中回传给后端（后端会再次解析并做目标对象预检，前端值只是提示）。
 *
 * 契约与 `lib/knowledge/ai-article-prefill` 同风格：
 * - 写入：`buildAskAIState(scope)`；
 * - 读取：`readAskAIScope(location.state)` 一律**校验**（React Router 的 state 不受
 *   类型约束，刷新后来自 history.state，缺字段/越界一律按「无上下文」处理）；
 * - 请求：`buildAskAIRequestScope(scope)` 生成只含非空字段的请求片段。
 *
 * 安全边界（与服务端一致）：target 必须是**服务端校验过的对象**；前端不构造 ID，
 * 只透传当前页面已经加载成功的对象 ID。
 */

/** 入口枚举（与 `service/bot/scope.go` 的 KnownEntrypoints 对齐）。 */
export const ASK_AI_ENTRYPOINTS = [
  'chat',
  'ticket_detail',
  'ticket_list',
  'incident_detail',
  'incident_create',
  'ci_detail',
] as const;
export type AskAIEntrypoint = (typeof ASK_AI_ENTRYPOINTS)[number];

/** 目标对象类型（与后端 TargetType 对齐）。 */
export const ASK_AI_TARGET_TYPES = ['ticket', 'incident', 'ci'] as const;
export type AskAITargetType = (typeof ASK_AI_TARGET_TYPES)[number];

/** 路由 state 的来源标记（校验用，避免误读其他页面的 state）。 */
export const ASK_AI_STATE_SOURCE = 'ask-ai-launcher';

/** 工作区路由（与 routes/route-paths.ts 的 `/ai/chat` 一致）。 */
export const ASK_AI_ROUTE = '/ai/chat';

/** summary 截断上限（与后端 ScopeSummaryMaxRunes 一致）。 */
export const ASK_AI_SUMMARY_MAX_LENGTH = 300;

export interface AskAIScope {
  entrypoint: AskAIEntrypoint;
  targetType?: AskAITargetType;
  targetId?: number;
  summary?: string;
}

export interface AskAIState {
  source: typeof ASK_AI_STATE_SOURCE;
  scope: AskAIScope;
}

/** 生成工作区路由 state（仅包含实际存在的字段）。 */
export function buildAskAIState(scope: AskAIScope): AskAIState {
  return { source: ASK_AI_STATE_SOURCE, scope: normalizeScope(scope) };
}

/**
 * 读取并校验路由 state；不合法（来源不符/入口未知/目标不成对/越界）一律返回 null。
 */
export function readAskAIScope(state: unknown): AskAIScope | null {
  if (!isRecord(state) || state.source !== ASK_AI_STATE_SOURCE) {
    return null;
  }
  const raw = state.scope;
  if (!isRecord(raw)) {
    return null;
  }
  const entrypoint = raw.entrypoint;
  if (typeof entrypoint !== 'string' || !isEntrypoint(entrypoint)) {
    return null;
  }
  const scope: AskAIScope = { entrypoint };

  const targetType = raw.targetType;
  const targetId = raw.targetId;
  const hasType = targetType !== undefined && targetType !== null && targetType !== '';
  const hasId = typeof targetId === 'number' && Number.isInteger(targetId) && targetId > 0;
  if (hasType !== hasId) {
    return null; // 目标必须成对出现（与后端 ErrScopeTargetPairIncomplete 同口径）
  }
  if (hasType) {
    if (typeof targetType !== 'string' || !isTargetType(targetType)) {
      return null;
    }
    scope.targetType = targetType;
    scope.targetId = targetId as number;
  }

  const summary = typeof raw.summary === 'string' ? raw.summary.trim() : '';
  if (summary) {
    scope.summary = truncateSummary(summary);
  }
  return scope;
}

/**
 * 生成请求片段：仅包含非空字段（未携带上下文时返回空对象，请求体与现状一致）。
 */
export function buildAskAIRequestScope(scope: AskAIScope | null | undefined): {
  entrypoint?: AskAIEntrypoint;
  targetType?: AskAITargetType;
  targetId?: number;
  summary?: string;
} {
  if (!scope) {
    return {};
  }
  const out: {
    entrypoint?: AskAIEntrypoint;
    targetType?: AskAITargetType;
    targetId?: number;
    summary?: string;
  } = {};
  if (scope.entrypoint && scope.entrypoint !== 'chat') {
    out.entrypoint = scope.entrypoint;
  }
  if (scope.targetType && scope.targetId) {
    out.targetType = scope.targetType;
    out.targetId = scope.targetId;
  }
  if (scope.summary) {
    out.summary = truncateSummary(scope.summary);
  }
  return out;
}

/** 入口的中文标签（工作区上下文条展示用）。 */
export function askAIEntrypointLabel(entrypoint: AskAIEntrypoint): string {
  const labels: Record<AskAIEntrypoint, string> = {
    chat: '对话',
    ticket_detail: '工单详情',
    ticket_list: '工单列表',
    incident_detail: '事件详情',
    incident_create: '事件创建',
    ci_detail: '配置项详情',
  };
  return labels[entrypoint] ?? entrypoint;
}

function normalizeScope(scope: AskAIScope): AskAIScope {
  const out: AskAIScope = { entrypoint: scope.entrypoint };
  if (scope.targetType && scope.targetId && scope.targetId > 0) {
    out.targetType = scope.targetType;
    out.targetId = scope.targetId;
  }
  if (scope.summary && scope.summary.trim()) {
    out.summary = truncateSummary(scope.summary.trim());
  }
  return out;
}

function truncateSummary(value: string): string {
  return value.length > ASK_AI_SUMMARY_MAX_LENGTH ? value.slice(0, ASK_AI_SUMMARY_MAX_LENGTH) : value;
}

function isEntrypoint(value: string): value is AskAIEntrypoint {
  return (ASK_AI_ENTRYPOINTS as readonly string[]).includes(value);
}

function isTargetType(value: string): value is AskAITargetType {
  return (ASK_AI_TARGET_TYPES as readonly string[]).includes(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
