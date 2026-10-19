/**
 * V2-01 个性化记忆 store：模式解析 + 三档数据面（on 服务端 / anonymous 本机 / off 禁用）。
 *
 * 关键负向断言：
 * - 埋点 off 或用户禁用 → **零请求**（能力面与偏好面都不触达）；
 * - 租户策略关闭 → 能力面一次判定后零偏好面读写；
 * - anonymous → 绝不调用 `updateUserPreferences`（不上传）；
 * - 未加载（idle）时 recordAction 不隐式发起请求。
 */
import {
  ASSISTANT_MEMORY_LOCAL_KEY,
  ASSISTANT_MEMORY_USER_DISABLED_KEY,
  deleteAssistantMemoryPhrase,
  isAssistantMemoryUserDisabled,
  recordAssistantMemoryAction,
  resolveAssistantMemoryMode,
  saveAssistantMemoryPhrase,
  useAssistantMemoryStore,
} from '../assistant-memory-store';
import { emptyAssistantMemory, normalizeAssistantMemory } from '../types';
import { getCapabilities } from '@/lib/api/capability-api';
import { getUserPreferences, updateUserPreferences } from '@/lib/api/user-preferences-api';
import { getTelemetryMode, trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/api/capability-api', () => ({
  __esModule: true,
  getCapabilities: jest.fn(),
}));
jest.mock('@/lib/api/user-preferences-api', () => ({
  __esModule: true,
  getUserPreferences: jest.fn(),
  updateUserPreferences: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  getTelemetryMode: jest.fn(() => 'on'),
  trackAssistantEvent: jest.fn(),
}));

const mockedCaps = getCapabilities as jest.MockedFunction<typeof getCapabilities>;
const mockedGetPrefs = getUserPreferences as jest.MockedFunction<typeof getUserPreferences>;
const mockedPutPrefs = updateUserPreferences as jest.MockedFunction<typeof updateUserPreferences>;
const mockedTelemetryMode = getTelemetryMode as jest.Mock;
const mockedTrack = trackAssistantEvent as jest.Mock;

const store = () => useAssistantMemoryStore.getState();

const seedLocal = (memory: unknown) => {
  window.localStorage.setItem(ASSISTANT_MEMORY_LOCAL_KEY, JSON.stringify(memory));
};

describe('resolveAssistantMemoryMode（V2-01 模式矩阵）', () => {
  it('三档与覆盖优先级', () => {
    expect(resolveAssistantMemoryMode({ telemetryMode: 'on', tenantEnabled: true, userDisabled: false })).toBe('server');
    expect(resolveAssistantMemoryMode({ telemetryMode: 'on', tenantEnabled: undefined, userDisabled: false })).toBe('server');
    expect(resolveAssistantMemoryMode({ telemetryMode: 'anonymous', tenantEnabled: true, userDisabled: false })).toBe('local');
    expect(resolveAssistantMemoryMode({ telemetryMode: 'off', tenantEnabled: true, userDisabled: false })).toBe('off');
    expect(resolveAssistantMemoryMode({ telemetryMode: 'on', tenantEnabled: false, userDisabled: false })).toBe('off');
    expect(resolveAssistantMemoryMode({ telemetryMode: 'on', tenantEnabled: true, userDisabled: true })).toBe('off');
  });
});

