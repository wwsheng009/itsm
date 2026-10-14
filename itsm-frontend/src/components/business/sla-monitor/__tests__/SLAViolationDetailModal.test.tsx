import React from 'react';
import { fireEvent, render, screen, waitFor } from '@/lib/test-utils';
import { SLAViolationDetailModal } from '../components/SLAViolationDetailModal';
import type { SLAViolation } from '../types';

const violation: SLAViolation = {
  id: 7,
  createdBy: 1,
  tenantId: 1,
  ticketId: 10,
  ticketNumber: 'TKT-0007',
  ticketTitle: '打印机故障',
  ticketPriority: 'high',
  slaName: '标准服务SLA',
  slaDefinitionId: 2,
  violationType: 'response',
  violationTime: '2026-01-01T09:00:00Z',
  severity: 'critical',
  isResolved: false,
  resolutionNotes: '',
  description: '响应超时',
};

describe('SLAViolationDetailModal', () => {
  it('hides mutating actions without write permission', () => {
    render(
      <SLAViolationDetailModal
        violation={violation}
        visible
        onClose={jest.fn()}
        onResolve={jest.fn()}
        onAcknowledge={jest.fn()}
      />
    );

    expect(screen.queryByRole('button', { name: /确\s*认/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /解\s*决/ })).not.toBeInTheDocument();
  });

  it('emits controlled actions when permission is granted', () => {
    const onResolve = jest.fn();
    const onAcknowledge = jest.fn();
    render(
      <SLAViolationDetailModal
        violation={violation}
        visible
        canManage
        onClose={jest.fn()}
        onResolve={onResolve}
        onAcknowledge={onAcknowledge}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: /确\s*认/ }));
    fireEvent.click(screen.getByRole('button', { name: /解\s*决/ }));
    expect(onAcknowledge).toHaveBeenCalledWith(violation);
    expect(onResolve).toHaveBeenCalledWith(violation);
  });

  it('offers dispute for pending violations and requires a reason', async () => {
    const onDispute = jest.fn();
    render(
      <SLAViolationDetailModal
        violation={violation}
        visible
        onClose={jest.fn()}
        onResolve={jest.fn()}
        onAcknowledge={jest.fn()}
        onDispute={onDispute}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: '提出争议' }));
    const textarea = await screen.findByPlaceholderText(/争议理由/);
    // 未填理由：确定不触发
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    expect(onDispute).not.toHaveBeenCalled();

    fireEvent.change(textarea, { target: { value: '客户认为已按时响应' } });
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    await waitFor(() => expect(onDispute).toHaveBeenCalledWith(violation, '客户认为已按时响应'));
  });

  it('shows settle actions for disputed violations and requires waiver reason', async () => {
    const onSettleAccepted = jest.fn();
    const onSettleWaived = jest.fn();
    render(
      <SLAViolationDetailModal
        violation={{ ...violation, resolutionState: 'disputed', disputeReason: '客户提出争议' }}
        visible
        onClose={jest.fn()}
        onResolve={jest.fn()}
        onAcknowledge={jest.fn()}
        onSettleAccepted={onSettleAccepted}
        onSettleWaived={onSettleWaived}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: '裁决：认账' }));
    expect(onSettleAccepted).toHaveBeenCalledWith(expect.objectContaining({ id: 7 }));

    fireEvent.click(screen.getByRole('button', { name: '裁决：豁免' }));
    const textarea = await screen.findByPlaceholderText(/豁免理由/);
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    expect(onSettleWaived).not.toHaveBeenCalled();

    fireEvent.change(textarea, { target: { value: '客户侧原因，属合同除外条款' } });
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    await waitFor(() =>
      expect(onSettleWaived).toHaveBeenCalledWith(expect.objectContaining({ id: 7 }), '客户侧原因，属合同除外条款')
    );
  });
});
