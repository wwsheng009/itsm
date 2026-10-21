import { useCallback, useEffect, useState } from 'react';
import { Button, Card, Checkbox, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Plus, RefreshCw } from 'lucide-react';
import { useNavigate } from 'react-router';
import { PageContainer } from '@/components/common/PageContainer';
import { useCan } from '@/components/auth/PermissionGate';
import { projectService } from '@/lib/services/project-service';
import type {
  ReportCadence,
  ReportGroup,
  ReportSection,
  ReportTemplate,
  ReportTemplateStatus,
} from '@/lib/services/project-service';

const CADENCE_LABEL: Record<ReportCadence, string> = {
  weekly: '周报',
  biweekly: '双周报',
  monthly: '月报',
};

const CADENCE_OPTIONS = Object.entries(CADENCE_LABEL).map(([value, label]) => ({ value, label }));

const SECTION_LABEL: Record<ReportSection, string> = {
  overview: '概览',
  tasks: '任务',
  milestones: '里程碑',
  gates: '阶段门',
  variance: '计划偏差',
  team: '团队归口',
};

const SECTION_OPTIONS = Object.entries(SECTION_LABEL).map(([value, label]) => ({ value, label }));

const STATUS_META: Record<ReportTemplateStatus, { label: string; color: string }> = {
  draft: { label: '草稿', color: 'default' },
  active: { label: '启用', color: 'success' },
  archived: { label: '已归档', color: 'default' },
};

const DEFAULT_SECTIONS: ReportSection[] = ['overview', 'tasks', 'milestones', 'gates', 'variance'];

/**
 * 报表中心（PE-17）：归口分组（项目数/健康度分布）+ 状态报告模板管理（模板 + 周期数据驱动）。
 * 报告本体在项目工作台「状态报告」Tab 生成/导出。
 */
