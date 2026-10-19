/**
 * I1-03 主动协助信号注册表单测：构建器、高价值判定、忽略记忆过滤、徽标/建议选择。
 */
import {
  buildApprovalSignal,
  buildNotificationSignals,
  buildPageSuggestionSignal,
  isHighValueNotification,
  notificationTypeLabel,
  selectProactiveSignals,
} from '../signals';
import type { ToolApproval } from '@/lib/api/ai-api';
import type { UserNotification } from '@/lib/api/ticket-notification-api';
import type { PageAssistantDeclaration } from '@/lib/ai/page-context/types';

const approval = (over: Partial<ToolApproval> = {}): ToolApproval => ({
  id: 1,
  toolName: 'create_ticket',
  argsRedacted: '{}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  conversationId: 1,
  userId: 7,
  createdAt: '2026-10-18T00:00:00Z',
  ...over,
});

const notification = (over: Partial<UserNotification> = {}): UserNotification => ({
  id: 1,
  title: 'SLA 临期',
  message: '工单 #42 将在 30 分钟内超时',
  type: 'sla_warning',
  read: false,
  userId: 7,
  tenantId: 1,
  createdAt: '2026-10-18T00:00:00Z',
  updatedAt: '2026-10-18T00:00:00Z',
  ...over,
});

const declaration: PageAssistantDeclaration = {
  pageKey: 'ticket.detail',
  pattern: '/tickets/:id',
  title: '工单详情',
  tasks: [
    {
      id: 'triage_plan',
      kind: 'task',
      label: '分诊建议（分类与优先级）',
      prompt: '请给出这张工单的分类与优先级建议，并说明判断依据。',
      readOnly: true,
    },
  ],
  questions: [],
};

describe('proactive signals 构建器（I1-03）', () => {
  it('待审批聚合：仅统计 pending；空 → null；动作落点 /ai/approval', () => {
    expect(buildApprovalSignal([])).toBeNull();
    expect(buildApprovalSignal(null)).toBeNull();

    const signal = buildApprovalSignal([
      approval({ id: 1 }),
      approval({ id: 2 }),
      approval({ id: 3, approvalState: 'approved' }),
    ]);
    expect(signal).not.toBeNull();
    expect(signal?.label).toBe('2 项 AI 操作待审批');
    expect(signal?.count).toBe(2);
    expect(signal?.scopeKey).toBe('approval:pending');
    expect(signal?.action).toEqual({ type: 'navigate', route: '/ai/approval' });
  });

  it('高价值通知判定：白名单/前缀/深链三档', () => {
    expect(isHighValueNotification(notification())).toBe(true);
    expect(isHighValueNotification(notification({ type: 'ai_task_completed' }))).toBe(true);
    expect(isHighValueNotification(notification({ type: 'bot_run_finished' }))).toBe(true);
    expect(
      isHighValueNotification(notification({ type: 'info', actionUrl: '/ai/approval' }))
    ).toBe(true);
    expect(isHighValueNotification(notification({ type: 'commented' }))).toBe(false);
    expect(isHighValueNotification(undefined)).toBe(false);
    expect(notificationTypeLabel('sla_violated')).toBe('SLA 超时提醒');
    expect(notificationTypeLabel('ai_unknown_event')).toBe('助手相关通知');
  });

  it('通知聚合：按类型分组计数、上限、深链与标签', () => {
    const signals = buildNotificationSignals([
      notification({ id: 1 }),
      notification({ id: 2, title: '另一条 SLA' }),
      notification({ id: 3, type: 'approval_required', actionUrl: '/ai/approval' }),
      notification({ id: 4, type: 'commented' }),
    ]);
    expect(signals).toHaveLength(2);
    expect(signals[0].label).toBe('2 条SLA 临期提醒');
    expect(signals[0].count).toBe(2);
    expect(signals[0].description).toBe('SLA 临期');
    expect(signals[0].severity).toBe('warning');
    expect(signals[0].action).toEqual({ type: 'navigate', route: '/notifications' });
    expect(signals[1].action).toEqual({ type: 'navigate', route: '/ai/approval' });

    const limited = buildNotificationSignals(
      [
        notification({ id: 1, type: 'sla_warning' }),
        notification({ id: 2, type: 'approval_required' }),
        notification({ id: 3, type: 'ai_task_completed' }),
        notification({ id: 4, type: 'bot_run_finished' }),
      ],
      2
    );
    expect(limited).toHaveLength(2);
  });

  it('页面建议：取首个任务卡；prompt/route 两种动作形态；无声明 → null', () => {
    expect(buildPageSuggestionSignal(undefined, declaration)).toBeNull();
    expect(buildPageSuggestionSignal('ticket.detail', null)).toBeNull();

    const signal = buildPageSuggestionSignal('ticket.detail', declaration);
    expect(signal?.id).toBe('page:ticket.detail:triage_plan');
    expect(signal?.scopeKey).toBe('page:ticket.detail');
    expect(signal?.label).toBe('建议：分诊建议（分类与优先级）');
    expect(signal?.count).toBe(0);
    expect(signal?.action).toEqual({
      type: 'prompt',
      prompt: '请给出这张工单的分类与优先级建议，并说明判断依据。',
      entrypoint: undefined,
    });

    const withRoute = buildPageSuggestionSignal('ticket.detail', {
      ...declaration,
      tasks: [{ ...declaration.tasks[0], route: '/tickets/42?tab=ai' }],
    });
    expect(withRoute?.action).toEqual({ type: 'navigate', route: '/tickets/42?tab=ai' });
  });

  it('选择器：徽标=待办求和（不含页面建议）；静默时不返回内联建议', () => {
    const selection = selectProactiveSignals({
      approvals: [approval({ id: 1 }), approval({ id: 2 })],
      notifications: [notification()],
      pageKey: 'ticket.detail',
      declaration,
      dismissedScopes: [],
      suggestionAllowed: false,
    });
    expect(selection.badgeCount).toBe(3);
    expect(selection.suggestion).toBeNull();
    expect(selection.visible.map(signal => signal.kind)).toEqual([
      'approval',
      'notification',
      'page_suggestion',
    ]);

    const withSuggestion = selectProactiveSignals({
      approvals: [approval({ id: 1 }), approval({ id: 2 })],
      notifications: [notification()],
      pageKey: 'ticket.detail',
      declaration,
      suggestionAllowed: true,
    });
    expect(withSuggestion.badgeCount).toBe(3);
    expect(withSuggestion.suggestion?.id).toBe('page:ticket.detail:triage_plan');
  });

  it('选择器：「不再提示」按 scopeKey 记忆（页面/审批各自生效）', () => {
    const pageDismissed = selectProactiveSignals({
      approvals: [approval({ id: 1 })],
      notifications: [],
      pageKey: 'ticket.detail',
      declaration,
      dismissedScopes: ['page:ticket.detail'],
      suggestionAllowed: true,
    });
    expect(pageDismissed.suggestion).toBeNull();
    expect(pageDismissed.badgeCount).toBe(1);

    const approvalDismissed = selectProactiveSignals({
      approvals: [approval({ id: 1 }), approval({ id: 2 })],
      notifications: [notification()],
      pageKey: 'ticket.detail',
      declaration,
      dismissedScopes: ['approval:pending', 'notification:sla_warning'],
      suggestionAllowed: true,
    });
    expect(approvalDismissed.badgeCount).toBe(0);
    expect(approvalDismissed.visible).toHaveLength(1);
    expect(approvalDismissed.suggestion).not.toBeNull();
  });
});