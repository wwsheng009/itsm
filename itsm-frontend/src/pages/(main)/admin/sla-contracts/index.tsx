import React, { useEffect, useState } from 'react';
import {
  App,
  Alert,
  Button,
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
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { Dayjs } from 'dayjs';
import { MSPAPI } from '@/lib/api/msp-api';
import {
  CreateMSPContractPayload,
  MSPContractItem,
  SLAApi,
  type SLADefinition,
} from '@/lib/api/sla-api';

const { Title, Text } = Typography;

/**
 * MSP SLA 合同管理（D-SLA-4，provider 面）。
 *
 * 后端契约：/api/v1/sla/contracts（RequireMSPDeploymentMode 404 gate + sla:contract:read/write）。
 * - 合同承载"SLA 条款 + 合同期 + 罚则 + 定义绑定（含类型维度）"；
 * - P1-2 并轨后 grant 写入口（/sla/grants）返回 410，本页为唯一写面；
 * - 非 MSP 部署（private/saas）返回 404：页面渲染"未启用"空态而非报错。
 */

interface ContractFormValues {
  contractNo: string;
  name: string;
  customerTenantId: number;
  effectiveUntil?: Dayjs | null;
  settlementCycle?: string;
  currency?: string;
  bindings: { slaDefinitionId: number; ticketTypeId?: number }[];
}

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  draft: { label: '草稿', color: 'default' },
  active: { label: '生效中', color: 'success' },
  expired: { label: '已过期', color: 'warning' },
  terminated: { label: '已终止', color: 'error' },
};

