/**
 * SLA 违规详情模态框（含 D-SLA-8 归责状态机操作）。
 *
 * 交互（方案 §13.1）：
 *   - customer 侧："提出争议"（pending → disputed，理由必填）；
 *   - provider 侧："裁决：认账 / 裁决：豁免"（disputed/pending → accepted|waived，豁免理由必填；
 *     后端权限 sla:violation:settle 二次校验，前端不做权限推断）；
 *   - 终态（accepted/waived）与结算锁定（settledFlag）后不再展示状态机操作。
 */

import React, { useEffect, useState } from 'react';
import { Alert, Button, Descriptions, Input, Modal, Space, Tag } from 'antd';
import type { SLAViolation } from '../types';
import { useI18n } from '@/lib/i18n/useI18n';

const { TextArea } = Input;

interface SLAViolationDetailModalProps {
  violation: SLAViolation | null;
  visible: boolean;
  onClose: () => void;
  canManage?: boolean;
  actionLoading?: boolean;
  onResolve: (violation: SLAViolation) => void | Promise<void>;
  onAcknowledge: (violation: SLAViolation) => void | Promise<void>;
  onDispute?: (violation: SLAViolation, disputeReason: string) => void | Promise<void>;
  onSettleAccepted?: (violation: SLAViolation) => void | Promise<void>;
  onSettleWaived?: (violation: SLAViolation, waivedReason: string) => void | Promise<void>;
}

const RESOLUTION_META: Record<string, { color: string; labelKey: string }> = {
  pending: { color: 'default', labelKey: 'sla.violation.statePending' },
  accepted: { color: 'red', labelKey: 'sla.violation.stateAccepted' },
  disputed: { color: 'orange', labelKey: 'sla.violation.stateDisputed' },
  waived: { color: 'green', labelKey: 'sla.violation.stateWaived' },
};

