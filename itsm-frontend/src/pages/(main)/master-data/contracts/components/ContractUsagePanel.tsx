import { useCallback, useEffect, useState } from 'react';
import {
  App,
  Button,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Typography,
} from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import {
  masterDataService,
  type ContractServiceItem,
  type ContractUsageEntry,
  type ContractUsageWritePayload,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface FormValues extends Omit<ContractUsageWritePayload, 'occurredAt'> {
  occurredAt?: Dayjs;
}

interface ContractUsagePanelProps {
  contractId: number;
  canWrite: boolean;
  onChanged: () => void;
}

export default function ContractUsagePanel({
  contractId,
  canWrite,
  onChanged,
}: ContractUsagePanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<ContractUsageEntry[]>([]);
  const [serviceItems, setServiceItems] = useState<ContractServiceItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm<FormValues>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [usage, serviceItemResult] = await Promise.all([
        masterDataService.contractUsage(contractId),
        masterDataService.contractItems(contractId),
      ]);
      setItems(usage.items ?? []);
      setServiceItems(serviceItemResult.items ?? []);
    } catch {
      message.error('加载用量台账失败');
    } finally {
      setLoading(false);
    }
  }, [contractId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue({
      sourceType: 'manual_adjustment',
      occurredAt: dayjs(),
      quantityDelta: 0,
      amountDelta: 0,
    });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    try {
      await masterDataService.recordContractUsage(contractId, {
        ...values,
        occurredAt: values.occurredAt?.toISOString(),
      });
      message.success('用量已写入台账');
      setModalOpen(false);
      await load();
      onChanged();
    } catch {
      message.error('写入用量台账失败');
    }
  };

  const columns: ColumnsType<ContractUsageEntry> = [
    {
      title: '服务项',
      dataIndex: 'serviceItemId',
      width: 180,
      render: (id: number) =>
        serviceItems.find(item => item.id === id)?.itemName ?? `#${id}`,
    },
    {
      title: '来源',
      key: 'source',
      width: 180,
      render: (_, item) => `${item.sourceType} / ${item.sourceRef}`,
    },
    { title: '数量增减', dataIndex: 'quantityDelta', width: 110 },
    { title: '金额增减', dataIndex: 'amountDelta', width: 110 },
    {
      title: '发生时间',
      dataIndex: 'occurredAt',
      width: 180,
      render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm'),
    },
    { title: '备注', dataIndex: 'note', render: (value: string) => value || '-' },
  ];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Text type="secondary">
          用量台账以 `服务项 + 来源类型 + 来源单号` 幂等；同源重复提交不会重复扣减额度。
        </Text>
        {canWrite && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            登记用量
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
        scroll={{ x: 900 }}
        locale={{ emptyText: '暂无用量记录' }}
      />
      <Modal
        title="登记合同用量"
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void submit()}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="serviceItemId" label="服务项" rules={[{ required: true }]}>
            <Select
              options={serviceItems.map(item => ({
                value: item.id,
                label: `${item.itemName}（剩余 ${item.availableToAllocate ?? '不限'}）`,
              }))}
            />
          </Form.Item>
          <Form.Item name="sourceType" label="来源类型" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'timesheet', label: '工时' },
                { value: 'ticket', label: '工单' },
                { value: 'device_snapshot', label: '设备快照' },
                { value: 'project_close', label: '项目结项' },
                { value: 'manual_adjustment', label: '手工调整' },
              ]}
            />
          </Form.Item>
          <Form.Item name="sourceRef" label="来源单号 / 事件 ID" rules={[{ required: true }]}>
            <Input maxLength={160} />
          </Form.Item>
          <Space size="middle" wrap>
            <Form.Item name="quantityDelta" label="数量增减">
              <InputNumber precision={4} />
            </Form.Item>
            <Form.Item name="amountDelta" label="金额增减">
              <InputNumber precision={2} />
            </Form.Item>
          </Space>
          <Form.Item name="occurredAt" label="发生时间">
            <DatePicker showTime className="w-full" />
          </Form.Item>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
