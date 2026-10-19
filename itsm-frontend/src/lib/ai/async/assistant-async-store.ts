/**
 * V2-02 异步任务体验：**先给可用答案，后台精算完成后通知**（方案 §11.4-4「先答后精 + 完成通知」）。
 *
 * 语义：
 * - 任务发起即返回快速参考（`fast`，帮助索引命中）→ 面板立刻可用（先给可用答案）；
 * - `status=running` 时前台展示"正在精算"，可继续做别的事；
 * - 收口（done/degraded/failed）后未读 → FAB 未读回执 + 面板回执卡（`seen` 标记）；
 * - `degraded` = 服务端未装配生成器（诚实降级，仅快速参考），不是错误。
 *
 * 边界：任务表为进程内（刷新即失，服务于"本次会话的后台续跑"）；完成通知的持久通道是
 * 服务端通知中心（`POST /ai/async/answers` 完成投递），此处只管前端回执与未读态。
 */
import { create } from 'zustand';

import {
  aiGetAsyncAnswer,
  aiStartAsyncAnswer,
  type AIAsyncAnswerFast,
  type AIAsyncAnswerRefined,
  type AIAsyncAnswerStatus,
  type AIAsyncAnswerTask,
} from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

/** 任务表容量上限（超出丢弃最旧的已收口任务）。 */
export const ASSISTANT_ASYNC_MAX_TASKS = 10;
/** 埋点用的任务卡标识（复用既有 task_started/completed/failed 事件，事件字典零变更）。 */
export const ASSISTANT_ASYNC_TASK_CARD_ID = 'async_answer';

export interface AssistantAsyncTask {
  taskId: string;
  prompt: string;
  pageKey?: string;
  route?: string;
  status: AIAsyncAnswerStatus;
  fast: AIAsyncAnswerFast;
  refined?: AIAsyncAnswerRefined;
  reason?: string;
  createdAt: number;
  finishedAt?: number;
  /** 已读标记：收口且未读 → FAB 未读回执 / 面板高亮回执卡。 */
  seen: boolean;
}

export function isTaskRunning(task: AssistantAsyncTask): boolean {
  return task.status === 'running';
}

/** 未读回执：已收口（成功/降级/失败）且用户尚未查看结果。 */
export function isTaskUnread(task: AssistantAsyncTask): boolean {
  return task.status !== 'running' && !task.seen;
}

export function countRunningTasks(tasks: AssistantAsyncTask[]): number {
  return tasks.filter(isTaskRunning).length;
}

export function countUnreadTasks(tasks: AssistantAsyncTask[]): number {
  return tasks.filter(isTaskUnread).length;
}

function parseTs(value?: string): number | undefined {
  if (!value) return undefined;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? undefined : parsed;
}

/**
 * 合并服务端快照（唯一写入口径）。
 * `seen`：running → 已读（无结果可看）；首次收口 → 未读；已收口后重复轮询 → 保留原标记。
 */
export function mergeServerTask(
  existing: AssistantAsyncTask | undefined,
  server: AIAsyncAnswerTask
): AssistantAsyncTask {
  const finished = server.status !== 'running';
  const wasFinished = existing ? existing.status !== 'running' : false;
  return {
    taskId: server.taskId,
    prompt: server.prompt,
    pageKey: server.pageKey,
    route: existing?.route,
    status: server.status,
    fast: server.fast ?? { source: 'none', items: [] },
    refined: server.refined,
    reason: server.reason,
    createdAt: existing?.createdAt ?? parseTs(server.createdAt) ?? Date.now(),
    finishedAt: parseTs(server.finishedAt),
    seen: finished ? (wasFinished && existing ? existing.seen : false) : true,
  };
}

