import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type ContractServiceItem,
  type ContractServiceItemWritePayload,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface ContractItemsPanelProps {
  contractId: number;
  contractStatus: string;
  canWrite: boolean;
  onChanged: () => void;
}

const editableStatuses = new Set(['draft', 'pending_sign', 'pending']);

export default function ContractItemsPanel({
  contractId,
  contractStatus,
  canWrite,
  onChanged,
}: ContractItemsPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<ContractServiceItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ContractServiceItem | null>(null);
  const [form] = Form.useForm<ContractServiceItemWritePayload>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await masterDataService.contractItems(contractId);
      setItems(result.items ?? []);
    } catch {
      message.error('加载服务项失败');
    } finally {
      setLoading(false);
    }
  }, [contractId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ billingUnit: 'month', quantity: 1, periodCount: 1, unitPrice: 0 });
    setModalOpen(true);
  };

  const openEdit = (item: ContractServiceItem) => {
    setEditing(item);
    form.setFieldsValue({
      itemCode: item.itemCode,
      itemName: item.itemName,
      spec: item.spec,
      billingUnit: item.billingUnit,
      quantity: item.quantity,
      periodCount: item.periodCount,
      unitPrice: item.unitPrice,
      amount: item.amount,
      quotaTotal: item.quotaTotal,
      note: item.note,
      sortOrder: item.sortOrder,
      status: item.status,
    });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    try {
      if (editing) {
        await masterDataService.updateContractItem(contractId, editing.id, values);
        message.success('服务项已更新');
      } else {
        await masterDataService.createContractItem(contractId, values);
        message.success('服务项已添加');
      }
      setModalOpen(false);
      setEditing(null);
      await load();
      onChanged();
    } catch {
      message.error(editing ? '更新服务项失败' : '添加服务项失败');
    }
  };

  const remove = async (item: ContractServiceItem) => {
    try {
      await masterDataService.deleteContractItem(contractId, item.id);
      message.success('服务项已删除');
      await load();
      onChanged();
    } catch {
      message.error('删除服务项失败');
    }
  };

  const columns: ColumnsType<ContractServiceItem> = useMemo(
    () => [
      {
        title: '服务项',
        key: 'item',
        render: (_, item) => (
          <div>
            <Text strong>{item.itemName}</Text>
            <div className="text-xs text-gray-500">
              {[item.itemCode || '-', item.spec || '-'].join(' · ')}
            </div>
          </div>
        ),
      },
      {
        title: '计量单位',
        dataIndex: 'billingUnit',
        width: 100,
        render: (value: string) => <Tag>{value}</Tag>,
      },
      {
        title: '数量 / 周期',
        key: 'quantity',
        width: 120,
        render: (_, item) => `${item.quantity} / ${item.periodCount}`,
      },
      {
        title: '单价',
        dataIndex: 'unitPrice',
        width: 100,
        render: (value: number) => value.toFixed(2),
      },
      {
        title: '金额',
        dataIndex: 'amount',
        width: 110,
        render: (value: number) => value.toFixed(2),
      },
      {
        title: '额度（总量 / 已耗 / 预占 / 可分配）',
        key: 'quota',
        width: 230,
        render: (_, item) =>
          item.quotaTotal == null
            ? '不限额'
            : `${item.quotaTotal} / ${item.quotaUsed} / ${item.quotaReserved} / ${
                item.availableToAllocate ?? '-'
              }`,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 90,
        render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag>,
      },
      {
        title: '操作',
        width: 150,
        render: (_, item) =>
          canWrite && editableStatuses.has(contractStatus) ? (
            <Space>
              <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(item)}>
                编辑
              </Button>
              <Popconfirm title="删除该服务项？" onConfirm={() => remove(item)}>
                <Button size="small" danger icon={<DeleteOutlined />}>
                  删除
                </Button>
              </Popconfirm>
            </Space>
          ) : (
            '-'
          ),
      },
    ],
    [canWrite, contractStatus, openEdit, remove]
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Text type="secondary">
          服务项是合同履约明细；签订后仅允许新增，不允许修改历史服务项。
        </Text>
        {canWrite && contractStatus !== 'terminated' && contractStatus !== 'renewed' && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            添加服务项
          </Button>
        )}
      </div>
      <Table
        rowKey="id"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={items}
        pagination={false}
        scroll={{ x: 1100 }}
        locale={{ emptyText: '暂无服务项' }}
      />
      <Modal
        title={editing ? '编辑服务项' : '添加服务项'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void submit()}
        width={620}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="itemName" label="服务项名称" rules={[{ required: true }]}>
            <Input maxLength={255} />
          </Form.Item>
          <Form.Item name="itemCode" label="服务项编码">
            <Input maxLength={64} />
          </Form.Item>
          <Form.Item name="spec" label="规格 / 范围">
            <Input maxLength={500} />
          </Form.Item>
          <Form.Item name="billingUnit" label="计量单位" rules={[{ required: true }]}>
            <Input placeholder="month / hour / ticket / device / project" />
          </Form.Item>
          <Space size="middle" wrap>
            <Form.Item name="quantity" label="数量">
              <InputNumber min={0} precision={4} />
            </Form.Item>
            <Form.Item name="periodCount" label="周期数">
              <InputNumber min={0.0001} precision={4} />
            </Form.Item>
            <Form.Item name="unitPrice" label="单价">
              <InputNumber min={0} precision={2} />
            </Form.Item>
            <Form.Item name="amount" label="金额（可议价覆盖）">
              <InputNumber min={0} precision={2} />
            </Form.Item>
            <Form.Item name="quotaTotal" label="额度总量">
              <InputNumber min={0} precision={4} />
            </Form.Item>
          </Space>
          <Form.Item
            name="note"
            label="备注"
            extra="议价金额与公式计算值不一致时必须填写说明。"
          >
            <Input maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
