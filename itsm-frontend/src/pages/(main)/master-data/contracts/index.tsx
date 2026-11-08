import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import { useCan } from '@/components/auth/PermissionGate';
import {
  masterDataService,
  type CustomerBranch,
  type ExternalContractReference,
  type ServiceCustomer,
  type SupportContract,
} from '@/lib/services/masterDataService';
import MasterDataTabs from '../components/MasterDataTabs';
import ContractDetailDrawer from './components/ContractDetailDrawer';

const { Title, Text } = Typography;

interface ContractFormValues {
  contractNumber: string;
  customerId: number;
  branchId?: number;
  status?: SupportContract['status'];
  contractName?: string;
  contractType?: string;
  amount?: number;
  currency?: string;
  autoRenew?: boolean;
  range?: [Dayjs | null, Dayjs | null];
}

const STATUS_META: Record<string, { label: string; color: string }> = {
  active: { label: '有效', color: 'green' },
  pending: { label: '待生效', color: 'blue' },
  draft: { label: '草稿', color: 'default' },
  pending_sign: { label: '待签订', color: 'blue' },
  expiring: { label: '即将到期', color: 'orange' },
  expired: { label: '已过期', color: 'orange' },
  terminated: { label: '已终止', color: 'red' },
  renewed: { label: '已续签', color: 'purple' },
};

const formatDate = (value?: string) =>
  value ? dayjs(value).format('YYYY-MM-DD') : '-';

const remainingDays = (contract: SupportContract): number | null => {
  if (contract.remainingDays != null) return contract.remainingDays;
  const until = contract.effectiveUntil ?? contract.endAt;
  if (!until) return null;
  return dayjs(until).startOf('day').diff(dayjs().startOf('day'), 'day');
};

