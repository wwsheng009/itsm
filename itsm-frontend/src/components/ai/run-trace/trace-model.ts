/**
 * O1-01 轨迹回放纯逻辑：确认状态标签、业务对象落点、耗时与载荷摘要。
 *
 * 与视图分离（`RunTraceView` 只做渲染），便于单测锁定口径：
 * - 业务对象落点只覆盖注册表已确认的路由模式（其余按纯文本展示，不臆造链接）；
 * - 载荷摘要固定截断（避免大 payload 撑爆回放视图）。
 */
import type { AIRunTrace, AIRunTraceRun } from '@/lib/api/ai-api';

/** 确认状态标签（与后端 bot.NormalizeConfirmationState 词汇表一致）。 */
export const CONFIRMATION_STATE_LABELS: Record<string, string> = {
  pending: '待确认',
  confirmed: '已确认',
  rejected: '已驳回',
  expired: '已过期',
  cancelled: '已取消',
  unknown: '未知',
};

export function confirmationLabel(state?: string): string {
  if (!state) return CONFIRMATION_STATE_LABELS.unknown;
  return CONFIRMATION_STATE_LABELS[state] ?? state;
}

/** 确认状态 → antd Tag 颜色。 */
export function confirmationColor(state?: string): string {
  switch (state) {
    case 'confirmed':
      return 'green';
    case 'pending':
      return 'gold';
    case 'rejected':
      return 'red';
    case 'expired':
      return 'default';
    default:
      return 'default';
  }
}

/** 运行状态 → antd Tag 颜色。 */
export function runStatusColor(status?: string): string {
  switch (status) {
    case 'completed':
      return 'green';
    case 'failed':
      return 'red';
    case 'running':
      return 'blue';
    case 'cancelled':
      return 'default';
    default:
      return 'default';
  }
}

/**
 * 业务对象深链（只映射注册表已确认的详情路由；未知类型返回 undefined）。
 */
export function targetRoute(targetType?: string, targetId?: string | number): string | undefined {
  if (!targetType || targetId === undefined || targetId === null || targetId === '') return undefined;
  const id = typeof targetId === 'string' ? Number.parseInt(targetId, 10) : targetId;
  if (!Number.isFinite(id) || id <= 0) return undefined;
  switch (targetType) {
    case 'ticket':
      return `/tickets/${id}`;
    case 'incident':
      return `/incidents/${id}`;
    case 'change':
      return `/changes/${id}`;
    case 'ci':
      return `/cmdb/cis/${id}`;
    default:
      return undefined;
  }
}

export interface TraceTargetRef {
  key: string;
  type: string;
  id: string;
  route?: string;
}

/** 汇总 run 与工具调用的业务对象引用（去重，保持出现顺序）。 */
export function collectTraceTargets(trace: AIRunTrace): TraceTargetRef[] {
  const refs: TraceTargetRef[] = [];
  const seen = new Set<string>();
  const push = (type?: string, id?: string | number) => {
    if (!type || id === undefined || id === null || id === '') return;
    const idText = String(id);
    const key = `${type}#${idText}`;
    if (seen.has(key)) return;
    seen.add(key);
    refs.push({ key, type, id: idText, route: targetRoute(type, idText) });
  };
  push(trace.run?.targetType, trace.run?.targetId);
  for (const tool of trace.toolInvocations ?? []) {
    push(tool.targetType, tool.targetId);
  }
  return refs;
}

/** 运行耗时（finished - started，毫秒；未收口 → undefined）。 */
export function runDurationMs(run?: AIRunTraceRun): number | undefined {
  if (!run?.startedAt || !run?.finishedAt) return undefined;
  const started = Date.parse(run.startedAt);
  const finished = Date.parse(run.finishedAt);
  if (Number.isNaN(started) || Number.isNaN(finished) || finished < started) return undefined;
  return finished - started;
}

/** 毫秒 → 人类可读（<1s 显示 ms，否则保留一位小数秒）。 */
export function formatDuration(ms?: number): string {
  if (ms === undefined || !Number.isFinite(ms)) return '—';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

/** 时间戳 → 本地短格式（非法/缺省 → '—'）。 */
export function formatTraceTime(iso?: string): string {
  if (!iso) return '—';
  const ts = Date.parse(iso);
  if (Number.isNaN(ts)) return iso;
  return new Date(ts).toLocaleString();
}

const PAYLOAD_SUMMARY_MAX = 160;

/** 事件载荷摘要（JSON 单行 + 截断；空 → '—'）。 */
export function summarizePayload(payload?: Record<string, unknown>): string {
  if (!payload || Object.keys(payload).length === 0) return '—';
  let text = '';
  try {
    text = JSON.stringify(payload);
  } catch {
    text = '[unserializable]';
  }
  return text.length > PAYLOAD_SUMMARY_MAX ? `${text.slice(0, PAYLOAD_SUMMARY_MAX)}…` : text;
}