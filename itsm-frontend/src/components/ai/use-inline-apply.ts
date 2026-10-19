/**
 * I1-01 Inline Apply 流程（hook）：diff 预览 → 用户确认应用 → 可撤销；**绝不自动应用/提交**。
 *
 * 口径（方案 §3.3 一键应用）：
 * - 应用前必须展示 diff（字段 before→after），用户显式确认后才调用 `onApply`；
 * - 应用后保留最近一次变更集，可 `undo()` 回滚（回调由页面提供：写回旧值 / 恢复本地表单值）；
 * - 埋点：inline_preview（展示 diff）/ inline_applied（确认应用）/ inline_undone（撤销）/ error_shown（失败）。
 */
import { useCallback, useState } from 'react';

import { recordAssistantMemoryAction } from '@/lib/ai/memory/assistant-memory-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

export interface InlineChangeSet {
  /** 稳定字段键（埋点/断言用）。 */
  field: string;
  /** 展示名（如「分类」）。 */
  label: string;
  /** 变更前（可空 = 无值）。 */
  before?: string;
  /** 变更后。 */
  after: string;
}

export interface InlineApplyFlowOptions {
  /** 使用面标识（埋点 extra.scope，如 triage_detail / triage_create）。 */
  scope: string;
  pageKey?: string;
  onApply: (changes: InlineChangeSet[]) => Promise<void> | void;
  onUndo?: (changes: InlineChangeSet[]) => Promise<void> | void;
}

export interface InlineApplyFlow {
  /** 待确认变更集（null = 未在预览）。 */
  pending: InlineChangeSet[] | null;
  applying: boolean;
  /** 最近一次已应用的变更集（null = 无可撤销）。 */
  applied: InlineChangeSet[] | null;
  preview: (changes: InlineChangeSet[]) => void;
  confirm: () => Promise<void>;
  cancel: () => void;
  undo: () => Promise<void>;
}

const fieldsOf = (changes: InlineChangeSet[]): string => changes.map(item => item.field).join(',');

export function useInlineApplyFlow({
  scope,
  pageKey,
  onApply,
  onUndo,
}: InlineApplyFlowOptions): InlineApplyFlow {
  const [pending, setPending] = useState<InlineChangeSet[] | null>(null);
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState<InlineChangeSet[] | null>(null);

  const preview = useCallback(
    (changes: InlineChangeSet[]) => {
      if (!changes.length) return;
      setPending(changes);
      trackAssistantEvent('inline_preview', {
        pageKey,
        extra: { scope, fields: fieldsOf(changes) },
      });
    },
    [pageKey, scope]
  );

  const confirm = useCallback(async () => {
    if (!pending || pending.length === 0) return;
    setApplying(true);
    try {
      await onApply(pending);
      trackAssistantEvent('inline_applied', {
        pageKey,
        extra: { scope, action: 'apply', fields: fieldsOf(pending) },
      });
      // V2-01「上次处理方式」：只记元数据（动作键），仅在记忆已就绪时生效（未加载不隐式请求）。
      recordAssistantMemoryAction(`inline_apply:${scope}`);
      setApplied(pending);
      setPending(null);
    } catch {
      // 失败保留 pending 便于重试；错误提示由页面 onApply 内部负责（fail-visible）。
      trackAssistantEvent('error_shown', { pageKey, errorCode: 'inline_apply_failed' });
    } finally {
      setApplying(false);
    }
  }, [onApply, pageKey, pending, scope]);

  const cancel = useCallback(() => setPending(null), []);

  const undo = useCallback(async () => {
    if (!applied || applied.length === 0 || !onUndo) return;
    try {
      await onUndo(applied);
      trackAssistantEvent('inline_undone', {
        pageKey,
        extra: { scope, fields: fieldsOf(applied) },
      });
      setApplied(null);
    } catch {
      trackAssistantEvent('error_shown', { pageKey, errorCode: 'inline_undo_failed' });
    }
  }, [applied, onUndo, pageKey, scope]);

  return { pending, applying, applied, preview, confirm, cancel, undo };
}