/**
 * I1-02 错误解释入口测试：错误值提取/截断；点击预填并打开助手面板；无错误不渲染。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import {
  ExplainErrorButton,
  buildExplainErrorPrompt,
  errorMessageOf,
} from '../ExplainErrorButton';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedTrack = trackAssistantEvent as jest.Mock;

beforeEach(() => {
  window.localStorage.clear();
  mockedTrack.mockReset();
  useAssistantStore.setState({ open: false, pendingPrompt: undefined });
});

describe('ExplainErrorButton（I1-02 错误解释入口）', () => {
  it('点击预填错误解释提示（含错误消息与位置）并打开助手面板', () => {
    render(
      <ExplainErrorButton
        error={new Error('ECONNREFUSED 127.0.0.1:5432')}
        context="/tickets/42"
      />
    );

    fireEvent.click(screen.getByTestId('explain-error-button'));

    const pending = useAssistantStore.getState().pendingPrompt ?? '';
    expect(pending).toContain('ECONNREFUSED 127.0.0.1:5432');
    expect(pending).toContain('排查步骤');
    expect(pending).toContain('/tickets/42');
    expect(useAssistantStore.getState().open).toBe(true);
    expect(mockedTrack).toHaveBeenCalledWith(
      'handoff',
      expect.objectContaining({ extra: { from: 'error_explain' } })
    );
  });

  it('无错误时不渲染入口', () => {
    const { container } = render(<ExplainErrorButton error={undefined} />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByTestId('explain-error-button')).not.toBeInTheDocument();
  });

  it('错误值提取：Error/字符串/对象兜底', () => {
    expect(errorMessageOf(new Error('boom'))).toBe('boom');
    expect(errorMessageOf('直接字符串错误')).toBe('直接字符串错误');
    expect(errorMessageOf({ code: 500 })).toBe('{"code":500}');
    // react-router errorElement 的 Response 形状（jsdom 无 Response 全局 → 鸭子类型契约直测）。
    expect(errorMessageOf({ status: 404, statusText: 'Not Found' })).toBe('HTTP 404 Not Found');
    expect(errorMessageOf(undefined)).toBe('未知错误');
  });

  it('提示词截断：超长错误消息截到 500 字符', () => {
    const long = 'x'.repeat(800);
    const prompt = buildExplainErrorPrompt(long);
    expect(prompt).toContain('x'.repeat(500));
    expect(prompt).not.toContain('x'.repeat(501));
  });
});