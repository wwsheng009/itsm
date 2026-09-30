/**
 * B3-02 launcher 组件测试（jsdom + @testing-library/react）。
 *
 * 覆盖：点击跳转携带入口上下文（含/不含目标）、拒绝态两种渲染（置灰 + Tooltip / 隐藏）、
 * 以及无障碍与 data 属性约定（页面集成测试据 data-entrypoint 定位）。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { AskAILauncher } from '../AskAILauncher';
import { ASK_AI_ROUTE, ASK_AI_STATE_SOURCE } from '@/lib/ai/ask-ai-scope';

jest.mock('react-router', () => ({
  useNavigate: jest.fn(),
}));

const { useNavigate } = require('react-router') as { useNavigate: jest.Mock };

const navigate = jest.fn();

beforeEach(() => {
  jest.clearAllMocks();
  useNavigate.mockReturnValue(navigate);
});

describe('AskAILauncher', () => {
  it('点击跳转工作区并携带入口上下文', () => {
    render(
      <AskAILauncher
        entrypoint="ticket_detail"
        targetType="ticket"
        targetId={42}
        summary="打印机故障"
      />
    );
    fireEvent.click(screen.getByTestId('ask-ai-launcher'));
    expect(navigate).toHaveBeenCalledWith(ASK_AI_ROUTE, {
      state: {
        source: ASK_AI_STATE_SOURCE,
        scope: { entrypoint: 'ticket_detail', targetType: 'ticket', targetId: 42, summary: '打印机故障' },
      },
    });
  });

  it('列表页入口不带目标', () => {
    render(<AskAILauncher entrypoint="ticket_list" />);
    fireEvent.click(screen.getByTestId('ask-ai-launcher'));
    expect(navigate).toHaveBeenCalledWith(ASK_AI_ROUTE, {
      state: { source: ASK_AI_STATE_SOURCE, scope: { entrypoint: 'ticket_list' } },
    });
  });

  it('无权限默认置灰且不可点击（Tooltip 说明）', () => {
    render(<AskAILauncher entrypoint="ci_detail" targetType="ci" targetId={7} denied />);
    const button = screen.getByTestId('ask-ai-launcher');
    expect(button).toBeDisabled();
    expect(button.getAttribute('data-entrypoint')).toBe('ci_detail');
    fireEvent.click(button);
    expect(navigate).not.toHaveBeenCalled();
  });

  it('denyMode=hidden 时不渲染', () => {
    const { container } = render(<AskAILauncher entrypoint="chat" denied denyMode="hidden" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('自定义文案与 onNavigate 注入生效', () => {
    const onNavigate = jest.fn();
    render(<AskAILauncher entrypoint="chat" label="问一问" onNavigate={onNavigate} />);
    fireEvent.click(screen.getByText('问一问'));
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
  });
});
