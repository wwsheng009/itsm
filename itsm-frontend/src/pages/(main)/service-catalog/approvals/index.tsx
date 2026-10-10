
import React, { useEffect, useRef, useState } from 'react';
import { Table, Tag, Button, Card, App, Space, Modal, Input, Alert } from 'antd';
import { ServiceCatalogApi } from '@/lib/api/service-catalog-api';
import { useI18n } from '@/lib/i18n';
import { ServiceRequestStatus } from '@/types/service-catalog';
import { htmlToPlainText } from '@/lib/rich-text/sanitize';
import { useCan } from '@/components/auth/PermissionGate';

interface ServiceRequestRecord {
  id: number;
  serviceName: string;
  requesterName: string;
  createdAt: string;
  reason?: string;
}

/**
 * 待审批列表加载失败的分类：403/2003 = 当前作用域无审批权限（RBAC 拒绝）→ 页内提示，
 * 不弹「获取失败」；其余错误保留 toast。与后端空集合语义（作用域内无审批人身份返回
 * 200 空列表）互补：真无权限时给出可读提示，而不是红色报错。
 */
export function isApprovalsForbidden(error: unknown): boolean {
  const e = error as { httpStatus?: number; code?: number } | null | undefined;
  if (!e || typeof e !== 'object') return false;
  return e.httpStatus === 403 || e.code === 403 || e.code === 2003;
}

export default function ServiceApprovalsPage() {
  const { message } = App.useApp();
  const { t } = useI18n();
  // 入口级门禁：通过/驳回 → service_request:approve（与后端 service_request_routes.go 对齐）。
  const canApproveRequest = useCan(['service_request:approve']);
  const [requests, setRequests] = useState<ServiceRequestRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [forbidden, setForbidden] = useState(false);
  const [detailModalVisible, setDetailModalVisible] = useState(false);
  const [rejectModalVisible, setRejectModalVisible] = useState(false);
  const [selectedRequest, setSelectedRequest] = useState<ServiceRequestRecord | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  // 写操作防重复提交：行级 in-flight 集合拦截审批按钮快速连点，
  // actionLoadingId 驱动按钮 loading；驳回弹窗用 confirmLoading + 同步 guard。
  const inFlightRef = useRef<Set<number>>(new Set());
  const [actionLoadingId, setActionLoadingId] = useState<number | null>(null);
  const [rejectSubmitting, setRejectSubmitting] = useState(false);

  useEffect(() => {
    loadApprovals();
  }, []);

  const loadApprovals = async () => {
    setLoading(true);
    try {
      const data = await ServiceCatalogApi.getServiceRequests({
        status: ServiceRequestStatus.PENDING_APPROVAL,
      });
      setForbidden(false);
      setRequests((data.requests || []) as ServiceRequestRecord[]);
    } catch (error) {
      if (isApprovalsForbidden(error)) {
        setForbidden(true);
        setRequests([]);
      } else {
        message.error(t('common.getFailed'));
      }
    } finally {
      setLoading(false);
    }
  };

  const handleApprove = async (id: number) => {
    if (inFlightRef.current.has(id)) return;
    inFlightRef.current.add(id);
    setActionLoadingId(id);
    try {
      await ServiceCatalogApi.approveServiceRequest(id);
      message.success(t('service.approveSuccess'));
      loadApprovals();
    } catch (error) {
      message.error(t('service.approveFailed'));
    } finally {
      inFlightRef.current.delete(id);
      setActionLoadingId(null);
    }
  };

  const handleReject = async (id: number) => {
    if (!rejectReason.trim()) {
      message.error(t('service.rejectReasonRequired'));
      return;
    }
    if (rejectSubmitting) return;
    setRejectSubmitting(true);
    try {
      await ServiceCatalogApi.rejectServiceRequest(id, rejectReason);
      message.success(t('service.rejectSuccess'));
      setRejectModalVisible(false);
      loadApprovals();
    } catch (error) {
      message.error(t('service.rejectFailed'));
    } finally {
      setRejectSubmitting(false);
    }
  };

  const columns = [
    { title: t('service.requestId'), dataIndex: 'id' },
    { title: t('service.serviceName'), dataIndex: 'serviceName' },
    { title: t('service.requester'), dataIndex: 'requesterName' },
    { title: t('service.createdAt'), dataIndex: 'createdAt' },
    {
      title: t('ticketDetail.labelStatus'),
      render: (_: unknown, record: ServiceRequestRecord) => (
        <Tag color="orange">{t('service.pending')}</Tag>
      ),
    },
    {
      title: t('common.actions'),
      render: (_: unknown, record: ServiceRequestRecord) => (
        <Space>
          {canApproveRequest && (
            <Button
              size="small"
              type="primary"
              loading={actionLoadingId === record.id}
              onClick={() => handleApprove(record.id)}
            >
              {t('service.approve')}
            </Button>
          )}
          <Button
            size="small"
            onClick={() => {
              setSelectedRequest(record);
              setDetailModalVisible(true);
            }}
          >
            {t('common.view')}
          </Button>
          {canApproveRequest && (
            <Button
              size="small"
              danger
              onClick={() => {
                setSelectedRequest(record);
                setRejectModalVisible(true);
              }}
            >
              {t('service.reject')}
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="p-6">
      <Card title={t('service.pendingApprovals')}>
        {(forbidden || !canApproveRequest) && (
          <Alert
            type="warning"
            showIcon
            message={t('service.approvalsNoPermission')}
            style={{ marginBottom: 16 }}
          />
        )}
        <Table columns={columns} dataSource={requests} loading={loading} rowKey="id" />
      </Card>

      {/* Detail Modal */}
      <Modal
        title={t('service.requestDetail')}
        open={detailModalVisible}
        onCancel={() => setDetailModalVisible(false)}
        footer={null}
      >
        {selectedRequest && (
          <div>
            <p>
              <strong>{t('service.serviceName')}:</strong> {selectedRequest.serviceName}
            </p>
            <p>
              <strong>{t('service.requester')}:</strong> {selectedRequest.requesterName}
            </p>
            <p>
              <strong>{t('service.reason')}:</strong> {htmlToPlainText(selectedRequest.reason || '', 400)}
            </p>
          </div>
        )}
      </Modal>

      {/* Reject Modal */}
      <Modal
        title={t('service.rejectRequest')}
        open={rejectModalVisible}
        onCancel={() => setRejectModalVisible(false)}
        confirmLoading={rejectSubmitting}
        onOk={() => {
          if (selectedRequest?.id) handleReject(selectedRequest.id);
        }}
      >
        <p>{t('service.rejectReason')}</p>
        <Input.TextArea
          value={rejectReason}
          onChange={e => setRejectReason(e.target.value)}
          rows={4}
        />
      </Modal>
    </div>
  );
}
