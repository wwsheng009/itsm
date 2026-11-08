import { useCallback, useEffect, useState } from 'react';
import {
  App,
  Button,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import { CheckCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import {
  masterDataService,
  type ContractAmendment,
  type ContractAmendmentWritePayload,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface FormValues extends Omit<ContractAmendmentWritePayload, 'effectiveFrom' | 'signedAt'> {
  effectiveFrom?: Dayjs;
}

interface ContractAmendmentsPanelProps {
  contractId: number;
  canWrite: boolean;
  onChanged: () => void;
}

export default function ContractAmendmentsPanel({
  contractId,
  canWrite,
  onChanged,
}: ContractAmendmentsPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<ContractAmendment[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm<FormValues>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await masterDataService.contractAmendments(contractId);
      setItems(result.items ?? []);
    } catch {
      message.error('加载变更单失败');
    } finally {
      setLoading(false);
    }
  }, [contractId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue({ amendType: 'amount', effectiveFrom: dayjs() });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    try {
      await masterDataService.createContractAmendment(contractId, {
        ...values,
        effectiveFrom: values.effectiveFrom?.toISOString(),
      });
      message.success('变更单已创建');
      setModalOpen(false);
      await load();
      onChanged();
    } catch {
      message.error('创建变更单失败');
    }
  };

  const effect = async (item: ContractAmendment) => {
    try {
      await masterDataService.effectContractAmendment(contractId, item.id);
      message.success('变更已生效');
      await load();
      onChanged();
    } catch {
      message.error('变更生效失败');
    }
  };

  const columns: ColumnsType<ContractAmendment> = [
    { title: '变更单号', dataIndex: 'amendmentNo', width: 120 },
    {
      title: '类型',
      dataIndex: 'amendType',
      width: 100,
      render: (value: string) => <Tag>{value}</Tag>,
    },
    {
      title: '生效日期',
      dataIndex: 'effectiveFrom',
      width: 120,
      render: (value: string) => dayjs(value).format('YYYY-MM-DD'),
    },
    {
      title: '金额增减',
      dataIndex: 'amountDelta',
      width: 120,
      render: (value: number) => (
        <Text type={value < 0 ? 'danger' : undefined}>{value.toFixed(2)}</Text>
      ),
    },
    {
      title: '展期天数',
      dataIndex: 'termDeltaDays',
      width: 100,
      render: (value?: number | null) => value ?? '-',
    },
    { title: '原因', dataIndex: 'reason', render: (value: string) => value || '-' },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (value: string) => (
        <Tag color={value === 'effective' ? 'green' : value === 'draft' ? 'blue' : 'default'}>
          {value}
        </Tag>
      ),
    },
    {
      title: '操作',
      width: 120,
      render: (_, item) =>
        canWrite && item.status === 'draft' ? (
          <Button
            size="small"
            icon={<CheckCircleOutlined />}
            onClick={() => void effect(item)}
          >
            生效
          </Button>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Text type="secondary">
          已生效合同的金额/期限调整必须走变更单；合同原始金额保持不变。
        </Text>
        {canWrite && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            新建变更单
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
        locale={{ emptyText: '暂无变更单' }}
      />
      <Modal
        title="新建合同变更单"
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void submit()}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="amendmentNo" label="变更单号" rules={[{ required: true }]}>
            <Input placeholder="A1 / A2" maxLength={64} />
          </Form.Item>
          <Form.Item name="amendType" label="变更类型" rules={[{ required: true }]}>
            <Input placeholder="amount / term / scope / id_only" />
          </Form.Item>
          <Form.Item name="effectiveFrom" label="生效日期" rules={[{ required: true }]}>
            <DatePicker className="w-full" />
          </Form.Item>
          <Space size="middle" wrap>
            <Form.Item name="amountDelta" label="金额增减">
              <InputNumber precision={2} />
            </Form.Item>
            <Form.Item name="termDeltaDays" label="展期天数">
              <InputNumber />
            </Form.Item>
          </Space>
          <Form.Item name="reason" label="变更原因">
            <Input.TextArea rows={3} maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