const SLAContractsPage: React.FC = () => {
  const { message } = App.useApp();
  const [contracts, setContracts] = useState<MSPContractItem[]>([]);
  const [definitions, setDefinitions] = useState<SLADefinition[]>([]);
  const [customers, setCustomers] = useState<{ id: number; code: string; name: string }[]>([]);
  const [loading, setLoading] = useState(false);
  const [mspDisabled, setMspDisabled] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm<ContractFormValues>();

  const loadContracts = async () => {
    setLoading(true);
    try {
      const data = await SLAApi.listContracts();
      setContracts(data.items);
      setMspDisabled(false);
    } catch (error) {
      // 404 = 非 MSP 部署（后端 RequireMSPDeploymentMode）。渲染空态并给出解释。
      const status = (error as { status?: number })?.status;
      if (status === 404) {
        setMspDisabled(true);
        setContracts([]);
      } else {
        message.error('加载 SLA 合同失败');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadContracts();
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
    let values: ContractFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return; // 表单校验失败
    }
    try {
      setCreating(true);
      const payload: CreateMSPContractPayload = {
        contractNo: values.contractNo,
        name: values.name,
        customerTenantId: values.customerTenantId,
        bindings: values.bindings.map(b => ({
          slaDefinitionId: b.slaDefinitionId,
          ticketTypeId: b.ticketTypeId ?? 0,
        })),
      };
      if (values.effectiveUntil) payload.effectiveUntil = values.effectiveUntil.toISOString();
      if (values.settlementCycle) payload.settlementCycle = values.settlementCycle;
      if (values.currency) payload.currency = values.currency;
      await SLAApi.createContract(payload);
      message.success('合同创建成功');
      setShowCreate(false);
      form.resetFields();
      await loadContracts();
    } catch (error) {
      message.error('创建合同失败（请检查编号唯一、客户归属与绑定）');
    } finally {
      setCreating(false);
    }
  };

  const handleStatusChange = async (row: MSPContractItem, status: string) => {
    try {
      await SLAApi.updateContract(row.id, { status });
      message.success('合同状态已更新');
      await loadContracts();
    } catch {
      message.error('更新合同状态失败');
    }
  };

  const customerLabel = (id: number) => {
    const c = customers.find(item => item.id === id);
    return c ? `${c.name}（#${id}）` : `#${id}`;
  };

  const columns: ColumnsType<MSPContractItem> = [
    {
      title: '合同',
      key: 'contract',
      render: (_, row) => (
        <div>
          <Text strong>{row.name}</Text>
          <div className="text-xs text-gray-500">{row.contractNo}</div>
        </div>
      ),
    },
    {
      title: '客户租户',
      dataIndex: 'customerTenantId',
      render: (id: number) => customerLabel(id),
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => {
        const conf = STATUS_LABELS[s] ?? { label: s, color: 'default' };
        return <Tag color={conf.color}>{conf.label}</Tag>;
      },
    },
    {
      title: '合同期',
      key: 'period',
      render: (_, row) =>
        `${new Date(row.effectiveFrom).toLocaleDateString()} ~ ${
          row.effectiveUntil ? new Date(row.effectiveUntil).toLocaleDateString() : '长期'
        }`,
    },
    {
      title: '定义绑定',
      key: 'bindings',
      render: (_, row) => {
        const bindings = row.bindings ?? [];
        if (bindings.length === 0) return <Text type="secondary">-</Text>;
        return (
          <Space wrap size={[4, 4]}>
            {bindings.slice(0, 3).map(b => (
              <Tag key={b.id} color={b.ticketTypeId === 0 ? undefined : 'blue'}>
                {b.definitionName || `#${b.slaDefinitionId}`}
                {b.ticketTypeId === 0 ? '' : ` · 类型#${b.ticketTypeId}`}
              </Tag>
            ))}
            {bindings.length > 3 && <Text type="secondary">+{bindings.length - 3}</Text>}
          </Space>
        );
      },
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, row) => (
        <Space>
          {row.status !== 'active' && row.status !== 'expired' && (
            <Button size="small" onClick={() => handleStatusChange(row, 'active')}>
              启用
            </Button>
          )}
          {row.status === 'active' && (
            <Popconfirm
              title="终止该合同？"
              description="终止后该客户新工单不再命中合同定义（存量工单快照不受影响）。"
              onConfirm={() => handleStatusChange(row, 'terminated')}
            >
              <Button size="small" danger>
                终止
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <Title level={4} style={{ marginBottom: 4 }}>
            SLA 合同
          </Title>
          <Text type="secondary">
            合同承载 SLA 条款、合同期与定义绑定（含客户工单类型维度），作为结算与对账的依据。
          </Text>
        </div>
        <Button type="primary" disabled={mspDisabled} onClick={() => setShowCreate(true)}>
          新建合同
        </Button>
      </div>

      {mspDisabled && (
        <Alert
          type="info"
          showIcon
          className="mb-4"
          message="当前部署未启用 MSP 合同能力"
          description="SLA 合同仅在 saas_msp 部署模式下可用（DEPLOYMENT_MODE=saas_msp）。"
        />
      )}

      <Table<MSPContractItem>
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={contracts}
        pagination={false}
        locale={{ emptyText: mspDisabled ? '未启用' : '暂无合同' }}
      />

      <Modal
        title="新建 SLA 合同"
        open={showCreate}
        onCancel={() => setShowCreate(false)}
        onOk={handleCreate}
        confirmLoading={creating}
        destroyOnHidden
        width={640}
      >
        <Form form={form} layout="vertical">
          <Space size="large" style={{ display: 'flex' }} align="start">
            <Form.Item
              name="contractNo"
              label="合同编号"
              rules={[{ required: true, message: '请输入合同编号' }]}
            >
              <Input placeholder="例如 HT-2026-001" maxLength={160} style={{ width: 260 }} />
            </Form.Item>
            <Form.Item
              name="name"
              label="合同名称"
              rules={[{ required: true, message: '请输入合同名称' }]}
            >
              <Input placeholder="例如 年度运维合同" maxLength={200} style={{ width: 260 }} />
            </Form.Item>
          </Space>
          <Space size="large" style={{ display: 'flex' }} align="start">
            <Form.Item
              name="customerTenantId"
              label="客户租户"
              rules={[{ required: true, message: '请选择客户租户' }]}
            >
              <Select
                showSearch
                optionFilterProp="label"
                placeholder="选择归属本服务商的客户"
                style={{ width: 260 }}
                options={customers.map(c => ({ value: c.id, label: `${c.name}（#${c.id}）` }))}
              />
            </Form.Item>
            <Form.Item
              name="effectiveUntil"
              label="合同期至（留空 = 长期）"
              extra="合同期内工单按本合同绑定定义计时"
            >
              <DatePicker showTime style={{ width: 260 }} />
            </Form.Item>
          </Space>
          <Space size="large" style={{ display: 'flex' }} align="start">
            <Form.Item name="settlementCycle" label="结算周期">
              <Select
                style={{ width: 160 }}
                allowClear
                placeholder="默认 monthly"
                options={[
                  { value: 'monthly', label: '每月' },
                  { value: 'quarterly', label: '每季度' },
                  { value: 'on_demand', label: '按需' },
                ]}
              />
            </Form.Item>
            <Form.Item name="currency" label="结算币种">
              <Input placeholder="例如 CNY" maxLength={16} style={{ width: 120 }} />
            </Form.Item>
          </Space>

          <Form.Item label="SLA 定义绑定（至少一条；类型 ID=0 表示类型无关）" required>
            <Form.List name="bindings" initialValue={[{ slaDefinitionId: undefined, ticketTypeId: 0 }]}>
              {(fields, { add, remove }) => (
                <>
                  {fields.map(({ key, name, ...rest }) => (
                    <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                      <Form.Item
                        {...rest}
                        name={[name, 'slaDefinitionId']}
                        rules={[{ required: true, message: '请选择定义' }]}
                        style={{ marginBottom: 4 }}
                      >
                        <Select
                          showSearch
                          optionFilterProp="label"
                          placeholder="SLA 定义"
                          style={{ width: 280 }}
                          options={definitions.map(d => ({
                            value: d.id,
                            label: `${d.name}（#${d.id}）`,
                          }))}
                        />
                      </Form.Item>
                      <Form.Item {...rest} name={[name, 'ticketTypeId']} style={{ marginBottom: 4 }}>
                        <InputNumber
                          min={0}
                          placeholder="类型 ID（0=无关）"
                          style={{ width: 150 }}
                        />
                      </Form.Item>
                      {fields.length > 1 && (
                        <MinusCircleOutlined onClick={() => remove(name)} />
                      )}
                    </Space>
                  ))}
                  <Button type="dashed" onClick={() => add()} icon={<PlusOutlined />}>
                    添加绑定
                  </Button>
                </>
              )}
            </Form.List>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default SLAContractsPage;
