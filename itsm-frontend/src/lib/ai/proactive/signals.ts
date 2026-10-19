/**
 * I1-03 主动协助信号注册表（方案 §3.6：允许的三种非打断形态）。
 *
 * 信号来源（全部只读聚合，不做任何自动动作）：
 * 1. `approval` —— AI 写操作待审批（`GET /agent/tools/invocations?state=pending`，`ai:read`）；
 * 2. `notification` —— 高价值通知（SLA 临期/审批结果/AI 任务等；WS 推送 + 未读列表过滤）；
 * 3. `page_suggestion` —— 当前页面 L1 声明的首个任务卡（零输入只读）→ 页面内联建议条。
 *
 * 计数口径：徽标只统计「待办」类信号（approval / notification）；页面建议不计数（建议≠待办）。
 * 「不再提示」以 `scopeKey` 去重：approval 全局一条、notification 按类型、页面按 pageKey。
 */
import type { PageAssistantDeclaration } from '@/lib/ai/page-context/types';
import type { ToolApproval } from '@/lib/api/ai-api';
import type { UserNotification } from '@/lib/api/ticket-notification-api';

export type ProactiveSignalKind = 'approval' | 'notification' | 'page_suggestion';
export type ProactiveSeverity = 'info' | 'warning';

export type ProactiveAction =
  | { type: 'navigate'; route: string }
  | { type: 'prompt'; prompt: string; entrypoint?: string };

export interface ProactiveSignal {
  id: string;
  kind: ProactiveSignalKind;
  /** 「不再提示」记忆键。 */
  scopeKey: string;
  label: string;
  description?: string;
  severity: ProactiveSeverity;
  /** 计入徽标的数量（页面建议恒 0）。 */
  count: number;
  action: ProactiveAction;
}

/** 高价值通知类型白名单（与后端通知 type 口径对齐；docs 事件字典）。 */
export const HIGH_VALUE_NOTIFICATION_TYPES: readonly string[] = [
  'sla_warning',
  'sla_violated',
  'approval_required',
  'ai_task_completed',
  'ai_approval_result',
  'bot_run_finished',
];

/** 前缀规则：AI/机器人/Agent 域通知。 */
const HIGH_VALUE_TYPE_PREFIXES: readonly string[] = ['ai_', 'bot_', 'agent_'];

const NOTIFICATION_TYPE_LABELS: Record<string, string> = {
  sla_warning: 'SLA 临期提醒',
  sla_violated: 'SLA 超时提醒',
  approval_required: '审批待办',
  ai_task_completed: 'AI 任务完成',
  ai_approval_result: 'AI 审批结果',
  bot_run_finished: '机器人任务完成',
};

export function notificationTypeLabel(type?: string): string {
  const key = (type ?? '').toLowerCase();
  return NOTIFICATION_TYPE_LABELS[key] ?? '助手相关通知';
}

/**
 * 高价值通知判定：类型白名单/前缀，或深链落在 AI 域（`/ai/**`）。
 * 非高价值通知不进信号集（避免与通知中心重复打扰）。
 */
export function isHighValueNotification(notification?: UserNotification | null): boolean {
  if (!notification) return false;
  const type = (notification.type ?? '').toLowerCase();
  if (HIGH_VALUE_NOTIFICATION_TYPES.includes(type)) return true;
  if (HIGH_VALUE_TYPE_PREFIXES.some(prefix => type.startsWith(prefix))) return true;
  return typeof notification.actionUrl === 'string' && notification.actionUrl.startsWith('/ai/');
}

/** 待审批聚合信号（0 条 → null）。 */
export function buildApprovalSignal(pending?: ToolApproval[] | null): ProactiveSignal | null {
  const count = (pending ?? []).filter(
    item => !item.approvalState || item.approvalState === 'pending'
  ).length;
  if (count <= 0) return null;
  return {
    id: 'approval:pending',
    kind: 'approval',
    scopeKey: 'approval:pending',
    label: `${count} 项 AI 操作待审批`,
    description: 'AI 写操作需你确认后才会执行（approval 中心）',
    severity: 'warning',
    count,
    action: { type: 'navigate', route: '/ai/approval' },
  };
}

