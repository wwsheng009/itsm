
import { useState, useEffect } from 'react';
import {
  Card,
  Form,
  Select,
  Input,
  Button,
  Table,
  message,
  Modal,
  Space,
  Tag,
  Alert,
  Spin,
  Tooltip,
  Typography,
  Row,
  Col,
} from 'antd';
import { Plus, UserPlus } from 'lucide-react';
import MSPService from '@/lib/services/msp-service';
import { UserApi } from '@/lib/api/user-api';
import type { MSPAllocation, CreateAllocationRequest, MSPAllocationRoleMapping } from '@/types/msp';
import { useAuthStore } from '@/lib/store/auth-store';
import ProvisionUserModal from '@/components/provisioning/ProvisionUserModal';

const ALLOCATION_ROLE_OPTIONS = [
  { value: 'primary', label: '主支持 (Primary)' },
  { value: 'backup', label: '备份 (Backup)' },
  { value: 'specialist', label: '专家 (Specialist)' },
] as const;

const MSP_TARGET_ROLE_LABELS: Record<string, string> = {
  msp_viewer: 'msp_viewer（观察者）',
  msp_tech: 'msp_tech（技术员）',
  msp_specialist: 'msp_specialist（专家）',
  msp_manager: 'msp_manager（经理）',
  msp_admin: 'msp_admin（全托管）',
};

