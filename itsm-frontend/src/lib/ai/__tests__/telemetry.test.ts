/**
 * O0-01 埋点 SDK 契约与行为测试。
 *
 * 覆盖验收项：
 * - 契约单测：事件名与方案 §13.2 字典逐项一致；
 * - 批量 / 采样 / 失败静默：埋点绝不阻塞交互；
 * - 脱敏断言：敏感 key 哈希、结构化字段透传、长文本截断；
 * - 合规三档（on / anonymous / off）与持久化、缓冲上限、默认传输契约。
 */
import {
  ASSISTANT_EVENT_NAMES,
  TELEMETRY_ENDPOINT,
  TELEMETRY_MODE_STORAGE_KEY,
  TELEMETRY_SESSION_STORAGE_KEY,
  TelemetryClient,
  createHttpTransport,
  getTelemetryMode,
  hashTelemetryText,
  isAssistantEventName,
  setTelemetryMode,
  type AssistantEventEnvelope,
  type AssistantEventName,
  type TelemetryStorage,
} from '@/lib/ai/telemetry';

jest.mock('@/lib/security', () => ({
  security: { csrf: { getToken: jest.fn().mockResolvedValue(null) } },
}));

const NOW = 1_700_000_000_000;
const now = () => NOW;

/** 方案 §13.2 事件字典（逐项冻结，改字典必须同步改这里）。 */
const EXPECTED_EVENT_NAMES = [
  'assistant_opened',
  'assistant_closed',
  'context_loaded',
  'context_degraded',
  'suggestion_shown',
  'suggestion_clicked',
  'suggestion_ignored',
  'task_started',
  'task_completed',
  'task_failed',
  'message_sent',
  'first_token',
  'stream_end',
  'message_feedback',
  'tool_call',
  'confirmation_shown',
  'confirmation_accepted',
  'confirmation_rejected',
  'approval_pending',
  'inline_preview',
  'inline_applied',
  'inline_undone',
  'help_opened',
  'help_gap',
  'doc_draft_created',
  'doc_draft_published',
  'doc_feedback',
  'error_shown',
  'error_recovered',
  'handoff',
  'proactive_shown',
  'proactive_clicked',
  'proactive_dismissed',
  'self_service_answered',
  'self_service_resolved',
  'self_service_ticket_created',
  'ai_create_field_corrected',
  // V2-01 个性化记忆：保存/删除常用语与模板、最近处理记录、用户开关。
  'memory_updated',
  'telemetry_disabled',
] as const;

function createMemoryStorage(
  seed: Record<string, string> = {}
): TelemetryStorage & { dump: () => Record<string, string> } {
  const map = new Map(Object.entries(seed));
  return {
    getItem: key => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
    removeItem: key => {
      map.delete(key);
    },
    dump: () => Object.fromEntries(map),
  };
}

function createRecorder() {
  const batches: AssistantEventEnvelope[][] = [];
  const send = jest.fn(async (events: AssistantEventEnvelope[]) => {
    batches.push(events);
  });
  return { send, batches, transport: { send } };
}

beforeEach(() => {
  try {
    window.localStorage.clear();
  } catch {
    /* 隐私模式下不可用 */
  }
});

describe('telemetry 事件字典契约（§13.2）', () => {
  it('事件名与方案字典逐项一致', () => {
    expect([...ASSISTANT_EVENT_NAMES].sort()).toEqual([...EXPECTED_EVENT_NAMES].sort());
  });

  it('isAssistantEventName 只接受字典内事件', () => {
    expect(isAssistantEventName('task_started')).toBe(true);
    expect(isAssistantEventName('telemetry_disabled')).toBe(true);
    expect(isAssistantEventName('task_unknown')).toBe(false);
    expect(isAssistantEventName(123)).toBe(false);
    expect(isAssistantEventName(undefined)).toBe(false);
  });
});

