import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Progress,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { BarChart3, Briefcase, LayoutTemplate, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { useNavigate } from 'react-router';
import { PageContainer } from '@/components/common/PageContainer';
import { useCan } from '@/components/auth/PermissionGate';
import { useI18n } from '@/lib/i18n';
import type { Project, ProjectStatus, ProjectType } from '@/lib/services/project-service';
import { projectService } from '@/lib/services/project-service';
import type { Department } from '@/lib/services/department-service';
import { departmentService } from '@/lib/services/department-service';
import { UserApi } from '@/lib/api/user-api';
import { dateValue, isoValue } from '@/lib/utils/date-value';
import { formatBudget, normalizeBudgetCurrency } from '@/lib/utils/budget';
import { ProjectHealthTag } from './components/ProjectHealthTag';

const { RangePicker } = DatePicker;

const STATUS_META: Record<ProjectStatus, { label: string; color: string }> = {
  planning: { label: '规划中', color: 'default' },
  active: { label: '进行中', color: 'processing' },
  suspended: { label: '已暂停', color: 'warning' },
  completed: { label: '已完成', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
};

const TYPE_META: Record<ProjectType, { label: string; color: string }> = {
  internal: { label: 'IT 内部项目', color: 'blue' },
  improvement: { label: '服务改进', color: 'purple' },
  delivery: { label: '交付项目', color: 'cyan' },
};

export default function ProjectsPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const canWrite = useCan(['project:write']);

  const [loading, setLoading] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [filters, setFilters] = useState<{ status?: string; type?: string; q?: string }>({});

  const [isModalVisible, setIsModalVisible] = useState(false);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();

  const [cancelTarget, setCancelTarget] = useState<Project | null>(null);
  const [cancelForm] = Form.useForm();

  const [departmentOptions, setDepartmentOptions] = useState<{ label: string; value: number }[]>([]);
  const [userNameById, setUserNameById] = useState<Record<number, string>>({});
  // B7：模板链路——新建弹窗内可直接选择已激活模板（创建后自动应用，失败给出明确提示）。
  const [templateOptions, setTemplateOptions] = useState<{ label: string; value: number }[]>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState<number | undefined>(undefined);

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    try {
      const data = await projectService.listProjects({ page, pageSize, ...filters });
      setProjects(data.items ?? data.projects ?? []);
      setTotal(data.total ?? 0);
    } catch (error) {
      console.error('Failed to fetch projects:', error);
      message.error(t('common.getFailed'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, filters, t]);

  const flattenDepartments = (list: Department[]): { label: string; value: number }[] => {
    const result: { label: string; value: number }[] = [];
    list.forEach((d) => {
      result.push({ label: d.name, value: d.id });
      if (d.children) result.push(...flattenDepartments(d.children));
    });
    return result;
  };

  const fetchOptions = async () => {
    try {
      const tree = await departmentService.getDepartmentTree();
      setDepartmentOptions(flattenDepartments(tree));
    } catch (error) {
      console.error('Failed to fetch departments:', error);
    }
    try {
      const response = await UserApi.getUsers({ page: 1, pageSize: 100 });
      const map: Record<number, string> = {};
      (response.users || []).forEach((u) => {
        map[u.id] = u.name || u.username;
      });
      setUserNameById(map);
    } catch (error) {
      console.error('Failed to fetch users:', error);
    }
    try {
      const templates = await projectService.listTemplates({ status: 'active', pageSize: 100 });
      setTemplateOptions(
        (templates.items ?? templates.projectTemplates ?? []).map((tpl) => ({
          value: tpl.id,
          label: `${tpl.name}（任务 ${tpl.taskCount} / 里程碑 ${tpl.milestoneCount}）`,
        })),
      );
    } catch (error) {
      console.error('Failed to fetch project templates:', error);
    }
  };

  useEffect(() => {
    fetchProjects();
  }, [fetchProjects]);

  useEffect(() => {
    fetchOptions();
  }, []);

  const openCreate = () => {
    setEditingProject(null);
    form.resetFields();
    form.setFieldsValue({ type: 'internal' });
    setSelectedTemplateId(undefined);
    setIsModalVisible(true);
  };

  const openEdit = (record: Project) => {
    setEditingProject(record);
    form.setFieldsValue({
      name: record.name,
      code: record.code,
      type: record.type,
      description: record.description,
      managerId: record.managerId,
      departmentId: record.departmentId,
      range:
        record.startDate && record.endDate
          ? [dateValue(record.startDate), dateValue(record.endDate)]
          : undefined,
      budgetAmount: record.budgetAmount,
      budgetCurrency: record.budgetCurrency,
    });
    setIsModalVisible(true);
  };

  const handleOk = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const payload = {
        ...values,
        startDate: isoValue(values.range?.[0]),
        endDate: isoValue(values.range?.[1]),
      };
      delete payload.range;
      // templateId 不属于项目主表字段（B7：由创建后 applyTemplate 单独处理），避免后端 400。
      delete payload.templateId;
      // PE-22：预算同写同清——填写金额即连同币种提交（币种缺省空串 = 租户默认）；金额 <=0 = 清空。
      if (values.budgetAmount !== undefined && values.budgetAmount !== null) {
        payload.budgetAmount = values.budgetAmount;
        // PE-28（D-63）：币种 trim + 大写归一（非空须 2-8 位字母，由后端强校验）。
        payload.budgetCurrency = normalizeBudgetCurrency(values.budgetCurrency);
      } else {
        delete payload.budgetAmount;
        delete payload.budgetCurrency;
      }
      if (editingProject) {
        await projectService.updateProject(editingProject.id, payload);
      } else {
        const created = await projectService.createProject(payload);
        // B7：创建即应用模板；失败不掩盖创建结果（项目已建，可到工作台重试）。
        if (selectedTemplateId) {
          try {
            const result = await projectService.applyTemplate(created.id, selectedTemplateId);
            message.success(
              `项目已创建并应用模板（阶段 ${result.stages} / 任务 ${result.tasks} / 检查项 ${result.checklists} / 里程碑 ${result.milestones}）`,
            );
          } catch (applyError) {
            console.error('Apply template after create failed:', applyError);
            message.warning('项目已创建，但模板应用失败：请到工作台「应用模板」重试（仅可应用一次）');
          }
        }
      }
      if (editingProject || !selectedTemplateId) message.success(t('common.saveSuccess'));
      setIsModalVisible(false);
      setEditingProject(null);
      setSelectedTemplateId(undefined);
      form.resetFields();
      fetchProjects();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Operation failed:', error);
      message.error(t('common.operationFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const runTransition = async (record: Project, action: string) => {
    try {
      await projectService.transitionProject(record.id, action, undefined, record.version);
      message.success('操作成功');
      fetchProjects();
    } catch (error) {
      console.error('Transition failed:', error);
      // 后端包络消息可能带 [RID: ...] 排查后缀，展示前剥离。
      const msg = (error instanceof Error ? error.message : '')
        .replace(/\s*\[RID:[^\]]*\]\s*$/, '')
        .trim();
      // PE-12：激活/结项前必须先通过阶段门（activate→立项门，complete→结项门）。
      // 旧实现只透传含「未终结」的错误，其余一律吞成「操作失败，请刷新后重试」，
      // 用户无法得知真实原因（如：立项门尚未提交审批）。
      if (msg.includes('阶段门门禁未通过') || msg.includes('阶段门禁未通过')) {
        const isActivate = action === 'activate';
        const gateLabel = isActivate ? '立项门' : action === 'complete' ? '结项门' : '阶段门';
        Modal.warning({
          title: `无法${isActivate ? '激活' : action === 'complete' ? '结项' : '流转'}：${gateLabel}未通过`,
          content: (
            <div>
              <p>{msg}</p>
              <p>
                请先在项目工作台「阶段门」提交并通过{gateLabel}
                {isActivate ? '（需项目经理任命 + 成员登记）' : action === 'complete' ? '（需关联单据全部终态）' : ''}。
              </p>
            </div>
          ),
          okText: '去阶段门处理',
          onOk: () => navigate(`/projects/${record.id}?tab=gates`),
        });
        return;
      }
      if (msg.includes('未终结') || msg.includes('版本') || msg.includes('刷新')) {
        message.error(msg);
        return;
      }
      message.error(msg || '操作失败，请刷新后重试');
    }
  };

  const submitCancel = async () => {
    if (!cancelTarget) return;
    try {
      const values = await cancelForm.validateFields();
      await projectService.transitionProject(cancelTarget.id, 'cancel', values.reason, cancelTarget.version);
      message.success('项目已取消');
      setCancelTarget(null);
      cancelForm.resetFields();
      fetchProjects();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Cancel failed:', error);
      message.error('取消失败');
    }
  };

  const handleDelete = (record: Project) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除项目「${record.name}」吗？存在关联任务/单据时会被拒绝。`,
      onOk: async () => {
        try {
          await projectService.deleteProject(record.id);
          message.success(t('common.deleteSuccess'));
          fetchProjects();
        } catch (error) {
          console.error('Delete failed:', error);
          message.error('删除失败：项目仍有关联任务或单据');
        }
      },
    });
  };

  const columns: ColumnsType<Project> = useMemo(
    () => [
      {
        title: '项目名称',
        dataIndex: 'name',
        key: 'name',
        render: (text: string, record) => (
          <Space>
            <Briefcase size={16} />
            <a onClick={() => navigate(`/projects/${record.id}`)}>{text}</a>
          </Space>
        ),
        // B5：关键列设置最小宽度，避免中文逐字竖排（表格整体横向滚动）。
        width: 240,
      },
      { title: '项目编号', dataIndex: 'code', key: 'code', width: 170 },
      {
        title: '类型',
        dataIndex: 'type',
        key: 'type',
        width: 120,
        render: (type: ProjectType) => <Tag color={TYPE_META[type]?.color}>{TYPE_META[type]?.label ?? type}</Tag>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 100,
        render: (status: ProjectStatus) => (
          <Tag color={STATUS_META[status]?.color}>{STATUS_META[status]?.label ?? status}</Tag>
        ),
      },
      {
        title: '健康度',
        dataIndex: 'health',
        key: 'health',
        width: 90,
        render: (_: string, record: Project) => (
          <ProjectHealthTag
            health={record.health}
            reasons={record.healthReasons}
            dataQuality={record.healthDataQuality}
            dataGaps={record.healthDataGaps}
          />
        ),
      },
      {
        title: '进度',
        dataIndex: 'progress',
        key: 'progress',
        width: 160,
        render: (percent: number) => <Progress percent={percent || 0} size="small" />,
      },
      {
        // A2：所属部门单独成列（与「归口团队」区分；部门缺失明确显示「未设置」）。
        title: '所属部门',
        dataIndex: 'departmentName',
        key: 'departmentName',
        width: 150,
        render: (name: string | undefined, record) =>
          name || (record.departmentId ? `部门 #${record.departmentId}` : <Tag color="warning">未设置</Tag>),
      },
      {
        title: '负责人',
        dataIndex: 'managerId',
        key: 'managerId',
        width: 120,
        // B3：优先使用后端富化名称，字典兜底，最后才显示 ID（并加前缀避免误读）。
        render: (id: number | undefined, record) =>
          record.managerName || (id ? userNameById[id] ?? `用户 #${id}` : <Tag color="warning">未设置</Tag>),
      },
      {
        title: '归口',
        dataIndex: 'ownerTeamName',
        key: 'ownerTeamName',
        width: 150,
        render: (name: string | undefined, record) =>
          record.ownerTeamId ? (
            <Space size={4}>
              <span>{name || `团队 #${record.ownerTeamId}`}</span>
              {record.ownerTeamDeleted && <Tag color="error">已停用</Tag>}
            </Space>
          ) : (
            <Tag>未设置</Tag>
          ),
      },
      {
        title: '计划结束',
        dataIndex: 'endDate',
        key: 'endDate',
        width: 120,
        render: (v?: string) => (v ? v.slice(0, 10) : '-'),
      },
      {
        // P2（PE-22）：轻量预算（未设置显示 '-'；不含财务口径）。
        title: '预算',
        key: 'budget',
        width: 140,
        render: (_: unknown, record) => formatBudget(record.budgetAmount, record.budgetCurrency) ?? '-',
      },
      {
        title: '操作',
        key: 'action',
        width: 260,
        render: (_: unknown, record) => (
          <Space size="small">
            <a onClick={() => navigate(`/projects/${record.id}`)}>工作台</a>
            {canWrite && (
              <>
                <a onClick={() => openEdit(record)}>
                  <Pencil size={14} /> 编辑
                </a>
                {record.status === 'planning' && <a onClick={() => runTransition(record, 'activate')}>激活</a>}
                {record.status === 'active' && (
                  <a onClick={() => runTransition(record, 'suspend')}>暂停</a>
                )}
                {record.status === 'suspended' && <a onClick={() => runTransition(record, 'resume')}>恢复</a>}
                {(record.status === 'active' || record.status === 'suspended') && (
                  <Tooltip title="存在未终结任务/单据时将被门禁阻止">
                    <a onClick={() => runTransition(record, 'complete')}>结项</a>
                  </Tooltip>
                )}
                {(record.status === 'planning' || record.status === 'active' || record.status === 'suspended') && (
                  <a onClick={() => setCancelTarget(record)}>取消</a>
                )}
                <a onClick={() => handleDelete(record)}>
                  <Trash2 size={14} />
                </a>
              </>
            )}
          </Space>
        ),
      },
    ],
    [canWrite, userNameById]
  );

  return (
    <PageContainer
      header={{
        title: '项目管理',
        breadcrumb: { items: [{ title: '首页' }, { title: '项目管理' }] },
      }}
      extra={[
        <Button key="refresh" icon={<RefreshCw />} onClick={fetchProjects} loading={loading}>
          刷新
        </Button>,
        <Button key="templates" icon={<LayoutTemplate />} onClick={() => navigate('/projects/templates')}>
          模板管理
        </Button>,
        <Button key="reports" icon={<BarChart3 />} onClick={() => navigate('/projects/reports')}>
          报表中心
        </Button>,
        <Button key="portfolio" icon={<BarChart3 />} onClick={() => navigate('/projects/portfolio')}>
          组合视图
        </Button>,
        canWrite ? (
          <Button key="create" type="primary" icon={<Plus />} onClick={openCreate}>
            新建项目
          </Button>
        ) : null,
      ]}
    >
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 140 }}
          options={Object.entries(STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))}
          onChange={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, status: value }));
          }}
        />
        <Select
          allowClear
          placeholder="类型"
          style={{ width: 160 }}
          options={Object.entries(TYPE_META).map(([value, meta]) => ({ value, label: meta.label }))}
          onChange={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, type: value }));
          }}
        />
        <Input.Search
          allowClear
          placeholder="项目名称/编号"
          style={{ width: 260 }}
          onSearch={(value) => {
            setPage(1);
            setFilters((prev) => ({ ...prev, q: value || undefined }));
          }}
        />
      </Space>

      <Table
        rowKey="id"
        columns={columns}
        dataSource={projects}
        loading={loading}
        // B5：列宽自适应 + 最小宽度，超宽横向滚动，避免中文竖排折行。
        scroll={{ x: 1400 }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (t2) => `共 ${t2} 个项目`,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />

      <Modal
        title={editingProject ? '编辑项目' : '新建项目'}
        open={isModalVisible}
        onOk={handleOk}
        onCancel={() => {
          setIsModalVisible(false);
          setEditingProject(null);
        }}
        confirmLoading={submitting}
        width={640}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="项目名称" rules={[{ required: true, message: '请输入项目名称' }]}>
            <Input placeholder="请输入项目名称" />
          </Form.Item>
          {!editingProject && (
            <Form.Item
              name="templateId"
              label="使用模板（可选）"
              extra="仅列出已激活模板；创建后自动生成计划。一个项目只能应用一次模板。"
            >
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder="不使用模板"
                options={templateOptions}
                notFoundContent={
                  <div style={{ padding: 8 }}>
                    暂无已激活模板。
                    <a onClick={() => navigate('/projects/templates')}> 前往项目模板创建/激活 →</a>
                  </div>
                }
                onChange={(value) => setSelectedTemplateId(value)}
              />
            </Form.Item>
          )}
          <Form.Item name="code" label="项目编号" extra="留空自动生成 PRJ-YYYYMMDD-XXXX；创建后不可修改">
            <Input placeholder="可留空" disabled={!!editingProject} />
          </Form.Item>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="type" label="项目类型">
              <Select
                options={Object.entries(TYPE_META).map(([value, meta]) => ({ value, label: meta.label }))}
              />
            </Form.Item>
            {/* A1：项目经理 / 所属部门 / 计划起止为业务必填（与后端严格模式一致）。 */}
            <Form.Item
              name="managerId"
              label="项目经理"
              rules={[{ required: true, message: '请选择项目经理' }]}
            >
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder="请选择项目经理"
                options={Object.entries(userNameById).map(([id, name]) => ({ value: Number(id), label: name }))}
              />
            </Form.Item>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item
              name="departmentId"
              label="所属部门"
              rules={[{ required: true, message: '请选择所属部门' }]}
            >
              <Select allowClear showSearch optionFilterProp="label" placeholder="请选择部门" options={departmentOptions} />
            </Form.Item>
            <Form.Item
              name="range"
              label="计划起止"
              rules={[{ required: true, message: '请选择计划起止日期' }]}
            >
              <RangePicker style={{ width: '100%' }} />
            </Form.Item>
          </div>
          <div className="grid grid-cols-2 gap-4">
            {/* P2（PE-22）：轻量预算（金额与币种成对提交；金额 0 = 清空预算）。 */}
            <Form.Item name="budgetAmount" label="预算金额" extra="留空 = 未设置（不做财务级核算）">
              {/* B2：显式设置上下限，避免组件默认 0/0 导致步进失效。 */}
              <InputNumber min={0} max={1000000000} step={1000} style={{ width: '100%' }} placeholder="如 120000" />
            </Form.Item>
            <Form.Item name="budgetCurrency" label="币种" extra="2-8 位字母（如 CNY / USD）；留空 = 租户默认">
              <Input maxLength={8} placeholder="如 CNY / USD" />
            </Form.Item>
          </div>
          <Form.Item name="description" label="项目描述">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`取消项目：${cancelTarget?.name ?? ''}`}
        open={!!cancelTarget}
        onOk={submitCancel}
        onCancel={() => {
          setCancelTarget(null);
          cancelForm.resetFields();
        }}
      >
        <Form form={cancelForm} layout="vertical">
          <Form.Item name="reason" label="取消原因" rules={[{ required: true, message: '取消需要填写原因' }]}>
            <Input.TextArea rows={3} placeholder="请填写取消原因（审计留痕）" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}