export default function MSPManagementPage() {
  const [form] = Form.useForm();
  const [mappingForm] = Form.useForm();
  const canWriteRoleMapping = useAuthStore(state => state.hasPermission('msp_allocation:write'));
  const [loading, setLoading] = useState(false);
  const [hasAccess, setHasAccess] = useState(false);
  const [allocations, setAllocations] = useState<MSPAllocation[]>([]);
  const [customers, setCustomers] = useState<{ id: number; code: string; name: string }[]>([]);
  const [mspUsers, setMSPUsers] = useState<{ id: number; username: string }[]>([]);
  const [roleMapping, setRoleMapping] = useState<MSPAllocationRoleMapping | null>(null);
  const [mappingLoading, setMappingLoading] = useState(true);
  const [mappingError, setMappingError] = useState<string | null>(null);
  const [mappingSaving, setMappingSaving] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [selectedAllocation, setSelectedAllocation] = useState<MSPAllocation | null>(null);
  const [deallocateLoading, setDeallocateLoading] = useState(false);
  const [accessError, setAccessError] = useState<string | null>(null);
  // MSP 建号通道：为客户建号弹窗（IP-P0-5）。
  const [provisionVisible, setProvisionVisible] = useState(false);
  const primaryTarget = Form.useWatch('primary', mappingForm);
  const backupTarget = Form.useWatch('backup', mappingForm);
  const specialistTarget = Form.useWatch('specialist', mappingForm);
  const highRiskMapping = [primaryTarget, backupTarget, specialistTarget].some(
    role => role === 'msp_manager' || role === 'msp_admin'
  );
  let mappingSourceLabel = '系统默认';
  if (mappingLoading) {
    mappingSourceLabel = '加载中';
  } else if (mappingError) {
    mappingSourceLabel = '加载失败';
  } else if (roleMapping?.overridden) {
    mappingSourceLabel = 'Provider 配置';
  }

  useEffect(() => {
    checkAccess();
  }, []);

  const checkAccess = async () => {
    try {
      const { isMSP, isAdmin } = await MSPService.isMSPUser();
      if (isMSP || isAdmin) {
        setHasAccess(true);
        loadData();
      } else {
        setAccessError('您没有权限访问此页面');
      }
    } catch (err: any) {
      setAccessError(err.message || '检查权限失败');
    }
  };

  const loadRoleMapping = async () => {
    setMappingLoading(true);
    setMappingError(null);
    try {
      const mapping = await MSPService.getAllocationRoleMapping();
      setRoleMapping(mapping);
      mappingForm.setFieldsValue(mapping.mapping);
    } catch (err: any) {
      setMappingError(err.message || '加载分配角色映射失败');
    } finally {
      setMappingLoading(false);
    }
  };

  const loadData = async () => {
    setLoading(true);
    void loadRoleMapping();
    try {
      const [allocRes, custRes, usersRes] = await Promise.all([
        MSPService.getAllocations(),
        MSPService.getCustomers(),
        UserApi.getUsers({ page: 1, pageSize: 200 }).catch(() => ({ users: [] })),
      ]);
      setAllocations(allocRes.allocations);
      setCustomers(custRes.customers);

      // 从用户 API 获取完整用户列表 - 处理多种返回格式
      type UsersResponse = { users?: { id: number; username?: string; name?: string }[]; data?: { users?: { id: number; username?: string; name?: string }[] } };
      const allUsers = (usersRes as UsersResponse)?.users || (usersRes as UsersResponse)?.data?.users || [];
      if (allUsers.length > 0) {
        setMSPUsers(allUsers.map((u) => ({ id: u.id, username: u.username || u.name || '' })));
      } else {
        // 降级：从已有分配中提取用户
        const uniqueUsers = Array.from(
          new Map(
            allocRes.allocations.map(a => [
              a.mspUserId,
              { id: a.mspUserId, username: a.mspUsername || '' },
            ])
          ).values()
        );
        setMSPUsers(uniqueUsers);
      }
    } catch (err: any) {
      message.error(err.message || '加载数据失败');
    } finally {
      setLoading(false);
    }
  };

  // D-10b：提前 return 必须放在所有被 effect 闭包引用的函数（loadData）之后——
  // 否则首帧 `hasAccess=false` 提前返回，loadData 的 const 绑定未初始化，
  // effect 里的 checkAccess() 再调用它即触发 TDZ：
  // "Cannot access 'loadData' before initialization"（E1 验收实锤）。
  if (accessError) {
    return (
      <div style={{ padding: 24 }}>
        <Alert message={accessError} type="error" showIcon />
      </div>
    );
  }

  if (!hasAccess) {
    return (
      <div style={{ padding: 24, textAlign: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }

  const handleCreate = async (values: CreateAllocationRequest) => {
    try {
      await MSPService.createAllocation(values);
      message.success('分配创建成功');
      setModalVisible(false);
      form.resetFields();
      loadData();
    } catch (err: any) {
      message.error(err.message || '创建分配失败');
    }
  };

  const handleDeallocate = async (allocation: MSPAllocation) => {
    Modal.confirm({
      title: '确认解除分配',
      content: `确定要解除 ${allocation.mspUsername} 对客户 ${allocation.customerTenantName || allocation.customerName || `#${allocation.customerTenantId}`} 的分配吗？`,
      onOk: async () => {
        setDeallocateLoading(true);
        try {
          await MSPService.deallocate(allocation.mspUserId, allocation.customerTenantId);
          message.success('分配已解除');
          loadData();
        } catch (err: any) {
          message.error(err.message || '解除分配失败');
        } finally {
          setDeallocateLoading(false);
        }
      },
    });
  };

  const persistRoleMapping = async (values: Record<string, string>) => {
    setMappingSaving(true);
    try {
      const updated = await MSPService.updateAllocationRoleMapping({
        mapping: {
          primary: values.primary,
          backup: values.backup,
          specialist: values.specialist,
        },
      });
      setRoleMapping(updated);
      mappingForm.setFieldsValue(updated.mapping);
      message.success('分配角色映射已保存');
    } catch (err: any) {
      message.error(err.message || '保存分配角色映射失败');
    } finally {
      setMappingSaving(false);
    }
  };

  const handleSaveRoleMapping = (values: Record<string, string>) => {
    const riskyValues = [values.primary, values.backup, values.specialist].some(
      role => role === 'msp_manager' || role === 'msp_admin'
    );
    if (!riskyValues) {
      return persistRoleMapping(values);
    }
    return new Promise<void>(resolve => {
      Modal.confirm({
        title: '确认保存高权限映射',
        content:
          '当前映射包含 msp_manager / msp_admin。该角色会授予客户租户内用户管理、报表、项目或 CMDB/变更写权限，请确认符合客户授权边界。',
        okText: '确认保存',
        okButtonProps: { danger: true },
        cancelText: '取消',
        onOk: async () => {
          await persistRoleMapping(values);
          resolve();
        },
        onCancel: () => resolve(),
      });
    });
  };

  const handleResetRoleMapping = () => {
    Modal.confirm({
      title: '恢复默认分配角色映射',
      content: '恢复后新分配将按系统默认映射（primary/backup→msp_tech，specialist→msp_specialist）。',
      onOk: async () => {
        setMappingSaving(true);
        try {
          const updated = await MSPService.resetAllocationRoleMapping();
          setRoleMapping(updated);
          mappingForm.setFieldsValue(updated.mapping);
          message.success('已恢复默认分配角色映射');
        } catch (err: any) {
          message.error(err.message || '恢复默认分配角色映射失败');
        } finally {
          setMappingSaving(false);
        }
      },
    });
  };

  const columns = [
    {
      title: 'MSP 员工',
      dataIndex: 'mspUsername',
      key:'mspUsername',
    },
    {
      title: '客户',
      dataIndex: 'customerTenantName',
      key: 'customerTenantName',
      render: (name: string, record: MSPAllocation) => name || record.customerName || `#${record.customerTenantId}`,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      render: (role: string) => {
        const config = {
          primary: { color: 'green', text: '主支持' },
          backup: { color: 'orange', text: '备份' },
          specialist: { color: 'blue', text: '专家' },
        };
        const cfg = config[role as keyof typeof config] || { color: 'default', text: role };
        return <Tag color={cfg.color}>{cfg.text}</Tag>;
      },
    },
    {
      title: '分配时间',
      dataIndex: 'assignedAt',
      key: 'assignedAt',
      render: (date: string) => new Date(date).toLocaleString('zh-CN'),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: any, record: MSPAllocation) => (
        <Space size="small">
          <Button size="small" onClick={() => handleDeallocate(record)} danger>
            解除
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div style={{ padding: 24 }}>
      <Card
        title="MSP 分配管理"
        extra={
          <Space>
            <Button icon={<UserPlus />} onClick={() => setProvisionVisible(true)}>
              为客户建号
            </Button>
            <Button type="primary" icon={<Plus />} onClick={() => setModalVisible(true)}>
              新建分配
            </Button>
          </Space>
        }
      >
        <Alert
          message="使用说明"
          description={
            <span>
              分配后即可在工作台中查看并处理该客户的工单。
              <Tooltip title="技术实现：MSP 员工通过 X-Customer-Tenant-ID 请求头访问对应客户租户的数据，管理员无需关注。">
                <Typography.Link style={{ marginLeft: 8 }}>开发者备注</Typography.Link>
              </Tooltip>
            </span>
          }
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
        />

        <Table
          columns={columns}
          dataSource={allocations}
          rowKey="id"
          loading={loading}
          pagination={{ pageSize: 10 }}
        />
      </Card>

      <Card
        title="分配角色映射"
        style={{ marginTop: 16 }}
        extra={
          <Space>
            <Tag color={mappingError ? 'red' : roleMapping?.overridden ? 'blue' : 'default'}>
              {mappingSourceLabel}
            </Tag>
            {highRiskMapping ? <Tag color='red'>高权限</Tag> : null}
          </Space>
        }
      >
        <Alert
          message="分配角色决定服务商员工进入客户租户后的 membership 角色"
          description="保存后仅影响新建分配；已有分配需执行 backfill_membership -apply -reconcile 重算。"
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
        />
        {mappingLoading ? (
          <div style={{ padding: '24px 0', textAlign: 'center' }}>
            <Spin />
          </div>
        ) : mappingError ? (
          <Alert
            message="分配角色映射加载失败"
            description={mappingError}
            type="error"
            showIcon
            action={
              <Button size='small' onClick={() => void loadRoleMapping()}>
                重试
              </Button>
            }
          />
        ) : (
          <>
            {roleMapping?.corrupt ? (
              <Alert
                message="配置损坏，已阻止新分配同步"
                description={
                  (roleMapping.warning || '配置内容无法解析') +
                  '；请重新保存一份合法映射，或恢复默认。'
                }
                type="error"
                showIcon
                style={{ marginBottom: 16 }}
              />
            ) : roleMapping?.warning ? (
              <Alert
                message="配置警告"
                description={roleMapping.warning}
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
              />
            ) : null}
            {highRiskMapping ? (
              <Alert
                message="当前映射包含高权限客户角色"
                description="msp_manager / msp_admin 会授予客户租户内用户管理、报表、项目或 CMDB/变更写权限。保存前会再次确认。"
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
              />
            ) : null}
            {!canWriteRoleMapping ? (
              <Alert
                message="只读视图"
                description="当前账号没有 msp_allocation:write 权限，只能查看分配角色映射，不能保存或恢复默认。"
                type="info"
                showIcon
                style={{ marginBottom: 16 }}
              />
            ) : null}
            <Form form={mappingForm} layout="vertical" onFinish={handleSaveRoleMapping}>
              <Row gutter={16}>
                {ALLOCATION_ROLE_OPTIONS.map(option => (
                  <Col xs={24} md={8} key={option.value}>
                    <Form.Item
                      name={option.value}
                      label={option.label}
                      rules={[{ required: true, message: '请选择目标角色' }]}
                      extra={`默认：${
                        MSP_TARGET_ROLE_LABELS[roleMapping?.defaults?.[option.value] ?? ''] ??
                        roleMapping?.defaults?.[option.value] ??
                        '-'
                      }`}
                    >
                      <Select
                        disabled={!canWriteRoleMapping}
                        options={(roleMapping?.allowedRoles?.length
                          ? roleMapping.allowedRoles
                          : Object.keys(MSP_TARGET_ROLE_LABELS)
                        ).map(code => ({
                          value: code,
                          label: MSP_TARGET_ROLE_LABELS[code] ?? code,
                        }))}
                      />
                    </Form.Item>
                  </Col>
                ))}
              </Row>
              <Space>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={mappingSaving}
                  disabled={!canWriteRoleMapping}
                >
                  保存
                </Button>
                <Button
                  onClick={handleResetRoleMapping}
                  loading={mappingSaving}
                  disabled={!canWriteRoleMapping || !roleMapping?.overridden}
                >
                  恢复默认
                </Button>
              </Space>
            </Form>
            <div style={{ marginTop: 12, color: 'var(--color-text-secondary, #888)', fontSize: 12 }}>
              当前来源：{roleMapping?.overridden ? 'Provider 配置' : '系统默认'}
              {roleMapping?.updatedAt
                ? `｜最近保存：${new Date(roleMapping.updatedAt).toLocaleString('zh-CN')}`
                : ''}
              {roleMapping?.updatedBy ? `｜操作人：${roleMapping.updatedBy}` : ''}
              ｜默认映射：primary/backup → msp_tech，specialist → msp_specialist。
            </div>
          </>
        )}
      </Card>

      <Modal
        title="创建 MSP 分配"
        open={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          form.resetFields();
        }}
        footer={null}
      >
        <Form form={form} layout="vertical" onFinish={handleCreate}>
          <Form.Item
            name="mspUserId"
            label="MSP 员工"
            rules={[{ required: true, message: '请选择 MSP 员工' }]}
          >
            <Select placeholder="选择 MSP 员工" showSearch optionFilterProp="children" options={mspUsers.map(user => ({ value: user.id, label: user.username }))} />
          </Form.Item>

          <Form.Item
            name="customerTenantId"
            label="客户租户"
            rules={[{ required: true, message: '请选择客户租户' }]}
          >
            <Select placeholder="选择客户租户" showSearch optionFilterProp="children" options={customers.map(cust => ({ value: cust.id, label: <>
                  {cust.code} - {cust.name}
                </> }))} />
          </Form.Item>

          <Form.Item
            name="role"
            label="分配角色"
            initialValue="primary"
            rules={[{ required: true, message: '请选择分配角色' }]}
          >
            <Select placeholder="选择角色" options={[{ value: "primary", label: "主支持 (Primary)" }, { value: "backup", label: "备份 (Backup)" }, { value: "specialist", label: "专家 (Specialist)" }]} />
          </Form.Item>

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                创建
              </Button>
              <Button onClick={() => setModalVisible(false)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      {/* MSP 建号通道（IP-P0-5）：仅在已分配客户内建号；服务端二次校验 allocation。 */}
      <ProvisionUserModal
        open={provisionVisible}
        title="为客户建号"
        customerOptions={customers}
        loadRoleOptions={customerId =>
          customerId ? MSPService.listCustomerGrantableRoles(customerId) : Promise.resolve([])
        }
        onClose={() => setProvisionVisible(false)}
        submit={(payload, customerId) => MSPService.provisionCustomerUser(customerId!, payload)}
      />
    </div>
  );
}
