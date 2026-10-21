import { useCallback, useEffect, useState } from 'react';
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Plus, RefreshCw } from 'lucide-react';
import { Link } from 'react-router';
import { projectService } from '@/lib/services/project-service';
import { formatBusinessDateTime } from '@/lib/formatters';
import type {
  ProjectRisk,
  RiskConvertTarget,
  RiskCreateRequest,
  RiskKind,
  RiskLevel,
  RiskStatus,
} from '@/lib/services/project-service';

const KIND_META: Record<RiskKind, { label: string; color: string }> = {
  risk: { label: '风险', color: 'volcano' },
  issue: { label: '问题', color: 'magenta' },
};

const LEVEL_META: Record<RiskLevel, { label: string; color: string }> = {
  low: { label: '低', color: 'default' },
  medium: { label: '中', color: 'blue' },
  high: { label: '高', color: 'red' },
};

const STATUS_META: Record<RiskStatus, { label: string; color: string }> = {
  open: { label: '开放', color: 'error' },
  mitigating: { label: '缓解中', color: 'warning' },
  closed: { label: '已关闭', color: 'success' },
};

const LEVEL_OPTIONS = [
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
];

interface ProjectRiskPanelProps {
  projectId: number;
  canWrite: boolean;
  /** 项目活跃成员（负责人下拉；来自工作台成员加载结果）。 */
  memberOptions: { value: number; label: string }[];
}

/**
 * 项目风险/问题台账面板（P2/PE-21）：
 * kind=risk|issue 台账 + 状态机（open→mitigating→closed，close 必填结论）+ 转问题单/变更单。
 */
