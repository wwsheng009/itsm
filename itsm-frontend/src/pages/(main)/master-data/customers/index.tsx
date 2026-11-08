import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Drawer,
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
import { useCan } from '@/components/auth/PermissionGate';
import {
  masterDataService,
  type CustomerBranch,
  type ServiceCustomer,
  type SupportContract,
} from '@/lib/services/masterDataService';
import MasterDataTabs from '../components/MasterDataTabs';

const { Title, Text } = Typography;

interface CustomerFormValues {
  name: string;
  shortName?: string;
  aliases?: string[];
  historicalNames?: string[];
  linkedCustomerTenantId?: number;
}

interface BranchFormValues {
  name: string;
  aliases?: string[];
}

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const statusTag = (status: string) =>
  status === 'active' ? <Tag color="green">活跃</Tag> : <Tag>停用</Tag>;

export default function CustomersPage() {
  const { message } = App.useApp();
  const canWrite = useCan(['customer_master:write']);
  const canReadContracts = useCan(['support_contract:read']);

  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [branches, setBranches] = useState<CustomerBranch[]>([]);
  const [contracts, setContracts] = useState<SupportContract[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [customerModal, setCustomerModal] = useState(false);
  const [branchModal, setBranchModal] = useState(false);
  const [editingCustomer, setEditingCustomer] = useState<ServiceCustomer | null>(null);
  const [detailCustomer, setDetailCustomer] = useState<ServiceCustomer | null>(null);
  const [selectedCustomer, setSelectedCustomer] = useState<ServiceCustomer | null>(null);
  const [editingBranch, setEditingBranch] = useState<CustomerBranch | null>(null);
  const [customerForm] = Form.useForm<CustomerFormValues>();
  const [branchForm] = Form.useForm<BranchFormValues>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [customerResult, branchResult] = await Promise.all([
        masterDataService.customers(),
        masterDataService.branches(),
      ]);
      setCustomers(customerResult.items);
      setBranches(branchResult.items);
      if (canReadContracts) {
        try {
          const contractResult = await masterDataService.contracts();
          setContracts(contractResult.items);
        } catch {
          setContracts([]);
        }
      }
    } catch {
      message.error('加载客户主数据失败');
    } finally {
      setLoading(false);
    }
  }, [canReadContracts, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const contractCountByCustomer = useMemo(() => {
    const result = new Map<number, number>();
    for (const contract of contracts) {
      result.set(contract.customerId, (result.get(contract.customerId) ?? 0) + 1);
    }
    return result;
  }, [contracts]);

  const filteredCustomers = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    return customers.filter(customer => {
      if (statusFilter !== 'all' && customer.status !== statusFilter) return false;
      if (!keyword) return true;
      const haystack = [
        customer.name,
        customer.shortName,
        ...(customer.aliases ?? []),
        ...(customer.historicalNames ?? []),
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return haystack.includes(keyword);
    });
  }, [customers, search, statusFilter]);

  const customerBranches = (customerId: number) =>
    branches.filter(branch => branch.customerId === customerId);

  const openCustomerModal = (customer?: ServiceCustomer) => {
    setEditingCustomer(customer ?? null);
    setCustomerModal(true);
    if (customer) {
      customerForm.setFieldsValue({
        name: customer.name,
        shortName: customer.shortName,
        aliases: customer.aliases ?? [],
        historicalNames: customer.historicalNames ?? [],
        linkedCustomerTenantId: customer.linkedCustomerTenantId ?? undefined,
      });
    } else {
      customerForm.resetFields();
    }
  };

  const saveCustomer = async () => {
    const values = await customerForm.validateFields();
    const payload = {
      name: values.name,
      shortName: values.shortName ?? '',
      aliases: values.aliases ?? [],
      historicalNames: values.historicalNames ?? [],
      status: editingCustomer?.status ?? 'active',
      linkedCustomerTenantId: values.linkedCustomerTenantId ?? null,
    };
    try {
      if (editingCustomer) {
        await masterDataService.updateCustomer(editingCustomer.id, payload);
        message.success('客户主数据已更新');
      } else {
        await masterDataService.createCustomer(payload);
        message.success('客户主数据已创建');
      }
      setCustomerModal(false);
      setEditingCustomer(null);
      customerForm.resetFields();
      await load();
    } catch {
      message.error('保存客户主数据失败');
    }
  };

  const disableCustomer = async (id: number) => {
    try {
      await masterDataService.disableCustomer(id);
      message.success('客户已停用');
      await load();
    } catch {
      message.error('停用客户失败');
    }
  };

  const openBranchModal = (customer: ServiceCustomer, branch?: CustomerBranch) => {
    setSelectedCustomer(customer);
    setEditingBranch(branch ?? null);
    setBranchModal(true);
    if (branch) {
      branchForm.setFieldsValue({
        name: branch.name,
        aliases: branch.aliases ?? [],
      });
    } else {
      branchForm.resetFields();
    }
  };

  const saveBranch = async () => {
    if (!selectedCustomer) return;
    const values = await branchForm.validateFields();
    const payload = {
      customerId: selectedCustomer.id,
      name: values.name,
      aliases: values.aliases ?? [],
      status: editingBranch?.status ?? 'active',
    };
    try {
      if (editingBranch) {
        await masterDataService.updateBranch(editingBranch.id, payload);
        message.success('分支已更新');
      } else {
        await masterDataService.createBranch(payload);
        message.success('分支已添加');
      }
      setBranchModal(false);
      setEditingBranch(null);
      branchForm.resetFields();
      await load();
    } catch {
      message.error('保存分支失败');
    }
  };

  const disableBranch = async (id: number) => {
    try {
      await masterDataService.disableBranch(id);
      message.success('分支已停用');
      await load();
    } catch {
      message.error('停用分支失败');
    }
  };

  const branchColumns: ColumnsType<CustomerBranch> = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '别名',
      dataIndex: 'aliases',
      render: (value: string[]) =>
        value?.length ? value.map(alias => <Tag key={alias}>{alias}</Tag>) : '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: statusTag,
    },
    {
      title: '操作',
      width: 150,
      render: (_, branch) =>
        canWrite ? (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => {
                const customer = customers.find(item => item.id === branch.customerId);
                if (customer) openBranchModal(customer, branch);
              }}
            >
              编辑
            </Button>
            {branch.status === 'active' && (
              <Popconfirm title="确定停用该分支？" onConfirm={() => disableBranch(branch.id)}>
                <Button size="small" danger icon={<DeleteOutlined />}>
                  停用
                </Button>
              </Popconfirm>
            )}
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  const expandedRowRender = (customer: ServiceCustomer) => (
    <div className="space-y-3">
      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="简称">{customer.shortName || '-'}</Descriptions.Item>
        <Descriptions.Item label="关联客户租户">
          {customer.linkedCustomerTenantId ? `#${customer.linkedCustomerTenantId}` : '未关联'}
        </Descriptions.Item>
        <Descriptions.Item label="别名">
          {customer.aliases?.length
            ? customer.aliases.map(alias => <Tag key={alias}>{alias}</Tag>)
            : '-'}
        </Descriptions.Item>
        <Descriptions.Item label="历史名称">
          {customer.historicalNames?.length
            ? customer.historicalNames.map(name => <Tag key={name}>{name}</Tag>)
            : '-'}
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatDate(customer.createdAt)}</Descriptions.Item>
        <Descriptions.Item label="更新时间">{formatDate(customer.updatedAt)}</Descriptions.Item>
      </Descriptions>
      <div className="flex items-center justify-between">
        <Text strong>服务分支</Text>
        {canWrite && (
          <Button
            size="small"
            icon={<PlusOutlined />}
            onClick={() => openBranchModal(customer)}
          >
            添加分支
          </Button>
        )}
      </div>
      <Table
        rowKey="id"
        size="small"
        columns={branchColumns}
        dataSource={customerBranches(customer.id)}
        pagination={false}
        locale={{ emptyText: '暂无分支' }}
      />
    </div>
  );

  const columns: ColumnsType<ServiceCustomer> = [
    {
      title: '客户名称',
      key: 'name',
      render: (_, customer) => (
        <div>
          <Text strong>{customer.name}</Text>
          <div className="text-xs text-gray-500">
            #{customer.id}
            {customer.shortName ? ` · ${customer.shortName}` : ''}
          </div>
        </div>
      ),
    },
    {
      title: '关联租户',
      dataIndex: 'linkedCustomerTenantId',
      width: 110,
      render: (value?: number | null) => (value ? `#${value}` : '-'),
    },
    {
      title: '别名',
      dataIndex: 'aliases',
      render: (value: string[]) => {
        if (!value?.length) return '-';
        return (
          <Space wrap size={[2, 2]}>
            {value.slice(0, 2).map(alias => (
              <Tag key={alias}>{alias}</Tag>
            ))}
            {value.length > 2 && <Text type="secondary">+{value.length - 2}</Text>}
          </Space>
        );
      },
    },
    {
      title: '分支数',
      key: 'branches',
      width: 90,
      render: (_, customer) => customerBranches(customer.id).length,
    },
    ...(canReadContracts
      ? [
          {
            title: '合同数',
            key: 'contracts',
            width: 90,
            render: (_: unknown, customer: ServiceCustomer) =>
              contractCountByCustomer.get(customer.id) ?? 0,
          },
        ]
      : []),
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: statusTag,
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 180,
      render: formatDate,
    },
    {
      title: '操作',
      width: 230,
      render: (_, customer) => (
        <Space>
          <Button size="small" icon={<EyeOutlined />} onClick={() => setDetailCustomer(customer)}>
            详情
          </Button>
          {canWrite && (
            <>
              <Button
                size="small"
                icon={<EditOutlined />}
                onClick={() => openCustomerModal(customer)}
              >
                编辑
              </Button>
              {customer.status === 'active' && (
                <Popconfirm
                  title="确定停用该客户？"
                  description="停用后不会删除历史合同和分支记录。"
                  onConfirm={() => disableCustomer(customer.id)}
                >
                  <Button size="small" danger icon={<DeleteOutlined />}>
                    停用
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
              客户主数据
            </Title>
            <Text type="secondary">
              客户档案是邮件报障、支持合同和运营匹配的共享主数据，不再归属邮件报障模块。
            </Text>
          </div>
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索名称、简称、别名或历史名称"
              style={{ width: 300 }}
              value={search}
              onChange={event => setSearch(event.target.value)}
            />
            <Select
              value={statusFilter}
              style={{ width: 120 }}
              onChange={setStatusFilter}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'active', label: '活跃' },
                { value: 'inactive', label: '停用' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>
              刷新
            </Button>
            {canWrite && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openCustomerModal()}>
                新增客户
              </Button>
            )}
          </Space>
        </div>

        <Alert
          className="mb-3"
          type="info"
          showIcon
          message="主数据独立管理"
          description="邮件报障只消费客户、分支和合同数据；客户主数据的维护入口已归位到 /master-data/customers。"
        />

        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={filteredCustomers}
          expandable={{ expandedRowRender }}
          pagination={{ pageSize: 20, showSizeChanger: true }}
        />
      </Card>

      <Modal
        title={editingCustomer ? '编辑客户主数据' : '新增客户主数据'}
        open={customerModal}
        onOk={saveCustomer}
        onCancel={() => {
          setCustomerModal(false);
          setEditingCustomer(null);
        }}
        width={640}
        destroyOnHidden
      >
        <Form form={customerForm} layout="vertical">
          <Form.Item name="name" label="客户名称" rules={[{ required: true }]}>
            <Input placeholder="例如：北京某某科技有限公司" />
          </Form.Item>
          <Form.Item name="shortName" label="简称">
            <Input placeholder="例如：某某科技" />
          </Form.Item>
          <Form.Item name="aliases" label="别名">
            <Select
              mode="tags"
              tokenSeparators={[',', '，']}
              placeholder="输入别名后回车，可多个"
            />
          </Form.Item>
          <Form.Item name="historicalNames" label="历史名称">
            <Select
              mode="tags"
              tokenSeparators={[',', '，']}
              placeholder="输入历史名称后回车，可多个"
            />
          </Form.Item>
          <Form.Item
            name="linkedCustomerTenantId"
            label="关联客户租户 ID"
            extra="可选；对应 tenants.id，用于把运营客户档案关联到真实客户租户。"
          >
            <InputNumber min={1} style={{ width: '100%' }} placeholder="例如：3" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={editingBranch ? '编辑服务分支' : '添加服务分支'}
        open={branchModal}
        onOk={saveBranch}
        onCancel={() => {
          setBranchModal(false);
          setEditingBranch(null);
        }}
        destroyOnHidden
      >
        <Form form={branchForm} layout="vertical">
          <Form.Item name="name" label="分支名称" rules={[{ required: true }]}>
            <Input placeholder="例如：上海分公司" />
          </Form.Item>
          <Form.Item name="aliases" label="别名">
            <Select
              mode="tags"
              tokenSeparators={[',', '，']}
              placeholder="输入别名后回车，可多个"
            />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={detailCustomer ? `客户详情 · ${detailCustomer.name}` : '客户详情'}
        open={!!detailCustomer}
        onClose={() => setDetailCustomer(null)}
        width={680}
      >
        {detailCustomer && (
          <div className="space-y-4">
            <Descriptions column={2} bordered size="small">
              <Descriptions.Item label="客户 ID">#{detailCustomer.id}</Descriptions.Item>
              <Descriptions.Item label="状态">{statusTag(detailCustomer.status)}</Descriptions.Item>
              <Descriptions.Item label="客户名称">{detailCustomer.name}</Descriptions.Item>
              <Descriptions.Item label="简称">{detailCustomer.shortName || '-'}</Descriptions.Item>
              <Descriptions.Item label="关联客户租户">
                {detailCustomer.linkedCustomerTenantId
                  ? `#${detailCustomer.linkedCustomerTenantId}`
                  : '未关联'}
              </Descriptions.Item>
              <Descriptions.Item label="合同数">
                {contractCountByCustomer.get(detailCustomer.id) ?? 0}
              </Descriptions.Item>
              <Descriptions.Item label="别名" span={2}>
                {detailCustomer.aliases?.length
                  ? detailCustomer.aliases.map(alias => <Tag key={alias}>{alias}</Tag>)
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="历史名称" span={2}>
                {detailCustomer.historicalNames?.length
                  ? detailCustomer.historicalNames.map(name => <Tag key={name}>{name}</Tag>)
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {formatDate(detailCustomer.createdAt)}
              </Descriptions.Item>
              <Descriptions.Item label="更新时间">
                {formatDate(detailCustomer.updatedAt)}
              </Descriptions.Item>
            </Descriptions>

            <div>
              <div className="flex items-center justify-between mb-2">
                <Text strong>服务分支</Text>
                {canWrite && (
                  <Button
                    size="small"
                    icon={<PlusOutlined />}
                    onClick={() => openBranchModal(detailCustomer)}
                  >
                    添加分支
                  </Button>
                )}
              </div>
              <Table
                rowKey="id"
                size="small"
                columns={branchColumns}
                dataSource={customerBranches(detailCustomer.id)}
                pagination={false}
                locale={{ emptyText: '暂无分支' }}
              />
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}
