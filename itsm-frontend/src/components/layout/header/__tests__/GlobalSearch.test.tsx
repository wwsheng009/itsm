/**
 * I0-03 全局搜索「问 AI」区测试：关键词非空常驻首项；点击/Ctrl+Enter 转交助手面板（预填不发送）；
 * Enter 仍走搜索（不与既有行为冲突）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import { GlobalSearch } from '../GlobalSearch';
import { globalSearch } from '@/lib/api/global-search-api';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/api/global-search-api', () => ({
  __esModule: true,
  globalSearch: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedGlobalSearch = globalSearch as jest.MockedFunction<typeof globalSearch>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const renderSearch = (onClose = jest.fn()) => {
  const utils = render(
    <MemoryRouter>
      <GlobalSearch open onClose={onClose} />
    </MemoryRouter>
  );
  return { onClose, container: utils.container };
};

beforeEach(() => {
  window.localStorage.clear();
  mockedGlobalSearch.mockReset();
  mockedTrack.mockReset();
  mockedGlobalSearch.mockResolvedValue({ results: [], total: 0 });
  useAssistantStore.setState({ open: false, pendingPrompt: undefined });
});

describe('GlobalSearch 问 AI（I0-03）', () => {
  it('关键词非空显示「问 AI」首项；点击后预填并打开助手面板（不发请求）', async () => {
    const { onClose } = renderSearch();

    const input = screen.getByPlaceholderText('搜索工单、事件、项目、任务...');
    fireEvent.change(input, { target: { value: '登录报错怎么办' } });

    const askAI = await screen.findByTestId('global-search-ask-ai');
    expect(askAI).toHaveTextContent('问 AI：登录报错怎么办');

    fireEvent.click(askAI);

    expect(useAssistantStore.getState().pendingPrompt).toBe('登录报错怎么办');
    expect(useAssistantStore.getState().open).toBe(true);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mockedGlobalSearch).not.toHaveBeenCalled();
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ extra: expect.objectContaining({ from: 'global_search' }) })
    );
  });

  it('Ctrl+Enter 走问 AI；普通 Enter 仍走搜索（行为不冲突）', async () => {
    const { onClose } = renderSearch();
    const input = screen.getByPlaceholderText('搜索工单、事件、项目、任务...');
    fireEvent.change(input, { target: { value: 'SLA 风险' } });

    // 普通 Enter → 全局搜索。
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
    await waitFor(() => expect(mockedGlobalSearch).toHaveBeenCalledWith('SLA 风险'));
    expect(useAssistantStore.getState().pendingPrompt).toBeUndefined();

    // Ctrl+Enter → 问 AI。
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', ctrlKey: true });
    expect(useAssistantStore.getState().pendingPrompt).toBe('SLA 风险');
    expect(onClose).toHaveBeenCalled();
  });

  it('关键词为空时不渲染问 AI 区', () => {
    renderSearch();
    expect(screen.queryByTestId('global-search-ask-ai')).not.toBeInTheDocument();
  });
});