export const SLAViolationDetailModal: React.FC<SLAViolationDetailModalProps> = ({
  violation,
  visible,
  onClose,
  onResolve,
  onAcknowledge,
  canManage = false,
  actionLoading = false,
  onDispute,
  onSettleAccepted,
  onSettleWaived,
}) => {
  const { t } = useI18n();
  const [reasonAction, setReasonAction] = useState<'dispute' | 'waive' | null>(null);
  const [reason, setReason] = useState('');
  const [reasonSubmitting, setReasonSubmitting] = useState(false);

  useEffect(() => {
    if (!visible) {
      setReasonAction(null);
      setReason('');
      setReasonSubmitting(false);
    }
  }, [visible]);

  if (!violation) return null;

  const resolutionState = violation.resolutionState ?? 'pending';
  const resolutionMeta = RESOLUTION_META[resolutionState] ?? RESOLUTION_META.pending;
  const reasonRequired = reasonAction === 'waive' || reasonAction === 'dispute';
  const canConfirmReason = !reasonRequired || reason.trim().length > 0;

  // 状态机可用性（终态/已锁定不再流转；权限由后端裁决）。
  const canDispute = !!onDispute && resolutionState === 'pending';
  const canSettleAccept = !!onSettleAccepted && (resolutionState === 'pending' || resolutionState === 'disputed');
  const canSettleWaive = !!onSettleWaived && (resolutionState === 'pending' || resolutionState === 'disputed');

  const closeReasonModal = () => {
    setReasonAction(null);
    setReason('');
  };

  const handleConfirmReason = async () => {
    if (!violation || !reasonAction || !canConfirmReason) return;
    setReasonSubmitting(true);
    try {
      if (reasonAction === 'dispute') {
        await onDispute?.(violation, reason.trim());
      } else {
        await onSettleWaived?.(violation, reason.trim());
      }
      closeReasonModal();
    } catch {
      /* 失败提示由父层负责；保留输入便于重试 */
    } finally {
      setReasonSubmitting(false);
    }
  };

  const severityColors: Record<string, string> = {
    critical: 'red',
    high: 'orange',
    medium: 'gold',
    low: 'blue',
  };

  const severityLabels: Record<string, string> = {
    critical: t('sla.violation.severityCritical'),
    high: t('sla.violation.severityHigh'),
    medium: t('sla.violation.severityMedium'),
    low: t('sla.violation.severityLow'),
  };

  return (
    <Modal
      title={t('sla.violation.detailTitle')}
      open={visible}
      onCancel={onClose}
      footer={
        <Space wrap>
          {canManage && !violation.isResolved && (
            <>
              <Button loading={actionLoading} onClick={() => void onAcknowledge(violation)}>
                {t('sla.violation.acknowledge')}
              </Button>
              <Button type="primary" danger loading={actionLoading} onClick={() => void onResolve(violation)}>
                {t('sla.violation.resolve')}
              </Button>
            </>
          )}
          {canDispute && (
            <Button
              onClick={() => {
                setReasonAction('dispute');
                setReason('');
              }}
            >
              {t('sla.violation.dispute')}
            </Button>
          )}
          {canSettleAccept && (
            <Button type="primary" loading={actionLoading} onClick={() => void onSettleAccepted?.(violation)}>
              {t('sla.violation.settleAccept')}
            </Button>
          )}
          {canSettleWaive && (
            <Button
              onClick={() => {
                setReasonAction('waive');
                setReason('');
              }}
            >
              {t('sla.violation.settleWaive')}
            </Button>
          )}
          <Button onClick={onClose}>{t('common.close')}</Button>
        </Space>
      }
      width={700}
    >
      <Descriptions bordered column={1} size="small">
        <Descriptions.Item label={t('sla.violation.fieldResolutionState')}>
          <Tag color={resolutionMeta.color}>{t(resolutionMeta.labelKey)}</Tag>
          {violation.settledFlag && (
            <Tag color="purple" style={{ marginLeft: 8 }}>
              {t('sla.violation.stateSettled')}
            </Tag>
          )}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldId')}>{violation.id}</Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldTicketId')}>{violation.ticketId}</Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldTicketNumber')}>
          {violation.ticketNumber || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldTicketTitle')}>
          {violation.ticketTitle || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldSlaDefId')}>{violation.slaDefinitionId}</Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldSlaName')}>
          {violation.slaName || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldViolationType')}>{violation.violationType}</Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldSeverity')}>
          <Tag color={severityColors[violation.severity] || 'default'}>
            {severityLabels[violation.severity] || violation.severity}
          </Tag>
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldStatus')}>
          <Tag color={violation.isResolved ? 'green' : 'red'}>
            {violation.isResolved ? t('sla.violation.statusResolved') : t('sla.violation.statusOpen')}
          </Tag>
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldViolationTime')}>
          {violation.violationTime ? new Date(violation.violationTime).toLocaleString() : '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldResolvedAt')}>
          {violation.resolvedAt ? new Date(violation.resolvedAt).toLocaleString() : '-'}
        </Descriptions.Item>
        {violation.disputeReason && (
          <Descriptions.Item label={t('sla.violation.fieldDisputeReason')}>
            {violation.disputeReason}
          </Descriptions.Item>
        )}
        {violation.waivedReason && (
          <Descriptions.Item label={t('sla.violation.fieldWaivedReason')}>
            {violation.waivedReason}
          </Descriptions.Item>
        )}
        <Descriptions.Item label={t('sla.violation.fieldResolutionNotes')}>
          {violation.resolutionNotes || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('sla.violation.fieldDescription')}>
          {violation.description || '-'}
        </Descriptions.Item>
      </Descriptions>

      <Modal
        title={reasonAction === 'dispute' ? t('sla.violation.disputeTitle') : t('sla.violation.settleTitleWaive')}
        open={reasonAction !== null}
        onCancel={closeReasonModal}
        onOk={() => void handleConfirmReason()}
        confirmLoading={reasonSubmitting}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
        width={520}
      >
        <TextArea
          rows={4}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          maxLength={500}
          showCount
          placeholder={
            reasonAction === 'dispute'
              ? t('sla.violation.disputePlaceholder')
              : t('sla.violation.settleWaivePlaceholder')
          }
        />
        {reasonAction === 'waive' && (
          <Alert type="info" showIcon style={{ marginTop: 8 }} message={t('sla.violation.settleWaiveHint')} />
        )}
      </Modal>
    </Modal>
  );
};

SLAViolationDetailModal.displayName = 'SLAViolationDetailModal';