export function ProjectRiskPanel({ projectId, canWrite, memberOptions }: ProjectRiskPanelProps) {
  const [risks, setRisks] = useState<ProjectRisk[]>([]);
  const [loading, setLoading] = useState(false);
  const [kindFilter, setKindFilter] = useState<RiskKind | ''>('');
  const [statusFilter, setStatusFilter] = useState<RiskStatus | ''>('');
  const [levelFilter, setLevelFilter] = useState<RiskLevel | ''>('');

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<ProjectRisk | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<RiskCreateRequest>();

  const [closeOpen, setCloseOpen] = useState(false);
  const [closingRisk, setClosingRisk] = useState<ProjectRisk | null>(null);
  const [closeForm] = Form.useForm<{ resolution: string }>();
  const [actionLoading, setActionLoading] = useState(false);

  const fetchRisks = useCallback(async () => {
    setLoading(true);
    try {
      const rows = await projectService.listRisks(projectId, {
        kind: kindFilter,
        status: statusFilter,
        level: levelFilter,
      });
      setRisks(rows);
    } catch (error) {
      console.error('Failed to fetch project risks:', error);
      message.error('加载风险/问题失败');
    } finally {
      setLoading(false);
    }
  }, [projectId, kindFilter, statusFilter, levelFilter]);

  useEffect(() => {
    void fetchRisks();
  }, [fetchRisks]);

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ kind: 'risk', level: 'medium' });
    setEditorOpen(true);
  };

  const openEdit = (risk: ProjectRisk) => {
    setEditing(risk);
    form.setFieldsValue({
      kind: risk.kind,
      title: risk.title,
      description: risk.description,
      level: risk.level,
      probability: risk.probability,
      impact: risk.impact,
      ownerId: risk.ownerId,
      relatedTicketId: risk.relatedTicketId,
    });
    setEditorOpen(true);
  };

  const submitEditor = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      if (editing) {
        await projectService.updateRisk(projectId, editing.id, {
          ...values,
          ownerId: values.ownerId ?? 0,
          relatedTicketId: values.relatedTicketId ?? 0,
          version: editing.version,
        });
        message.success('已更新');
      } else {
        await projectService.createRisk(projectId, values);
        message.success('已创建');
      }
      setEditorOpen(false);
      await fetchRisks();
    } catch (error) {
      console.error('Save project risk failed:', error);
      message.error('保存失败（检查必填项与成员/工单有效性）');
    } finally {
      setSaving(false);
    }
  };

  const transition = async (risk: ProjectRisk, action: 'mitigate' | 'reopen') => {
    setActionLoading(true);
    try {
      await projectService.transitionRisk(projectId, risk.id, action);
      message.success(action === 'mitigate' ? '已标记缓解中' : '已重开');
      await fetchRisks();
    } catch (error) {
      console.error('Transition risk failed:', error);
      message.error('状态流转失败');
    } finally {
      setActionLoading(false);
    }
  };

  const openClose = (risk: ProjectRisk) => {
    setClosingRisk(risk);
    closeForm.resetFields();
    setCloseOpen(true);
  };

  const submitClose = async () => {
    const values = await closeForm.validateFields();
    if (!closingRisk) return;
    setActionLoading(true);
    try {
      await projectService.transitionRisk(projectId, closingRisk.id, 'close', values.resolution);
      message.success('已关闭');
      setCloseOpen(false);
      await fetchRisks();
    } catch (error) {
      console.error('Close risk failed:', error);
      message.error('关闭失败');
    } finally {
      setActionLoading(false);
    }
  };

  const convert = (risk: ProjectRisk, target: RiskConvertTarget) => {
    Modal.confirm({
      title: target === 'problem' ? '升级为问题单？' : '创建变更单？',
      content:
        target === 'problem'
          ? '将以该风险/问题的标题与描述创建 ITSM 问题单（open），并回写关联。'
          : '将以该风险/问题的标题与描述创建变更单（draft），并回写关联。',
      okText: '确认转换',
      cancelText: '取消',
      onOk: async () => {
        try {
          const result = await projectService.convertRisk(projectId, risk.id, target);
          message.success(
            target === 'problem' ? `已创建问题单 #${result.createdId}` : `已创建变更单 #${result.createdId}`
          );
          await fetchRisks();
        } catch (error) {
          const existingId = (error as { response?: { data?: { data?: { existingId?: number } } } })?.response
            ?.data?.data?.existingId;
          if (existingId) {
            message.warning(`该风险已转换（既有单据 #${existingId}），如需新单据请先另建风险`);
          } else {
            console.error('Convert risk failed:', error);
            message.error('转换失败（问题单/变更单服务未配置或参数无效）');
          }
        }
      },
    });
  };

  const remove = async (risk: ProjectRisk) => {
    try {
      await projectService.deleteRisk(projectId, risk.id);
      message.success('已删除');
      await fetchRisks();
    } catch (error) {
      console.error('Delete risk failed:', error);
      message.error('删除失败');
    }
  };

  const columns: ColumnsType<ProjectRisk> = [
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 80,
      render: (kind: RiskKind) => <Tag color={KIND_META[kind]?.color}>{KIND_META[kind]?.label ?? kind}</Tag>,
    },
    { title: '标题', dataIndex: 'title', key: 'title' },
    {
      title: '等级',
      dataIndex: 'level',
      key: 'level',
      width: 80,
      render: (level: RiskLevel, risk) => (
        <Space size={4}>
          <Tag color={LEVEL_META[level]?.color}>{LEVEL_META[level]?.label ?? level}</Tag>
          {(risk.probability || risk.impact) && (
            <span style={{ color: '#8c8c8c', fontSize: 12 }}>
              {risk.probability ? `P:${LEVEL_META[risk.probability]?.label}` : ''}
              {risk.probability && risk.impact ? ' / ' : ''}
              {risk.impact ? `I:${LEVEL_META[risk.impact]?.label}` : ''}
            </span>
          )}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: RiskStatus) => (
        <Tag color={STATUS_META[status]?.color}>{STATUS_META[status]?.label ?? status}</Tag>
      ),
    },
    {
      title: '负责人',
      dataIndex: 'ownerId',
      key: 'ownerId',
      width: 120,
      render: (ownerId?: number, risk?: ProjectRisk) =>
        ownerId ? risk?.ownerName ?? `用户 #${ownerId}` : <Tag color="warning">未指派</Tag>,
    },
    {
      title: '关联',
      key: 'links',
      width: 200,
      render: (_: unknown, risk) => (
        <Space size={4} wrap>
          {risk.relatedTicketId && <Tag>工单 #{risk.relatedTicketId}</Tag>}
          {risk.relatedProblemId && (
            <Link to={`/problems/${risk.relatedProblemId}`}>
              <Tag color="magenta">问题 #{risk.relatedProblemId}</Tag>
            </Link>
          )}
          {risk.relatedChangeId && (
            <Link to={`/changes/${risk.relatedChangeId}`}>
              <Tag color="blue">变更 #{risk.relatedChangeId}</Tag>
            </Link>
          )}
          {!risk.relatedTicketId && !risk.relatedProblemId && !risk.relatedChangeId && '-'}
        </Space>
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 170,
        render: (v: string) => formatBusinessDateTime(v) || '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 320,
      render: (_: unknown, risk) => (
        <Space size={4} wrap>
          {canWrite && (
            <>
              <Button size="small" type="link" onClick={() => openEdit(risk)}>
                编辑
              </Button>
              {risk.status === 'open' && (
                <Button size="small" type="link" loading={actionLoading} onClick={() => transition(risk, 'mitigate')}>
                  缓解
                </Button>
              )}
              {risk.status !== 'closed' && (
                <Button size="small" type="link" danger loading={actionLoading} onClick={() => openClose(risk)}>
                  关闭
                </Button>
              )}
              {risk.status === 'closed' && (
                <Button size="small" type="link" loading={actionLoading} onClick={() => transition(risk, 'reopen')}>
                  重开
                </Button>
              )}
              {!risk.relatedProblemId && (
                <Button size="small" type="link" onClick={() => convert(risk, 'problem')}>
                  转问题
                </Button>
              )}
              {!risk.relatedChangeId && (
                <Button size="small" type="link" onClick={() => convert(risk, 'change')}>
                  转变更
                </Button>
              )}
              <Popconfirm title="删除该风险/问题？" okText="删除" cancelText="取消" onConfirm={() => remove(risk)}>
                <Button size="small" type="link" danger>
                  删除
                </Button>
              </Popconfirm>
            </>
          )}
        </Space>
      ),
    },
  ];

  return (
    <>
      <Space wrap style={{ marginBottom: 12 }}>
        {canWrite && (
          <Button type="primary" icon={<Plus size={16} />} onClick={openCreate} data-testid="risk-create">
            新建风险/问题
          </Button>
        )}
        <Select
          style={{ width: 130 }}
          value={kindFilter}
          onChange={(v) => setKindFilter(v)}
          options={[
            { value: '', label: '全部类型' },
            { value: 'risk', label: '风险' },
            { value: 'issue', label: '问题' },
          ]}
        />
        <Select
          style={{ width: 130 }}
          value={statusFilter}
          onChange={(v) => setStatusFilter(v)}
          options={[
            { value: '', label: '全部状态' },
            { value: 'open', label: '开放' },
            { value: 'mitigating', label: '缓解中' },
            { value: 'closed', label: '已关闭' },
          ]}
        />
        <Select
          style={{ width: 120 }}
          value={levelFilter}
          onChange={(v) => setLevelFilter(v)}
          options={[{ value: '', label: '全部等级' }, ...LEVEL_OPTIONS]}
        />
        <Button icon={<RefreshCw size={16} />} onClick={fetchRisks} loading={loading}>
          刷新
        </Button>
        <span style={{ color: '#8c8c8c' }}>
          关闭须填写结论；转问题/变更走 ITSM 域（重复转换会被拒绝并给出既有单据）。
        </span>
      </Space>

      <Table rowKey="id" columns={columns} dataSource={risks} pagination={false} size="small" loading={loading} />

      <Modal
        title={editing ? '编辑风险/问题' : '新建风险/问题'}
        open={editorOpen}
        onOk={submitEditor}
        onCancel={() => setEditorOpen(false)}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="kind" label="类型" initialValue="risk">
            <Select
              options={[
                { value: 'risk', label: '风险' },
                { value: 'issue', label: '问题' },
              ]}
            />
          </Form.Item>
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input maxLength={255} placeholder="一句话描述风险/问题" />
          </Form.Item>
          <Form.Item name="level" label="等级" initialValue="medium">
            <Select options={LEVEL_OPTIONS} />
          </Form.Item>
          <Space size="middle" style={{ display: 'flex' }}>
            <Form.Item name="probability" label="概率" style={{ flex: 1 }}>
              <Select allowClear options={LEVEL_OPTIONS} placeholder="可选" />
            </Form.Item>
            <Form.Item name="impact" label="影响" style={{ flex: 1 }}>
              <Select allowClear options={LEVEL_OPTIONS} placeholder="可选" />
            </Form.Item>
          </Space>
          {/* B8：新建必填负责人（无 owner 的风险无人推进）；编辑历史无 owner 记录时允许补填前保存。 */}
          <Form.Item
            name="ownerId"
            label="负责人（项目活跃成员）"
            rules={editing ? [] : [{ required: true, message: '请选择负责人（须为项目活跃成员）' }]}
          >
            <Select options={memberOptions} placeholder="请选择负责人" showSearch optionFilterProp="label" />
          </Form.Item>
          <Form.Item name="relatedTicketId" label="关联工单 ID（可选，须同租户）">
            {/* B2：数字控件显式上下限，保证步进可用。 */}
            <InputNumber min={1} max={2147483647} precision={0} style={{ width: '100%' }} placeholder="例如 1024" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="背景、影响面、应对思路（可选）" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`关闭：${closingRisk?.title ?? ''}`}
        open={closeOpen}
        onOk={submitClose}
        onCancel={() => setCloseOpen(false)}
        confirmLoading={actionLoading}
        okText="确认关闭"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={closeForm} layout="vertical" preserve={false}>
          <Form.Item
            name="resolution"
            label="结论（必填）"
            rules={[{ required: true, message: '请填写关闭结论' }]}
          >
            <Input.TextArea rows={3} placeholder="处置结果/规避措施/验证结论" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

export default ProjectRiskPanel;