/**
 * I0-01：交互状态条组件测试（8 类状态 + closed/ready 不占位）。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import { AssistantInteractionBanner } from '../AssistantInteractionBanner';
import type { AssistantInteractionState } from '@/lib/ai/interaction-state';

const renderBanner = (props: React.ComponentProps<typeof AssistantInteractionBanner>) =>
  render(<AssistantInteractionBanner {...props} />);

describe('AssistantInteractionBanner（I0-01 状态机可见反馈）', () => {
  it('closed / ready 不渲染占位', () => {
    const { container: closed } = renderBanner({ state: 'closed' });
    const { container: ready } = renderBanner({ state: 'ready' });
    expect(closed).toBeEmptyDOMElement();
    expect(ready).toBeEmptyDOMElement();
  });

  it('opening：骨架屏 + 准备提示', () => {
    renderBanner({ state: 'opening' });
    const banner = screen.getByTestId('assistant-interaction-banner');
    expect(banner).toHaveAttribute('data-state', 'opening');
    expect(banner).toHaveTextContent('正在准备页面上下文与建议');
  });

  it('streaming：生成中 + aria-live 可感知', () => {
    renderBanner({ state: 'streaming' });
    const banner = screen.getByTestId('assistant-interaction-banner');
    expect(banner).toHaveAttribute('data-state', 'streaming');
    expect(banner).toHaveAttribute('role', 'status');
    expect(banner).toHaveAttribute('aria-live', 'polite');
    expect(banner).toHaveTextContent('生成中');
  });

  it('tool_running：显示正在执行的工具名', () => {
    renderBanner({ state: 'tool_running', runningTool: 'list_tickets' });
    expect(screen.getByTestId('assistant-interaction-banner')).toHaveTextContent('list_tickets');
  });

  it('tool_running：工具名缺失时如实显示未知工具（不空白）', () => {
    renderBanner({ state: 'tool_running' });
    expect(screen.getByTestId('assistant-interaction-banner')).toHaveTextContent('未知工具');
  });

  it('awaiting_confirmation：提示写操作需确认', () => {
    renderBanner({ state: 'awaiting_confirmation' });
    expect(screen.getByTestId('assistant-interaction-banner')).toHaveTextContent('等待确认');
  });

  it('awaiting_approval：显示数量并可跳转审批', () => {
    const onOpenApproval = jest.fn();
    renderBanner({ state: 'awaiting_approval', pendingApprovalCount: 2, onOpenApproval });
    const banner = screen.getByTestId('assistant-interaction-banner');
    expect(banner).toHaveTextContent('2 个写操作等待人工审批');
    fireEvent.click(screen.getByText('去审批'));
    expect(onOpenApproval).toHaveBeenCalledTimes(1);
  });

  it('degraded：字典文案 + 恢复动作回调（原因码 → 动作）', () => {
    const onAction = jest.fn();
    renderBanner({ state: 'degraded', errorCode: 'AI_PROVIDER_KEY_MISSING', onAction });
    expect(screen.getByText('模型实例未配置密钥')).toBeTruthy();
    fireEvent.click(screen.getByText('去配置'));
    expect(onAction).toHaveBeenCalledWith('open_settings');
  });

  it('degraded：未登记原因码用后端文案兜底，不丢错误', () => {
    renderBanner({ state: 'degraded', errorCode: 'brand_new_code', errorDetail: '后端返回的具体失败原因' });
    expect(screen.getByText('回答失败')).toBeTruthy();
    expect(screen.getByText('后端返回的具体失败原因')).toBeTruthy();
  });

  it('degraded：未注入 onAction 时不渲染假按钮（不假装可操作）', () => {
    renderBanner({ state: 'degraded', errorCode: 'rate_limited' });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('done：回执提示', () => {
    renderBanner({ state: 'done' });
    expect(screen.getByTestId('assistant-interaction-banner')).toHaveTextContent('本轮已完成');
  });

  it('handoff：转人工提示', () => {
    renderBanner({ state: 'handoff' });
    expect(screen.getByTestId('assistant-interaction-banner')).toHaveTextContent('已转人工处理');
  });

  it('每个状态都带稳定 data-state（供 E2E/埋点定位）', () => {
    const states: AssistantInteractionState[] = [
      'opening',
      'streaming',
      'tool_running',
      'awaiting_confirmation',
      'awaiting_approval',
      'degraded',
      'done',
      'handoff',
    ];
    for (const state of states) {
      const { unmount } = renderBanner({ state, errorCode: 'rate_limited' });
      expect(screen.getByTestId('assistant-interaction-banner')).toHaveAttribute('data-state', state);
      unmount();
    }
  });
});
