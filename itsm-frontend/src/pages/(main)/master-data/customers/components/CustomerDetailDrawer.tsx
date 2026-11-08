import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Button,
  Descriptions,
  Drawer,
  Popconfirm,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, EditOutlined, MergeCellsOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { Tenant } from '@/lib/api/api-config';
import {
  masterDataService,
  type CustomerBranch,
  type CustomerContact,
  type CustomerDetailResponse,
  type CustomerTenantLink,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';
import CustomerBranchModal from './CustomerBranchModal';
import CustomerContactModal from './CustomerContactModal';
import Customer360Panel from './Customer360Panel';
import CustomerExternalIdsPanel from './CustomerExternalIdsPanel';
import CustomerMergeHistoryPanel from './CustomerMergeHistoryPanel';
import CustomerProjectsPanel from './CustomerProjectsPanel';
import CustomerRelationsPanel from './CustomerRelationsPanel';
import CustomerTenantLinkModal from './CustomerTenantLinkModal';

const { Text } = Typography;

interface CustomerDetailDrawerProps {
  open: boolean;
  customerId: number | null;
  tenants: Tenant[];
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onEdit: (customer: ServiceCustomer) => void;
  onMerge: (customer: ServiceCustomer) => void;
}

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const formatAddress = (value?: Record<string, unknown>) => {
  if (!value) return '-';
  const parts = ['country', 'province', 'city', 'district', 'street', 'postalCode']
    .map(key => value[key])
    .filter(Boolean);
  return parts.length ? parts.join(' ') : '-';
};

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

const contactRoleLabel: Record<string, string> = {
  technical: '技术',
  business: '业务',
  billing: '财务',
  escalation: '升级投诉',
  noc: '值班/监控',
  executive: '高管',
};

export default function CustomerDetailDrawer({
  open,
  customerId,
  tenants,
  canWrite,
  onClose,
  onChanged,
  onEdit,
  onMerge,
}: CustomerDetailDrawerProps) {
  const { message } = App.useApp();
  const [detail, setDetail] = useState<CustomerDetailResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [contactModal, setContactModal] = useState<{
    open: boolean;
    contact: CustomerContact | null;
  }>({ open: false, contact: null });
  const [linkModal, setLinkModal] = useState<{
    open: boolean;
    link: CustomerTenantLink | null;
  }>({ open: false, link: null });
  const [branchModal, setBranchModal] = useState<{
    open: boolean;
    branch: CustomerBranch | null;
  }>({ open: false, branch: null });

  const reload = useCallback(async () => {
    if (!customerId) return;
    setLoading(true);
    try {
      const result = await masterDataService.customerDetail(customerId);
      setDetail(result);
    } catch {
      message.error('加载客户详情失败');
    } finally {
      setLoading(false);
    }
  }, [customerId, message]);

  useEffect(() => {
    if (open && customerId) {
      void reload();
    } else if (!open) {
      setDetail(null);
    }
  }, [customerId, open, reload]);

  const tenantName = (id: number) => {
    const tenant = tenants.find(item => item.id === id);
    return tenant ? `${tenant.name}（${tenant.code}）` : `#${id}`;
  };

  const afterMutation = async () => {
    await reload();
    onChanged();
  };

  const deleteContact = async (contact: CustomerContact) => {
    if (!customerId) return;
    try {
      await masterDataService.deleteContact(customerId, contact.id);
      message.success('联系人已删除');
      await afterMutation();
    } catch {
      message.error('删除联系人失败');
    }
  };

  const deleteLink = async (link: CustomerTenantLink) => {
    if (!customerId) return;
    try {
      await masterDataService.deleteTenantLink(customerId, link.id);
      message.success('租户关联已删除');
      await afterMutation();
    } catch {
      message.error('删除租户关联失败');
    }
  };

  const disableBranch = async (branch: CustomerBranch) => {
    try {
      await masterDataService.disableBranch(branch.id);
      message.success('服务点已停用');
      await afterMutation();
    } catch {
      message.error('停用服务点失败');
    }
  };

  const disableCustomer = async () => {
    if (!detail) return;
    try {
      await masterDataService.disableCustomer(detail.customer.id);
      message.success('客户已停用');
      await afterMutation();
    } catch {
      message.error('停用客户失败');
    }
  };

  const contactColumns: ColumnsType<CustomerContact> = [
    {
      title: '姓名',
      key: 'name',
      render: (_, contact) => (
        <div>
          <Text strong>{contact.name}</Text>
          <div className="text-xs text-gray-500">
            {contact.title || '-'} · {contact.department || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '角色',
      dataIndex: 'contactRole',
      width: 110,
      render: (role: string) => contactRoleLabel[role] ?? role,
    },
    {
      title: '服务点',
      dataIndex: 'branchId',
      width: 130,
      render: (branchId?: number | null) =>
        branchId
          ? (detail?.branches?.find(branch => branch.id === branchId)?.name ?? `#${branchId}`)
          : '-',
    },
    {
      title: '联系方式',
      key: 'contact',
      render: (_, contact) => (
        <div className="text-xs">
          <div>{contact.email || '-'}</div>
          <div>{contact.mobile || contact.phone || '-'}</div>
        </div>
      ),
    },
    {
      title: '标记',
      key: 'flags',
      width: 140,
      render: (_, contact) => (
        <Space size={[2, 2]} wrap>
          {contact.isPrimary && <Tag color="blue">主要</Tag>}
          {!contact.optIn && <Tag>不接收通知</Tag>}
          {contact.status !== 'active' && <Tag>停用</Tag>}
        </Space>
      ),
    },
    {
      title: '操作',
      width: 150,
      render: (_, contact) =>
        canWrite ? (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => setContactModal({ open: true, contact })}
            >
              编辑
            </Button>
            <Popconfirm title="删除该联系人？" onConfirm={() => deleteContact(contact)}>
              <Button size="small" danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  const linkColumns: ColumnsType<CustomerTenantLink> = [
    {
      title: '目标租户',
      dataIndex: 'linkedTenantId',
      render: (id: number) => tenantName(id),
    },
    {
      title: '关联类型',
      dataIndex: 'linkType',
      width: 110,
      render: (value: string) => <Tag>{value}</Tag>,
    },
    {
      title: '结算锚点',
      dataIndex: 'isBillingAnchor',
      width: 100,
      render: (value: boolean) => (value ? <Tag color="gold">是</Tag> : '-'),
    },
    {
      title: '开通状态',
      dataIndex: 'provisionStatus',
      width: 110,
      render: (value: string) => <Tag>{value}</Tag>,
    },
    { title: '备注', dataIndex: 'note', render: (value?: string) => value || '-' },
    {
      title: '操作',
      width: 150,
      render: (_, link) =>
        canWrite ? (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => setLinkModal({ open: true, link })}
            >
              编辑
            </Button>
            <Popconfirm title="删除该租户关联？" onConfirm={() => deleteLink(link)}>
              <Button size="small" danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  const branchColumns: ColumnsType<CustomerBranch> = [
    {
      title: '服务点',
      key: 'name',
      render: (_, branch) => (
        <div>
          <Text strong>{branch.name}</Text>
          <div className="text-xs text-gray-500">
            {branch.code || '-'} · {branch.branchType || 'site'}
          </div>
        </div>
      ),
    },
    {
      title: '地址',
      dataIndex: 'address',
      render: (value?: Record<string, unknown>) => formatAddress(value),
    },
    {
      title: '联系方式',
      key: 'contact',
      render: (_, branch) => (
        <div className="text-xs">
          <div>{branch.contactPhone || '-'}</div>
          <div>{branch.timezone || '-'}</div>
        </div>
      ),
    },
    {
      title: '标记',
      key: 'flags',
      width: 120,
      render: (_, branch) => (
        <Space size={[2, 2]} wrap>
          {branch.isPrimary && <Tag color="blue">主服务点</Tag>}
          {statusTag(branch.status)}
        </Space>
      ),
    },
    {
      title: '操作',
      width: 170,
      render: (_, branch) =>
        canWrite ? (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => setBranchModal({ open: true, branch })}
            >
              编辑
            </Button>
            {branch.status === 'active' && (
              <Popconfirm title="停用该服务点？" onConfirm={() => disableBranch(branch)}>
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

  const customer = detail?.customer;
  const lifecycle = customer ? lifecycleMeta[customer.lifecycleStage] : undefined;

  const overview = useMemo(() => {
    if (!customer) return null;
    return (
      <div className="space-y-4">
        <Descriptions title="基本信息" bordered size="small" column={2}>
          <Descriptions.Item label="客户编码">{customer.code || '-'}</Descriptions.Item>
          <Descriptions.Item label="客户名称">{customer.name}</Descriptions.Item>
          <Descriptions.Item label="简称">{customer.shortName || '-'}</Descriptions.Item>
          <Descriptions.Item label="法定全称">{customer.legalName || '-'}</Descriptions.Item>
          <Descriptions.Item label="英文名">{customer.enName || '-'}</Descriptions.Item>
          <Descriptions.Item label="统一社会信用代码 / 税号">{customer.taxId || '-'}</Descriptions.Item>
          <Descriptions.Item label="工商注册号">{customer.registrationNo || '-'}</Descriptions.Item>
          <Descriptions.Item label="邓白氏编码">{customer.dunsNumber || '-'}</Descriptions.Item>
          <Descriptions.Item label="客户类型">{customer.customerType}</Descriptions.Item>
          <Descriptions.Item label="等级">
            <Tag color={tierColor(customer.tier)}>{customer.tier}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="生命周期">
            {lifecycle ? <Tag color={lifecycle.color}>{lifecycle.label}</Tag> : customer.lifecycleStage}
          </Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(customer.status)}</Descriptions.Item>
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
        </Descriptions>

        <Descriptions title="分类分级" bordered size="small" column={2}>
          <Descriptions.Item label="行业">{customer.industry || '-'}</Descriptions.Item>
          <Descriptions.Item label="细分行业">{customer.subIndustry || '-'}</Descriptions.Item>
          <Descriptions.Item label="规模">{customer.sizeClass || '-'}</Descriptions.Item>
          <Descriptions.Item label="所有制">{customer.ownershipType || '-'}</Descriptions.Item>
          <Descriptions.Item label="VIP 级别">{customer.vipLevel}</Descriptions.Item>
          <Descriptions.Item label="信用评级">{customer.creditRating || '-'}</Descriptions.Item>
        </Descriptions>

        <Descriptions title="联系方式" bordered size="small" column={2}>
          <Descriptions.Item label="官网">{customer.website || '-'}</Descriptions.Item>
          <Descriptions.Item label="总机">{customer.mainPhone || '-'}</Descriptions.Item>
          <Descriptions.Item label="服务邮箱">{customer.serviceEmail || '-'}</Descriptions.Item>
          <Descriptions.Item label="主域名">
            {customer.emailDomains?.length
              ? customer.emailDomains.map(domain => (
                  <Tag key={domain} color="blue">
                    {domain}
                  </Tag>
                ))
              : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="地址" span={2}>
            {formatAddress(customer.address)}
          </Descriptions.Item>
          <Descriptions.Item label="时区">{customer.timezone || '-'}</Descriptions.Item>
          <Descriptions.Item label="语言">{customer.language || '-'}</Descriptions.Item>
        </Descriptions>

        <Descriptions title="服务配置" bordered size="small" column={2}>
          <Descriptions.Item label="服务档位">{customer.serviceTier || '-'}</Descriptions.Item>
          <Descriptions.Item label="SLA 定义">
            {customer.slaDefinitionId ? `#${customer.slaDefinitionId}` : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="服务时间">{customer.supportHours || '-'}</Descriptions.Item>
          <Descriptions.Item label="客户经理">
            {customer.accountManagerName || (customer.accountManagerId ? `#${customer.accountManagerId}` : '-')}
          </Descriptions.Item>
          <Descriptions.Item label="服务负责人">
            {customer.serviceOwnerId ? `#${customer.serviceOwnerId}` : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="客户成功">
            {customer.csManagerId ? `#${customer.csManagerId}` : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="值班组">
            {customer.oncallGroupId ? `#${customer.oncallGroupId}` : '-'}
          </Descriptions.Item>
        </Descriptions>

        <Descriptions title="财务与生命周期" bordered size="small" column={2}>
          <Descriptions.Item label="结算币种">{customer.billingCurrency || '-'}</Descriptions.Item>
          <Descriptions.Item label="账期">{customer.paymentTerms || '-'}</Descriptions.Item>
          <Descriptions.Item label="信用额度">{customer.creditLimit ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="开票抬头">{customer.invoiceTitle || '-'}</Descriptions.Item>
          <Descriptions.Item label="开票地址" span={2}>
            {formatAddress(customer.billingAddress)}
          </Descriptions.Item>
          <Descriptions.Item label="开通时间">{formatDate(customer.onboardedAt)}</Descriptions.Item>
          <Descriptions.Item label="合同开始">{formatDate(customer.contractStartAt)}</Descriptions.Item>
          <Descriptions.Item label="合同结束">{formatDate(customer.contractEndAt)}</Descriptions.Item>
          <Descriptions.Item label="流失时间">{formatDate(customer.churnAt)}</Descriptions.Item>
          <Descriptions.Item label="流失原因">{customer.churnReason || '-'}</Descriptions.Item>
          <Descriptions.Item label="数据来源">{customer.dataSource || '-'}</Descriptions.Item>
          <Descriptions.Item label="数据质量分">{customer.dataQualityScore ?? 0}</Descriptions.Item>
          <Descriptions.Item label="版本">v{customer.version}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatDate(customer.createdAt)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatDate(customer.updatedAt)}</Descriptions.Item>
        </Descriptions>
      </div>
    );
  }, [customer, lifecycle]);

  const tabs = [
    {
      key: 'customer360',
      label: '客户 360',
      children: customer ? <Customer360Panel customerId={customer.id} /> : null,
    },
    { key: 'overview', label: '概览', children: overview },
    {
      key: 'tenantLinks',
      label: `关联租户 (${detail?.tenantLinks?.length ?? 0})`,
      children: (
        <div className="space-y-3">
          {canWrite && (
            <Button
              type="primary"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => setLinkModal({ open: true, link: null })}
            >
              添加关联租户
            </Button>
          )}
          <Table
            rowKey="id"
            size="small"
            columns={linkColumns}
            dataSource={detail?.tenantLinks ?? []}
            pagination={false}
            locale={{ emptyText: '暂无关联租户' }}
          />
        </div>
      ),
    },
    {
      key: 'contacts',
      label: `联系人 (${detail?.contacts?.length ?? 0})`,
      children: (
        <div className="space-y-3">
          {canWrite && (
            <Button
              type="primary"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => setContactModal({ open: true, contact: null })}
            >
              添加联系人
            </Button>
          )}
          <Table
            rowKey="id"
            size="small"
            columns={contactColumns}
            dataSource={detail?.contacts ?? []}
            pagination={false}
            locale={{ emptyText: '暂无联系人' }}
          />
        </div>
      ),
    },
    {
      key: 'branches',
      label: `服务点 (${detail?.branches?.length ?? 0})`,
      children: (
        <div className="space-y-3">
          {canWrite && (
            <Button
              type="primary"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => setBranchModal({ open: true, branch: null })}
            >
              添加服务点
            </Button>
          )}
          <Table
            rowKey="id"
            size="small"
            columns={branchColumns}
            dataSource={detail?.branches ?? []}
            pagination={false}
            locale={{ emptyText: '暂无服务点' }}
          />
        </div>
      ),
    },
    {
      key: 'externalIds',
      label: `外部标识 (${detail?.externalIds?.length ?? 0})`,
      children: customer ? (
        <CustomerExternalIdsPanel
          customerId={customer.id}
          canWrite={canWrite}
          onChanged={() => void afterMutation()}
        />
      ) : null,
    },
    {
      key: 'relations',
      label: '客户关系',
      children: customer ? (
        <CustomerRelationsPanel
          customerId={customer.id}
          canWrite={canWrite}
          onChanged={() => void afterMutation()}
        />
      ) : null,
    },
    {
      key: 'projects',
      label: '关联项目',
      children: customer ? (
        <CustomerProjectsPanel
          customerId={customer.id}
          branches={detail?.branches ?? []}
          canWrite={canWrite}
          onChanged={() => void afterMutation()}
        />
      ) : null,
    },
    {
      key: 'mergeHistory',
      label: '合并记录',
      children: customer ? (
        <CustomerMergeHistoryPanel
          customerId={customer.id}
          canWrite={canWrite}
          onChanged={() => void afterMutation()}
        />
      ) : null,
    },
  ];

  return (
    <>
      <Drawer
        title={
          customer ? (
            <Space wrap>
              <span>{customer.name}</span>
              <Tag color={tierColor(customer.tier)}>{customer.tier}</Tag>
              {lifecycle && <Tag color={lifecycle.color}>{lifecycle.label}</Tag>}
              {statusTag(customer.status)}
            </Space>
          ) : (
            '客户详情'
          )
        }
        open={open}
        onClose={onClose}
        width={980}
        loading={loading}
        extra={
          customer && canWrite ? (
            <Space>
              <Button icon={<EditOutlined />} onClick={() => onEdit(customer)}>
                编辑
              </Button>
              <Button icon={<MergeCellsOutlined />} onClick={() => onMerge(customer)}>
                合并
              </Button>
              {customer.status === 'active' && (
                <Popconfirm
                  title="停用该客户？"
                  description="停用后不会删除历史合同、服务点和联系人。"
                  onConfirm={disableCustomer}
                >
                  <Button danger icon={<DeleteOutlined />}>
                    停用
                  </Button>
                </Popconfirm>
              )}
            </Space>
          ) : null
        }
      >
        {customer && (
          <div className="space-y-4">
            <Space wrap size={[24, 8]}>
              <Text type="secondary">分支 {customer.branchCount ?? 0}</Text>
              <Text type="secondary">合同 {customer.contractCount ?? 0}</Text>
              <Text type="secondary">联系人 {customer.contactCount ?? 0}</Text>
              <Text type="secondary">关联租户 {customer.tenantLinkCount ?? 0}</Text>
              <Text type="secondary">质量分 {customer.dataQualityScore ?? 0}</Text>
            </Space>
            <Tabs items={tabs} />
          </div>
        )}
      </Drawer>

      {customerId && (
        <>
          <CustomerContactModal
            open={contactModal.open}
            customerId={customerId}
            contact={contactModal.contact}
            branches={detail?.branches ?? []}
            onCancel={() => setContactModal({ open: false, contact: null })}
            onSaved={async () => {
              setContactModal({ open: false, contact: null });
              await afterMutation();
            }}
          />
          <CustomerTenantLinkModal
            open={linkModal.open}
            customerId={customerId}
            link={linkModal.link}
            tenants={tenants}
            onCancel={() => setLinkModal({ open: false, link: null })}
            onSaved={async () => {
              setLinkModal({ open: false, link: null });
              await afterMutation();
            }}
          />
          <CustomerBranchModal
            open={branchModal.open}
            customerId={customerId}
            branch={branchModal.branch}
            onCancel={() => setBranchModal({ open: false, branch: null })}
            onSaved={async () => {
              setBranchModal({ open: false, branch: null });
              await afterMutation();
            }}
          />
        </>
      )}
    </>
  );
}
