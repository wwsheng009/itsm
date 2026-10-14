import React, { useEffect, useState } from 'react';
import {
  App,
  Alert,
  Button,
  DatePicker,
  Form,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Dayjs } from 'dayjs';
import { MSPAPI } from '@/lib/api/msp-api';
import {
  CreateSLAGrantPayload,
  SLAApi,
  type SLADefinition,
  type SLADefinitionGrantItem,
} from '@/lib/api/sla-api';

const { Title, Text } = Typography;

/**
 * SLA 下发授权管理（D-SLA-1，provider 面）。
 *
 * 后端契约：/api/v1/sla/grants（RequireMSPDeploymentMode 404 gate + sla:grant:read/write）。
 * - 非 MSP 部署（private/saas）返回 404：页面渲染"未启用"空态而非报错；
 * - 同客户同工单类型重复授权由后端自动停用旧授权（deactivate-then-activate）；
 * - customer 侧只读视图见 /sla 工作台"服务商下发"分段（getProvidedDefinitions）。
 */
interface CreateGrantFormValues {
  slaDefinitionId: number;
  customerTenantId: number;
  ticketTypeId?: number;
  validUntil?: Dayjs | null;
}

const SLAGrantsPage: React.FC = () => {
  const { message } = App.useApp();
  const [grants, setGrants] = useState<SLADefinitionGrantItem[]>([]);
  const [definitions, setDefinitions] = useState<SLADefinition[]>([]);
  const [customers, setCustomers] = useState<{ id: number; code: string; name: string }[]>([]);
  const [loading, setLoading] = useState(false);
  const [mspDisabled, setMspDisabled] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm<CreateGrantFormValues>();

  const loadGrants = async () => {
    setLoading(true);
    try {
      const data = await SLAApi.listGrants();
      setGrants(data.items);
      setMspDisabled(false);
    } catch (error) {
      // 404 = 非 MSP 部署（后端 RequireMSPDeploymentMode）。渲染空态并给出解释。
      const status = (error as { status?: number })?.status;
      if (status === 404) {
        setMspDisabled(true);
        setGrants([]);
      } else {
        message.error('加载 SLA 下发授权失败');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadGrants();
    // 下拉数据源：provider 自有定义 + 当前 MSP 员工可访问客户（服务端 allocation 收口）。
    (async () => {
      try {
        const defs = await SLAApi.getSLADefinitions({ pageSize: 200, isActive: true });
        setDefinitions(defs.items || []);
      } catch {
        /* 下拉为空即可，不阻塞页面 */
      }
      try {
        const resp = await MSPAPI.getCustomers();
        setCustomers(resp?.customers ?? []);
      } catch {
        /* 非 MSP 员工/部署时为空 */
      }
    })();
  }, []);

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      setCreating(true);
      const payload: CreateSLAGrantPayload = {
        slaDefinitionId: values.slaDefinitionId,
        customerTenantId: values.customerTenantId,
        ticketTypeId: values.ticketTypeId ?? 0,
      };
      if (values.validUntil) payload.validUntil = values.validUntil.toISOString();
      await SLAApi.createGrant(payload);
      message.success('下发授权创建成功');
      setShowCreate(false);
      form.resetFields();
      await loadGrants();
    } catch (error) {
      if ((error as { errorFields?: unknown })?.errorFields) return; // 表单校验失败
      message.error('创建下发授权失败');
    } finally {
      setCreating(false);
    }
  };

  const handleDeactivate = async (id: number) => {
    try {
      await SLAApi.deactivateGrant(id);
      message.success('已停用该授权');
      await loadGrants();
    } catch {
      message.error('停用失败');
    }
  };

  const customerLabel = (id: number) => {
    const c = customers.find(item => item.id === id);
    return c ? `${c.name}（#${id}）` : `#${id}`;
  };

  const columns: ColumnsType<SLADefinitionGrantItem> = [
    {
      title: 'SLA 定义',
      dataIndex: 'definitionName',
      render: (_: string, row) => row.definitionName || `#${row.slaDefinitionId}`,
    },
    {
      title: '客户租户',
      dataIndex: 'customerTenantId',
      render: (id: number) => customerLabel(id),
    },
    {
      title: '工单类型',
      dataIndex: 'ticketTypeId',
      render: (id: number) => (id === 0 ? <Tag>类型无关</Tag> : <Tag color="blue">#{id}</Tag>),
    },
    {
      title: '有效期',
      dataIndex: 'validUntil',
      render: (v?: string) => (v ? new Date(v).toLocaleString() : '长期'),
    },
    {
      title: '状态',
      dataIndex: 'isActive',
      render: (active: boolean) =>
        active ? <Tag color="success">生效中</Tag> : <Tag color="default">已停用</Tag>,
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, row) =>
        row.isActive ? (
          <Popconfirm
            title="停用该下发授权？"
            description="停用后该客户新工单不再命中此定义（存量工单快照不受影响）。"
            onConfirm={() => handleDeactivate(row.id)}
          >
            <Button size="small" danger>
              停用
            </Button>
          </Popconfirm>
        ) : null,
    },
  ];

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <Title level={4} style={{ marginBottom: 4 }}>
            SLA 下发授权
          </Title>
          <Text type="secondary">
            将服务商 SLA 定义授权给客户租户（合同个案优先于客户自建定义与等级策略）。
          </Text>
        </div>
        <Button type="primary" disabled={mspDisabled} onClick={() => setShowCreate(true)}>
          新建下发
        </Button>
      </div>

      {mspDisabled && (
        <Alert
          type="info"
          showIcon
          className="mb-4"
          message="当前部署未启用 MSP 下发能力"
          description="SLA 下发授权仅在 saas_msp 部署模式下可用（DEPLOYMENT_MODE=saas_msp）。"
        />
      )}

      <Table<SLADefinitionGrantItem>
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={grants}
        pagination={false}
        locale={{ emptyText: mspDisabled ? '未启用' : '暂无下发授权' }}
      />

      <Modal
        title="新建 SLA 下发授权"
        open={showCreate}
        onCancel={() => setShowCreate(false)}
        onOk={handleCreate}
        confirmLoading={creating}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" initialValues={{ ticketTypeId: 0 }}>
          <Form.Item
            name="slaDefinitionId"
            label="SLA 定义"
            rules={[{ required: true, message: '请选择要下发的 SLA 定义' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择本服务商的 SLA 定义"
              options={definitions.map(d => ({
                value: d.id,
                label: `${d.name}（#${d.id}）`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="customerTenantId"
            label="客户租户"
            rules={[{ required: true, message: '请选择客户租户' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择归属本服务商的客户"
              options={customers.map(c => ({
                value: c.id,
                label: `${c.name}（#${c.id}）`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="ticketTypeId"
            label="工单类型 ID"
            extra="0 = 类型无关授权（对该客户全部类型生效，匹配优先级低于显式类型绑定）。"
          >
            <InputNumber min={0} style={{ width: '100%' }} placeholder="0 = 类型无关" />
          </Form.Item>
          <Form.Item
            name="validUntil"
            label="有效期至（留空 = 长期有效）"
            extra="对应合同期的临时授权；到期后该定义不再对客户工单生效。"
          >
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default SLAGrantsPage;
