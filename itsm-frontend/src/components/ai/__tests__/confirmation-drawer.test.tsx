/**
 * B1-07 ConfirmationDrawer 组件测试。
 *
 * 覆盖：pending 渲染（脱敏参数 + 倒计时）、过期禁用、决策回调（通过/拒绝）、
 * 拒绝原因必填、dry-run 预览快照、已处理只读。
 */
import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

import ConfirmationDrawer, { deriveConfirmationView, formatRemaining } from '../confirmation-drawer';
import type { ToolInvocationDetail } from '@/lib/api/ai-api';

const baseDetail: ToolInvocationDetail = {
  id: 42,
  toolName: 'mcp__mock__create_issue',
  argsRedacted: '{"title":"打印机故障","token":"****"}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  confirmationState: 'pending',
  createdAt: '2026-09-27T00:00:00Z',
  conversationId: 1,
  userId: 7,
  provider: 'mcp',
  serverName: 'mock',
  risk: 'act_medium',
  expiresAt: '2026-09-28T00:00:00Z',
};

const nowMs = Date.parse('2026-09-27T23:59:00Z'); // 距过期 1 分钟

describe('ConfirmationDrawer (B1-07)', () => {
  it('renders redacted args and remaining countdown for a pending confirmation', () => {
    const onDecision = jest.fn();
    render(
      <ConfirmationDrawer open detail={baseDetail} onClose={jest.fn()} onDecision={onDecision} now={() => nowMs} />
    );

    expect(screen.getByText('待确认')).toBeInTheDocument();
    // prettier JSON（缩进/空格）与后端脱敏口径一致：敏感键仍为掩码值。
    expect(screen.getByTestId('confirmation-args')).toHaveTextContent('"token": "****"');
    expect(screen.getByTestId('confirmation-args')).not.toHaveTextContent('s3cr3t');
    expect(screen.getByText(/剩余 1 分/)).toBeInTheDocument();
    expect(screen.getByTestId('confirmation-approve')).toBeEnabled();
  });

  it('disables actions once the confirmation is expired by time', () => {
    render(
      <ConfirmationDrawer
        open
        detail={baseDetail}
        onClose={jest.fn()}
        onDecision={jest.fn()}
        now={() => Date.parse('2026-09-28T00:00:01Z')}
      />
    );

    expect(screen.getByText('已过期')).toBeInTheDocument();
    expect(screen.getByText(/重新发起/)).toBeInTheDocument();
    expect(screen.getByTestId('confirmation-approve')).toBeDisabled();
    expect(screen.getByTestId('confirmation-reject')).toBeDisabled();
  });

  it('requires a reason before rejecting, then calls onDecision(false, reason)', async () => {
    const onDecision = jest.fn().mockResolvedValue(undefined);
    render(
      <ConfirmationDrawer open detail={baseDetail} onClose={jest.fn()} onDecision={onDecision} now={() => nowMs} />
    );

    const reject = screen.getByTestId('confirmation-reject');
    expect(reject).toBeDisabled();

    fireEvent.change(screen.getByTestId('confirmation-reason'), { target: { value: '变更窗口外，禁止执行' } });
    await waitFor(() => expect(screen.getByTestId('confirmation-reject')).toBeEnabled());
    fireEvent.click(screen.getByTestId('confirmation-reject'));

    await waitFor(() => expect(onDecision).toHaveBeenCalledWith(false, '变更窗口外，禁止执行'));
  });

  it('approves without a reason', async () => {
    const onDecision = jest.fn().mockResolvedValue(undefined);
    render(
      <ConfirmationDrawer open detail={baseDetail} onClose={jest.fn()} onDecision={onDecision} now={() => nowMs} />
    );

    fireEvent.click(screen.getByTestId('confirmation-approve'));
    await waitFor(() => expect(onDecision).toHaveBeenCalledWith(true, ''));
  });

  it('shows the dry-run preview snapshot when the invocation is a preview', () => {
    render(
      <ConfirmationDrawer
        open
        detail={{ ...baseDetail, status: 'preview', approvalState: 'auto', confirmationState: 'decided', result: '{"wouldUpdate":3}' }}
        onClose={jest.fn()}
        onDecision={jest.fn()}
        now={() => nowMs}
      />
    );

    expect(screen.getByTestId('confirmation-dryrun')).toHaveTextContent('wouldUpdate');
  });

  it('renders handled confirmations as read-only', () => {
    render(
      <ConfirmationDrawer
        open
        detail={{ ...baseDetail, approvalState: 'approved', confirmationState: 'confirmed', status: 'done' }}
        onClose={jest.fn()}
        onDecision={jest.fn()}
        now={() => nowMs}
      />
    );

    expect(screen.getByText('已处理')).toBeInTheDocument();
    expect(screen.getByTestId('confirmation-approve')).toBeDisabled();
  });

  it('surfaces API errors inside the drawer', () => {
    render(
      <ConfirmationDrawer
        open
        detail={baseDetail}
        onClose={jest.fn()}
        onDecision={jest.fn()}
        now={() => nowMs}
        errorMessage="该确认单已有他人决策或状态已终止，请刷新后查看"
      />
    );

    expect(screen.getByText(/已有他人决策/)).toBeInTheDocument();
  });
});

describe('deriveConfirmationView / formatRemaining (B1-07)', () => {
  it('prefers backend confirmationState and falls back to approvalState', () => {
    expect(deriveConfirmationView({ ...baseDetail, confirmationState: 'expired' }, nowMs)).toBe('expired');
    expect(
      deriveConfirmationView({ ...baseDetail, confirmationState: undefined, approvalState: 'rejected' }, nowMs)
    ).toBe('decided');
    expect(deriveConfirmationView(null, nowMs)).toBe('unknown');
  });

  it('treats a past expiresAt as expired even without explicit state', () => {
    expect(
      deriveConfirmationView(
        { ...baseDetail, confirmationState: undefined, expiresAt: '2026-09-27T23:00:00Z' },
        nowMs
      )
    ).toBe('expired');
  });

  it('formats remaining time with day/hour/minute granularity', () => {
    expect(formatRemaining('2026-09-30T00:00:00Z', Date.parse('2026-09-27T00:00:00Z'))).toBe('剩余 3 天 0 小时');
    expect(formatRemaining('2026-09-27T05:30:00Z', Date.parse('2026-09-27T00:00:00Z'))).toBe('剩余 5 小时 30 分');
    expect(formatRemaining('2026-09-27T00:00:30Z', Date.parse('2026-09-27T00:00:00Z'))).toBe('剩余 30 秒');
    expect(formatRemaining(null, nowMs)).toBe('未设置有效期');
  });
});
