/**
 * V2-02 异步任务 store 测试：状态合并（未读语义）、埋点（task_started/completed/failed）、
 * 容量上限、发起/轮询失败不静默。
 */
import { aiGetAsyncAnswer, aiStartAsyncAnswer, type AIAsyncAnswerTask } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

import {
  ASSISTANT_ASYNC_MAX_TASKS,
  countRunningTasks,
  countUnreadTasks,
  isTaskUnread,
  mergeServerTask,
  pollAssistantAsyncAnswer,
  startAssistantAsyncAnswer,
  useAssistantAsyncStore,
} from '../assistant-async-store';

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiStartAsyncAnswer: jest.fn(),
  aiGetAsyncAnswer: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedStart = aiStartAsyncAnswer as jest.MockedFunction<typeof aiStartAsyncAnswer>;
const mockedGet = aiGetAsyncAnswer as jest.MockedFunction<typeof aiGetAsyncAnswer>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const fast = {
  source: 'help_index',
  items: [{ title: '网络故障排查', snippet: '检查 DNS 与网关' }],
};

const serverTask = (over: Partial<AIAsyncAnswerTask> = {}): AIAsyncAnswerTask => ({
  taskId: 'aa_1',
  status: 'running',
  prompt: '连不上网怎么办',
  pageKey: 'ticket.detail',
  createdAt: '2026-10-18T10:00:00.000Z',
  fast,
  ...over,
});

beforeEach(() => {
  jest.clearAllMocks();
  useAssistantAsyncStore.getState().reset();
});

describe('mergeServerTask（V2-02 未读语义）', () => {
  it('running → 已读；首次收口 → 未读；重复轮询保留原已读标记', () => {
    const running = mergeServerTask(undefined, serverTask());
    expect(running.seen).toBe(true);
    expect(isTaskUnread(running)).toBe(false);

    const done = serverTask({ status: 'done', finishedAt: '2026-10-18T10:00:30.000Z', refined: { model: 'm1', answer: '完整回答' } });
    const firstFinish = mergeServerTask(running, done);
    expect(firstFinish.seen).toBe(false);
    expect(isTaskUnread(firstFinish)).toBe(true);
    expect(firstFinish.refined?.answer).toBe('完整回答');
    expect(firstFinish.finishedAt).toBe(Date.parse('2026-10-18T10:00:30.000Z'));

    const seenTask = { ...firstFinish, seen: true };
    const rePoll = mergeServerTask(seenTask, done);
    expect(rePoll.seen).toBe(true);
  });

  it('degraded（未配置模型）也计为可读回执，不是错误', () => {
    const degraded = mergeServerTask(undefined, serverTask({ status: 'degraded', reason: 'generator_unavailable', finishedAt: '2026-10-18T10:00:01.000Z' }));
    expect(degraded.seen).toBe(false);
    expect(isTaskUnread(degraded)).toBe(true);
  });
});