export default function ProjectReportsPage() {
  const navigate = useNavigate();
  const canWrite = useCan(['project:write']);
  const [groups, setGroups] = useState<ReportGroup[]>([]);
  const [groupsLoading, setGroupsLoading] = useState(false);
  const [templates, setTemplates] = useState<ReportTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(false);
  const [statusFilter, setStatusFilter] = useState<ReportTemplateStatus | ''>('');
  const [keyword, setKeyword] = useState('');
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ReportTemplate | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  const fetchGroups = useCallback(async () => {
    setGroupsLoading(true);
    try {
      setGroups(await projectService.reportGroups());
    } catch (error) {
      console.error('Failed to fetch report groups:', error);
    } finally {
      setGroupsLoading(false);
    }
  }, []);

  const fetchTemplates = useCallback(
    async (current = 1, pageSize = 10, status = statusFilter, q = keyword) => {
      setTemplatesLoading(true);
      try {
        const result = await projectService.listReportTemplates({
          page: current,
          pageSize,
          status,
          q: q || undefined,
        });
        setTemplates(result.items);
        setPagination({ current: result.page, pageSize: result.pageSize, total: result.total });
      } catch (error) {
        console.error('Failed to fetch report templates:', error);
      } finally {
        setTemplatesLoading(false);
      }
    },
    [keyword, statusFilter]
  );

  useEffect(() => {
    void fetchGroups();
    void fetchTemplates();
  }, []);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ cadence: 'weekly', sections: DEFAULT_SECTIONS, status: 'draft' });
    setModalOpen(true);
  };

  const openEdit = (record: ReportTemplate) => {
    setEditing(record);
    form.setFieldsValue({
      name: record.name,
      description: record.description,
      cadence: record.cadence,
      sections: record.sections.length > 0 ? record.sections : DEFAULT_SECTIONS,
      status: record.status,
    });
    setModalOpen(true);
  };

  const submit = async () => {
    try {
      const values = await form.validateFields();
      setSaving(true);
      if (editing) {
        await projectService.updateReportTemplate(editing.id, { ...values, version: editing.version });
        message.success('模板已更新');
      } else {
        await projectService.createReportTemplate(values);
        message.success('模板已创建（草稿态，启用后可在工作台生成报告）');
      }
      setModalOpen(false);
      await fetchTemplates(pagination.current, pagination.pageSize);
    } catch (error) {
      if ((error as { errorFields?: unknown }).errorFields) return;
      console.error('Save report template failed:', error);
      message.error('保存失败（名称可能已存在或版本冲突，请刷新重试）');
    } finally {
      setSaving(false);
    }
  };

  const toggleStatus = async (record: ReportTemplate) => {
    const next: ReportTemplateStatus = record.status === 'active' ? 'archived' : 'active';
    try {
      await projectService.updateReportTemplate(record.id, { status: next, version: record.version });
      message.success(next === 'active' ? '模板已启用' : '模板已归档');
      await fetchTemplates(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Toggle report template failed:', error);
      message.error('状态切换失败（可能已被其他操作更新）');
    }
  };

  const remove = async (record: ReportTemplate) => {
    try {
      await projectService.deleteReportTemplate(record.id);
      message.success('模板已删除（软删；名称不释放）');
      await fetchTemplates(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Delete report template failed:', error);
      message.error('删除失败');
    }
  };

  const groupColumns: ColumnsType<ReportGroup> = [
    {
      title: '归口团队',
      dataIndex: 'ownerTeamName',
      key: 'ownerTeamName',
      render: (value: string, record) => (
        <Space size={4}>
          <span>{value}</span>
          {record.ownerTeamDeleted && <Tag color="error">已停用</Tag>}
        </Space>
      ),
    },
    {
      // A2（回归遗留）：归口未设置时仍可按部门归因，避免整表只有「未归口」。
      title: '所属部门',
      key: 'departmentName',
      width: 160,
      render: (_: unknown, record) =>
        record.departmentName || (record.departmentId ? `部门 #${record.departmentId}` : '未设置'),
    },
    { title: '项目数', dataIndex: 'projectCount', key: 'projectCount', width: 100 },
    {
      title: '健康度分布',
      key: 'health',
      render: (_, record) => (
        <Space size={4}>
          <Tag color="success">健康 {record.greenCount}</Tag>
          <Tag color="warning">关注 {record.yellowCount}</Tag>
          <Tag color="error">风险 {record.redCount}</Tag>
        </Space>
      ),
    },
  ];

  const templateColumns: ColumnsType<ReportTemplate> = [
    { title: '模板名称', dataIndex: 'name', key: 'name' },
    {
      title: '周期',
      dataIndex: 'cadence',
      key: 'cadence',
      width: 110,
      render: (value: ReportCadence) => CADENCE_LABEL[value] ?? value,
    },
    {
      title: '章节',
      dataIndex: 'sections',
      key: 'sections',
      render: (value: ReportSection[]) => (
        <Space size={4} wrap>
          {(value.length > 0 ? value : DEFAULT_SECTIONS).map((section) => (
            <Tag key={section}>{SECTION_LABEL[section] ?? section}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: ReportTemplateStatus) => (
        <Tag color={STATUS_META[value]?.color}>{STATUS_META[value]?.label}</Tag>
      ),
    },
    { title: '版本', dataIndex: 'version', key: 'version', width: 80 },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      render: (_, record) =>
        canWrite ? (
          <Space size="middle">
            <a onClick={() => openEdit(record)}>编辑</a>
            <a onClick={() => toggleStatus(record)}>{record.status === 'active' ? '归档' : '启用'}</a>
            <Popconfirm title="删除该模板？（软删，名称不释放）" onConfirm={() => remove(record)}>
              <a>删除</a>
            </Popconfirm>
          </Space>
        ) : (
          <span style={{ color: '#bfbfbf' }}>无写权限</span>
        ),
    },
  ];

  return (
    <PageContainer
      header={{
        title: '报表中心',
        breadcrumb: { items: [{ title: '首页' }, { title: '项目管理' }, { title: '报表中心' }] },
      }}
      extra={
        <Button onClick={() => navigate('/projects/portfolio')} data-testid="goto-portfolio">
          组合视图与度量
        </Button>
      }
    >
      <Space direction="vertical" style={{ width: '100%' }} size="middle">
        <Card
          title="归口分组（项目数 / 健康度分布）"
          extra={
            <Button icon={<RefreshCw size={16} />} loading={groupsLoading} onClick={fetchGroups}>
              刷新
            </Button>
          }
        >
          <Table
            rowKey={(record) =>
              record.ownerTeamId
                ? `team-${record.ownerTeamId}-dept-${record.departmentId ?? 0}`
                : `unassigned-dept-${record.departmentId ?? 0}`
            }
            columns={groupColumns}
            dataSource={groups}
            pagination={false}
            size="small"
            loading={groupsLoading}
            data-testid="report-groups-table"
          />
        </Card>

        <Card
          title="状态报告模板（模板 + 周期数据驱动）"
          extra={
            canWrite && (
              <Button type="primary" icon={<Plus size={16} />} onClick={openCreate} data-testid="report-template-create">
                新建模板
              </Button>
            )
          }
        >
          <Space wrap style={{ marginBottom: 12 }}>
            <Select
              allowClear
              placeholder="状态"
              style={{ width: 140 }}
              value={statusFilter || undefined}
              onChange={(value) => {
                const next = (value ?? '') as ReportTemplateStatus | '';
                setStatusFilter(next);
                void fetchTemplates(1, pagination.pageSize, next, keyword);
              }}
              options={Object.entries(STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))}
            />
            <Input.Search
              allowClear
              placeholder="名称/描述"
              style={{ width: 220 }}
              onSearch={(value) => {
                setKeyword(value);
                void fetchTemplates(1, pagination.pageSize, statusFilter, value);
              }}
            />
            <Button icon={<RefreshCw size={16} />} onClick={() => fetchTemplates(pagination.current, pagination.pageSize)}>
              刷新
            </Button>
          </Space>
          <Table
            rowKey="id"
            columns={templateColumns}
            dataSource={templates}
            size="small"
            loading={templatesLoading}
            pagination={{
              current: pagination.current,
              pageSize: pagination.pageSize,
              total: pagination.total,
              showSizeChanger: true,
              onChange: (page, pageSize) => fetchTemplates(page, pageSize),
            }}
            data-testid="report-templates-table"
          />
        </Card>
      </Space>

      <Modal
        title={editing ? '编辑报告模板' : '新建报告模板'}
        open={modalOpen}
        onOk={submit}
        confirmLoading={saving}
        onCancel={() => setModalOpen(false)}
        width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="模板名称" rules={[{ required: true, message: '请输入模板名称' }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="cadence" label="报告周期" rules={[{ required: true, message: '请选择周期' }]}>
            <Select options={CADENCE_OPTIONS} />
          </Form.Item>
          <Form.Item name="sections" label="输出章节（未选时生成走默认章节）">
            <Checkbox.Group options={SECTION_OPTIONS} />
          </Form.Item>
          <Form.Item name="status" label="状态（仅启用模板可在工作台生成报告）">
            <Select
              options={[
                { value: 'draft', label: '草稿' },
                { value: 'active', label: '启用' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}