/**
 * V1-01 列表页 AI 批量助手（方案 §7 V1-01）：选中 N 行 → 建议分类 / 批量草稿 / 批量分派。
 *
 * 写边界（沿用 I1-01 Inline Apply）：
 *   - 数据端点只读：只返回建议/草稿，不写任何业务字段；
 *   - 「采纳 / 分派」是用户显式动作：逐行或全部确认后调用既有写端点
 *     （PUT /tickets/:id、POST /tickets/:id/assign），绝不自动提交；
 *   - 草稿只提供复制，不自动评论/发送。
 *
 * 埋点（O0-01 SDK，pageKey=ticket.list）：task_started/task_completed/task_failed
 * 记录批量任务耗时与失败率，inline_applied 记录采纳/分派/复制动作（供 O0-03 看板）。
 */

import React, { useCallback, useState } from 'react';
import { Alert, Button, Card, Space, Tag, Typography, message } from 'antd';
import { ClipboardCopy, FileText, Sparkles, UserCheck } from 'lucide-react';

import { TicketApi } from '@/lib/api/ticket-api';
import type { Ticket, TicketPriority } from '@/lib/api/types';
import {
  BATCH_REPLY_DRAFT_MAX_TICKETS,
  BATCH_TRIAGE_MAX_TICKETS,
  aiBatchReplyDrafts,
  aiBatchTriage,
  type BatchReplyDraftItem,
  type BatchTriageItem,
} from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useCan } from '@/components/auth/PermissionGate';

const { Text, Paragraph } = Typography;

const ticketPriorities: TicketPriority[] = ['low', 'medium', 'high', 'urgent', 'critical'];
const toTicketPriority = (value?: string): TicketPriority =>
  value && ticketPriorities.includes(value as TicketPriority) ? (value as TicketPriority) : 'medium';

const categoryLabels: Record<string, string> = {
  database: '数据库',
  network: '网络',
  server: '服务器',
  application: '应用',
  security: '安全',
  storage: '存储',
  user_access: '用户访问',
  userAccess: '用户访问',
  general: '通用',
};

const priorityLabels: Record<string, string> = {
  critical: '紧急',
  high: '高',
  medium: '中',
  low: '低',
};

const priorityColors: Record<string, string> = {
  critical: 'red',
  high: 'orange',
  medium: 'blue',
  low: 'green',
};

interface TicketBatchAIPanelProps {
  readonly selectedTickets: Ticket[];
  /** 任何写回（采纳/分派）成功后触发列表刷新。 */
  readonly onApplied?: () => void;
}

