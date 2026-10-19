/**
 * I1-03 主动协助信号 hook：聚合「待审批 + 高价值通知 + 页面建议」，供 FAB 徽标与内联建议条消费。
 *
 * 数据面（全部只读，不触发任何写动作）：
 * - `GET /agent/tools/invocations?state=pending`（ai:read）→ 待审批计数；
 * - `GET /notifications?read=false&size=20`（复用现有通知系统）→ 高价值未读通知；
 * - `notificationWS.onNotification` → 实时推送（与 Header 共用单例；只读订阅，互不干扰）。
 *
 * 约束：`active=false`（无权限/总开关关闭）时**零请求**；静默模式下只出徽标不出内联条。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';

import { aiGetToolApprovals, type ToolApproval } from '@/lib/api/ai-api';
import {
  TicketNotificationApi,
  type UserNotification,
} from '@/lib/api/ticket-notification-api';
import { notificationWS } from '@/lib/services/notification-ws';
import { useAssistantProactiveStore } from '@/lib/store/assistant-proactive-store';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import { selectProactiveSignals, type ProactiveSignal } from './signals';

/** 待审批计数刷新间隔（与 Header 通知轮询同量级，避免额外压力）。 */
export const PROACTIVE_REFRESH_INTERVAL_MS = 60000;
const MAX_SIGNAL_NOTIFICATIONS = 20;

export interface ProactiveSignalsState {
  visible: ProactiveSignal[];
  badgeCount: number;
  suggestion: ProactiveSignal | null;
  refresh: () => void;
}

export function useProactiveSignals(active: boolean): ProactiveSignalsState {
  const enabled = useAssistantProactiveStore(state => state.enabled);
  const silent = useAssistantProactiveStore(state => state.silent);
  const dismissedScopes = useAssistantProactiveStore(state => state.dismissedScopes);
  const { pageKey, declaration } = usePageAssistantContext();

  const [approvals, setApprovals] = useState<ToolApproval[]>([]);
  const [notifications, setNotifications] = useState<UserNotification[]>([]);
  const [pushed, setPushed] = useState<UserNotification[]>([]);
  const running = active && enabled;

  const refresh = useCallback(async () => {
    if (!running) return;
    const [approvalResult, notificationResult] = await Promise.allSettled([
      aiGetToolApprovals('pending'),
      TicketNotificationApi.getUserNotifications({
        page: 1,
        size: MAX_SIGNAL_NOTIFICATIONS,
        read: false,
      }),
    ]);
    if (approvalResult.status === 'fulfilled') {
      const items = approvalResult.value.items ?? [];
      // 空 → 空时不触发重渲染（避免无意义的状态更新与测试期 act 噪音）。
      setApprovals(prev => (prev.length === 0 && items.length === 0 ? prev : items));
    }
    if (notificationResult.status === 'fulfilled') {
      const items = notificationResult.value.notifications ?? [];
      setNotifications(prev => (prev.length === 0 && items.length === 0 ? prev : items));
    }
  }, [running]);

  useEffect(() => {
    if (!running) return undefined;
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, PROACTIVE_REFRESH_INTERVAL_MS);
    const unsubscribe = notificationWS.onNotification(notification => {
      if (!notification || notification.read) return;
      setPushed(prev => [notification, ...prev.filter(item => item.id !== notification.id)].slice(0, MAX_SIGNAL_NOTIFICATIONS));
      // 通知到达顺带刷新待审批计数（审批结果可能只经通知中心投递）。
      void refresh();
    });
    return () => {
      window.clearInterval(timer);
      unsubscribe();
    };
  }, [running, refresh]);

  const mergedNotifications = useMemo(() => {
    const map = new Map<number, UserNotification>();
    for (const item of [...pushed, ...notifications]) {
      if (!map.has(item.id)) map.set(item.id, item);
    }
    return [...map.values()];
  }, [pushed, notifications]);

  const selection = useMemo(
    () => {
      // 总开关关闭（或无权限）→ 完全静默：无徽标、无内联条（历史聚合结果不外泄）。
      if (!running) return { visible: [], badgeCount: 0, suggestion: null };
      return selectProactiveSignals({
        approvals,
        notifications: mergedNotifications,
        pageKey,
        declaration,
        dismissedScopes,
        suggestionAllowed: !silent,
      });
    },
    [running, approvals, mergedNotifications, pageKey, declaration, dismissedScopes, silent]
  );

  return useMemo(() => ({ ...selection, refresh }), [selection, refresh]);
}