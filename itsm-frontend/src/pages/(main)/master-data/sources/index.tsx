import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { useCan } from '@/components/auth/PermissionGate';
import {
  masterDataService,
  type SourceOrganization,
} from '@/lib/services/masterDataService';
import MasterDataTabs from '../components/MasterDataTabs';

const { Title, Text } = Typography;

interface SourceFormValues {
  name: string;
  emailAddresses?: string;
  emailDomains?: string;
}

const splitTokens = (value?: string) =>
  (value ?? '')
    .split(/[,，\n]/)
    .map(item => item.trim())
    .filter(Boolean);

export default function SourcesPage() {
  const { message } = App.useApp();
  const canWrite = useCan(['customer_master:write']);
  const [items, setItems] = useState<SourceOrganization[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<SourceOrganization | null>(null);
  const [form] = Form.useForm<SourceFormValues>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await masterDataService.sourceOrganizations();
      setItems(result.items);
    } catch {
      message.error('加载来源组织失败');
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    void load();
  }, [load]);

  const filteredItems = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    if (!keyword) return items;
    return items.filter(item =>
      [item.name, ...(item.emailAddresses ?? []), ...(item.emailDomains ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
    );
  }, [items, search]);

  const openModal = (item?: SourceOrganization) => {
    setEditing(item ?? null);
    setModalOpen(true);
    if (item) {
      form.setFieldsValue({
        name: item.name,
        emailAddresses: item.emailAddresses?.join(', '),
        emailDomains: item.emailDomains?.join(', '),
      });
    } else {
      form.resetFields();
    }
  };

  const save = async () => {
    const values = await form.validateFields();
    const payload = {
      name: values.name,
      emailAddresses: splitTokens(values.emailAddresses),
      emailDomains: splitTokens(values.emailDomains).map(domain => domain.replace(/^@/, '')),
      status: editing?.status ?? 'active',
    };
    try {
      if (editing) {
        await masterDataService.updateSourceOrganization(editing.id, payload);
        message.success('来源组织已更新');
      } else {
        await masterDataService.createSourceOrganization(payload);
        message.success('来源组织已创建');
      }
      setModalOpen(false);
      setEditing(null);
      form.resetFields();
      await load();
    } catch {
      message.error('保存来源组织失败');
    }
  };

  const disable = async (id: number) => {
    try {
      await masterDataService.disableSourceOrganization(id);
      message.success('来源组织已停用');
      await load();
    } catch {
      message.error('停用来源组织失败');
    }
  };

  const columns: ColumnsType<SourceOrganization> = [
    {
      title: '组织名称',
      key: 'name',
      render: (_, item) => (
        <div>
          <Text strong>{item.name}</Text>
          <div className="text-xs text-gray-500">#{item.id}</div>
        </div>
      ),
    },
    {
      title: '邮箱地址',
      dataIndex: 'emailAddresses',
      render: (value: string[]) =>
        value?.length ? value.map(address => <Tag key={address}>{address}</Tag>) : '-',
    },
    {
      title: '邮箱域名',
      dataIndex: 'emailDomains',
      render: (value: string[]) =>
        value?.length
          ? value.map(domain => (
              <Tag key={domain} color="blue">
                {domain}
              </Tag>
            ))
          : '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (status: string) =>
        status === 'active' ? <Tag color="green">活跃</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '操作',
      width: 160,
      render: (_, item) =>
        canWrite ? (
          <Space>
            <Button size="small" icon={<EditOutlined />} onClick={() => openModal(item)}>
              编辑
            </Button>
            {item.status === 'active' && (
              <Popconfirm title="确定停用该来源组织？" onConfirm={() => disable(item.id)}>
                <Button size="small" danger icon={<DeleteOutlined />}>
                  停用
                </Button>
              </Popconfirm>
            )}
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <div className="space-y-4">
      <MasterDataTabs />
      <Card>
        <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
          <div>
            <Title level={4} className="!mb-1">
              来源组织
            </Title>
            <Text type="secondary">
              通过发件人邮箱地址或域名识别客户身份；本页属于主数据，不再挂在邮件报障下。
            </Text>
          </div>
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索组织、邮箱或域名"
              style={{ width: 280 }}
              value={search}
              onChange={event => setSearch(event.target.value)}
            />
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>
              刷新
            </Button>
            {canWrite && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openModal()}>
                新增组织
              </Button>
            )}
          </Space>
        </div>

        <Alert
          className="mb-3"
          type="info"
          showIcon
          message="来源组织是主数据"
          description="邮件报障使用来源组织做发件人识别；维护入口已归位到 /master-data/sources。"
        />

        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={filteredItems}
          pagination={{ pageSize: 20, showSizeChanger: true }}
        />
      </Card>

      <Modal
        title={editing ? '编辑来源组织' : '新增来源组织'}
        open={modalOpen}
        onOk={save}
        onCancel={() => {
          setModalOpen(false);
          setEditing(null);
        }}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="组织名称" rules={[{ required: true }]}>
            <Input placeholder="例如：某某科技 IT 部门" />
          </Form.Item>
          <Form.Item name="emailAddresses" label="邮箱地址（逗号或换行分隔）">
            <Input.TextArea rows={2} placeholder="support@example.com, noc@example.com" />
          </Form.Item>
          <Form.Item name="emailDomains" label="邮箱域名（逗号分隔）">
            <Input placeholder="example.com, example.org" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