export function TicketBatchAIPanel({ selectedTickets, onApplied }: TicketBatchAIPanelProps) {
  const canUseAI = useCan(['ai:read']);
  const canUpdate = useCan(['ticket:update']);
  const canAssign = useCan(['ticket:assign']);

  const [triage, setTriage] = useState<BatchTriageItem[] | null>(null);
  const [drafts, setDrafts] = useState<BatchReplyDraftItem[] | null>(null);
  const [busy, setBusy] = useState<'triage' | 'draft' | null>(null);
  const [applying, setApplying] = useState<number | null>(null);
  const [applyingAll, setApplyingAll] = useState(false);

  const ticketIds = selectedTickets.map(ticket => ticket.id);

  const runTriage = useCallback(async () => {
    if (ticketIds.length === 0) return;
    if (ticketIds.length > BATCH_TRIAGE_MAX_TICKETS) {
      message.warning(`AI 建议分类单次最多处理 ${BATCH_TRIAGE_MAX_TICKETS} 张工单`);
      return;
    }
    setBusy('triage');
    const startedAt = Date.now();
    trackAssistantEvent('task_started', {
      pageKey: 'ticket.list',
      taskCardId: 'batch_triage',
      targetType: 'ticket',
      extra: { count: ticketIds.length },
    });
    try {
      const res = await aiBatchTriage(ticketIds);
      setTriage(res.items);
      trackAssistantEvent('task_completed', {
        pageKey: 'ticket.list',
        taskCardId: 'batch_triage',
        outcomeType: 'entity',
        durationMs: Date.now() - startedAt,
        extra: { count: ticketIds.length, processed: res.processed, degraded: res.degraded },
      });
    } catch {
      setTriage(null);
      trackAssistantEvent('task_failed', {
        pageKey: 'ticket.list',
        taskCardId: 'batch_triage',
        errorCode: 'batch_triage_failed',
      });
      message.error('AI 批量分诊失败，请稍后重试');
    } finally {
      setBusy(null);
    }
  }, [ticketIds]);

  const runDrafts = useCallback(async () => {
    if (ticketIds.length === 0) return;
    if (ticketIds.length > BATCH_REPLY_DRAFT_MAX_TICKETS) {
      message.warning(`批量草稿单次最多处理 ${BATCH_REPLY_DRAFT_MAX_TICKETS} 张工单`);
      return;
    }
    setBusy('draft');
    const startedAt = Date.now();
    trackAssistantEvent('task_started', {
      pageKey: 'ticket.list',
      taskCardId: 'batch_draft',
      targetType: 'ticket',
      extra: { count: ticketIds.length },
    });
    try {
      const res = await aiBatchReplyDrafts(ticketIds);
      setDrafts(res.items);
      trackAssistantEvent('task_completed', {
        pageKey: 'ticket.list',
        taskCardId: 'batch_draft',
        outcomeType: 'draft',
        durationMs: Date.now() - startedAt,
        extra: { count: ticketIds.length, succeeded: res.succeeded, degraded: res.degraded },
      });
    } catch {
      setDrafts(null);
      trackAssistantEvent('task_failed', {
        pageKey: 'ticket.list',
        taskCardId: 'batch_draft',
        errorCode: 'batch_draft_failed',
      });
      message.error('AI 批量草稿生成失败，请稍后重试');
    } finally {
      setBusy(null);
    }
  }, [ticketIds]);

  const acceptOne = useCallback(
    async (item: BatchTriageItem, opts?: { silent?: boolean }) => {
      if (item.status !== 'ok') return false;
      try {
        await TicketApi.updateTicket(item.ticketId, {
          category: item.category,
          priority: toTicketPriority(item.priority),
        });
        trackAssistantEvent('inline_applied', {
          pageKey: 'ticket.list',
          targetType: 'ticket',
          targetId: item.ticketId,
          taskCardId: 'batch_triage',
          extra: {
            action: opts?.silent ? 'accept_all' : 'accept',
            category: item.category ?? '',
            priority: item.priority ?? '',
          },
        });
        return true;
      } catch {
        return false;
      }
    },
    []
  );

  const handleAcceptOne = useCallback(
    async (item: BatchTriageItem) => {
      setApplying(item.ticketId);
      const ok = await acceptOne(item);
      setApplying(null);
      if (ok) {
        message.success(`已采纳 #${item.ticketId} 的分类建议`);
        onApplied?.();
      } else {
        message.error(`采纳 #${item.ticketId} 失败`);
      }
    },
    [acceptOne, onApplied]
  );

  const assembleAll = useCallback(
    async (items: BatchTriageItem[]) => {
      const targets = items.filter(item => item.status === 'ok');
      if (targets.length === 0) return;
      setApplyingAll(true);
      let succeeded = 0;
      for (const item of targets) {
        // 逐行串行写回：单行失败不阻塞其余，避免并发写触发版本冲突。
        const ok = await acceptOne(item, { silent: true });
        if (ok) succeeded++;
      }
      setApplyingAll(false);
      if (succeeded > 0) {
        message.success(`已采纳 ${succeeded} 条分类建议${succeeded < targets.length ? `（${targets.length - succeeded} 条失败）` : ''}`);
        onApplied?.();
      } else {
        message.error('批量采纳失败，请稍后重试');
      }
    },
    [acceptOne, onApplied]
  );

  const assignOne = useCallback(async (item: BatchTriageItem) => {
    if (!item.assigneeId || item.status !== 'ok') return false;
    try {
      await TicketApi.assignTicket(item.ticketId, { assigneeId: item.assigneeId });
      trackAssistantEvent('inline_applied', {
        pageKey: 'ticket.list',
        targetType: 'ticket',
        targetId: item.ticketId,
        taskCardId: 'batch_triage',
        extra: { action: 'assign', assigneeId: item.assigneeId },
      });
      return true;
    } catch {
      return false;
    }
  }, []);

  const handleAssignOne = useCallback(
    async (item: BatchTriageItem) => {
      setApplying(item.ticketId);
      const ok = await assignOne(item);
      setApplying(null);
      if (ok) {
        message.success(`已按建议分派 #${item.ticketId}`);
        onApplied?.();
      } else {
        message.error(`分派 #${item.ticketId} 失败`);
      }
    },
    [assignOne, onApplied]
  );

  const handleAssignAll = useCallback(
    async (items: BatchTriageItem[]) => {
      const targets = items.filter(item => item.status === 'ok' && Boolean(item.assigneeId));
      if (targets.length === 0) return;
      setApplyingAll(true);
      let succeeded = 0;
      for (const item of targets) {
        const ok = await assignOne(item);
        if (ok) succeeded++;
      }
      setApplyingAll(false);
      if (succeeded > 0) {
        message.success(`已按建议分派 ${succeeded} 张工单${succeeded < targets.length ? `（${targets.length - succeeded} 张失败）` : ''}`);
        onApplied?.();
      } else {
        message.error('批量分派失败，请稍后重试');
      }
    },
    [assignOne, onApplied]
  );

  const copyDraft = useCallback(async (item: BatchReplyDraftItem) => {
    const text = item.draft ?? '';
    try {
      await navigator.clipboard?.writeText(text);
    } catch {
      // 剪贴板不可用（非 HTTPS/权限拒绝）时仍给出提示，用户可手动选择文本。
    }
    trackAssistantEvent('inline_applied', {
      pageKey: 'ticket.list',
      targetType: 'ticket',
      targetId: item.ticketId,
      taskCardId: 'batch_draft',
      extra: { action: 'copy', chars: text.length },
    });
    message.success(`已复制 #${item.ticketId} 草稿`);
  }, []);

  const clearResults = useCallback(() => {
    setTriage(null);
    setDrafts(null);
  }, []);

  if (!canUseAI || selectedTickets.length === 0) return null;

  const triageOK = triage?.filter(item => item.status === 'ok') ?? [];
  const draftOK = drafts?.filter(item => item.status === 'ok') ?? [];

  return (
    <Card
      size='small'
      className='rounded-lg shadow-sm'
      data-testid='ticket-batch-ai-panel'
      title={
        <Space>
          <Sparkles size={16} />
          <span>AI 批量助手</span>
          <Text type='secondary' className='text-xs'>
            已选 {selectedTickets.length} 条
          </Text>
        </Space>
      }
    >
      <Space wrap>
        <Button
          onClick={() => void runTriage()}
          loading={busy === 'triage'}
          disabled={busy !== null}
          data-testid='batch-ai-triage'
        >
          AI 建议分类
        </Button>
        <Button
          onClick={() => void runDrafts()}
          loading={busy === 'draft'}
          disabled={busy !== null}
          data-testid='batch-ai-drafts'
        >
          批量草稿
        </Button>
        {triage && canUpdate && triageOK.length > 0 && (
          <Button
            type='primary'
            ghost
            onClick={() => void assembleAll(triage)}
            loading={applyingAll}
            data-testid='batch-ai-accept-all'
          >
            全部采纳分类
          </Button>
        )}
        {triage && canAssign && triageOK.some(item => item.assigneeId) && (
          <Button
            onClick={() => void handleAssignAll(triage)}
            loading={applyingAll}
            data-testid='batch-ai-assign-all'
          >
            按建议分派
          </Button>
        )}
        {(triage || drafts) && (
          <Button type='link' onClick={clearResults} data-testid='batch-ai-clear'>
            清除结果
          </Button>
        )}
      </Space>
      <Paragraph type='secondary' className='!mb-0 mt-2 text-xs'>
        仅生成建议与草稿；采纳 / 分派需你确认后才写回，草稿不会自动发送。
      </Paragraph>

      {triage && (
        <div className='mt-3' data-testid='batch-ai-triage-result'>
          <Alert
            type={triage.some(item => item.status === 'not_found') ? 'warning' : 'success'}
            showIcon
            title={`已生成 ${triageOK.length} 条建议${
              triage.length > triageOK.length ? `（${triage.length - triageOK.length} 条不可用）` : ''
            }`}
          />
          {triage.map(item => (
            <div
              key={item.ticketId}
              className='mt-2 flex flex-wrap items-center gap-2 rounded border border-gray-200 p-2'
              data-testid={`batch-ai-triage-row-${item.ticketId}`}
            >
              <Text strong>#{item.ticketId}</Text>
              {item.status === 'ok' ? (
                <>
                  <Tag>{categoryLabels[item.category ?? ''] ?? item.category ?? '未分类'}</Tag>
                  <Tag color={priorityColors[item.priority ?? ''] ?? 'default'}>
                    优先级 {priorityLabels[item.priority ?? ''] ?? item.priority}
                  </Tag>
                  {item.assigneeId ? <Tag icon={<UserCheck size={12} />}>建议处理人 #{item.assigneeId}</Tag> : null}
                  <Text type='secondary' className='text-xs'>
                    置信度 {Math.round((item.confidence ?? 0) * 100)}%
                  </Text>
                  <Space className='ml-auto'>
                    {canUpdate && (
                      <Button
                        size='small'
                        loading={applying === item.ticketId}
                        onClick={() => void handleAcceptOne(item)}
                        data-testid={`batch-ai-accept-${item.ticketId}`}
                      >
                        采纳
                      </Button>
                    )}
                    {canAssign && item.assigneeId ? (
                      <Button
                        size='small'
                        onClick={() => void handleAssignOne(item)}
                        data-testid={`batch-ai-assign-${item.ticketId}`}
                      >
                        分派
                      </Button>
                    ) : null}
                  </Space>
                </>
              ) : (
                <Text type='secondary'>不可用（工单不存在或无权访问）</Text>
              )}
            </div>
          ))}
        </div>
      )}

      {drafts && (
        <div className='mt-3' data-testid='batch-ai-draft-result'>
          <Alert
            type={draftOK.length > 0 ? 'success' : 'warning'}
            showIcon
            title={`已生成 ${draftOK.length} 份草稿${
              drafts.length > draftOK.length ? `（${drafts.length - draftOK.length} 条失败/不可用）` : ''
            }`}
          />
          {drafts.map(item => (
            <div
              key={item.ticketId}
              className='mt-2 rounded border border-gray-200 p-2'
              data-testid={`batch-ai-draft-row-${item.ticketId}`}
            >
              <div className='flex items-center gap-2'>
                <FileText size={14} />
                <Text strong>#{item.ticketId}</Text>
                {item.status === 'ok' ? (
                  <Button
                    size='small'
                    className='ml-auto'
                    icon={<ClipboardCopy size={14} />}
                    onClick={() => void copyDraft(item)}
                    data-testid={`batch-ai-copy-${item.ticketId}`}
                  >
                    复制草稿
                  </Button>
                ) : (
                  <Text type='secondary' className='ml-auto text-xs'>
                    {item.status === 'not_found' ? '不可用（工单不存在或无权访问）' : item.reason || '生成失败'}
                  </Text>
                )}
              </div>
              {item.status === 'ok' && (
                <div
                  className='mt-1 max-h-24 overflow-auto whitespace-pre-wrap text-sm text-gray-700'
                  data-testid={`batch-ai-draft-text-${item.ticketId}`}
                >
                  {item.draft}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

export default TicketBatchAIPanel;