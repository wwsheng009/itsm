/**
 * 对话内待审批卡片测试（M1-05，jsdom + @testing-library/react）。
 *
 * 覆盖任务卡要求的四态（pending / approved / rejected / expired）与跳转交互，
 * 外加两条边界：记录不可得（404/查询失败）与「已处理态禁止重复操作」。
 *
 * 状态判定走纯函数 `deriveApprovalState`，接口拉取用 `aiGetToolInvocation` 打桩（不轮询）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { ToolApprovalCard, deriveApprovalState, PENDING_TTL_DAYS } from '../tool-approval-card';
import type { ToolInvocationDetail } from '@/lib/api/ai-api';

jest.mock('@/lib/api/ai-api', () => ({
  aiGetToolInvocation: jest.fn(),
}));

// eslint-disable-next-line @typescript-eslint/no-var-requires
const { aiGetToolInvocation } = require('@/lib/api/ai-api') as { aiGetToolInvocation: jest.Mock };

const detail = (patch: Partial<ToolInvocationDetail> = {}): ToolInvocationDetail => ({
  id: 9,
  toolName: 'mcp__mock__create_issue',
  argsRedacted: '{"title":"打印机故障"}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  createdAt: new Date().toISOString(),
  conversationId: 0,
  userId: 7,
  provider: 'mcp',
  serverName: 'mock',
  rawToolName: 'create_issue',
  callableName: 'mcp__mock__create_issue',
  risk: 'high',
  ...patch,
});

describe('deriveApprovalState', () => {
  const now = Date.parse('2026-09-27T12:00:00Z');

  it('待审批保留期内为 pending，超期为 expired', () => {
    expect(deriveApprovalState(detail({ createdAt: '2026-09-26T12:00:00Z' }), PENDING_TTL_DAYS, now)).toBe('pending');
    expect(deriveApprovalState(detail({ createdAt: '2026-09-01T12:00:00Z' }), PENDING_TTL_DAYS, now)).toBe('expired');
  });

  it('终态映射：approved / rejected / 执行完成（无决策字段）', () => {
    expect(deriveApprovalState(detail({ approvalState: 'approved' }), PENDING_TTL_DAYS, now)).toBe('approved');
    expect(deriveApprovalState(detail({ approvalState: 'rejected' }), PENDING_TTL_DAYS, now)).toBe('rejected');
    expect(
      deriveApprovalState(detail({ approvalState: '', status: 'done' }), PENDING_TTL_DAYS, now)
    ).toBe('approved');
  });

  it('记录缺失 → unknown（不得误判为待审批）', () => {
    expect(deriveApprovalState(undefined, PENDING_TTL_DAYS, now)).toBe('unknown');
  });
});

describe('ToolApprovalCard', () => {
  beforeEach(() => {
    aiGetToolInvocation.mockReset();
  });

  it('pending：展示来源/风险/待审批与 invocation，并跳转审批页', async () => {
    aiGetToolInvocation.mockResolvedValue(detail());
    const onOpenApproval = jest.fn();

    render(
      <ToolApprovalCard
        invocationId={9}
        tool="mcp__mock__create_issue"
        provider="mcp"
        server="mock"
        onOpenApproval={onOpenApproval}
      />
    );

    expect(await screen.findByText('待审批')).toBeTruthy();
    expect(screen.getByText('MCP · mock')).toBeTruthy();
    expect(screen.getByText('风险 · high')).toBeTruthy();
    expect(screen.getByText(/已提交人工审批（#9）/)).toBeTruthy();

    fireEvent.click(screen.getByText('前往审批'));
    expect(onOpenApproval).toHaveBeenCalledWith(9);
    // 只按需拉取一次（不轮询）。
    expect(aiGetToolInvocation).toHaveBeenCalledTimes(1);
  });

  it('approved：只读展示，不提供内联操作入口（禁止重复操作）', async () => {
    aiGetToolInvocation.mockResolvedValue(
      detail({ approvalState: 'approved', status: 'done', approvedBy: 3, durationMs: 12 })
    );
    const onOpenApproval = jest.fn();

    render(<ToolApprovalCard invocationId={9} tool="mcp__mock__create_issue" onOpenApproval={onOpenApproval} />);

    expect(await screen.findByText('已处理')).toBeTruthy();
    expect(screen.getByText(/该请求已处理（#9）/)).toBeTruthy();
    // 没有「前往审批」这个动作按钮，只有只读确认入口。
    expect(screen.queryByText('前往审批')).toBeNull();
    expect(screen.getByText('前往审批页确认')).toBeTruthy();
  });

  it('rejected：展示驳回原因，且不触发任何执行动作', async () => {
    aiGetToolInvocation.mockResolvedValue(
      detail({ approvalState: 'rejected', status: 'rejected', approvalReason: '风险过高' })
    );

    render(<ToolApprovalCard invocationId={9} tool="mcp__mock__create_issue" />);

    expect(await screen.findByText('已驳回')).toBeTruthy();
    expect(screen.getByText('驳回原因：风险过高')).toBeTruthy();
    expect(screen.getByText(/该请求已被驳回（#9）/)).toBeTruthy();
    expect(screen.queryByText('前往审批')).toBeNull();
  });

  it('expired：超期待审批显示过期提示且禁止操作', async () => {
    const stale = new Date(Date.now() - (PENDING_TTL_DAYS + 1) * 24 * 60 * 60 * 1000).toISOString();
    aiGetToolInvocation.mockResolvedValue(detail({ createdAt: stale }));

    render(<ToolApprovalCard invocationId={9} tool="mcp__mock__create_issue" />);

    expect(await screen.findByText('已过期/不可用')).toBeTruthy();
    expect(screen.getByText(/待审批时间过长（#9）/)).toBeTruthy();
    expect(screen.queryByText('前往审批')).toBeNull();
  });

  it('记录不可得（查询失败）：降级为不可用状态并保留人工确认入口', async () => {
    aiGetToolInvocation.mockRejectedValue(new Error('HTTP 404'));
    const onOpenApproval = jest.fn();

    render(<ToolApprovalCard invocationId={9} tool="mcp__mock__create_issue" onOpenApproval={onOpenApproval} />);

    expect(await screen.findByText('已过期/不可用')).toBeTruthy();
    fireEvent.click(screen.getByText('前往审批页确认'));
    expect(onOpenApproval).toHaveBeenCalledWith(9);
  });

  it('刷新状态：终态化后由 pending 收敛为已处理（放弃操作入口）', async () => {
    aiGetToolInvocation.mockResolvedValueOnce(detail());
    aiGetToolInvocation.mockResolvedValueOnce(detail({ approvalState: 'approved', status: 'done' }));

    render(<ToolApprovalCard invocationId={9} tool="mcp__mock__create_issue" />);

    expect(await screen.findByText('待审批')).toBeTruthy();
    fireEvent.click(screen.getByText('刷新状态'));

    await waitFor(() => expect(screen.getByText('已处理')).toBeTruthy());
    expect(screen.queryByText('前往审批')).toBeNull();
  });
});