export default function ContractsPage() {
  const { message } = App.useApp();
  const canWrite = useCan(['support_contract:write']);

  const [contracts, setContracts] = useState<SupportContract[]>([]);
  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [branches, setBranches] = useState<CustomerBranch[]>([]);
  const [references, setReferences] = useState<ExternalContractReference[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [modalOpen, setModalOpen] = useState(false);
  const [editingContract, setEditingContract] = useState<SupportContract | null>(null);
  const [detailContract, setDetailContract] = useState<SupportContract | null>(null);
  const [form] = Form.useForm<ContractFormValues>();
  const selectedCustomerId = Form.useWatch('customerId', form);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [contractResult, customerResult, branchResult, referenceResult] =
        await Promise.all([
          masterDataService.contracts(),
          masterDataService.customers({ page: 1, pageSize: 200 }),
          masterDataService.branches(),
          masterDataService.externalContractReferences(),
        ]);
      setContracts(contractResult.items);
      setCustomers(customerResult.items);
      setBranches(branchResult.items);
      setReferences(referenceResult.items);
    } catch {
      message.error('加载支持合同失败');
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    void load();
  }, [load]);

  const customerName = (id: number) =>
    customers.find(customer => customer.id === id)?.name ?? `#${id}`;
  const branchName = (id?: number) =>
    id ? branches.find(branch => branch.id === id)?.name ?? `#${id}` : '-';
  const referencesByContract = useMemo(() => {
    const result = new Map<number, ExternalContractReference[]>();
    for (const reference of references) {
      const items = result.get(reference.supportContractId) ?? [];
      items.push(reference);
      result.set(reference.supportContractId, items);
    }
    return result;
  }, [references]);

  const filteredContracts = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    return contracts.filter(contract => {
      if (statusFilter !== 'all' && contract.status !== statusFilter) return false;
      if (!keyword) return true;
      return [contract.contractNumber, customerName(contract.customerId), branchName(contract.branchId)]
        .join(' ')
        .toLowerCase()
        .includes(keyword);
    });
  }, [branches, contracts, customers, search, statusFilter]);

  const openContractModal = (contract?: SupportContract) => {
    setEditingContract(contract ?? null);
    setModalOpen(true);
    if (contract) {
      form.setFieldsValue({
        contractNumber: contract.contractNumber,
        customerId: contract.customerId,
        branchId: contract.branchId,
        status: contract.status,
        contractName: contract.contractName,
        contractType: contract.contractType,
        amount: contract.amount,
        currency: contract.currency,
        autoRenew: contract.autoRenew,
        range: [
          contract.effectiveFrom ?? contract.startAt
            ? dayjs(contract.effectiveFrom ?? contract.startAt)
            : null,
          contract.effectiveUntil ?? contract.endAt
            ? dayjs(contract.effectiveUntil ?? contract.endAt)
            : null,
        ],
      });
    } else {
      form.resetFields();
      form.setFieldsValue({ status: 'draft', currency: 'CNY', autoRenew: false });
    }
  };

  const saveContract = async () => {
    const values = await form.validateFields();
    const payload = {
      contractNumber: values.contractNumber,
      customerId: values.customerId,
      branchId: values.branchId,
      status: values.status ?? editingContract?.status ?? 'draft',
      contractName: values.contractName,
      contractType: values.contractType ?? 'support',
      amount: values.amount,
      currency: values.currency ?? 'CNY',
      autoRenew: values.autoRenew ?? false,
      startAt: values.range?.[0]?.toISOString(),
      endAt: values.range?.[1]?.toISOString(),
      effectiveFrom: values.range?.[0]?.toISOString(),
      effectiveUntil: values.range?.[1]?.toISOString(),
      version: editingContract?.version,
    };
    try {
      if (editingContract) {
        await masterDataService.updateContract(editingContract.id, payload);
        message.success('支持合同已更新');
      } else {
        await masterDataService.createContract(payload);
        message.success('支持合同已创建');
      }
      setModalOpen(false);
      setEditingContract(null);
      form.resetFields();
      await load();
    } catch {
      message.error('保存支持合同失败');
    }
  };

  const terminateContract = async (id: number) => {
    try {
      await masterDataService.terminateContract(id);
      message.success('支持合同已终止');
      await load();
    } catch {
      message.error('终止支持合同失败');
    }
  };

  const contractStatusTag = (contract: SupportContract) => {
    const remaining = remainingDays(contract);
    if (
      contract.status === 'active' &&
      remaining !== null &&
      remaining >= 0 &&
      remaining <= 30
    ) {
      return <Tag color="warning">即将到期</Tag>;
    }
    const value = contract.displayStatus ?? contract.status;
    const meta = STATUS_META[value] ?? {
      label: value,
      color: 'default',
    };
    return <Tag color={meta.color}>{meta.label}</Tag>;
  };

  const subjectBranches = branches.filter(
    branch => branch.customerId === selectedCustomerId
  );

  const columns: ColumnsType<SupportContract> = [
    {
      title: '合同号',
      key: 'contract',
      render: (_, contract) => (
        <div>
          <Text strong>{contract.contractNumber}</Text>
          <div className="text-xs text-gray-500">
            #{contract.id} · {contract.contractName || '未命名合同'}
          </div>
        </div>
      ),
    },
    {
      title: '类型',
      dataIndex: 'contractType',
      width: 100,
      render: (value?: string) => <Tag>{value || 'support'}</Tag>,
    },
    {
      title: '客户',
      dataIndex: 'customerId',
      render: (id: number) => customerName(id),
    },
    {
      title: '分支',
      dataIndex: 'branchId',
      render: (id?: number) => branchName(id),
    },
    {
      title: '状态',
      key: 'status',
      width: 110,
      render: (_, contract) => contractStatusTag(contract),
    },
    {
      title: '有效期',
      key: 'period',
      width: 210,
      render: (_, contract) =>
        `${formatDate(contract.effectiveFrom ?? contract.startAt)} ~ ${
          contract.effectiveUntil ?? contract.endAt
            ? formatDate(contract.effectiveUntil ?? contract.endAt)
            : '长期'
        }`,
    },
    {
      title: '剩余',
      key: 'remaining',
      width: 100,
      render: (_, contract) => {
        const remaining = remainingDays(contract);
        if (remaining === null) return '长期';
        if (remaining < 0) return <Text type="danger">已过期 {Math.abs(remaining)} 天</Text>;
        return `${remaining} 天`;
      },
    },
    {
      title: '合同额 / 已分配 / 已实耗',
      key: 'amounts',
      width: 210,
      render: (_, contract) => (
        <div className="text-xs">
          <div>
            原始 {contract.amount == null ? '-' : Number(contract.amount).toFixed(2)}
            {contract.currency ? ` ${contract.currency}` : ''}
          </div>
          <div>
            有效 {contract.effectiveAmount == null ? '-' : Number(contract.effectiveAmount).toFixed(2)}
          </div>
          <div>
            分配 {contract.allocatedAmount ?? 0} / 实耗 {contract.usedAmount ?? 0}
          </div>
        </div>
      ),
    },
    {
      title: '外部映射',
      key: 'references',
      width: 100,
      render: (_, contract) => referencesByContract.get(contract.id)?.length ?? 0,
    },
    {
      title: '操作',
      width: 240,
      render: (_, contract) => (
        <Space>
          <Button
            size="small"
            icon={<EyeOutlined />}
            onClick={() => setDetailContract(contract)}
          >
            详情
          </Button>
          {canWrite && (
            <>
              <Button
                size="small"
                icon={<EditOutlined />}
                onClick={() => openContractModal(contract)}
              >
                编辑
              </Button>
              {contract.status === 'active' && (
                <Popconfirm
                  title="终止该支持合同？"
                  description="终止不会删除历史记录；外部合同映射仍会保留。"
                  onConfirm={() => terminateContract(contract.id)}
                >
                  <Button size="small" danger icon={<DeleteOutlined />}>
                    终止
                  </Button>
                </Popconfirm>
              )}
            </>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="space-y-4">
      <MasterDataTabs />
      <Card>
        <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
          <div>
            <Title level={4} className="!mb-1">
              支持合同
            </Title>
            <Text type="secondary">
              支持合同是客户主数据的一部分，用于服务有效期、分支归属和外部合同号映射。
            </Text>
          </div>
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索合同号、客户或分支"
              style={{ width: 280 }}
              value={search}
              onChange={event => setSearch(event.target.value)}
            />
            <Select
              value={statusFilter}
              style={{ width: 120 }}
              onChange={setStatusFilter}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'draft', label: '草稿' },
                { value: 'pending_sign', label: '待签订' },
                { value: 'active', label: '有效' },
                { value: 'pending', label: '待生效' },
                { value: 'expired', label: '已过期' },
                { value: 'terminated', label: '已终止' },
                { value: 'renewed', label: '已续签' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>
              刷新
            </Button>
            {canWrite && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openContractModal()}>
                新增合同
              </Button>
            )}
          </Space>
        </div>

        <Alert
          className="mb-3"
          type="info"
          showIcon
          message="支持合同与 MSP SLA 合同不同"
          description="本页维护客户支持合同聚合根（合同额、服务项、变更单、用量台账、项目分配与外部映射）；provider 的 SLA 条款与结算合同在 /admin/sla-contracts。"
        />

        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={filteredContracts}
          pagination={{ pageSize: 20, showSizeChanger: true }}
        />
      </Card>

      <Modal
        title={editingContract ? '编辑支持合同' : '新增支持合同'}
        open={modalOpen}
        onOk={saveContract}
        onCancel={() => {
          setModalOpen(false);
          setEditingContract(null);
        }}
        width={680}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="contractNumber" label="合同号" rules={[{ required: true }]}>
            <Input placeholder="例如：HT-2026-001" />
          </Form.Item>
          <Form.Item name="contractName" label="合同名称">
            <Input placeholder="例如：2026 年度运维支持合同" maxLength={255} />
          </Form.Item>
          <Space size="middle" wrap>
            <Form.Item name="contractType" label="合同类型">
              <Select
                style={{ width: 160 }}
                options={[
                  { value: 'support', label: 'support · 支持' },
                  { value: 'maintenance', label: 'maintenance · 运维' },
                  { value: 'implementation', label: 'implementation · 实施' },
                  { value: 'framework', label: 'framework · 框架' },
                ]}
              />
            </Form.Item>
            <Form.Item name="amount" label="合同授权额">
              <InputNumber min={0} precision={2} style={{ width: 160 }} />
            </Form.Item>
            <Form.Item name="currency" label="币种">
              <Input style={{ width: 100 }} maxLength={8} />
            </Form.Item>
          </Space>
          <Form.Item name="customerId" label="客户" rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择客户主数据"
              options={customers.map(customer => ({
                value: customer.id,
                label: `${customer.name}（#${customer.id}）`,
              }))}
              onChange={() => form.setFieldValue('branchId', undefined)}
            />
          </Form.Item>
          <Form.Item name="branchId" label="服务分支">
            <Select
              allowClear
              placeholder="可选"
              options={subjectBranches.map(branch => ({
                value: branch.id,
                label: branch.name,
              }))}
            />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select
              options={[
                { value: 'draft', label: '草稿' },
                { value: 'pending_sign', label: '待签订' },
                { value: 'active', label: '有效' },
                { value: 'pending', label: '待生效' },
                { value: 'expired', label: '已过期' },
                { value: 'terminated', label: '已终止' },
                { value: 'renewed', label: '已续签' },
              ]}
            />
          </Form.Item>
          <Form.Item name="range" label="有效期">
            <DatePicker.RangePicker className="w-full" />
          </Form.Item>
          <Form.Item name="autoRenew" valuePropName="checked">
            <Checkbox>自动续约</Checkbox>
          </Form.Item>
        </Form>
      </Modal>

      <ContractDetailDrawer
        open={!!detailContract}
        contract={detailContract}
        customers={customers}
        references={
          detailContract ? (referencesByContract.get(detailContract.id) ?? []) : []
        }
        canWrite={canWrite}
        onClose={() => setDetailContract(null)}
        onChanged={() => void load()}
        onEdit={contract => {
          setDetailContract(null);
          openContractModal(contract);
        }}
      />
    </div>
  );
}