describe('useAssistantAsyncStore', () => {
  it('start 入表并上报 task_started；容量上限只保留最新 10 条', () => {
    for (let i = 0; i < ASSISTANT_ASYNC_MAX_TASKS + 2; i += 1) {
      useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask({ taskId: `aa_${i}` })));
    }
    const tasks = useAssistantAsyncStore.getState().tasks;
    expect(tasks).toHaveLength(ASSISTANT_ASYNC_MAX_TASKS);
    expect(tasks[0].taskId).toBe(`aa_${ASSISTANT_ASYNC_MAX_TASKS + 1}`);
    expect(mockedTrack).toHaveBeenCalledWith('task_started', { taskCardId: 'async_answer', pageKey: 'ticket.detail' });
  });

  it('settle：running → done 上报 task_completed（含耗时）；running → failed 上报 task_failed', () => {
    const running = mergeServerTask(undefined, serverTask({ taskId: 'aa_done' }));
    useAssistantAsyncStore.getState().start(running);
    useAssistantAsyncStore.getState().settle(
      serverTask({
        taskId: 'aa_done',
        status: 'done',
        finishedAt: '2026-10-18T10:00:30.000Z',
        refined: { model: 'gpt-4o', answer: '完整回答' },
      })
    );
    expect(mockedTrack).toHaveBeenCalledWith('task_completed', {
      taskCardId: 'async_answer',
      pageKey: 'ticket.detail',
      durationMs: 30000,
      model: 'gpt-4o',
    });

    const running2 = mergeServerTask(undefined, serverTask({ taskId: 'aa_fail' }));
    useAssistantAsyncStore.getState().start(running2);
    useAssistantAsyncStore.getState().settle(serverTask({ taskId: 'aa_fail', status: 'failed', reason: 'timeout', finishedAt: '2026-10-18T10:01:00.000Z' }));
    expect(mockedTrack).toHaveBeenCalledWith('task_failed', {
      taskCardId: 'async_answer',
      pageKey: 'ticket.detail',
      durationMs: 60000,
      errorCode: 'timeout',
    });
  });

  it('markSeen / markAllSeen / remove 驱动计数（运行中 + 未读）', () => {
    useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask({ taskId: 'aa_run' })));
    useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask({ taskId: 'aa_done' })));
    useAssistantAsyncStore.getState().settle(serverTask({ taskId: 'aa_done', status: 'done', finishedAt: '2026-10-18T10:00:30.000Z', refined: { answer: 'x' } }));

    let tasks = useAssistantAsyncStore.getState().tasks;
    expect(countRunningTasks(tasks)).toBe(1);
    expect(countUnreadTasks(tasks)).toBe(1);

    useAssistantAsyncStore.getState().markSeen('aa_done');
    tasks = useAssistantAsyncStore.getState().tasks;
    expect(countUnreadTasks(tasks)).toBe(0);

    useAssistantAsyncStore.getState().remove('aa_run');
    tasks = useAssistantAsyncStore.getState().tasks;
    expect(tasks.map(task => task.taskId)).toEqual(['aa_done']);

    useAssistantAsyncStore.getState().markAllSeen();
    expect(countUnreadTasks(useAssistantAsyncStore.getState().tasks)).toBe(0);
    useAssistantAsyncStore.getState().reset();
    expect(useAssistantAsyncStore.getState().tasks).toEqual([]);
  });
});

describe('startAssistantAsyncAnswer / pollAssistantAsyncAnswer', () => {
  it('发起成功入表；发起失败返回 null 且不入表（fail-visible）', async () => {
    mockedStart.mockResolvedValueOnce(serverTask({ taskId: 'aa_ok' }));
    const task = await startAssistantAsyncAnswer({ prompt: '连不上网', pageKey: 'ticket.detail', route: '/tickets/1' });
    expect(task?.taskId).toBe('aa_ok');
    expect(task?.route).toBe('/tickets/1');
    expect(useAssistantAsyncStore.getState().tasks[0].taskId).toBe('aa_ok');

    mockedStart.mockRejectedValueOnce(new Error('503'));
    const failed = await startAssistantAsyncAnswer({ prompt: 'x' });
    expect(failed).toBeNull();
    expect(useAssistantAsyncStore.getState().tasks).toHaveLength(1);
  });

  it('轮询成功合并精算结果；轮询失败保留现状（不虚假置错）', async () => {
    useAssistantAsyncStore.getState().start(mergeServerTask(undefined, serverTask({ taskId: 'aa_poll' })));
    mockedGet.mockResolvedValueOnce(
      serverTask({ taskId: 'aa_poll', status: 'done', finishedAt: '2026-10-18T10:00:20.000Z', refined: { model: 'm2', answer: '精算完成' } })
    );
    await pollAssistantAsyncAnswer('aa_poll');
    const merged = useAssistantAsyncStore.getState().tasks[0];
    expect(merged.status).toBe('done');
    expect(merged.refined?.answer).toBe('精算完成');
    expect(merged.seen).toBe(false);

    mockedGet.mockRejectedValueOnce(new Error('404'));
    await pollAssistantAsyncAnswer('aa_poll');
    expect(useAssistantAsyncStore.getState().tasks[0].refined?.answer).toBe('精算完成');
  });
});