interface AssistantAsyncState {
  tasks: AssistantAsyncTask[];
  /** 新任务入表（发起成功即调用；running 状态已读）。 */
  start: (task: AssistantAsyncTask) => void;
  /** 服务端快照合并（轮询/启动响应）；running → 收口 时上报完成/失败埋点。 */
  settle: (server: AIAsyncAnswerTask) => void;
  markSeen: (taskId: string) => void;
  markAllSeen: () => void;
  remove: (taskId: string) => void;
  reset: () => void;
}

function trimTasks(tasks: AssistantAsyncTask[]): AssistantAsyncTask[] {
  if (tasks.length <= ASSISTANT_ASYNC_MAX_TASKS) return tasks;
  const running = tasks.filter(isTaskRunning);
  const settled = tasks.filter(task => !isTaskRunning(task)).slice(0, Math.max(0, ASSISTANT_ASYNC_MAX_TASKS - running.length));
  return [...running, ...settled].slice(0, ASSISTANT_ASYNC_MAX_TASKS);
}

export const useAssistantAsyncStore = create<AssistantAsyncState>((set, get) => ({
  tasks: [],

  start: task => {
    trackAssistantEvent('task_started', {
      taskCardId: ASSISTANT_ASYNC_TASK_CARD_ID,
      pageKey: task.pageKey,
    });
    set(state => ({ tasks: trimTasks([task, ...state.tasks.filter(item => item.taskId !== task.taskId)]) }));
  },

  settle: server => {
    const existing = get().tasks.find(task => task.taskId === server.taskId);
    // 已移除/未发起的任务忽略：轮询不复活已移除回执（用户移除是终态）。
    if (!existing) return;
    const merged = mergeServerTask(existing, server);
    if (isTaskRunning(existing) && !isTaskRunning(merged)) {
      const durationMs = merged.finishedAt ? Math.max(0, merged.finishedAt - merged.createdAt) : undefined;
      if (merged.status === 'done') {
        trackAssistantEvent('task_completed', {
          taskCardId: ASSISTANT_ASYNC_TASK_CARD_ID,
          pageKey: merged.pageKey,
          durationMs,
          model: merged.refined?.model,
        });
      } else {
        trackAssistantEvent('task_failed', {
          taskCardId: ASSISTANT_ASYNC_TASK_CARD_ID,
          pageKey: merged.pageKey,
          durationMs,
          errorCode: merged.reason ?? merged.status,
        });
      }
    }
    set(state => ({
      tasks: trimTasks(
        state.tasks.some(task => task.taskId === merged.taskId)
          ? state.tasks.map(task => (task.taskId === merged.taskId ? merged : task))
          : [merged, ...state.tasks]
      ),
    }));
  },

  markSeen: taskId =>
    set(state => ({
      tasks: state.tasks.map(task => (task.taskId === taskId ? { ...task, seen: true } : task)),
    })),

  markAllSeen: () => set(state => ({ tasks: state.tasks.map(task => (task.seen ? task : { ...task, seen: true })) })),

  remove: taskId => set(state => ({ tasks: state.tasks.filter(task => task.taskId !== taskId) })),

  reset: () => set({ tasks: [] }),
}));

/** 发起异步精算并入库；失败返回 null（调用方 fail-visible 提示，不静默假装已发起）。 */
export async function startAssistantAsyncAnswer(params: {
  prompt: string;
  pageKey?: string;
  route?: string;
}): Promise<AssistantAsyncTask | null> {
  try {
    const server = await aiStartAsyncAnswer(params);
    const task = mergeServerTask(undefined, server);
    task.route = params.route;
    useAssistantAsyncStore.getState().start(task);
    return task;
  } catch {
    return null;
  }
}

/** 轮询任务快照并合并（404/网络失败保留现状：不虚假置错，也不覆盖已有结果）。 */
export async function pollAssistantAsyncAnswer(taskId: string): Promise<void> {
  try {
    const server = await aiGetAsyncAnswer(taskId);
    useAssistantAsyncStore.getState().settle(server);
  } catch {
    // 静默：任务可能已过 TTL（服务端 404）；前端保留最后快照供回执展示。
  }
}