/**
 * V2-02 异步精算回执卡（Dock 面板内）：
 *
 * - running：先给可用答案——列出快速参考（帮助索引命中），并提示"完整回答正在后台精算"；
 * - 收口（done/degraded/failed）：展示精算回答或如实降级说明 + "完成回执"；
 * - 未读回执由 store 的 `seen` 驱动（FAB 状态点消费同一数据面）；面板打开即视为已读；
 * - 面板挂载期间按固定间隔轮询 running 任务（完成后清理定时器）。
 */
import React, { useEffect, useMemo } from 'react';
import { Button, Tag, Tooltip, Typography, theme } from 'antd';
import { Sparkles, X } from 'lucide-react';

import {
  isTaskRunning,
  isTaskUnread,
  pollAssistantAsyncAnswer,
  useAssistantAsyncStore,
  type AssistantAsyncTask,
} from '@/lib/ai/async/assistant-async-store';

export const ASSISTANT_ASYNC_CARD_TESTID = 'assistant-async-card';
export const ASSISTANT_ASYNC_FAST_TESTID = 'assistant-async-fast';
export const ASSISTANT_ASYNC_REFINED_TESTID = 'assistant-async-refined';
export const ASSISTANT_ASYNC_STATUS_TESTID = 'assistant-async-status';
export const ASSISTANT_ASYNC_DISMISS_TESTID = 'assistant-async-dismiss';
export const ASSISTANT_ASYNC_SEEN_TESTID = 'assistant-async-seen';

/** running 任务轮询间隔（完成后立即停止；与通知中心投递互为兜底）。 */
export const ASSISTANT_ASYNC_POLL_INTERVAL_MS = 3000;
/** 面板内最多同时展示的任务数（其余保留在 store，FAB 徽标仍计数）。 */
export const ASSISTANT_ASYNC_VISIBLE_LIMIT = 2;

/** 失败/降级原因 → 可读文案（诚实说明，不显示原始错误码给终端用户）。 */
export function asyncReasonText(reason?: string, status?: string): string {
  switch (reason) {
    case 'timeout':
      return '精算超时，快速参考仍可用';
    case 'provider_error':
      return '模型调用失败，快速参考仍可用';
    case 'generator_unavailable':
      return '当前未配置模型，仅提供快速参考';
    case 'not_found':
      return '任务已过期';
    default:
      return status === 'failed' ? '精算未完成，快速参考仍可用' : '';
  }
}

const statusLabel: Record<string, string> = {
  running: '精算中',
  done: '已完成',
  degraded: '仅快速参考',
  failed: '精算未完成',
};

const TaskRow: React.FC<{ task: AssistantAsyncTask }> = ({ task }) => {
  const { token } = theme.useToken();
  const remove = useAssistantAsyncStore(state => state.remove);
  const running = isTaskRunning(task);
  const reason = asyncReasonText(task.reason, task.status);

  return (
    <div
      data-testid={ASSISTANT_ASYNC_CARD_TESTID}
      data-task-status={task.status}
      style={{
        border: `1px solid ${token.colorBorderSecondary}`,
        borderRadius: 8,
        background: token.colorFillQuaternary,
        padding: '8px 10px',
        marginBottom: 8,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <Sparkles size={13} style={{ color: token.colorPrimary, flexShrink: 0 }} />
        <Typography.Text
          style={{ flex: 1, fontSize: 12 }}
          ellipsis={{ tooltip: task.prompt }}
        >
          {task.prompt}
        </Typography.Text>
        <Tag
          data-testid={ASSISTANT_ASYNC_STATUS_TESTID}
          color={running ? 'processing' : task.status === 'done' ? 'success' : 'default'}
          style={{ marginInlineEnd: 0, fontSize: 11, lineHeight: '18px' }}
        >
          {statusLabel[task.status] ?? task.status}
        </Tag>
        <Tooltip title="移除回执">
          <Button
            data-testid={ASSISTANT_ASYNC_DISMISS_TESTID}
            type="text"
            size="small"
            aria-label="移除回执"
            icon={<X size={12} />}
            onClick={() => remove(task.taskId)}
          />
        </Tooltip>
      </div>

      {task.fast.items.length > 0 && (
        <div data-testid={ASSISTANT_ASYNC_FAST_TESTID} style={{ marginTop: 6 }}>
          <div style={{ fontSize: 11, color: token.colorTextSecondary, marginBottom: 2 }}>
            {running ? '先看这些（快速参考）' : '快速参考'}
          </div>
          {task.fast.items.map((item, index) => (
            <div key={`${task.taskId}-${index}`} style={{ fontSize: 12, marginBottom: 2 }}>
              <Typography.Text strong style={{ fontSize: 12 }}>
                {item.title}
              </Typography.Text>
              {item.snippet ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {' '}
                  — {item.snippet}
                </Typography.Text>
              ) : null}
            </div>
          ))}
        </div>
      )}
      {task.fast.items.length === 0 && running && (
        <div style={{ fontSize: 11, color: token.colorTextSecondary, marginTop: 6 }}>
          暂无快速参考，完整回答正在后台精算…
        </div>
      )}

      {task.refined?.answer && (
        <div
          data-testid={ASSISTANT_ASYNC_REFINED_TESTID}
          style={{
            marginTop: 6,
            fontSize: 12,
            whiteSpace: 'pre-wrap',
            maxHeight: 160,
            overflowY: 'auto',
            borderTop: `1px dashed ${token.colorBorderSecondary}`,
            paddingTop: 6,
          }}
        >
          {task.refined.answer}
        </div>
      )}
      {!running && reason && (
        <div data-testid={ASSISTANT_ASYNC_SEEN_TESTID} style={{ fontSize: 11, color: token.colorTextTertiary, marginTop: 4 }}>
          {reason}
        </div>
      )}
    </div>
  );
};

const AssistantAsyncAnswerCard: React.FC = () => {
  const tasks = useAssistantAsyncStore(state => state.tasks);
  const markSeen = useAssistantAsyncStore(state => state.markSeen);

  const visible = useMemo(() => {
    const prioritized = tasks.filter((task, index) => index === 0 || isTaskRunning(task) || isTaskUnread(task));
    return prioritized.slice(0, ASSISTANT_ASYNC_VISIBLE_LIMIT);
  }, [tasks]);
  const runningCount = tasks.filter(isTaskRunning).length;

  // 面板打开且回执可见 → 视为已读（清 FAB 未读回执；任务本体保留到用户移除/容量淘汰）。
  useEffect(() => {
    visible
      .filter(task => !isTaskRunning(task) && !task.seen)
      .forEach(task => markSeen(task.taskId));
  }, [visible, markSeen]);

  // 挂载即在跑一次，随后按间隔轮询 running 任务；无 running → 零定时器零请求。
  useEffect(() => {
    if (runningCount === 0) return undefined;
    const pollRunning = () => {
      useAssistantAsyncStore
        .getState()
        .tasks.filter(isTaskRunning)
        .forEach(task => {
          void pollAssistantAsyncAnswer(task.taskId);
        });
    };
    pollRunning();
    const timer = window.setInterval(pollRunning, ASSISTANT_ASYNC_POLL_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [runningCount]);

  if (visible.length === 0) return null;

  return (
    <div data-testid="assistant-async-answers" style={{ padding: '0 12px' }}>
      {visible.map(task => (
        <TaskRow key={task.taskId} task={task} />
      ))}
    </div>
  );
};

export default AssistantAsyncAnswerCard;