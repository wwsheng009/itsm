import React from 'react';

import { render, screen } from '@/lib/test-utils';
import { ApprovalTimeline } from '../ApprovalTimeline';
import type { ApprovalStep } from '../types';

describe('ApprovalTimeline membership identity', () => {
  it('renders approver membership badge when provided', () => {
    const approvals: ApprovalStep[] = [
      {
        id: 1,
        level: 1,
        status: 'approved',
        approverId: 5,
        approverName: '张三',
        approverMembershipId: 42,
        comment: '同意',
        createdAt: '2026-11-12T04:00:00Z',
        processedAt: '2026-11-12T04:10:00Z',
      },
    ];

    render(<ApprovalTimeline approvals={approvals} showApprovalActions={false} />);

    expect(screen.getByTestId('membership-badge-42')).toBeInTheDocument();
    expect(screen.getByText('租户身份 #42')).toBeInTheDocument();
  });

  it('omits membership badge for historical approvals without membership id', () => {
    const approvals: ApprovalStep[] = [
      {
        id: 2,
        level: 1,
        status: 'approved',
        approverName: '李四',
        createdAt: '2026-11-12T04:00:00Z',
      },
    ];

    render(<ApprovalTimeline approvals={approvals} showApprovalActions={false} />);

    expect(screen.queryByTestId(/membership-badge-/)).toBeNull();
  });
});