describe('useAssistantMemoryStore（V2-01 数据面）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    window.localStorage.clear();
    store().reset();
    mockedTelemetryMode.mockReturnValue('on');
    mockedCaps.mockResolvedValue({ items: [], acceptanceVersion: 'test' });
    mockedGetPrefs.mockResolvedValue({ preferences: {} });
    mockedPutPrefs.mockResolvedValue({ preferences: {} });
  });

  it('埋点 off → 禁用且零请求（能力面/偏好面都不触达）', async () => {
    mockedTelemetryMode.mockReturnValue('off');
    await store().load();
    expect(store().mode).toBe('off');
    expect(store().status).toBe('disabled');
    expect(mockedCaps).not.toHaveBeenCalled();
    expect(mockedGetPrefs).not.toHaveBeenCalled();
    expect(mockedPutPrefs).not.toHaveBeenCalled();
  });

  it('租户策略关闭 → 判定后零偏好面读写', async () => {
    mockedCaps.mockResolvedValue({
      items: [],
      acceptanceVersion: 'test',
      assistant: { memoryEnabled: false },
    });
    await store().load();
    expect(store().mode).toBe('off');
    expect(store().status).toBe('disabled');
    expect(mockedCaps).toHaveBeenCalledTimes(1);
    expect(mockedGetPrefs).not.toHaveBeenCalled();
  });

  it('anonymous → 仅本机：读本机缓存、保存不调用服务端', async () => {
    mockedTelemetryMode.mockReturnValue('anonymous');
    seedLocal({ version: 1, phrases: ['本机常用语'], templates: [], recent: [] });

    await store().load();
    expect(store().mode).toBe('local');
    expect(store().memory.phrases).toEqual(['本机常用语']);
    expect(mockedGetPrefs).not.toHaveBeenCalled();

    await saveAssistantMemoryPhrase('新增短语');
    expect(store().memory.phrases).toEqual(['新增短语', '本机常用语']);
    expect(mockedPutPrefs).not.toHaveBeenCalled();
    expect(normalizeAssistantMemory(JSON.parse(window.localStorage.getItem(ASSISTANT_MEMORY_LOCAL_KEY) || '{}')).phrases).toEqual([
      '新增短语',
      '本机常用语',
    ]);
  });

  it('server → 读取服务端并镜像本机；保存调用 updateUserPreferences 且埋点 memory_updated', async () => {
    mockedGetPrefs.mockResolvedValue({
      preferences: {
        assistantMemory: { version: 1, phrases: ['云端短语'], templates: [{ name: '模板A', body: '正文' }], recent: [] },
      },
    });
    await store().load();
    expect(store().mode).toBe('server');
    expect(store().memory.phrases).toEqual(['云端短语']);
    expect(normalizeAssistantMemory(JSON.parse(window.localStorage.getItem(ASSISTANT_MEMORY_LOCAL_KEY) || '{}')).phrases).toEqual([
      '云端短语',
    ]);

    await saveAssistantMemoryPhrase('新短语');
    expect(mockedPutPrefs).toHaveBeenCalledWith({
      assistantMemory: expect.objectContaining({ phrases: ['新短语', '云端短语'] }),
    });
    expect(mockedTrack).toHaveBeenCalledWith('memory_updated', expect.objectContaining({
      extra: expect.objectContaining({ op: 'phrase_saved' }),
    }));

    await deleteAssistantMemoryPhrase('新短语');
    expect(mockedPutPrefs).toHaveBeenLastCalledWith({
      assistantMemory: expect.objectContaining({ phrases: ['云端短语'] }),
    });
  });

  it('server 读取失败 → 回落本机缓存并如实置错（sync_failed）', async () => {
    seedLocal({ version: 1, phrases: ['缓存短语'], templates: [], recent: [] });
    mockedGetPrefs.mockRejectedValue(new Error('network down'));
    await store().load();
    expect(store().status).toBe('ready');
    expect(store().error).toBe('sync_failed');
    expect(store().memory.phrases).toEqual(['缓存短语']);
  });

  it('server 保存失败 → 本机兜底且置错（save_failed）', async () => {
    await store().load();
    mockedPutPrefs.mockRejectedValue(new Error('save down'));
    await saveAssistantMemoryPhrase('失败短语');
    expect(store().error).toBe('save_failed');
    expect(store().memory.phrases).toEqual(['失败短语']);
    expect(normalizeAssistantMemory(JSON.parse(window.localStorage.getItem(ASSISTANT_MEMORY_LOCAL_KEY) || '{}')).phrases).toEqual([
      '失败短语',
    ]);
  });

  it('recordAction：未加载（idle）时静默跳过、不发请求；就绪后记录元数据动作键', async () => {
    recordAssistantMemoryAction('inline_apply:triage_detail');
    expect(mockedPutPrefs).not.toHaveBeenCalled();

    await store().load();
    recordAssistantMemoryAction('inline_apply:triage_detail');
    expect(mockedPutPrefs).toHaveBeenCalledWith({
      assistantMemory: expect.objectContaining({
        recent: [expect.objectContaining({ key: 'inline_apply:triage_detail' })],
      }),
    });
    // 非法键（自由文本）绝不写入。
    mockedPutPrefs.mockClear();
    recordAssistantMemoryAction('把工单转给二线（对话正文）');
    expect(mockedPutPrefs).not.toHaveBeenCalled();
  });

  it('用户级开关：关闭立即停用且零请求；重新开启恢复加载；清空删除云端键', async () => {
    await store().load();
    await store().setUserEnabled(false);
    expect(store().status).toBe('disabled');
    expect(isAssistantMemoryUserDisabled()).toBe(true);
    expect(window.localStorage.getItem(ASSISTANT_MEMORY_USER_DISABLED_KEY)).toBe('1');

    mockedCaps.mockClear();
    mockedGetPrefs.mockClear();
    await store().load();
    expect(mockedCaps).not.toHaveBeenCalled();
    expect(mockedGetPrefs).not.toHaveBeenCalled();

    await store().setUserEnabled(true);
    expect(isAssistantMemoryUserDisabled()).toBe(false);
    expect(store().status).toBe('ready');

    await store().clearAll();
    expect(mockedPutPrefs).toHaveBeenLastCalledWith({ assistantMemory: null });
    expect(store().memory).toEqual(emptyAssistantMemory());
  });
});