
import {
  Building2,
  AlertCircle,
  CheckCircle,
  Clock,
  Plus,
  Search,
  Edit,
  Trash2,
  Eye,
  Users,
  Calendar,
  PauseCircle,
  PlayCircle,
  BarChart3,
  Rocket,
} from 'lucide-react';

import React, { useState, useEffect } from 'react';
import dayjs from 'dayjs';
import type { Dayjs } from 'dayjs';
import {
  Card,
  Table,
  Button,
  Input,
  Select,
  Space,
  Typography,
  Modal,
  Form,
  Row,
  Col,
  Statistic,
  Tooltip,
  Popconfirm,
  App,
  Tag,
  DatePicker,
  InputNumber,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { TenantAPI } from '@/lib/api/tenant-api';
import type { TenantQuota, UpdateTenantRequest } from '@/lib/api/api-config';
import TenantUsageModal from './components/TenantUsageModal';
import TenantOnboardingModal from './components/TenantOnboardingModal';

const { Title, Text } = Typography;

// 租户状态配置
const TENANT_STATUS = {
  active: {
    label: '活跃',
    color: 'success',
    icon: CheckCircle,
  },
  suspended: {
    label: '暂停',
    color: 'warning',
    icon: AlertCircle,
  },
  expired: { label: '过期', color: 'error', icon: AlertCircle },
  deleted: { label: '已删除', color: 'default', icon: AlertCircle },
};

// 租户类型配置（后端 snake_case；msp/customer 为 legacy 兼容值，仅展示）
const TENANT_TYPES: Record<string, { label: string; color: string }> = {
  standard: { label: '标准租户(兼容)', color: 'blue' },
  internal: { label: '内部组织', color: 'cyan' },
  saas_customer: { label: 'SaaS客户', color: 'green' },
  msp_provider: { label: 'MSP服务商', color: 'gold' },
  msp_customer: { label: 'MSP客户', color: 'purple' },
  msp: { label: 'MSP(兼容)', color: 'orange' },
  customer: { label: '客户(兼容)', color: 'default' },
};

type Tenant = {
  id: number;
  name: string;
  code: string;
  domain?: string;
  type: string;
  status: keyof typeof TENANT_STATUS;
  mspProviderId?: number | null;
  userCount?: number;
  ticketCount?: number;
  expiresAt?: string;
  quota?: TenantQuota;
};

export type TenantFormValues = {
  name: string;
  code: string;
  domain?: string;
  type: string;
  mspProviderId?: number;
  status: string;
  expiresAt?: Dayjs;
  // IP-P2-6 硬配额（0/空 = 不限）
  maxUsers?: number;
  maxTicketsPerMonth?: number;
  maxStorageMB?: number;
};

/** 仅 MSP 客户需要选择所属 MSP 服务商（表单显隐 / 校验 / 载荷共用同一判定）。 */
export const isMspProviderRequired = (type?: string): boolean => type === 'msp_customer';

/** MSP 服务商必填规则（Form.Item rules）。 */
export const mspProviderRules = [{ required: true, message: '请选择 MSP 服务商' }];

/**
 * 组装创建/更新租户载荷：
 * - quota 仅收敛 >0 的键（空对象 = 清空 → 不限）；
 * - mspProviderId 仅在 type='msp_customer' 时携带。
 */
export function buildTenantPayload(
  values: TenantFormValues
): UpdateTenantRequest & { name: string; type: string } {
  // IP-P2-6：显式提交 quota 对象（全空 = {} 清空 → 不限）；仅收敛 >0 的键。
  const quota: TenantQuota = {};
  if (values.maxUsers && values.maxUsers > 0) quota.maxUsers = values.maxUsers;
  if (values.maxTicketsPerMonth && values.maxTicketsPerMonth > 0)
    quota.maxTicketsPerMonth = values.maxTicketsPerMonth;
  if (values.maxStorageMB && values.maxStorageMB > 0) quota.maxStorageMB = values.maxStorageMB;

  const payload: UpdateTenantRequest & { name: string; type: string } = {
    name: values.name,
    domain: values.domain,
    type: values.type,
    status: values.status,
    expiresAt: values.expiresAt ? values.expiresAt.toISOString() : undefined,
    quota,
  };
  // 仅 MSP 客户携带所属服务商；其他类型不带该字段。
  if (isMspProviderRequired(values.type) && values.mspProviderId !== undefined) {
    payload.mspProviderId = values.mspProviderId;
  }
  return payload;
}

export default function TenantManagement() {
  const { message } = App.useApp();
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [selectedTenant, setSelectedTenant] = useState<Tenant | null>(null);
  const [viewOnly, setViewOnly] = useState(false);
  // IP-P2-6 收尾：打开用量弹窗的目标租户（null = 关闭）。
  const [usageTenant, setUsageTenant] = useState<Tenant | null>(null);
  // 开通闭环：打开开通弹窗的目标租户（null = 关闭）。
  const [onboardingTenant, setOnboardingTenant] = useState<Tenant | null>(null);
  // MSP 客户表单的服务商候选（仅 active 的 msp_provider）。
  const [mspProviders, setMspProviders] = useState<Tenant[]>([]);
  const [form] = Form.useForm();
  const [searchTerm, setSearchTerm] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [typeFilter, setTypeFilter] = useState('all');
  const [stats, setStats] = useState({
    total: 0,
    active: 0,
    suspended: 0,
    expired: 0,
  });

  // 加载租户数据
  const loadTenants = async () => {
    setLoading(true);
    try {
      const response = await TenantAPI.getTenants({
        search: searchTerm || undefined,
        status: statusFilter !== 'all' ? statusFilter : undefined,
        type: typeFilter !== 'all' ? typeFilter : undefined,
      });

      setTenants(response.tenants as Tenant[]);

      // 计算统计数据
      const total = response.tenants.length;
      const active = response.tenants.filter(
        (t: { status: string }) => t.status === 'active'
      ).length;
      const suspended = response.tenants.filter(
        (t: { status: string }) => t.status === 'suspended'
      ).length;
      const expired = response.tenants.filter(
        (t: { status: string }) => t.status === 'expired'
      ).length;

      setStats({
        total,
        active,
        suspended,
        expired,
      });
    } catch (error) {
      message.error('加载租户数据失败');
    } finally {
      setLoading(false);
    }
  };

  // 初始化加载数据
  useEffect(() => {
    loadTenants();
  }, [searchTerm, statusFilter, typeFilter]);

  // 新建/编辑弹窗打开时加载 MSP 服务商候选（网络失败降级为空列表，不阻塞表单）。
  useEffect(() => {
    if (!showModal) return;
    let alive = true;
    TenantAPI.getTenants({ type: 'msp_provider', size: 100 })
      .then(response => {
        if (!alive) return;
        setMspProviders(
          (response.tenants as Tenant[]).filter(tenant => tenant.status === 'active')
        );
      })
      .catch(() => {
        if (alive) setMspProviders([]);
      });
    return () => {
      alive = false;
    };
  }, [showModal]);

  // 处理保存租户
  const handleSaveTenant = async () => {
    try {
      const values = (await form.validateFields()) as TenantFormValues;
      const payload = buildTenantPayload(values);

      if (selectedTenant) {
        // 更新租户
        await TenantAPI.updateTenant(selectedTenant.id, payload);
        message.success('租户更新成功');
      } else {
        // 创建租户
        await TenantAPI.createTenant({
          ...payload,
          code: values.code,
        });
        message.success('租户创建成功');
      }

      setShowModal(false);
      form.resetFields();
      setSelectedTenant(null);
      loadTenants(); // 重新加载数据
    } catch (error) {
      message.error('保存租户失败');
    }
  };

  const openTenantModal = (tenant: Tenant | null, readonly = false) => {
    setSelectedTenant(tenant);
    setViewOnly(readonly);
    if (tenant) {
      form.setFieldsValue({
        ...tenant,
        mspProviderId: tenant.mspProviderId ?? undefined,
        expiresAt: tenant.expiresAt ? dayjs(tenant.expiresAt) : undefined,
        // IP-P2-6：quota 对象摊平为三个表单字段（缺省留空 = 不限）。
        maxUsers: tenant.quota?.maxUsers,
        maxTicketsPerMonth: tenant.quota?.maxTicketsPerMonth,
        maxStorageMB: tenant.quota?.maxStorageMB,
      });
    } else {
      form.resetFields();
    }
    setShowModal(true);
  };

  // 处理删除租户
  const handleDeleteTenant = async (id: number) => {
    try {
      await TenantAPI.deleteTenant(id);
      message.success('租户删除成功');
      loadTenants(); // 重新加载数据
    } catch (error) {
      message.error('删除租户失败');
    }
  };

  const handleChangeTenantStatus = async (tenant: Tenant, status: keyof typeof TENANT_STATUS) => {
    setLoading(true);
    try {
      await TenantAPI.updateTenant(tenant.id, { status });
      message.success(`租户状态已更新为${TENANT_STATUS[status].label}`);
      loadTenants();
    } catch (error) {
      message.error('更新租户状态失败');
    } finally {
      setLoading(false);
    }
  };

  // 表格列定义
  const columns: ColumnsType<Tenant> = [
    {
      title: '租户信息',
      key: 'info',
      render: (_: unknown, record: Tenant) => (
        <div className="flex items-center">
          <div className="flex-shrink-0 h-10 w-10 rounded-full bg-blue-100 flex items-center justify-center">
            <Building2 className="h-5 w-5 text-blue-600" />
          </div>
          <div className="ml-4">
            <div className="text-sm font-medium text-gray-900">{record.name}</div>
            <div className="text-sm text-gray-500">
              {record.code} • {record.domain || ''}
            </div>
          </div>
        </div>
      ),
    },
    {
      title: '类型/状态',
      key: 'type-status',
      render: (_: unknown, record: Tenant) => (
        <div className="space-y-1">
          <Tag color={TENANT_TYPES[record.type]?.color || 'default'}>
            {TENANT_TYPES[record.type]?.label || record.type}
          </Tag>
          <div className="flex items-center">
            <Tag color={TENANT_STATUS[record.status]?.color || 'default'}>
              {TENANT_STATUS[record.status]?.label || record.status}
            </Tag>
          </div>
        </div>
      ),
    },
    {
      title: '资源使用',
      key: 'usage',
      render: (_: unknown, record: Tenant) => (
        <div className="space-y-1">
          <div className="flex items-center">
            <Users className="w-4 h-4 mr-1 text-gray-400" />
            <span>{record.userCount || 0} 用户</span>
          </div>
          <div className="text-xs text-gray-500">{record.ticketCount || 0} 工单</div>
        </div>
      ),
    },
    {
      title: '配额',
      key: 'quota',
      render: (_: unknown, record: Tenant) => {
        const q = record.quota;
        if (!q || (!q.maxUsers && !q.maxTicketsPerMonth && !q.maxStorageMB)) {
          return <Text type="secondary">不限</Text>;
        }
        return (
          <Space size={4} wrap>
            {q.maxUsers ? <Tag>用户 {q.maxUsers}</Tag> : null}
            {q.maxTicketsPerMonth ? <Tag>月工单 {q.maxTicketsPerMonth}</Tag> : null}
            {q.maxStorageMB ? <Tag>存储 {q.maxStorageMB}MB</Tag> : null}
          </Space>
        );
      },
    },
    {
      title: '到期时间',
      key: 'expires',
      dataIndex: 'expiresAt',
      render: (expiresAt: string) => (
        <div className="flex items-center">
          <Calendar className="w-4 h-4 mr-1 text-gray-400" />
          {expiresAt ? new Date(expiresAt).toLocaleDateString() : '无'}
        </div>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 150,
      render: (_: unknown, record: Tenant) => (
        <Space size="small">
          <Tooltip title="编辑">
            <Button
              type="text"
              icon={<Edit className="w-4 h-4" />}
              onClick={() => openTenantModal(record)}
            />
          </Tooltip>
          <Tooltip title="查看">
            <Button
              type="text"
              icon={<Eye className="w-4 h-4" />}
              onClick={() => openTenantModal(record, true)}
            />
          </Tooltip>
          <Tooltip title="用量">
            <Button
              type="text"
              icon={<BarChart3 className="w-4 h-4" />}
              onClick={() => setUsageTenant(record)}
            />
          </Tooltip>
          <Tooltip title="开通/供给">
            <Button
              type="text"
              icon={<Rocket className="w-4 h-4" />}
              onClick={() => setOnboardingTenant(record)}
            />
          </Tooltip>
          {record.status === 'active' ? (
            record.code === 'default' ? (
              <Tooltip title="系统默认租户不可暂停（会导致整站无法访问）">
                <Button type="text" disabled icon={<PauseCircle className="w-4 h-4" />} />
              </Tooltip>
            ) : (
              <Tooltip title="暂停租户">
                <Button
                  type="text"
                  icon={<PauseCircle className="w-4 h-4" />}
                  onClick={() => handleChangeTenantStatus(record, 'suspended')}
                />
              </Tooltip>
            )
          ) : (
            <Tooltip title="恢复租户">
              <Button
                type="text"
                icon={<PlayCircle className="w-4 h-4" />}
                onClick={() => handleChangeTenantStatus(record, 'active')}
              />
            </Tooltip>
          )}
          {record.code === 'default' ? (
            <Tooltip title="系统默认租户不可删除">
              <Button type="text" danger disabled icon={<Trash2 className="w-4 h-4" />} />
            </Tooltip>
          ) : (
            <Popconfirm
              title="确认删除"
              description="确定要删除这个租户吗？此操作不可恢复。"
              onConfirm={() => handleDeleteTenant(record.id)}
              okText="确认"
              cancelText="取消"
            >
              <Tooltip title="删除">
                <Button type="text" danger icon={<Trash2 className="w-4 h-4" />} />
              </Tooltip>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="space-y-6">
      <div>
        <Title level={2} className="!mb-2">
          <Building2 className="inline-block w-6 h-6 mr-2" />
          租户管理
        </Title>
        <Text type="secondary">管理系统中的租户和组织</Text>
      </div>

      {/* 统计卡片 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card className="enterprise-card">
            <Statistic
              title="总租户数"
              value={stats.total}
              prefix={<Building2 className="w-5 h-5" />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="enterprise-card">
            <Statistic
              title="活跃租户"
              value={stats.active}
              prefix={<CheckCircle className="w-5 h-5" />}
              styles={{ content: { color: '#52c41a' } }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="enterprise-card">
            <Statistic
              title="暂停租户"
              value={stats.suspended}
              prefix={<AlertCircle className="w-5 h-5" />}
              styles={{ content: { color: '#faad14' } }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="enterprise-card">
            <Statistic
              title="过期租户"
              value={stats.expired}
              prefix={<Clock className="w-5 h-5" />}
              styles={{ content: { color: '#1890ff' } }}
            />
          </Card>
        </Col>
      </Row>

      {/* 搜索和过滤 */}
      <Card>
        <Row gutter={[16, 16]} align="middle">
          <Col xs={24} md={12} lg={8}>
            <Input
              placeholder="搜索租户名称、编码或域名..."
              prefix={<Search className="w-4 h-4 text-gray-400" />}
              value={searchTerm}
              onChange={e => setSearchTerm(e.target.value)}
              allowClear
            />
          </Col>
          <Col xs={24} md={8} lg={4}>
            <Select
              placeholder="筛选状态"
              value={statusFilter}
              onChange={setStatusFilter}
              style={{ width: '100%' }}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'active', label: '活跃' },
                { value: 'suspended', label: '暂停' },
                { value: 'expired', label: '过期' },
                { value: 'deleted', label: '已删除' },
              ]}
            />
          </Col>
          <Col xs={24} md={8} lg={4}>
            <Select
              placeholder="筛选类型"
              value={typeFilter}
              onChange={setTypeFilter}
              style={{ width: '100%' }}
              options={[
                { value: 'all', label: '全部类型' },
                { value: 'standard', label: '标准租户' },
                { value: 'internal', label: '内部组织' },
                { value: 'saas_customer', label: 'SaaS客户' },
                { value: 'msp_provider', label: 'MSP服务商' },
                { value: 'msp_customer', label: 'MSP客户' },
              ]}
            />
          </Col>
          <Col xs={24} md={4} lg={8} className="text-right">
            <Button
              type="primary"
              icon={<Plus className="w-4 h-4" />}
              onClick={() => {
                openTenantModal(null);
              }}
            >
              新建租户
            </Button>
          </Col>
        </Row>
      </Card>

      {/* 租户列表 */}
      <Card className="enterprise-card">
        <Table<Tenant>
          columns={columns}
          dataSource={tenants}
          rowKey="id"
          loading={loading}
          scroll={{ x: 920 }}
          pagination={{
            total: tenants.length,
            pageSize: 10,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: total => `共 ${total} 条记录`,
          }}
          className="enterprise-table"
        />
      </Card>

      {/* 租户编辑模态框 */}
      <Modal
        title={
          <span>
            {selectedTenant ? (
              <>
                <Edit className="w-4 h-4 mr-2" />
                {viewOnly ? '查看租户' : '编辑租户'}
              </>
            ) : (
              <>
                <Plus className="w-4 h-4 mr-2" />
                新建租户
              </>
            )}
          </span>
        }
        open={showModal}
        onOk={viewOnly ? undefined : handleSaveTenant}
        onCancel={() => {
          setShowModal(false);
          setSelectedTenant(null);
          setViewOnly(false);
          form.resetFields();
        }}
        width={600}
        confirmLoading={loading}
        okText="保存"
        cancelText="取消"
        footer={
          viewOnly
            ? [
                <Button
                  key="close"
                  onClick={() => {
                    setShowModal(false);
                    setSelectedTenant(null);
                    setViewOnly(false);
                    form.resetFields();
                  }}
                >
                  关闭
                </Button>,
              ]
            : undefined
        }
      >
        <Form
          form={form}
          layout="vertical"
          className="mt-4"
          disabled={viewOnly}
          initialValues={{ status: 'active' }}
          onValuesChange={changed => {
            // 类型切走 msp_customer 时清空服务商，避免提交脏字段。
            if ('type' in changed && !isMspProviderRequired(changed.type)) {
              form.setFieldValue('mspProviderId', undefined);
            }
          }}
        >
          <Form.Item
            label="租户名称"
            name="name"
            rules={[{ required: true, message: '请输入租户名称' }]}
          >
            <Input placeholder="请输入租户名称" />
          </Form.Item>

          <Form.Item
            label="租户编码"
            name="code"
            rules={[
              { required: true, message: '请输入租户编码' },
              {
                pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]*$/,
                message: '只能包含字母、数字、下划线或连字符，且以字母或数字开头',
              },
            ]}
          >
            <Input
              disabled={!!selectedTenant}
              placeholder="例如 finops_001（字母、数字、下划线、连字符）"
            />
          </Form.Item>

          <Form.Item label="域名" name="domain">
            <Input placeholder="请输入域名" />
          </Form.Item>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label="租户类型"
                name="type"
                rules={[{ required: true, message: '请选择租户类型' }]}
              >
                <Select placeholder="请选择租户类型" options={[
                  { value: 'internal', label: '内部组织' },
                  { value: 'saas_customer', label: 'SaaS客户' },
                  { value: 'msp_provider', label: 'MSP服务商' },
                  { value: 'msp_customer', label: 'MSP客户' },
                ]} />
              </Form.Item>
            </Col>

            <Col span={12}>
              <Form.Item
                label="状态"
                name="status"
                rules={[{ required: true, message: '请选择状态' }]}
              >
                <Select placeholder="请选择状态" options={[
                  { value: 'active', label: '活跃' },
                  { value: 'suspended', label: '暂停' },
                  { value: 'expired', label: '过期' },
                  { value: 'deleted', label: '已删除' },
                ]} />
              </Form.Item>
            </Col>
          </Row>

          {/* MSP 客户必选所属服务商；其他类型不展示、不提交。 */}
          <Form.Item noStyle shouldUpdate={(prev, cur) => prev.type !== cur.type}>
            {({ getFieldValue }) =>
              isMspProviderRequired(getFieldValue('type')) ? (
                <Form.Item
                  label="MSP 服务商"
                  name="mspProviderId"
                  rules={mspProviderRules}
                >
                  <Select
                    placeholder="请选择 MSP 服务商"
                    showSearch
                    optionFilterProp="label"
                    options={mspProviders.map(provider => ({
                      value: provider.id,
                      label: `${provider.name}（${provider.code}）`,
                    }))}
                  />
                </Form.Item>
              ) : null
            }
          </Form.Item>

          <Form.Item label="到期时间" name="expiresAt">
            <DatePicker style={{ width: '100%' }} placeholder="选择到期时间" />
          </Form.Item>

          {/* IP-P2-6 硬配额：0/空 = 不限；超限写入统一 422 TENANT_QUOTA_EXCEEDED。 */}
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item label="用户上限" name="maxUsers">
                <InputNumber min={0} style={{ width: '100%' }} placeholder="0 = 不限" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item label="月工单上限" name="maxTicketsPerMonth">
                <InputNumber min={0} style={{ width: '100%' }} placeholder="0 = 不限" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item label="存储上限 (MB)" name="maxStorageMB">
                <InputNumber min={0} style={{ width: '100%' }} placeholder="0 = 不限" />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Modal>

      {/* IP-P2-6 收尾：租户用量（配额 vs 用量，口径与写入校验一致）。 */}
      <TenantUsageModal
        open={!!usageTenant}
        tenantId={usageTenant?.id}
        tenantName={usageTenant?.name}
        onClose={() => setUsageTenant(null)}
      />

      {/* 开通闭环：模板供给 → 首个管理员 → 可用。 */}
      <TenantOnboardingModal
        open={!!onboardingTenant}
        tenant={onboardingTenant ?? undefined}
        onClose={() => setOnboardingTenant(null)}
      />
    </div>
  );
}
