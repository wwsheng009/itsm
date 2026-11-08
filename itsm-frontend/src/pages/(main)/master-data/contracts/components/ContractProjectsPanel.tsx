import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Button,
  Checkbox,
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
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type ContractProjectLink,
  type ContractProjectLinkWritePayload,
  type ContractServiceItem,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface ContractProjectsPanelProps {
  contractId: number;
  canWrite: boolean;
  onChanged: () => void;
}

export default function ContractProjectsPanel({
  contractId,
  canWrite,
  onChanged,
}: ContractProjectsPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<ContractProjectLink[]>([]);
  const [serviceItems, setServiceItems] = useState<ContractServiceItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm<ContractProjectLinkWritePayload>();
  const relationType = Form.useWatch('relationType', form);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [projects, serviceItemResult] = await Promise.all([
        masterDataService.contractProjects(contractId),
        masterDataService.contractItems(contractId),
      ]);
      setItems(projects.items ?? []);
      setServiceItems(serviceItemResult.items ?? []);
    } catch {
      message.error('加载合同项目关联失败');
    } finally {
      setLoading(false);
    }
  }, [contractId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue({ relationType: 'basis', isPrimary: false });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    try {
      await masterDataService.createContractProjectLink(contractId, values);
      message.success('合同项目关联已创建');
      setModalOpen(false);
      await load();
      onChanged();
    } catch {
      message.error('创建合同项目关联失败');
    }
  };

  const remove = async (item: ContractProjectLink) => {
    try {
      await masterDataService.deleteContractProjectLink(contractId, item.id);
      message.success('合同项目关联已解除');
      await load();
      onChanged();
    } catch {
      message.error('解除合同项目关联失败');
    }
  };

  const columns: ColumnsType<ContractProjectLink> = useMemo(
    () => [
      {
        title: '项目',
        key: 'project',
        render: (_, item) => (
          <div>
            <Text strong>{item.projectName || `#${item.projectId}`}</Text>
            <div className="text-xs text-gray-500">{item.projectCode || '-'}</div>
          </div>
        ),
      },
      {
        title: '关系',
        dataIndex: 'relationType',
        width: 100,
        render: (value: string) => <Tag color={value === 'billing' ? 'gold' : 'blue'}>{value}</Tag>,
      },
      {
        title: '服务项',
        dataIndex: 'serviceItemName',
        width: 160,
        render: (value: string, item) => value || item.serviceItemId || '-',
      },
      {
        title: '分配额度',
        key: 'allocation',
        width: 200,
        render: (_, item) =>
          [
            item.allocatedAmount != null ? `金额 ${item.allocatedAmount}` : '',
            item.allocatedHours != null ? `数量 ${item.allocatedHours}` : '',
            item.allocationPct != null ? `比例 ${item.allocationPct}%` : '',
          ]
            .filter(Boolean)
            .join(' / ') || '-',
      },
      {
        title: '主依据',
        dataIndex: 'isPrimary',
        width: 90,
        render: (value: boolean) => (value ? <Tag color="blue">是</Tag> : '-'),
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 90,
        render: (value: string) => <Tag>{value}</Tag>,
      },
      {
        title: '操作',
        width: 100,
        render: (_, item) =>
          canWrite ? (
            <Popconfirm title="解除该合同项目关联？" onConfirm={() => remove(item)}>
              <Button size="small" danger icon={<DeleteOutlined />}>
                解除
              </Button>
            </Popconfirm>
          ) : (
            '-'
          ),
      },
    ],
    [canWrite, remove]
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Text type="secondary">
          `basis`/`coverage` 是语义关联；只有 `billing` 行可携带金额/数量额度。项目客户必须与合同客户一致。
        </Text>
        {canWrite && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            关联项目
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
        scroll={{ x: 1000 }}
        locale={{ emptyText: '暂无合同项目关联' }}
      />
      <Modal
        title="关联合同与项目"
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void submit()}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="projectId" label="项目 ID" rules={[{ required: true }]}>
            <InputNumber min={1} className="w-full" placeholder="输入项目 ID" />
          </Form.Item>
          <Form.Item name="relationType" label="关系类型">
            <Select
              options={[
                { value: 'basis', label: 'basis · 立项依据' },
                { value: 'coverage', label: 'coverage · 运维覆盖' },
                { value: 'billing', label: 'billing · 额度分配' },
                { value: 'change', label: 'change · 变更追加' },
              ]}
            />
          </Form.Item>
          {relationType === 'billing' && (
            <>
              <Form.Item
                name="serviceItemId"
                label="服务项"
                rules={[{ required: true, message: 'billing 行必须选择服务项' }]}
              >
                <Select
                  options={serviceItems.map(item => ({
                    value: item.id,
                    label: `${item.itemName}（可分配 ${item.availableToAllocate ?? '不限'}）`,
                  }))}
                />
              </Form.Item>
              <Space size="middle" wrap>
                <Form.Item name="allocatedAmount" label="分配金额">
                  <InputNumber min={0} precision={2} />
                </Form.Item>
                <Form.Item name="allocatedHours" label="分配数量">
                  <InputNumber min={0} precision={2} />
                </Form.Item>
                <Form.Item name="allocationPct" label="分配比例 %">
                  <InputNumber min={0} max={100} />
                </Form.Item>
              </Space>
            </>
          )}
          {relationType === 'basis' && (
            <Form.Item name="isPrimary" valuePropName="checked">
              <Checkbox>设为本项目主合同依据</Checkbox>
            </Form.Item>
          )}
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
