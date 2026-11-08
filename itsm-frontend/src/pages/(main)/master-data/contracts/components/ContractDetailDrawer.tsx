import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  App,
  Button,
  DatePicker,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Progress,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd';
import {
  CheckCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import {
  masterDataService,
  type ContractBalance,
  type ContractProjectLink,
  type ExternalContractReference,
  type ServiceCustomer,
  type SupportContract,
} from '@/lib/services/masterDataService';
import ContractAmendmentsPanel from './ContractAmendmentsPanel';
import ContractItemsPanel from './ContractItemsPanel';
import ContractProjectsPanel from './ContractProjectsPanel';
import ContractUsagePanel from './ContractUsagePanel';

const { Text } = Typography;

const statusMeta: Record<string, { label: string; color: string }> = {
  draft: { label: '草稿', color: 'default' },
  pending_sign: { label: '待签订', color: 'blue' },
  pending: { label: '待生效', color: 'blue' },
  active: { label: '有效', color: 'green' },
  expiring: { label: '即将到期', color: 'orange' },
  expired: { label: '已过期', color: 'orange' },
  terminated: { label: '已终止', color: 'red' },
  renewed: { label: '已续签', color: 'purple' },
};

const formatDate = (value?: string) => (value ? dayjs(value).format('YYYY-MM-DD') : '-');
const formatMoney = (value?: number | null) =>
  value == null ? '-' : Number(value).toLocaleString('zh-CN', { minimumFractionDigits: 2 });

interface TerminateFormValues {
  reason: string;
}

interface RenewFormValues {
  contractNumber: string;
  range: [Dayjs | null, Dayjs | null];
  amount?: number;
  carryProjectIds?: number[];
}

interface ContractDetailDrawerProps {
  open: boolean;
  contract: SupportContract | null;
  customers: ServiceCustomer[];
  references: ExternalContractReference[];
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onEdit: (contract: SupportContract) => void;
}

export default function ContractDetailDrawer({
  open,
  contract,
  customers,
  references,
  canWrite,
  onClose,
  onChanged,
  onEdit,
}: ContractDetailDrawerProps) {
  const { message } = App.useApp();
  const [detail, setDetail] = useState<SupportContract | null>(null);
  const [balance, setBalance] = useState<ContractBalance | null>(null);
  const [projects, setProjects] = useState<ContractProjectLink[]>([]);
  const [loading, setLoading] = useState(false);
  const [terminateOpen, setTerminateOpen] = useState(false);
  const [renewOpen, setRenewOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [terminateForm] = Form.useForm<TerminateFormValues>();
  const [renewForm] = Form.useForm<RenewFormValues>();

  const load = useCallback(async () => {
    if (!contract) return;
    setLoading(true);
    try {
      const [detailResult, balanceResult, projectResult] = await Promise.all([
        masterDataService.contractDetail(contract.id),
        masterDataService.contractBalance(contract.id),
        masterDataService.contractProjects(contract.id),
      ]);
      setDetail(detailResult);
      setBalance(balanceResult);
      setProjects(projectResult.items ?? []);
    } catch {
      message.error('加载合同详情失败');
    } finally {
      setLoading(false);
    }
  }, [contract, message]);

  useEffect(() => {
    if (open && contract) {
      void load();
    } else if (!open) {
      setDetail(null);
      setBalance(null);
      setProjects([]);
    }
  }, [contract, load, open]);

  const afterMutation = async () => {
    await load();
    onChanged();
  };

  const sign = async () => {
    if (!detail) return;
    setActionLoading(true);
    try {
      await masterDataService.signContract(detail.id);
      message.success('合同已签订');
      await afterMutation();
    } catch (error) {
      message.error((error as Error).message || '合同签订失败');
    } finally {
      setActionLoading(false);
    }
  };

  const terminate = async () => {
    if (!detail) return;
    const values = await terminateForm.validateFields();
    setActionLoading(true);
    try {
      await masterDataService.terminateContractWithReason(detail.id, values.reason);
      message.success('合同已终止');
      setTerminateOpen(false);
      await afterMutation();
    } catch (error) {
      message.error((error as Error).message || '合同终止失败');
    } finally {
      setActionLoading(false);
    }
  };

  const renew = async () => {
    if (!detail) return;
    const values = await renewForm.validateFields();
    setActionLoading(true);
    try {
      await masterDataService.renewContract(detail.id, {
        contractNumber: values.contractNumber,
        effectiveFrom: values.range?.[0]?.toISOString(),
        effectiveUntil: values.range?.[1]?.toISOString(),
        amount: values.amount,
        currency: detail.currency,
        carryProjectIds: values.carryProjectIds,
      });
      message.success('续签合同已生成');
      setRenewOpen(false);
      await afterMutation();
    } catch (error) {
      message.error((error as Error).message || '合同续签失败');
    } finally {
      setActionLoading(false);
    }
  };

  const openRenew = () => {
    if (!detail) return;
    renewForm.resetFields();
    renewForm.setFieldsValue({
      contractNumber: `${detail.contractNumber}-R`,
      range: [
        detail.effectiveUntil ? dayjs(detail.effectiveUntil) : dayjs(),
        detail.effectiveUntil ? dayjs(detail.effectiveUntil).add(1, 'year') : dayjs().add(1, 'year'),
      ],
      amount: detail.amount ?? undefined,
      carryProjectIds: [],
    });
    setRenewOpen(true);
  };

  const customerName = (id: number) =>
    customers.find(customer => customer.id === id)?.name ?? `#${id}`;

  const referenceColumns: ColumnsType<ExternalContractReference> = [
    { title: '外部合同号', dataIndex: 'externalContractNumber' },
    { title: '来源组织', dataIndex: 'sourceOrganizationId', render: (id: number) => `#${id}` },
    { title: '创建时间', dataIndex: 'createdAt', render: (value: string) => formatDate(value) },
  ];

  const current = detail ?? contract;
  const displayStatus = current?.displayStatus ?? current?.status ?? '';
  const status = statusMeta[displayStatus] ?? { label: displayStatus, color: 'default' };
  const progressBase = balance?.effectiveAmount ?? balance?.amount ?? 0;
  const committed = (balance?.allocatedAmount ?? 0) + (balance?.usedAmount ?? 0);
  const progressPercent = progressBase > 0 ? Math.min(100, (committed / progressBase) * 100) : 0;

  const overview = current ? (
    <div className="space-y-4">
      <Alert
        type="info"
        showIcon
        message="合同聚合根"
        description="合同额、服务项、变更单、用量台账与项目分配分开展示；预占额度与实耗额度不混用。"
      />
      <Descriptions bordered size="small" column={2}>
        <Descriptions.Item label="合同号">{current.contractNumber}</Descriptions.Item>
        <Descriptions.Item label="合同名称">{current.contractName || '-'}</Descriptions.Item>
        <Descriptions.Item label="合同类型">{current.contractType || 'support'}</Descriptions.Item>
        <Descriptions.Item label="客户">{customerName(current.customerId)}</Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={status.color}>{status.label}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="剩余天数">{current.remainingDays ?? '长期'}</Descriptions.Item>
        <Descriptions.Item label="合同原始额">
          {formatMoney(current.amount)} {current.currency}
        </Descriptions.Item>
        <Descriptions.Item label="有效合同额">
          {formatMoney(balance?.effectiveAmount ?? current.effectiveAmount)} {current.currency}
        </Descriptions.Item>
        <Descriptions.Item label="生效日期">
          {formatDate(current.effectiveFrom ?? current.startAt)}
        </Descriptions.Item>
        <Descriptions.Item label="截止日期">
          {current.effectiveUntil ? formatDate(current.effectiveUntil) : '长期'}
        </Descriptions.Item>
        <Descriptions.Item label="签订时间">{formatDate(current.signedAt)}</Descriptions.Item>
        <Descriptions.Item label="变更单数">{current.amendmentCount ?? 0}</Descriptions.Item>
        <Descriptions.Item label="自动续约">{current.autoRenew ? '是' : '否'}</Descriptions.Item>
        <Descriptions.Item label="版本">v{current.version ?? 1}</Descriptions.Item>
      </Descriptions>

      <div className="rounded border border-gray-200 p-3">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <Text strong>额度视图</Text>
          <Space wrap size={[16, 4]}>
            <Text type="secondary">已分配 {formatMoney(balance?.allocatedAmount)}</Text>
            <Text type="secondary">已实耗 {formatMoney(balance?.usedAmount)}</Text>
            <Text type="secondary">剩余可消耗 {formatMoney(balance?.remainingAmount)}</Text>
            <Text type="secondary">可再分配 {formatMoney(balance?.availableAmount)}</Text>
          </Space>
        </div>
        <Progress
          percent={Math.round(progressPercent)}
          status={progressBase > 0 && committed > progressBase ? 'exception' : 'normal'}
          format={() => `${progressPercent.toFixed(1)}%`}
        />
        <Text type="secondary">
          预占 = 有效 billing 行分配额；实耗 = 用量台账累计。二者分开统计。
        </Text>
      </div>

      <div>
        <Text strong>外部合同号映射</Text>
        <Table
          className="mt-2"
          rowKey="id"
          size="small"
          columns={referenceColumns}
          dataSource={references}
          pagination={false}
          locale={{ emptyText: '暂无外部合同号映射' }}
        />
      </div>
    </div>
  ) : null;

  return (
    <>
      <Drawer
        title={
          current ? (
            <Space wrap>
              <span>{current.contractNumber}</span>
              <Tag color={status.color}>{status.label}</Tag>
              {current.contractType && <Tag>{current.contractType}</Tag>}
            </Space>
          ) : (
            '支持合同详情'
          )
        }
        open={open}
        onClose={onClose}
        width={1080}
        loading={loading}
        extra={
          current && canWrite ? (
            <Space wrap>
              <Button icon={<ReloadOutlined />} onClick={() => void load()}>
                刷新
              </Button>
              <Button icon={<EditOutlined />} onClick={() => onEdit(current)}>
                编辑
              </Button>
              {(current.status === 'draft' ||
                current.status === 'pending_sign' ||
                current.status === 'pending') && (
                <Popconfirm title="签订合同？" onConfirm={() => void sign()}>
                  <Button
                    type="primary"
                    icon={<CheckCircleOutlined />}
                    loading={actionLoading}
                  >
                    签订
                  </Button>
                </Popconfirm>
              )}
              {current.status !== 'terminated' && current.status !== 'renewed' && (
                <Button onClick={openRenew} loading={actionLoading}>
                  续签
                </Button>
              )}
              {current.status !== 'terminated' && current.status !== 'renewed' && (
                <Button
                  danger
                  icon={<DeleteOutlined />}
                  onClick={() => {
                    terminateForm.resetFields();
                    setTerminateOpen(true);
                  }}
                >
                  终止
                </Button>
              )}
            </Space>
          ) : null
        }
      >
        {current && (
          <div className="space-y-4">
            <Tabs
              items={[
                { key: 'overview', label: '概览', children: overview },
                {
                  key: 'items',
                  label: '服务项',
                  children: (
                    <ContractItemsPanel
                      contractId={current.id}
                      contractStatus={current.status}
                      canWrite={canWrite}
                      onChanged={() => void afterMutation()}
                    />
                  ),
                },
                {
                  key: 'amendments',
                  label: '变更历史',
                  children: (
                    <ContractAmendmentsPanel
                      contractId={current.id}
                      canWrite={canWrite}
                      onChanged={() => void afterMutation()}
                    />
                  ),
                },
                {
                  key: 'usage',
                  label: '用量台账',
                  children: (
                    <ContractUsagePanel
                      contractId={current.id}
                      canWrite={canWrite}
                      onChanged={() => void afterMutation()}
                    />
                  ),
                },
                {
                  key: 'projects',
                  label: `关联项目 (${projects.length})`,
                  children: (
                    <ContractProjectsPanel
                      contractId={current.id}
                      canWrite={canWrite}
                      onChanged={() => void afterMutation()}
                    />
                  ),
                },
              ]}
            />
          </div>
        )}
      </Drawer>

      <Modal
        title="终止支持合同"
        open={terminateOpen}
        onCancel={() => setTerminateOpen(false)}
        onOk={() => void terminate()}
        confirmLoading={actionLoading}
        okButtonProps={{ danger: true }}
        destroyOnHidden
      >
        <Form form={terminateForm} layout="vertical">
          <Form.Item
            name="reason"
            label="终止原因"
            rules={[{ required: true, min: 5, message: '请填写至少 5 个字符的原因' }]}
          >
            <Input.TextArea rows={4} maxLength={255} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="续签支持合同"
        open={renewOpen}
        onCancel={() => setRenewOpen(false)}
        onOk={() => void renew()}
        confirmLoading={actionLoading}
        width={620}
        destroyOnHidden
      >
        <Form form={renewForm} layout="vertical">
          <Form.Item name="contractNumber" label="新合同号" rules={[{ required: true }]}>
            <Input maxLength={160} />
          </Form.Item>
          <Form.Item name="range" label="新有效期" rules={[{ required: true }]}>
            <DatePicker.RangePicker className="w-full" />
          </Form.Item>
          <Form.Item name="amount" label="新合同额">
            <InputNumber min={0} precision={2} className="w-full" />
          </Form.Item>
          <Form.Item
            name="carryProjectIds"
            label="结转关联项目"
            extra="仅结转所选项目的 active 关联；旧合同项目链路会保留 ended 历史。"
          >
            <Select
              mode="multiple"
              options={projects
                .filter(link => link.relationType !== 'billing')
                .map(link => ({
                  value: link.projectId,
                  label: `${link.projectName || `#${link.projectId}`}（${link.relationType}）`,
                }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
