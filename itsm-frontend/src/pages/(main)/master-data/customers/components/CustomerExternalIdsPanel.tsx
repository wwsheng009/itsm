import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Button,
  Empty,
  Form,
  Input,
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
  type CustomerExternalID,
  type CustomerExternalIDWritePayload,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface CustomerExternalIdsPanelProps {
  customerId: number;
  canWrite: boolean;
  onChanged: () => void;
}

export default function CustomerExternalIdsPanel({
  customerId,
  canWrite,
  onChanged,
}: CustomerExternalIdsPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<CustomerExternalID[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<CustomerExternalID | null>(null);
  const [form] = Form.useForm<CustomerExternalIDWritePayload>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await masterDataService.externalIds(customerId);
      setItems(result.items ?? []);
    } catch {
      message.error('加载外部标识失败');
    } finally {
      setLoading(false);
    }
  }, [customerId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (item: CustomerExternalID) => {
    setEditing(item);
    form.setFieldsValue({
      systemCode: item.systemCode,
      externalId: item.externalId,
      externalUrl: item.externalUrl,
    });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    try {
      if (editing) {
        await masterDataService.updateCustomerExternalID(customerId, editing.id, values);
        message.success('外部标识已更新');
      } else {
        await masterDataService.createCustomerExternalID(customerId, values);
        message.success('外部标识已添加');
      }
      setModalOpen(false);
      await load();
      onChanged();
    } catch {
      message.error(editing ? '更新外部标识失败' : '添加外部标识失败');
    }
  };

  const remove = async (item: CustomerExternalID) => {
    try {
      await masterDataService.deleteCustomerExternalID(customerId, item.id);
      message.success('外部标识已删除');
      await load();
      onChanged();
    } catch {
      message.error('删除外部标识失败');
    }
  };

  const columns: ColumnsType<CustomerExternalID> = useMemo(
    () => [
      {
        title: '外部系统',
        dataIndex: 'systemCode',
        width: 130,
        render: (value: string) => <Tag color="blue">{value}</Tag>,
      },
      {
        title: '外部标识',
        dataIndex: 'externalId',
        render: (value: string, item) =>
          item.externalUrl ? (
            <a href={item.externalUrl} target="_blank" rel="noreferrer">
              {value}
            </a>
          ) : (
            value
          ),
      },
      {
        title: '对账链接',
        dataIndex: 'externalUrl',
        render: (value?: string) =>
          value ? (
            <Text type="secondary" ellipsis={{ tooltip: value }}>
              {value}
            </Text>
          ) : (
            '-'
          ),
      },
      {
        title: '创建时间',
        dataIndex: 'createdAt',
        width: 180,
        render: (value: string) =>
          value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-',
      },
      {
        title: '操作',
        width: 150,
        render: (_, item) =>
          canWrite ? (
            <Space>
              <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(item)}>
                编辑
              </Button>
              <Popconfirm title="删除该外部标识？" onConfirm={() => remove(item)}>
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
    [canWrite, openEdit, remove]
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Text type="secondary">
          外部标识用于 ERP / CRM / OA / 外部工单系统对账；同一租户内
          <Text code>系统编码 + 外部标识</Text>唯一。
        </Text>
        {canWrite && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            添加外部标识
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
        locale={{ emptyText: <Empty description="暂无外部标识" /> }}
      />
      <Modal
        title={editing ? '编辑外部标识' : '添加外部标识'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void submit()}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="systemCode"
            label="外部系统编码"
            rules={[{ required: true, message: '请输入外部系统编码' }]}
          >
            <Input placeholder="如 erp / crm / oa" maxLength={64} />
          </Form.Item>
          <Form.Item
            name="externalId"
            label="外部标识"
            rules={[{ required: true, message: '请输入外部标识' }]}
          >
            <Input maxLength={160} />
          </Form.Item>
          <Form.Item name="externalUrl" label="对账链接">
            <Input placeholder="https://..." maxLength={500} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