describe('TelemetryClient 行为', () => {
  it('达到 batchSize 触发批量上报，信封含 name/ts/sessionId 并持久化会话 id', async () => {
    const { send, batches, transport } = createRecorder();
    const storage = createMemoryStorage();
    const client = new TelemetryClient({
      transport,
      batchSize: 2,
      storage,
      now,
      sessionIdFactory: () => 'sid-1',
    });

    client.track('assistant_opened', { pageKey: 'ticket.list', source: 'fab' });
    expect(send).not.toHaveBeenCalled();

    client.track('suggestion_shown', { cardId: 'c1', cardKind: 'task', rank: 1 });
    await client.flush();

    expect(send).toHaveBeenCalledTimes(1);
    expect(batches[0]).toHaveLength(2);
    expect(batches[0][0]).toMatchObject({
      name: 'assistant_opened',
      ts: NOW,
      pageKey: 'ticket.list',
      source: 'fab',
      sessionId: 'sid-1',
    });
    expect(batches[0][1]).toMatchObject({ name: 'suggestion_shown', cardId: 'c1', cardKind: 'task', rank: 1 });
    expect(storage.dump()[TELEMETRY_SESSION_STORAGE_KEY]).toBe('sid-1');
    client.dispose();
  });

  it('定时器到点自动 flush（批量未满也上报）', async () => {
    jest.useFakeTimers();
    try {
      const { send, transport } = createRecorder();
      const client = new TelemetryClient({
        transport,
        batchSize: 100,
        flushIntervalMs: 5000,
        storage: null,
        now,
      });
      client.track('message_sent', { latencyMs: 120 });
      expect(send).not.toHaveBeenCalled();

      await jest.advanceTimersByTimeAsync(5000);
      expect(send).toHaveBeenCalledTimes(1);
      client.dispose();
    } finally {
      jest.useRealTimers();
    }
  });

  it('采样未命中不产生事件，命中则正常上报', async () => {
    const { send, transport } = createRecorder();
    const missed = new TelemetryClient({
      transport,
      batchSize: 1,
      sampleRate: 0.5,
      random: () => 0.9,
      storage: null,
      now,
    });
    missed.track('message_sent');
    expect(send).not.toHaveBeenCalled();
    expect(missed.pendingCount).toBe(0);

    const hit = new TelemetryClient({
      transport,
      batchSize: 1,
      sampleRate: 0.5,
      random: () => 0.1,
      storage: null,
      now,
    });
    hit.track('message_sent');
    await hit.flush();
    expect(send).toHaveBeenCalledTimes(1);
    missed.dispose();
    hit.dispose();
  });

  it('extra 脱敏：敏感 key 哈希、数字/布尔保留、长文本截断、复杂结构哈希', async () => {
    const { batches, transport } = createRecorder();
    const client = new TelemetryClient({ transport, batchSize: 1, storage: null, now });

    client.track('help_gap', {
      pageKey: 'knowledge.list',
      extra: {
        query: '数据库连不上',
        note: 'x'.repeat(300),
        code: 200,
        ok: true,
        path: 'y'.repeat(300),
        payload: { nested: true },
      },
    });
    await client.flush();

    const extra = batches[0][0].extra ?? {};
    expect(extra.query).toBe(`h:${hashTelemetryText('数据库连不上')}`);
    expect(extra.note).toBe(`h:${hashTelemetryText('x'.repeat(300))}`);
    expect(extra.code).toBe(200);
    expect(extra.ok).toBe(true);
    expect(extra.path).toBe('y'.repeat(120));
    expect(String(extra.payload).startsWith('h:')).toBe(true);
    // 顶层结构化字段（pageKey 等）不脱敏
    expect(batches[0][0].pageKey).toBe('knowledge.list');
    client.dispose();
  });

  it('transport 失败静默：不抛出、回调 onError、后续事件仍可上报', async () => {
    const onError = jest.fn();
    const send = jest
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(undefined);
    const client = new TelemetryClient({
      transport: { send },
      batchSize: 1,
      storage: null,
      onError,
      now,
    });

    expect(() => client.track('task_started', { taskCardId: 't1' })).not.toThrow();
    await expect(client.flush()).resolves.toBeUndefined();
    expect(onError).toHaveBeenCalledWith(expect.any(Error));

    client.track('task_completed', { taskCardId: 't1' });
    await client.flush();
    expect(send).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenCalledTimes(1);
    client.dispose();
  });

  it('关闭（off）：先留痕 telemetry_disabled 再停采，偏好持久化', async () => {
    const { send, batches, transport } = createRecorder();
    const storage = createMemoryStorage();
    const client = new TelemetryClient({ transport, batchSize: 50, storage, now });

    client.setMode('off');
    client.track('task_started'); // 关闭后丢弃
    await client.flush();

    expect(send).toHaveBeenCalledTimes(1);
    expect(batches[0]).toHaveLength(1);
    expect(batches[0][0].name).toBe('telemetry_disabled');
    expect(batches[0][0].extra?.previousMode).toBe('on');
    expect(client.currentMode).toBe('off');
    expect(storage.dump()[TELEMETRY_MODE_STORAGE_KEY]).toBe('off');

    client.track('message_sent');
    expect(client.pendingCount).toBe(0);
    expect(send).toHaveBeenCalledTimes(1);

    // 重新开启后恢复采集
    client.setMode('on');
    client.track('message_sent');
    expect(client.pendingCount).toBe(1);
    client.dispose();
  });

  it('anonymous 模式剥离可关联字段，保留结构化字段', async () => {
    const { batches, transport } = createRecorder();
    const client = new TelemetryClient({ transport, batchSize: 1, storage: null, mode: 'anonymous', now });

    client.track('tool_call', {
      toolName: 'create_ticket',
      conversationId: 5,
      runId: 'r-1',
      targetId: 7,
      pageKey: 'ticket.detail',
    });
    await client.flush();

    const envelope = batches[0][0];
    expect(envelope.sessionId).toBeUndefined();
    expect(envelope.conversationId).toBeUndefined();
    expect(envelope.runId).toBeUndefined();
    expect(envelope.targetId).toBeUndefined();
    expect(envelope.toolName).toBe('create_ticket');
    expect(envelope.pageKey).toBe('ticket.detail');
    client.dispose();
  });

  it('未知名运行时丢弃（不抛错、不入队）', () => {
    const { send, transport } = createRecorder();
    const client = new TelemetryClient({ transport, batchSize: 1, storage: null, now });
    expect(() => client.track('unknown_event' as unknown as AssistantEventName)).not.toThrow();
    expect(send).not.toHaveBeenCalled();
    expect(client.pendingCount).toBe(0);
    client.dispose();
  });

  it('缓冲上限生效：超出丢弃最旧事件', async () => {
    const { batches, transport } = createRecorder();
    const client = new TelemetryClient({ transport, batchSize: 100, maxQueueSize: 3, storage: null, now });
    for (const name of [
      'assistant_opened',
      'assistant_closed',
      'context_loaded',
      'context_degraded',
      'suggestion_shown',
    ] as const) {
      client.track(name);
    }
    await client.flush();
    expect(batches[0].map(event => event.name)).toEqual([
      'context_loaded',
      'context_degraded',
      'suggestion_shown',
    ]);
    client.dispose();
  });

  it('模式持久化：新客户端读取存储的模式', () => {
    const storage = createMemoryStorage({ [TELEMETRY_MODE_STORAGE_KEY]: 'anonymous' });
    const client = new TelemetryClient({ transport: createRecorder().transport, storage, now });
    expect(client.currentMode).toBe('anonymous');
    client.setMode('on');
    expect(storage.dump()[TELEMETRY_MODE_STORAGE_KEY]).toBe('on');
    client.dispose();
  });
});

