import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type ProjectCustomerLink,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface ProjectCustomerLinksPanelProps {
  projectId: number;
  canWrite: boolean;
}

interface LinkFormValues {
  customerId: number;
  relationType: string;
  isPrimary?: boolean;
  note?: string;
}

const linkTypeMeta: Record<string, { label: string; color: string }> = {
  presales: { label: '售前', color: 'purple' },
  delivery: { label: '交付', color: 'blue' },
  implementation: { label: '实施', color: 'cyan' },
  support: { label: '支持', color: 'green' },
  renewal: { label: '续约', color: 'gold' },
  escalation: { label: '升级', color: 'red' },
};

const linkTypeTag = (value: string) => {
  const meta = linkTypeMeta[value];
  return meta ? <Tag color={meta.color}>{meta.label}</Tag> : <Tag>{value}</Tag>;
};

export default function ProjectCustomerLinksPanel({
  projectId,
  canWrite,
}: ProjectCustomerLinksPanelProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<LinkFormValues>();
  const [links, setLinks] = useState<ProjectCustomerLink[]>([]);
  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const [linkResult, customerResult] = await Promise.allSettled([
        masterDataService.projectCustomerLinks(projectId),
        masterDataService.customers({ page: 1, pageSize: 300 }),
      ]);
      if (linkResult.status === 'fulfilled') {
        setLinks(linkResult.value.items ?? []);
      } else {
        message.error('加载项目关联客户失败');
      }
      if (customerResult.status === 'fulfilled') {
        setCustomers(customerResult.value.items ?? []);
      }
    } finally {
      setLoading(false);
    }
  }, [message, projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue({ relationType: 'delivery', isPrimary: false });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      await masterDataService.createProjectCustomerLink(projectId, {
        customerId: values.customerId,
        relationType: values.relationType,
        isPrimary: values.isPrimary ?? false,
        note: values.note,
      });
      message.success('客户关联已添加');
      setModalOpen(false);
      await load();
    } catch (error) {
      message.error((error as Error).message || '添加客户关联失败');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (link: ProjectCustomerLink) => {
    try {
      await masterDataService.deleteProjectCustomerLink(projectId, link.id);
      message.success('客户关联已删除');
      await load();
    } catch {
      message.error('删除客户关联失败');
    }
  };

  const columns: ColumnsType<ProjectCustomerLink> = [
    {
      title: '客户',
      key: 'customer',
      render: (_, link) => (
        <div>
          <Space size={6} wrap>
            <Text strong>{link.customer?.name ?? `#${link.customerId}`}</Text>
            {link.isPrimary && <Tag color="gold">主客户</Tag>}
          </Space>
          <div className="text-xs text-gray-500">
            {[link.customer?.code, link.customer?.shortName, link.customer?.tier]
              .filter(Boolean)
              .join(' · ') || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '关联类型',
      dataIndex: 'relationType',
      width: 110,
      render: (value: string) => linkTypeTag(value),
    },
    {
      title: '服务点',
      key: 'branch',
      width: 150,
      render: (_, link) => link.branch?.name ?? '-',
    },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (_, link) =>
        link.customer?.status === 'active' ? <Tag color="green">活跃</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '备注',
      dataIndex: 'note',
      ellipsis: true,
      render: (value?: string) => value || '-',
    },
    {
      title: '操作',
      width: 90,
      render: (_, link) =>
        canWrite ? (
          <Popconfirm title="解除该客户与项目的关联？" onConfirm={() => remove(link)}>
            <Button size="small" danger icon={<DeleteOutlined />}>
              解除
            </Button>
          </Popconfirm>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <div className="space-y-3">
      <Space wrap>
        {canWrite && customers.length > 0 && (
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            关联客户
          </Button>
        )}
        <Button icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={links}
        pagination={false}
        size="small"
        locale={{ emptyText: '暂无关联客户' }}
      />

      <Alert
        type="info"
        showIcon
        message="同一客户可按不同关系类型多次关联；每个项目仅允许一个主客户。"
      />

      <Modal
        title="关联客户"
        open={modalOpen}
        onOk={submit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="customerId"
            label="客户"
            rules={[{ required: true, message: '请选择客户' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              options={customers.map(customer => ({
                value: customer.id,
                label: `${customer.name}（${customer.code || '无编码'}）`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="relationType"
            label="关联类型"
            rules={[{ required: true, message: '请选择关联类型' }]}
          >
            <Select
              options={Object.entries(linkTypeMeta).map(([value, meta]) => ({
                value,
                label: meta.label,
              }))}
            />
          </Form.Item>
          <Form.Item name="isPrimary" label="设为主客户" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
