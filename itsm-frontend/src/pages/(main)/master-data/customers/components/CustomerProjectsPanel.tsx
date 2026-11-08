import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  Modal,
  Popconfirm,
  Progress,
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
  type CustomerBranch,
  type ProjectCustomerLink,
} from '@/lib/services/masterDataService';
import { projectService, type Project } from '@/lib/services/project-service';

const { Text } = Typography;

interface CustomerProjectsPanelProps {
  customerId: number;
  branches: CustomerBranch[];
  canWrite: boolean;
  onChanged: () => void;
}

interface LinkFormValues {
  projectId: number;
  relationType: string;
  branchId?: number;
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

const projectTypeMeta: Record<string, string> = {
  internal: '内部',
  improvement: '改进',
  delivery: '交付',
};

const projectStatusMeta: Record<string, { label: string; color: string }> = {
  planning: { label: '规划中', color: 'default' },
  active: { label: '进行中', color: 'processing' },
  suspended: { label: '已暂停', color: 'warning' },
  completed: { label: '已完成', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
};

const linkTypeTag = (value: string) => {
  const meta = linkTypeMeta[value];
  return meta ? <Tag color={meta.color}>{meta.label}</Tag> : <Tag>{value}</Tag>;
};

export default function CustomerProjectsPanel({
  customerId,
  branches,
  canWrite,
  onChanged,
}: CustomerProjectsPanelProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<LinkFormValues>();
  const [links, setLinks] = useState<ProjectCustomerLink[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!customerId) return;
    setLoading(true);
    try {
      const [linkResult, projectResult] = await Promise.allSettled([
        masterDataService.customerProjects(customerId),
        projectService.listProjects({ page: 1, pageSize: 200 }),
      ]);
      if (linkResult.status === 'fulfilled') {
        setLinks(linkResult.value.items ?? []);
      } else {
        message.error('加载客户关联项目失败');
      }
      if (projectResult.status === 'fulfilled') {
        setProjects(projectResult.value.items ?? projectResult.value.projects ?? []);
      }
    } finally {
      setLoading(false);
    }
  }, [customerId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const linkedProjectIds = useMemo(
    () => new Set(links.map(link => link.projectId)),
    [links]
  );

  const projectOptions = projects
    .filter(project => project.type !== 'internal')
    .map(project => ({
      value: project.id,
      label: `${project.name}（${project.code}）`,
      disabled: linkedProjectIds.has(project.id),
    }));

  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue({ relationType: 'delivery', isPrimary: false });
    setModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      await masterDataService.createCustomerProjectLink(customerId, {
        projectId: values.projectId,
        relationType: values.relationType,
        branchId: values.branchId ? values.branchId : null,
        isPrimary: values.isPrimary ?? false,
        note: values.note,
      });
      message.success('项目关联已添加');
      setModalOpen(false);
      await load();
      onChanged();
    } catch (error) {
      message.error((error as Error).message || '添加项目关联失败');
    } finally {
      setSaving(false);
    }
  };

  const remove = async (link: ProjectCustomerLink) => {
    try {
      await masterDataService.deleteCustomerProjectLink(customerId, link.id);
      message.success('项目关联已删除');
      await load();
      onChanged();
    } catch {
      message.error('删除项目关联失败');
    }
  };

  const columns: ColumnsType<ProjectCustomerLink> = [
    {
      title: '项目',
      key: 'project',
      render: (_, link) => (
        <div>
          <Space size={6} wrap>
            <Text strong>{link.project?.name ?? `#${link.projectId}`}</Text>
            {link.isPrimary && <Tag color="gold">主关联</Tag>}
          </Space>
          <div className="text-xs text-gray-500">
            {[link.project?.code, projectTypeMeta[link.project?.type ?? '']]
              .filter(Boolean)
              .join(' · ') || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '项目状态',
      key: 'status',
      width: 110,
      render: (_, link) => {
        const status = projectStatusMeta[link.project?.status ?? ''];
        return status ? <Tag color={status.color}>{status.label}</Tag> : '-';
      },
    },
    {
      title: '进度',
      key: 'progress',
      width: 150,
      render: (_, link) => (
        <Progress percent={link.project?.progress ?? 0} size="small" />
      ),
    },
    {
      title: '关联类型',
      dataIndex: 'relationType',
      width: 100,
      render: (value: string) => linkTypeTag(value),
    },
    {
      title: '服务点',
      key: 'branch',
      width: 140,
      render: (_, link) => link.branch?.name ?? '-',
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
        {canWrite && projects.length > 0 && (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>
            关联项目
          </Button>
        )}
        <Button size="small" icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
      </Space>

      <Table
        rowKey="id"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={links}
        pagination={false}
        locale={{ emptyText: '暂无关联项目' }}
      />

      <Alert
        type="info"
        showIcon
        message="项目关联口径"
        description="项目主数据不新增客户列，客户与项目通过关联表双向查询；内部项目不可关联客户。"
      />

      <Modal
        title="关联项目"
        open={modalOpen}
        onOk={submit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="projectId"
            label="项目"
            rules={[{ required: true, message: '请选择项目' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              options={projectOptions}
              placeholder="仅展示可关联的外部项目"
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
          <Form.Item name="branchId" label="服务点">
            <Select
              allowClear
              options={[
                { value: 0, label: '不指定服务点' },
                ...branches
                  .filter(branch => branch.status === 'active')
                  .map(branch => ({
                    value: branch.id,
                    label: `${branch.name}${branch.code ? `（${branch.code}）` : ''}`,
                  })),
              ]}
            />
          </Form.Item>
          <Form.Item name="isPrimary" label="设为主关联" valuePropName="checked">
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
