import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeft, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { useNavigate } from 'react-router';
import { PageContainer } from '@/components/common/PageContainer';
import { useCan } from '@/components/auth/PermissionGate';
import type {
  ProjectTemplate,
  ProjectType,
  TaskPriority,
  TemplatePayload,
  TemplateStatus,
} from '@/lib/services/project-service';
import { projectService } from '@/lib/services/project-service';
import { formatBusinessDateTime } from '@/lib/formatters';

// 项目模板管理（P1/PE-11）。权威定义：docs/project-management/plan/p1-template-design.md
// 规则：draft 可编不可用；active 可一键生成；被项目应用过的模板不可删除（改为归档）。

const STATUS_META: Record<TemplateStatus, { label: string; color: string }> = {
  draft: { label: '草稿', color: 'default' },
  active: { label: '已激活', color: 'processing' },
  archived: { label: '已归档', color: 'warning' },
};

const TYPE_META: Record<ProjectType, { label: string; color: string }> = {
  internal: { label: 'IT 内部项目', color: 'blue' },
  improvement: { label: '服务改进', color: 'purple' },
  delivery: { label: '交付项目', color: 'cyan' },
};

const PRIORITY_OPTIONS: { label: string; value: TaskPriority }[] = [
  { label: '低', value: 'low' },
  { label: '中', value: 'medium' },
  { label: '高', value: 'high' },
  { label: '紧急', value: 'critical' },
];

interface MilestoneFormValue {
  name?: string;
  offsetDays?: number;
}

interface TaskFormValue {
  stage?: string;
  name?: string;
  description?: string;
  priority?: TaskPriority;
  startOffsetDays?: number;
  durationDays?: number;
  estimateHours?: number;
  checklist?: string;
}

interface TemplateFormValues {
  name: string;
  description?: string;
  type: ProjectType;
  status: TemplateStatus;
  milestones?: MilestoneFormValue[];
  tasks?: TaskFormValue[];
}

/** 表单 → payload（过滤空行；checklist 由逗号/顿号/换行分隔）。 */
function buildPayload(values: TemplateFormValues): TemplatePayload {
  const milestones = (values.milestones ?? [])
    .filter((m) => (m?.name ?? '').trim() !== '')
    .map((m) => ({ name: (m.name ?? '').trim(), offsetDays: Number(m.offsetDays ?? 0) }));
  const tasks = (values.tasks ?? [])
    .filter((t) => (t?.name ?? '').trim() !== '')
    .map((t) => {
      const checklist = (t.checklist ?? '')
        .split(/[,，\n]/)
        .map((item) => item.trim())
        .filter(Boolean);
      return {
        stage: (t.stage ?? '').trim() || undefined,
        name: (t.name ?? '').trim(),
        description: (t.description ?? '').trim() || undefined,
        priority: t.priority,
        startOffsetDays: t.startOffsetDays ?? undefined,
        durationDays: t.durationDays ?? undefined,
        estimateHours: t.estimateHours ?? undefined,
        checklist: checklist.length ? checklist : undefined,
      };
    });
  return { milestones, tasks };
}

/** payload → 表单值（编辑回填；checklist 反转为逗号分隔）。 */
function payloadToFormValues(record: ProjectTemplate): TemplateFormValues {
  const payload = record.payload ?? {};
  return {
    name: record.name,
    description: record.description || undefined,
    type: record.type,
    status: record.status,
    milestones: (payload.milestones ?? []).map((m) => ({ name: m.name, offsetDays: m.offsetDays })),
    tasks: (payload.tasks ?? []).map((t) => ({
      stage: t.stage,
      name: t.name,
      description: t.description,
      priority: t.priority,
      startOffsetDays: t.startOffsetDays,
      durationDays: t.durationDays,
      estimateHours: t.estimateHours,
      checklist: (t.checklist ?? []).join(', '),
    })),
  };
}