describe('应用级开关入口（单例）', () => {
  it('setTelemetryMode / getTelemetryMode 委托单例', () => {
    const originalFetch = global.fetch;
    global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200 }) as unknown as typeof fetch;
    try {
      setTelemetryMode('off');
      expect(getTelemetryMode()).toBe('off');
      setTelemetryMode('on');
      expect(getTelemetryMode()).toBe('on');
    } finally {
      global.fetch = originalFetch;
    }
  });
});

describe('createHttpTransport 默认传输契约', () => {
  it('POST 到 telemetry 端点，携带 CSRF、credentials 与 keepalive', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: true, status: 200 });
    const originalFetch = global.fetch;
    global.fetch = fetchMock as unknown as typeof fetch;
    try {
      const transport = createHttpTransport({ csrfTokenProvider: async () => 'csrf-1' });
      await transport.send([{ name: 'assistant_opened', ts: 1 }]);

      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
      expect(url).toBe(TELEMETRY_ENDPOINT);
      expect(init.method).toBe('POST');
      expect(init.credentials).toBe('include');
      expect(init.keepalive).toBe(true);
      expect((init.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-1');
      expect(JSON.parse(String(init.body))).toEqual({ events: [{ name: 'assistant_opened', ts: 1 }] });
    } finally {
      global.fetch = originalFetch;
    }
  });

  it('HTTP 非 2xx 抛错（由客户端侧静默丢弃）', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 503 });
    const originalFetch = global.fetch;
    global.fetch = fetchMock as unknown as typeof fetch;
    try {
      const transport = createHttpTransport({ csrfTokenProvider: async () => null });
      await expect(transport.send([{ name: 'task_failed', ts: 1 }])).rejects.toThrow('HTTP 503');
    } finally {
      global.fetch = originalFetch;
    }
  });
});