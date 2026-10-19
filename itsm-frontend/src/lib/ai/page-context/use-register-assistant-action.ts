/**
 * D1-02 页面注册动作 handler（卸载自动注销）。
 *
 * 用法（页面）：
 * ```tsx
 * useRegisterAssistantAction('reply_draft', ctx => openReplyEditor());
 * ```
 * 未注册的动作按动作桥回退规则处理（route → 跳转；否则发送提示词）。
 */
import { useEffect } from 'react';

import { registerAssistantAction } from '@/lib/ai/assistant-actions';
import type { AssistantActionHandler } from '@/lib/ai/assistant-actions';

export function useRegisterAssistantAction(
  actionId: string,
  handler: AssistantActionHandler | null
): void {
  useEffect(() => {
    if (!actionId || !handler) return;
    return registerAssistantAction(actionId, handler);
  }, [actionId, handler]);
}