/** 高价值通知按类型聚合为信号（上限 `limit` 类）。 */
export function buildNotificationSignals(
  notifications?: UserNotification[] | null,
  limit = 3
): ProactiveSignal[] {
  const groups = new Map<string, { count: number; sample: UserNotification }>();
  for (const item of notifications ?? []) {
    if (!isHighValueNotification(item)) continue;
    const type = (item.type ?? 'ai_related').toLowerCase();
    const current = groups.get(type);
    if (current) {
      current.count += 1;
    } else {
      groups.set(type, { count: 1, sample: item });
    }
  }
  const signals: ProactiveSignal[] = [];
  for (const [type, group] of groups) {
    if (signals.length >= limit) break;
    signals.push({
      id: `notification:${type}`,
      kind: 'notification',
      scopeKey: `notification:${type}`,
      label: `${group.count} 条${notificationTypeLabel(type)}`,
      description: group.sample.title || group.sample.message || undefined,
      severity: type.startsWith('sla_') || type === 'approval_required' ? 'warning' : 'info',
      count: group.count,
      action: {
        type: 'navigate',
        route:
          typeof group.sample.actionUrl === 'string' && group.sample.actionUrl
            ? group.sample.actionUrl
            : '/notifications',
      },
    });
  }
  return signals;
}

/** 页面首个任务卡 → 内联建议信号（页面无声明/无任务卡 → null）。 */
export function buildPageSuggestionSignal(
  pageKey?: string,
  declaration?: PageAssistantDeclaration | null
): ProactiveSignal | null {
  const task = declaration?.tasks?.[0];
  if (!pageKey || !task) return null;
  return {
    id: `page:${pageKey}:${task.id}`,
    kind: 'page_suggestion',
    scopeKey: `page:${pageKey}`,
    label: `建议：${task.label}`,
    description: declaration?.title || undefined,
    severity: 'info',
    count: 0,
    action: task.route
      ? { type: 'navigate', route: task.route }
      : { type: 'prompt', prompt: task.prompt, entrypoint: declaration?.entrypoint },
  };
}

export interface ProactiveSelection {
  /** 过滤「不再提示」后的可见信号（badge + 内联条共用）。 */
  visible: ProactiveSignal[];
  /** 徽标计数（待办类信号求和）。 */
  badgeCount: number;
  /** 当前可展示的内联建议（静默模式/未启用时为 null）。 */
  suggestion: ProactiveSignal | null;
}

export interface ProactiveSelectionInput {
  approvals?: ToolApproval[] | null;
  notifications?: UserNotification[] | null;
  pageKey?: string;
  declaration?: PageAssistantDeclaration | null;
  dismissedScopes?: string[];
  /** 是否允许内联建议条（静默模式关闭 + 总开关开启时才为 true）。 */
  suggestionAllowed?: boolean;
}

/** 汇总信号 → 过滤「不再提示」→ 计算徽标与内联建议。 */
export function selectProactiveSignals(input: ProactiveSelectionInput): ProactiveSelection {
  const dismissed = new Set(input.dismissedScopes ?? []);
  const raw = [
    buildApprovalSignal(input.approvals),
    ...buildNotificationSignals(input.notifications),
    buildPageSuggestionSignal(input.pageKey, input.declaration),
  ].filter((signal): signal is ProactiveSignal => signal !== null);

  const visible = raw.filter(signal => !dismissed.has(signal.scopeKey));
  const badgeCount = visible
    .filter(signal => signal.kind !== 'page_suggestion')
    .reduce((total, signal) => total + signal.count, 0);
  const suggestion = input.suggestionAllowed
    ? visible.find(signal => signal.kind === 'page_suggestion') ?? null
    : null;

  return { visible, badgeCount, suggestion };
}