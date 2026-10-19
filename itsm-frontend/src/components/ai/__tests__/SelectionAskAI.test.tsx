/**
 * I1-02 划词问 AI 测试：选区浮现 → 点击预填/打开面板/埋点；短选区、输入框内选区、滚动隐藏均不打扰。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { SELECTION_ASK_AI_TESTID, SelectionAskAI } from '../SelectionAskAI';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

const mockSelection = (text: string, anchorNode: Node | null = document.body) => {
  const range = {
    getBoundingClientRect: () => ({ left: 100, top: 120, width: 60, height: 18, bottom: 138, right: 160 }),
  } as unknown as Range;
  (window as unknown as { getSelection: () => Selection | null }).getSelection = () =>
    ({
      toString: () => text,
      anchorNode,
      getRangeAt: () => range,
    }) as unknown as Selection;
};

beforeEach(() => {
  window.localStorage.clear();
  mockedTrack.mockReset();
  useAssistantStore.setState({ open: false, pendingPrompt: undefined });
  mockSelection('');
});

describe('SelectionAskAI（I1-02 划词问 AI）', () => {
  it('选中文本 → 悬浮按钮 → 点击预填并打开助手面板（只填不发送）', () => {
    render(<SelectionAskAI />);
    mockSelection('数据库连接超时，重试三次仍失败');

    fireEvent.mouseUp(document);
    const button = screen.getByTestId(SELECTION_ASK_AI_TESTID);
    expect(button).toBeInTheDocument();

    fireEvent.click(button);

    const pending = useAssistantStore.getState().pendingPrompt ?? '';
    expect(pending).toContain('数据库连接超时');
    expect(pending).toContain('解释');
    expect(useAssistantStore.getState().open).toBe(true);
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ extra: { from: 'selection' } })
    );
    // 点击后浮层收起（不重复打扰）。
    expect(screen.queryByTestId(SELECTION_ASK_AI_TESTID)).not.toBeInTheDocument();
  });

  it('短选区（<2 字符）不浮现', () => {
    render(<SelectionAskAI />);
    mockSelection('a');

    fireEvent.mouseUp(document);
    expect(screen.queryByTestId(SELECTION_ASK_AI_TESTID)).not.toBeInTheDocument();
  });

  it('输入框内选区不浮现（不干扰表单输入）', () => {
    render(<SelectionAskAI />);
    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    mockSelection('这是输入框中的文本', textarea);

    fireEvent.mouseUp(document);
    expect(screen.queryByTestId(SELECTION_ASK_AI_TESTID)).not.toBeInTheDocument();
    textarea.remove();
  });

  it('滚动或 ESC 收起浮层', () => {
    render(<SelectionAskAI />);
    mockSelection('需要解释的一段选区文本');

    fireEvent.mouseUp(document);
    expect(screen.getByTestId(SELECTION_ASK_AI_TESTID)).toBeInTheDocument();

    fireEvent.scroll(window);
    expect(screen.queryByTestId(SELECTION_ASK_AI_TESTID)).not.toBeInTheDocument();

    fireEvent.mouseUp(document);
    expect(screen.getByTestId(SELECTION_ASK_AI_TESTID)).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId(SELECTION_ASK_AI_TESTID)).not.toBeInTheDocument();
  });
});