export default function ProjectTemplatesPage() {
  const navigate = useNavigate();
  const canWrite = useCan(['project:write']);

  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState<ProjectTemplate[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [filters, setFilters] = useState<{ status?: TemplateStatus | ''; type?: ProjectType | ''; q?: string }>({});

  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ProjectTemplate | null>(null);
  const [pendingValues, setPendingValues] = useState<TemplateFormValues | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm<TemplateFormValues>();

  const fetchTemplates = useCallback(async () => {
    setLoading(true);
    try {
      const data = await projectService.listTemplates({ page, pageSize, ...filters });
      setItems(data.items ?? data.projectTemplates ?? []);
      setTotal(data.total ?? 0);
    } catch (error) {
      console.error('Failed to fetch project templates:', error);
      message.error('模板列表加载失败');
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, filters]);

  useEffect(() => {
    fetchTemplates();
  }, [fetchTemplates]);

  // 弹窗打开后再回填（避免未挂载时 setFieldsValue 丢失）。
  useEffect(() => {
    if (!modalOpen) return;
    form.resetFields();
    form.setFieldsValue(pendingValues ?? { type: 'internal', status: 'draft' });
  }, [modalOpen, pendingValues]);

  const openCreate = () => {
    setEditing(null);
    setPendingValues(null);
    setModalOpen(true);
  };

  const openEdit = async (record: ProjectTemplate) => {
    try {
      const detail = await projectService.getTemplate(record.id);
      setEditing(detail);
      setPendingValues(payloadToFormValues(detail));
      setModalOpen(true);
    } catch (error) {
      console.error('Failed to fetch template detail:', error);
      message.error('模板详情加载失败');
    }
  };

  const handleSubmit = async (values: TemplateFormValues) => {
    const payload = buildPayload(values);
    if (!payload.tasks?.length && !payload.milestones?.length) {
      message.error('至少填写一个任务或里程碑');
      return;
    }
    setSubmitting(true);
    try {
      const base = {
        name: values.name.trim(),
        description: values.description?.trim() || undefined,
        type: values.type,
        payload,
      };
      if (editing) {
        await projectService.updateTemplate(editing.id, {
          ...base,
          status: values.status,
          version: editing.version,
        });
      } else {
        await projectService.createTemplate({ ...base, status: values.status });
      }
      message.success(editing ? '模板已更新' : '模板已创建');
      setModalOpen(false);
      setEditing(null);
      setPendingValues(null);
      fetchTemplates();
    } catch (error) {
      console.error('Failed to save project template:', error);
      message.error(editing ? '模板更新失败（可能已被他人修改，请刷新后重试）' : '模板创建失败（名称可能已存在）');
    } finally {
      setSubmitting(false);
    }
  };

  const toggleStatus = async (record: ProjectTemplate, status: TemplateStatus) => {
    try {
      await projectService.updateTemplate(record.id, { status, version: record.version });
      message.success(status === 'active' ? '模板已激活' : '模板已归档');
      fetchTemplates();
    } catch (error) {
      console.error('Failed to change template status:', error);
      message.error('状态变更失败（可能已被他人修改，请刷新后重试）');
    }
  };

  const removeTemplate = async (record: ProjectTemplate) => {
    try {
      await projectService.deleteTemplate(record.id);
      message.success('模板已删除');
      fetchTemplates();
    } catch (error) {
      console.error('Failed to delete project template:', error);
      message.error('删除失败：被项目应用过的模板不可删除，请改为归档');
    }
  };

  const columns: ColumnsType<ProjectTemplate> = [
    {
      title: '模板名称',
      dataIndex: 'name',
      key: 'name',
      width: 240,
      render: (name: string, record) => (
        <Tooltip title={record.description || undefined}>
          <span>{name}</span>
        </Tooltip>
      ),
    },
    {
      title: '适用类型',
      dataIndex: 'type',
      key: 'type',
      width: 130,
      render: (type: ProjectType) => <Tag color={TYPE_META[type]?.color}>{TYPE_META[type]?.label ?? type}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: TemplateStatus) => (
        <Tag color={STATUS_META[status]?.color}>{STATUS_META[status]?.label ?? status}</Tag>
      ),
    },
    {
      title: '定义（阶段/任务/检查项/里程碑）',
      key: 'counts',
      width: 220,
      render: (_, record) =>
        `${record.stageCount} / ${record.taskCount} / ${record.checklistCount} / ${record.milestoneCount}`,
    },
    { title: '版本', dataIndex: 'version', key: 'version', width: 70 },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 170,
      render: (value: string) => formatBusinessDateTime(value) || '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 220,
      render: (_, record) => (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {canWrite && <a onClick={() => openEdit(record)}>编辑</a>}
          {canWrite && record.status === 'draft' && <a onClick={() => toggleStatus(record, 'active')}>激活</a>}
          {canWrite && record.status === 'active' && <a onClick={() => toggleStatus(record, 'archived')}>归档</a>}
          {canWrite && record.status === 'archived' && <a onClick={() => toggleStatus(record, 'active')}>重新激活</a>}
          {canWrite && (
            <Popconfirm title="确认删除该模板？被项目应用过的模板不可删除。" onConfirm={() => removeTemplate(record)}>
              <a>删除</a>
            </Popconfirm>
          )}
        </div>
      ),
    },
  ];

  return (
    <PageContainer
      header={{
        title: '项目模板',
        breadcrumb: {
          items: [
            { title: '首页' },
            { title: '项目管理', onClick: () => navigate('/projects') } as never,
            { title: '项目模板' },
          ],
        },
      }}
      extra={[
        <Button key="back" icon={<ArrowLeft />} onClick={() => navigate('/projects')}>
          返回项目列表
        </Button>,
        <Button key="refresh" icon={<RefreshCw />} onClick={fetchTemplates} loading={loading}>
          刷新
        </Button>,
        canWrite ? (
          <Button key="create" type="primary" icon={<Plus />} onClick={openCreate}>
            新建模板
          </Button>
        ) : null,
      ]}
    >
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
        <Input.Search
          allowClear
          placeholder="搜索模板名称/描述"
          style={{ width: 260 }}
          onSearch={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, q: value.trim() || undefined }));
          }}
        />
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 140 }}
          value={filters.status || undefined}
          onChange={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, status: (value as TemplateStatus) || '' }));
          }}
          options={Object.entries(STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))}
        />
        <Select
          allowClear
          placeholder="适用类型"
          style={{ width: 160 }}
          value={filters.type || undefined}
          onChange={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, type: (value as ProjectType) || '' }));
          }}
          options={Object.entries(TYPE_META).map(([value, meta]) => ({ value, label: meta.label }))}
        />
      </div>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={items}
        // B5：表头/名称不竖排；空态提供新建引导（B7）。
        scroll={{ x: 1100 }}
        locale={{
          emptyText: (
            <div style={{ padding: '24px 0' }}>
              <div style={{ marginBottom: 8 }}>暂无项目模板：模板用于一键生成阶段/任务/检查项/里程碑。</div>
              {canWrite ? (
                <Button type="primary" icon={<Plus size={14} />} onClick={openCreate}>
                  新建模板
                </Button>
              ) : null}
            </div>
          ),
        }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (count) => `共 ${count} 条`,
          onChange: (nextPage, nextPageSize) => {
            setPage(nextPage);
            setPageSize(nextPageSize);
          },
        }}
      />

      <Modal
        title={editing ? `编辑模板：${editing.name}` : '新建模板'}
        open={modalOpen}
        width={880}
        onCancel={() => {
          setModalOpen(false);
          setEditing(null);
          setPendingValues(null);
        }}
        onOk={() => form.submit()}
        confirmLoading={submitting}
        okText="保存"
        cancelText="取消"
      >
        <Form form={form} layout="vertical" preserve={false} onFinish={handleSubmit}>
          <div style={{ display: 'flex', gap: 12 }}>
            <Form.Item name="name" label="模板名称" rules={[{ required: true, message: '请填写模板名称' }]} style={{ flex: 2 }}>
              <Input placeholder="如：标准交付模板" maxLength={128} />
            </Form.Item>
            <Form.Item name="type" label="适用类型" style={{ flex: 1 }}>
              <Select options={Object.entries(TYPE_META).map(([value, meta]) => ({ value, label: meta.label }))} />
            </Form.Item>
            <Form.Item name="status" label="状态" style={{ flex: 1 }}>
              <Select
                // B13（回归遗留）：关闭虚拟滚动，选项可被无障碍树逐个寻址。
                virtual={false}
                aria-label="模板状态"
                options={[
                  { value: 'draft', label: '草稿（不可应用）' },
                  { value: 'active', label: '已激活（可应用）' },
                  { value: 'archived', label: '已归档' },
                ]}
              />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input placeholder="模板用途说明" maxLength={2000} />
          </Form.Item>

          <Form.List name="milestones">
            {(fields, { add, remove }) => (
              <div style={{ marginBottom: 16 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                  <span style={{ fontWeight: 600 }}>里程碑（日期 = 项目开始日期 + 偏移天数）</span>
                  <Button size="small" icon={<Plus size={14} />} onClick={() => add({ offsetDays: 0 })}>
                    添加里程碑
                  </Button>
                </div>
                {fields.map((field) => (
                  <div key={field.key} style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
                    <Form.Item
                      name={[field.name, 'name']}
                      label="名称"
                      rules={[{ required: true, message: '请填写里程碑名称' }]}
                      style={{ flex: 3 }}
                    >
                      <Input placeholder="如：验收通过" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'offsetDays']} label="偏移天数" style={{ flex: 1 }}>
                      <InputNumber min={0} max={3650} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item label=" ">
                      <Button type="text" icon={<Trash2 size={16} />} onClick={() => remove(field.name)} />
                    </Form.Item>
                  </div>
                ))}
              </div>
            )}
          </Form.List>

          <Form.List name="tasks">
            {(fields, { add, remove }) => (
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                  <span style={{ fontWeight: 600 }}>任务（阶段相同的任务归入同一阶段分组）</span>
                  <Button size="small" icon={<Plus size={14} />} onClick={() => add({ priority: 'medium' })}>
                    添加任务
                  </Button>
                </div>
                {fields.map((field) => (
                  <div
                    key={field.key}
                    style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: 12, marginBottom: 8 }}
                  >
                    <div style={{ display: 'flex', gap: 8 }}>
                      <Form.Item name={[field.name, 'stage']} label="阶段" style={{ flex: 1 }}>
                        <Input placeholder="如：启动（可空）" />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, 'name']}
                        label="任务名"
                        rules={[{ required: true, message: '请填写任务名' }]}
                        style={{ flex: 2 }}
                      >
                        <Input placeholder="如：环境部署" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'priority']} label="优先级" style={{ width: 110 }}>
                        <Select options={PRIORITY_OPTIONS} />
                      </Form.Item>
                      <Form.Item label=" ">
                        <Button type="text" icon={<Trash2 size={16} />} onClick={() => remove(field.name)} />
                      </Form.Item>
                    </div>
                    <div style={{ display: 'flex', gap: 8 }}>
                      <Form.Item name={[field.name, 'startOffsetDays']} label="开始偏移（天）" style={{ flex: 1 }}>
                        <InputNumber min={0} max={3650} style={{ width: '100%' }} />
                      </Form.Item>
                      <Form.Item name={[field.name, 'durationDays']} label="工期（天）" style={{ flex: 1 }}>
                        <InputNumber min={0} max={3650} style={{ width: '100%' }} />
                      </Form.Item>
                      <Form.Item name={[field.name, 'estimateHours']} label="估算工时" style={{ flex: 1 }}>
                        {/* B1/B2（回归遗留）：显式上下限 + 超限即时提示，避免默认 valuemax=0 与静默钳制。 */}
                        <InputNumber
                          min={0}
                          max={1000000}
                          step={1}
                          style={{ width: '100%' }}
                          onChange={(value) => {
                            if (typeof value === 'number' && value > 1000000) {
                              message.warning('模板任务估算工时上限为 1,000,000 小时');
                            }
                          }}
                        />
                      </Form.Item>
                    </div>
                    <Form.Item name={[field.name, 'description']} label="说明">
                      <Input placeholder="可选" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'checklist']} label="检查项（逗号分隔，生成三级子任务）">
                      <Input placeholder="备份完成, 回滚演练" />
                    </Form.Item>
                  </div>
                ))}
              </div>
            )}
          </Form.List>
        </Form>
      </Modal>
    </PageContainer>
  );
}