import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Input,
  Popconfirm,
  Progress,
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
  MergeCellsOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { useCan } from '@/components/auth/PermissionGate';
import type { Tenant } from '@/lib/api/api-config';
import { TenantAPI } from '@/lib/api/tenant-api';
import { UserApi, type User } from '@/lib/api/user-api';
import { SLAApi, type SLADefinition } from '@/lib/api/sla-api';
import {
  masterDataService,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';
import MasterDataTabs from '../components/MasterDataTabs';
import CustomerDetailDrawer from './components/CustomerDetailDrawer';
import CustomerFormModal from './components/CustomerFormModal';
import CustomerMergeModal from './components/CustomerMergeModal';

const { Title, Text } = Typography;

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const statusTag = (status: string) =>
  status === 'active' ? <Tag color="green">活跃</Tag> : <Tag>停用</Tag>;

const tierColor = (tier: string) =>
  tier === 'S' ? 'red' : tier === 'A' ? 'orange' : tier === 'B' ? 'blue' : 'default';

const lifecycleMeta: Record<string, { label: string; color: string }> = {
  prospect: { label: '潜在客户', color: 'default' },
  onboarding: { label: '开通中', color: 'processing' },
  active: { label: '活跃', color: 'green' },
  renewal: { label: '续约中', color: 'blue' },
  churned: { label: '已流失', color: 'red' },
  archived: { label: '已归档', color: 'default' },
};

export default function CustomersPage() {
  const { message } = App.useApp();
  const canWrite = useCan(['customer_master:write']);

  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(false);
  const [keywordInput, setKeywordInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [tierFilter, setTierFilter] = useState('all');
  const [lifecycleFilter, setLifecycleFilter] = useState('all');
  const [sortBy, setSortBy] = useState<'updatedAt' | 'qualityScore' | 'name'>('updatedAt');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');
  const [qualityBelow, setQualityBelow] = useState<number | undefined>();
  const [tenantFilter, setTenantFilter] = useState('all');
  const [accountManagerFilter, setAccountManagerFilter] = useState<number | undefined>();

  const [users, setUsers] = useState<User[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [slaDefinitions, setSlaDefinitions] = useState<SLADefinition[]>([]);

  const [customerModalOpen, setCustomerModalOpen] = useState(false);
  const [editingCustomer, setEditingCustomer] = useState<ServiceCustomer | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailCustomerId, setDetailCustomerId] = useState<number | null>(null);
  const [reopenDetailId, setReopenDetailId] = useState<number | null>(null);
  const [mergeSource, setMergeSource] = useState<ServiceCustomer | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setKeyword(keywordInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [keywordInput]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await masterDataService.customers({
        keyword: keyword || undefined,
        status: statusFilter === 'all' ? undefined : statusFilter,
        tier: tierFilter === 'all' ? undefined : tierFilter,
        lifecycleStage: lifecycleFilter === 'all' ? undefined : lifecycleFilter,
        qualityBelow,
        hasTenantLink: tenantFilter === 'all' ? undefined : tenantFilter === 'linked',
        accountManagerId: accountManagerFilter,
        sortBy,
        sortOrder,
        page,
        pageSize,
      });
      setCustomers(result.items ?? []);
      setTotal(result.total ?? 0);
    } catch {
      message.error('加载客户主数据失败');
    } finally {
      setLoading(false);
    }
  }, [
    accountManagerFilter,
    keyword,
    lifecycleFilter,
    message,
    page,
    pageSize,
    qualityBelow,
    sortBy,
    sortOrder,
    statusFilter,
    tenantFilter,
    tierFilter,
  ]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    let alive = true;
    void Promise.allSettled([
      UserApi.getUsers({ page: 1, pageSize: 200 }).then(result => {
        if (alive) setUsers(result.users ?? []);
      }),
      TenantAPI.getTenants({ page: 1, size: 100 }).then(result => {
        if (alive) setTenants(result.tenants ?? []);
      }),
      SLAApi.getSLADefinitions({ page: 1, pageSize: 100, isActive: true }).then(result => {
        if (alive) setSlaDefinitions(result.items ?? []);
      }),
    ]);
    return () => {
      alive = false;
    };
  }, []);

  const openCreate = () => {
    setEditingCustomer(null);
    setCustomerModalOpen(true);
  };

  const openEdit = (customer: ServiceCustomer) => {
    setEditingCustomer(customer);
    setCustomerModalOpen(true);
  };

  const openDetail = (customer: ServiceCustomer) => {
    setDetailCustomerId(customer.id);
    setDetailOpen(true);
  };

  const openMerge = (customer: ServiceCustomer) => {
    setMergeSource(customer);
  };

  const editFromDetail = (customer: ServiceCustomer) => {
    setReopenDetailId(customer.id);
    setDetailOpen(false);
    openEdit(customer);
  };

  const disableCustomer = async (customer: ServiceCustomer) => {
    try {
      await masterDataService.disableCustomer(customer.id);
      message.success('客户已停用');
      await load();
    } catch {
      message.error('停用客户失败');
    }
  };

  const columns: ColumnsType<ServiceCustomer> = useMemo(
    () => [
      {
        title: '客户名称',
        key: 'name',
        render: (_, customer) => (
          <div>
            <Space size={6} wrap>
              <Text strong>{customer.name}</Text>
              <Tag color={tierColor(customer.tier)}>{customer.tier}</Tag>
            </Space>
            <div className="text-xs text-gray-500">
              {[`#${customer.id}`, customer.shortName, customer.legalName]
                .filter(Boolean)
                .join(' · ')}
            </div>
          </div>
        ),
      },
      {
        title: '客户编码',
        dataIndex: 'code',
        width: 120,
        render: (value?: string) => value || '-',
      },
      {
        title: '别名',
        dataIndex: 'aliases',
        width: 180,
        render: (value?: string[]) => {
          if (!value?.length) return '-';
          return (
            <Space size={[2, 2]} wrap>
              {value.slice(0, 2).map(alias => (
                <Tag key={alias}>{alias}</Tag>
              ))}
              {value.length > 2 && <Text type="secondary">+{value.length - 2}</Text>}
            </Space>
          );
        },
      },
      {
        title: '质量分',
        dataIndex: 'dataQualityScore',
        width: 90,
        render: (value?: number) => (
          <Progress
            type="circle"
            size={30}
            percent={value ?? 0}
            status={(value ?? 0) < 60 ? 'exception' : 'normal'}
            format={percent => `${percent ?? 0}`}
          />
        ),
      },
      {
        title: '客户经理',
        key: 'accountManager',
        width: 130,
        render: (_, customer) =>
          customer.accountManagerName ||
          (customer.accountManagerId ? `#${customer.accountManagerId}` : '-'),
      },
      {
        title: '关联租户',
        dataIndex: 'tenantLinkCount',
        width: 100,
        render: (value?: number) => value ?? 0,
      },
      {
        title: '联系人',
        dataIndex: 'contactCount',
        width: 90,
        render: (value?: number) => value ?? 0,
      },
      {
        title: '服务点',
        dataIndex: 'branchCount',
        width: 90,
        render: (value?: number) => value ?? 0,
      },
      {
        title: '合同',
        dataIndex: 'contractCount',
        width: 80,
        render: (value?: number) => value ?? 0,
      },
      {
        title: '生命周期',
        dataIndex: 'lifecycleStage',
        width: 110,
        render: (value: string) => {
          const meta = lifecycleMeta[value];
          return meta ? <Tag color={meta.color}>{meta.label}</Tag> : <Tag>{value}</Tag>;
        },
      },
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
        width: 310,
        render: (_, customer) => (
          <Space>
            <Button size="small" icon={<EyeOutlined />} onClick={() => openDetail(customer)}>
              详情
            </Button>
            {canWrite && (
              <>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(customer)}>
                  编辑
                </Button>
                <Button
                  size="small"
                  icon={<MergeCellsOutlined />}
                  onClick={() => openMerge(customer)}
                >
                  合并
                </Button>
                {customer.status === 'active' && (
                  <Popconfirm
                    title="确定停用该客户？"
                    description="停用后不会删除历史合同、服务点和联系人。"
                    onConfirm={() => disableCustomer(customer)}
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
    ],
    [canWrite, openDetail, openEdit]
  );

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
              维护客户主数据、关系网、项目关联、外部标识，并在客户 360 中查看服务健康与交付全景。
            </Text>
          </div>
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索名称、编码、税号、别名或历史名称"
              style={{ width: 320 }}
              value={keywordInput}
              onChange={event => setKeywordInput(event.target.value)}
            />
            <Select
              value={tierFilter}
              style={{ width: 100 }}
              onChange={value => {
                setTierFilter(value);
                setPage(1);
              }}
              options={[
                { value: 'all', label: '全部等级' },
                { value: 'S', label: 'S' },
                { value: 'A', label: 'A' },
                { value: 'B', label: 'B' },
                { value: 'C', label: 'C' },
              ]}
            />
            <Select
              value={lifecycleFilter}
              style={{ width: 130 }}
              onChange={value => {
                setLifecycleFilter(value);
                setPage(1);
              }}
              options={[
                { value: 'all', label: '全部生命周期' },
                { value: 'prospect', label: '潜在客户' },
                { value: 'onboarding', label: '开通中' },
                { value: 'active', label: '活跃' },
                { value: 'renewal', label: '续约中' },
                { value: 'churned', label: '已流失' },
                { value: 'archived', label: '已归档' },
              ]}
            />
            <Select
              value={statusFilter}
              style={{ width: 110 }}
              onChange={value => {
                setStatusFilter(value);
                setPage(1);
              }}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'active', label: '活跃' },
                { value: 'inactive', label: '停用' },
                { value: 'suspended', label: '暂停' },
              ]}
            />
            <Select
              value={qualityBelow ?? 'all'}
              style={{ width: 130 }}
              onChange={value => {
                setQualityBelow(value === 'all' ? undefined : Number(value));
                setPage(1);
              }}
              options={[
                { value: 'all', label: '全部质量分' },
                { value: 60, label: '质量分 < 60' },
                { value: 80, label: '质量分 < 80' },
              ]}
            />
            <Select
              value={tenantFilter}
              style={{ width: 130 }}
              onChange={value => {
                setTenantFilter(value);
                setPage(1);
              }}
              options={[
                { value: 'all', label: '全部租户关联' },
                { value: 'linked', label: '已关联租户' },
                { value: 'unlinked', label: '未关联租户' },
              ]}
            />
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              value={accountManagerFilter}
              style={{ width: 150 }}
              placeholder="客户经理"
              onChange={value => {
                setAccountManagerFilter(value);
                setPage(1);
              }}
              options={users.map(user => ({
                value: user.id,
                label: user.name || user.username,
              }))}
            />
            <Select
              value={`${sortBy}:${sortOrder}`}
              style={{ width: 160 }}
              onChange={value => {
                const [nextSortBy, nextSortOrder] = value.split(':') as [
                  'updatedAt' | 'qualityScore' | 'name',
                  'asc' | 'desc',
                ];
                setSortBy(nextSortBy);
                setSortOrder(nextSortOrder);
                setPage(1);
              }}
              options={[
                { value: 'updatedAt:desc', label: '最近更新' },
                { value: 'qualityScore:desc', label: '质量分从高到低' },
                { value: 'qualityScore:asc', label: '质量分从低到高' },
                { value: 'name:asc', label: '名称 A-Z' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>
              刷新
            </Button>
            {canWrite && (
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                新增客户
              </Button>
            )}
          </Space>
        </div>

        <Alert
          className="mb-3"
          type="info"
          showIcon
          message="客户主数据 P2"
          description="本页已接入主档字段、联系人、租户映射、服务点、客户关系网、项目关联、合并/撤销、质量分、外部标识对账和客户 360；AI 五级匹配已接入邮件报障链路，合同聚合根归入 C1。"
        />

        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={customers}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            showTotal: value => `共 ${value} 条`,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPageSize !== pageSize ? 1 : nextPage);
              setPageSize(nextPageSize);
            },
          }}
          scroll={{ x: 1500 }}
        />
      </Card>

      <CustomerFormModal
        open={customerModalOpen}
        customer={editingCustomer}
        users={users}
        slaDefinitions={slaDefinitions}
        onCancel={() => {
          setCustomerModalOpen(false);
          setEditingCustomer(null);
          setReopenDetailId(null);
        }}
        onSaved={async () => {
          setCustomerModalOpen(false);
          setEditingCustomer(null);
          await load();
          if (reopenDetailId) {
            setDetailCustomerId(reopenDetailId);
            setDetailOpen(true);
            setReopenDetailId(null);
          }
        }}
      />

      <CustomerDetailDrawer
        open={detailOpen}
        customerId={detailCustomerId}
        tenants={tenants}
        canWrite={canWrite}
        onClose={() => {
          setDetailOpen(false);
          setDetailCustomerId(null);
        }}
        onChanged={() => void load()}
        onEdit={editFromDetail}
        onMerge={openMerge}
      />

      <CustomerMergeModal
        open={!!mergeSource}
        source={mergeSource}
        onCancel={() => setMergeSource(null)}
        onSaved={async () => {
          const sourceId = mergeSource?.id;
          setMergeSource(null);
          await load();
          if (sourceId && detailCustomerId === sourceId) {
            setDetailOpen(false);
            setDetailCustomerId(null);
          }
        }}
      />
    </div>
  );
}