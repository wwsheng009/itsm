/**
 * D1-02 页面注册 L2 运行时数据（显式调用；卸载自动清理）。
 *
 * 用法（详情页）：
 * ```tsx
 * useRegisterAssistantPageData(ticket ? { targetType: 'ticket', targetId: ticket.id, summary: ticket.title } : null);
 * ```
 * 约束：仅在对象**加载成功后**注册（ID 必须是服务端已返回的实体 ID）；
 * 服务端仍会对 target 做存在性 + 读权限预检（fail-closed），前端值只是提示。
 */
import { useEffect, useMemo } from 'react';

import { useAssistantContextStore } from '@/lib/store/assistant-context-store';
import type { AssistantPageData } from '@/lib/store/assistant-context-store';

export function useRegisterAssistantPageData(data: AssistantPageData | null | undefined): void {
  const owner = useMemo(() => Symbol('assistant-page-data'), []);
  const register = useAssistantContextStore(state => state.registerPageData);
  const clear = useAssistantContextStore(state => state.clearPageData);

  const targetType = data?.targetType;
  const targetId = data?.targetId;
  const summary = data?.summary;

  useEffect(() => {
    if (!targetType || !targetId || targetId <= 0) {
      clear(owner);
      return;
    }
    register(owner, { targetType, targetId, ...(summary ? { summary } : {}) });
    return () => clear(owner);
  }, [owner, register, clear, targetType, targetId, summary]);
}