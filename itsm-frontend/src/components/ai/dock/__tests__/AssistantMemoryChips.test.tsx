/**
 * V2-01 记忆条组件测试：chip 只预填不发送、显式保存、管理面（最近处理/关闭/清空）、策略关闭零渲染。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import AssistantMemoryChips, {
  ASSISTANT_MEMORY_CHIP_TESTID,
  ASSISTANT_MEMORY_CHIPS_TESTID,
  ASSISTANT_MEMORY_CLEAR_TESTID,
  ASSISTANT_MEMORY_DISABLE_TESTID,
  ASSISTANT_MEMORY_MANAGE_TESTID,
  ASSISTANT_MEMORY_REENABLE_TESTID,
  ASSISTANT_MEMORY_SAVE_PHRASE_TESTID,
  ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID,
  ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID,
  ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID,
} from '../AssistantMemoryChips';
import { useAssistantMemoryStore } from '@/lib/ai/memory/assistant-memory-store';
import { getCapabilities } from '@/lib/api/capability-api';
import { getUserPreferences, updateUserPreferences } from '@/lib/api/user-preferences-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.setTimeout(300000);

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
  getTelemetryMode: () => 'on',
  trackAssistantEvent: jest.fn(),
}));

const mockedCaps = getCapabilities as jest.MockedFunction<typeof getCapabilities>;
const mockedGetPrefs = getUserPreferences as jest.MockedFunction<typeof getUserPreferences>;
const mockedPutPrefs = updateUserPreferences as jest.MockedFunction<typeof updateUserPreferences>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const memoryPayload = {
  version: 1,
  phrases: ['申请 VPN', '重置密码'],
  templates: [{ name: '网络故障', body: '无法访问内网，已尝试重启' }],
  recent: [{ key: 'inline_apply:triage_detail', at: '2026-10-18T10:00:00.000Z' }],
};

const renderChips = (query = '', onInsert = jest.fn()) => {
  const view = render(
    <App>
      <AssistantMemoryChips query={query} onInsert={onInsert} />
    </App>
  );
  return { ...view, onInsert };
};

beforeEach(() => {
  jest.clearAllMocks();
  window.localStorage.clear();
  useAssistantMemoryStore.getState().reset();
  mockedCaps.mockResolvedValue({ items: [], acceptanceVersion: 'test' });
  mockedGetPrefs.mockResolvedValue({ preferences: { assistantMemory: memoryPayload } });
  mockedPutPrefs.mockResolvedValue({ preferences: {} });
});

describe('AssistantMemoryChips（V2-01 记忆条）', () => {
  it('渲染常用语/模板 chips；点击只预填输入框（绝不发送）并复用 suggestion 埋点', async () => {
    const { onInsert } = renderChips();
    await screen.findByTestId(ASSISTANT_MEMORY_CHIPS_TESTID);
    const chips = await screen.findAllByTestId(ASSISTANT_MEMORY_CHIP_TESTID);
    expect(chips.map(chip => chip.textContent)).toEqual(['申请 VPN', '重置密码', '网络故障']);

    fireEvent.click(chips[0]);
    expect(onInsert).toHaveBeenCalledWith('申请 VPN');
    // 模板 chip 预填正文（而非仅名字）。
    fireEvent.click(chips[2]);
    expect(onInsert).toHaveBeenLastCalledWith('无法访问内网，已尝试重启');

    expect(mockedTrack).toHaveBeenCalledWith(
      'suggestion_shown',
      expect.objectContaining({ cardId: 'memory' })
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'suggestion_clicked',
      expect.objectContaining({ cardId: 'memory', extra: expect.objectContaining({ kind: 'phrase' }) })
    );
  });

  it('存为常用语：显式保存并上报 memory_updated', async () => {
    renderChips('帮我开通访客 Wi-Fi');
    await screen.findByTestId(ASSISTANT_MEMORY_CHIPS_TESTID);

    fireEvent.click(screen.getByTestId(ASSISTANT_MEMORY_SAVE_PHRASE_TESTID));
    await waitFor(() =>
      expect(mockedPutPrefs).toHaveBeenCalledWith({
        assistantMemory: expect.objectContaining({
          phrases: expect.arrayContaining(['帮我开通访客 Wi-Fi']),
        }),
      })
    );
    expect(mockedTrack).toHaveBeenCalledWith(
      'memory_updated',
      expect.objectContaining({ extra: expect.objectContaining({ op: 'phrase_saved' }) })
    );
  });

  it('存为模板：弹层命名后保存（模板名 + 正文入记忆）', async () => {
    renderChips('无法访问内网，已尝试重启');
    await screen.findByTestId(ASSISTANT_MEMORY_CHIPS_TESTID);

    fireEvent.click(screen.getByTestId(ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID));
    await screen.findByTestId(ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID);
    fireEvent.change(screen.getByTestId(ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID), {
      target: { value: '网络故障回复' },
    });
    const dialog = screen.getByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /保\s*存/ }));

    await waitFor(() =>
      expect(mockedPutPrefs).toHaveBeenCalledWith({
        assistantMemory: expect.objectContaining({
          templates: expect.arrayContaining([
            expect.objectContaining({ name: '网络故障回复', body: '无法访问内网，已尝试重启' }),
          ]),
        }),
      })
    );
  });

  it('管理面：展示「最近处理方式」元数据，可关闭（用户级）并可清空', async () => {
    renderChips();
    await screen.findByTestId(ASSISTANT_MEMORY_CHIPS_TESTID);

    fireEvent.click(screen.getByTestId(ASSISTANT_MEMORY_MANAGE_TESTID));
    const panel = await screen.findByTestId('assistant-memory-manage-panel');
    // 最近处理方式：结构化动作键 → 可读标签（元数据，无正文）。
    expect(within(panel).getByText(/工单详情 · 应用字段建议/)).toBeInTheDocument();

    // 用户级关闭：确认后进入「已关闭」态并渲染重新开启入口（零请求路径）。
    fireEvent.click(within(panel).getByTestId(ASSISTANT_MEMORY_DISABLE_TESTID));
    const confirmDialog = await screen.findByRole('dialog');
    fireEvent.click(within(confirmDialog).getByRole('button', { name: /关\s*闭/ }));
    expect(await screen.findByTestId(ASSISTANT_MEMORY_REENABLE_TESTID)).toBeInTheDocument();

    mockedCaps.mockClear();
    mockedGetPrefs.mockClear();
    fireEvent.click(screen.getByTestId(ASSISTANT_MEMORY_REENABLE_TESTID));
    // 重新开启会重新解析（能力面）；断言其恢复渲染（chips 或记忆条）。
    await waitFor(() =>
      expect(screen.queryByTestId(ASSISTANT_MEMORY_REENABLE_TESTID)).not.toBeInTheDocument()
    );
  });

  it('清空记忆：确认后删除云端键（assistantMemory=null）并清空 UI', async () => {
    renderChips();
    await screen.findByTestId(ASSISTANT_MEMORY_CHIPS_TESTID);

    fireEvent.click(screen.getByTestId(ASSISTANT_MEMORY_MANAGE_TESTID));
    const panel = await screen.findByTestId('assistant-memory-manage-panel');
    fireEvent.click(within(panel).getByTestId(ASSISTANT_MEMORY_CLEAR_TESTID));
    const confirmDialog = await screen.findByRole('dialog');
    fireEvent.click(within(confirmDialog).getByRole('button', { name: /清\s*空/ }));

    await waitFor(() =>
      expect(mockedPutPrefs).toHaveBeenCalledWith({ assistantMemory: null })
    );
    await waitFor(() =>
      expect(screen.queryAllByTestId(ASSISTANT_MEMORY_CHIP_TESTID)).toHaveLength(0)
    );
  });

  it('租户策略关闭 → 记忆条不渲染（零数据面读写）', async () => {
    mockedCaps.mockResolvedValue({
      items: [],
      acceptanceVersion: 'test',
      assistant: { memoryEnabled: false },
    });
    renderChips();
    await waitFor(() => expect(mockedCaps).toHaveBeenCalled());
    expect(screen.queryByTestId(ASSISTANT_MEMORY_CHIPS_TESTID)).not.toBeInTheDocument();
    expect(mockedGetPrefs).not.toHaveBeenCalled();
  });
});