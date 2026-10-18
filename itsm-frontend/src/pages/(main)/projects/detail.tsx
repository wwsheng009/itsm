import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Progress,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeft, FilePlus2, Flag, GitCompare, Plus, RefreshCw } from 'lucide-react';
import { useNavigate, useParams } from 'react-router';
import { PageContainer } from '@/components/common/PageContainer';
import { useCan } from '@/components/auth/PermissionGate';
import type {
  AssetLinkCandidate,
  AssetLinkItem,
  AssetLinkType,
  BaselineDiff,
  BaselineMilestoneDeviation,
  BaselineTaskDeviation,
  HoursSummary,
  MemberCandidate,
  MemberView,
  MilestoneStatus,
  Project,
  ProjectBaseline,
  ProjectClosure,
  ProjectGate,
  ProjectGateKey,
  ProjectGateStatus,
  ProjectMilestone,
  ProjectRole,
  ProjectStatus,
  ProjectTask,
  ProjectTemplate,
  RecordDomain,
  RecordItem,
  TaskDependency,
  TaskPriority,
} from '@/lib/services/project-service';
import { CLOSURE_ITEM_ORDER, GATE_ORDER, projectService } from '@/lib/services/project-service';
import { dateValue, isoValue } from '@/lib/utils/date-value';
import { ProjectReportPanel } from './components/ProjectReportPanel';
import { ProjectResourceHeatmap } from './components/ProjectResourceHeatmap';
import { ProjectRiskPanel } from './components/ProjectRiskPanel';
import { ProjectAITab } from './components/ProjectAITab';

const STATUS_META: Record<ProjectStatus, { label: string; color: string }> = {
  planning: { label: '规划中', color: 'default' },
  active: { label: '进行中', color: 'processing' },
  suspended: { label: '已暂停', color: 'warning' },
  completed: { label: '已完成', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
};

const HEALTH_META: Record<string, { label: string; color: string }> = {
  green: { label: '健康', color: 'success' },
  yellow: { label: '关注', color: 'warning' },
  red: { label: '风险', color: 'error' },
};

const TASK_STATUS_META: Record<string, { label: string; color: string }> = {
  todo: { label: '待开始', color: 'default' },
  in_progress: { label: '进行中', color: 'processing' },
  blocked: { label: '已阻塞', color: 'error' },
  done: { label: '已完成', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
};

const PRIORITY_META: Record<TaskPriority, { label: string; color: string }> = {
  low: { label: '低', color: 'default' },
  medium: { label: '中', color: 'blue' },
  high: { label: '高', color: 'orange' },
  critical: { label: '紧急', color: 'red' },
};

const ROLE_META: Record<ProjectRole, string> = { pm: '项目经理(协同)', member: '成员', stakeholder: '干系人' };
const ORIGIN_META: Record<string, { label: string; color: string }> = {
  internal: { label: '本单位', color: 'blue' },
  external: { label: '外部邀请', color: 'purple' },
  provider: { label: '服务方', color: 'cyan' },
  platform: { label: '平台', color: 'default' },
  unknown: { label: '未知', color: 'default' },
};
const RECORD_TYPE_META: Record<RecordDomain, string> = {
  ticket: '工单',
  incident: '事件',
  problem: '问题',
  change: '变更',
  service_request: '服务请求',
};

const MILESTONE_STATUS_META: Record<MilestoneStatus, { label: string; color: string }> = {
  planned: { label: '计划中', color: 'processing' },
  reached: { label: '已达成', color: 'success' },
  missed: { label: '已错过', color: 'error' },
};

const CHANGE_TYPE_META: Record<string, { label: string; color: string }> = {
  added: { label: '新增', color: 'blue' },
  removed: { label: '删除', color: 'default' },
  changed: { label: '变更', color: 'warning' },
  unchanged: { label: '未变', color: 'default' },
};

// P1（PE-12）阶段门：门定义与状态元数据（顺序与后端 GateOrder 一致）。
const GATE_META: Record<ProjectGateKey, { label: string; hint: string }> = {
  initiation: { label: '立项门', hint: 'planning → active 前（需项目经理 + 成员登记）' },
  plan: { label: '计划门', hint: '进入执行前（任务/里程碑齐备；通过时冻结基线）' },
  acceptance: { label: '验收门', hint: '交付完成前（任务全完成 + 里程碑全达成）' },
  closure: { label: '结项门', hint: '→ completed 前（关联单据全部终态）' },
};

const GATE_STATUS_META: Record<ProjectGateStatus, { label: string; color: string }> = {
  pending: { label: '未提交', color: 'default' },
  in_review: { label: '审批中', color: 'processing' },
  passed: { label: '已通过', color: 'success' },
  rejected: { label: '已拒绝', color: 'error' },
};

// P1（PE-15）结项清单：状态与清单项元数据（顺序与后端 closureChecklistOrder 一致）。
const CLOSURE_STATUS_META: Record<string, { label: string; color: string }> = {
  pending: { label: '存在未通过项', color: 'warning' },
  ready: { label: '可结项', color: 'success' },
  completed: { label: '项目已结项', color: 'default' },
  cancelled: { label: '项目已取消', color: 'default' },
};

const CLOSURE_ITEM_META: Record<string, { label: string; hint: string }> = {
  tasks: { label: '任务清零', hint: '无 todo/in_progress/blocked 任务' },
  records: { label: '关联单据终态', hint: '工单/事件/问题/变更/服务请求均已关闭或转出' },
  gate: { label: '结项阶段门', hint: '结项门须已通过（见「阶段门」Tab）' },
  retro: { label: '复盘 KB 草稿', hint: '自动生成草稿（不自动发布），人工补充后发布' },
  assets: { label: '资产/许可登记', hint: '确认已登记，或豁免并说明原因' },
  members: { label: '成员回收', hint: '回收 member 角色成员；保留 PM 与干系人' },
  csat: { label: 'CSAT 调研', hint: '结项时自动触发；缺失不阻塞结项，可手动重试' },
};

// PE-26：采购/资产登记类型（与后端四词表一致）。
const ASSET_LINK_TYPE_META: Record<AssetLinkType, { label: string; color: string }> = {
  asset: { label: '资产', color: 'blue' },
  asset_license: { label: '许可证', color: 'purple' },
  vendor: { label: '供应商', color: 'cyan' },
  contract: { label: '合同', color: 'geekblue' },
};

const ASSET_LINK_TYPE_OPTIONS = (Object.keys(ASSET_LINK_TYPE_META) as AssetLinkType[]).map((key) => ({
  value: key,
  label: ASSET_LINK_TYPE_META[key].label,
}));

const assetLinkTypeLabel = (linkType: string): string =>
  ASSET_LINK_TYPE_META[linkType as AssetLinkType]?.label ?? linkType;

/** 门禁阻塞项文案（type → 可读说明）。 */
const GATE_BLOCKER_META: Record<string, string> = {
  gate_not_passed: '前置阶段门未通过',
  manager_missing: '未任命项目经理',
  member_missing: '未登记项目成员/干系人',
  task_missing: '计划为空：至少需要 1 个任务',
  milestone_missing: '计划为空：至少需要 1 个里程碑',
  task_open: '存在未完成任务',
  milestone_open: '存在未达成里程碑',
  record_open: '存在未终结关联单据',
};

/** 阶段门表格行（无 record = 尚未提交）。 */
interface GateRow {
  gate: ProjectGateKey;
  record?: ProjectGate;
}

/** 里程碑计划到期日是否已过（仍处 planned）。 */
function milestoneOverdue(m: ProjectMilestone): boolean {
  return m.status === 'planned' && new Date(m.dueDate).getTime() < Date.now();
}

/** 到期 ≤7 天预警。 */
function expiringSoon(expiresAt?: string): boolean {
  if (!expiresAt) return false;
  const diff = new Date(expiresAt).getTime() - Date.now();
  return diff > 0 && diff <= 7 * 24 * 3600 * 1000;
}

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const projectId = Number(id);
  const navigate = useNavigate();
  const canWrite = useCan(['project:write']);

  const [project, setProject] = useState<Project | null>(null);
  const [loading, setLoading] = useState(false);
  const [tasks, setTasks] = useState<ProjectTask[]>([]);
  // P2（PE-22）：任务工时汇总（概览展示；随任务变更刷新）。
  const [hoursSummary, setHoursSummary] = useState<HoursSummary | null>(null);
  const [members, setMembers] = useState<MemberView[]>([]);
  const [records, setRecords] = useState<RecordItem[]>([]);
  const [recordTotal, setRecordTotal] = useState(0);
  const [recordType, setRecordType] = useState<RecordDomain | ''>('');

  const [taskModalOpen, setTaskModalOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<ProjectTask | null>(null);
  const [taskForm] = Form.useForm();

  const [memberModalOpen, setMemberModalOpen] = useState(false);
  const [candidates, setCandidates] = useState<MemberCandidate[]>([]);
  const [candidateLoading, setCandidateLoading] = useState(false);
  const [memberForm] = Form.useForm();

  // P1（PE-14）：里程碑与任务依赖；P1（PE-13）：计划基线
  const [milestones, setMilestones] = useState<ProjectMilestone[]>([]);
  const [dependencies, setDependencies] = useState<TaskDependency[]>([]);
  const [baselines, setBaselines] = useState<ProjectBaseline[]>([]);
  const [baselineDiff, setBaselineDiff] = useState<BaselineDiff | null>(null);
  const [baselineLoading, setBaselineLoading] = useState(false);

  // P1（PE-12）：阶段门（四道门 + 裁决）
  const [gates, setGates] = useState<ProjectGate[]>([]);
  const [gateLoading, setGateLoading] = useState(false);
  const [gateRejectTarget, setGateRejectTarget] = useState<ProjectGateKey | null>(null);
  const [gateRejectComment, setGateRejectComment] = useState('');

  // P1（PE-15）：结项清单（自动项派生 + 复盘/资产登记/成员回收/CSAT 动作）
  const [closure, setClosure] = useState<ProjectClosure | null>(null);
  const [closureLoading, setClosureLoading] = useState(false);
  const [closureActionLoading, setClosureActionLoading] = useState(false);
  const [assetsModalOpen, setAssetsModalOpen] = useState(false);
  const [assetsAction, setAssetsAction] = useState<'confirm' | 'waive'>('confirm');
  const [assetsNote, setAssetsNote] = useState('');
  const [assetsReason, setAssetsReason] = useState('');

  // P2（PE-26）：采购/资产登记明细（结构化台账链接）。
  const [assetLinkModalOpen, setAssetLinkModalOpen] = useState(false);
  const [assetLinkType, setAssetLinkType] = useState<AssetLinkType>('asset');
  const [assetLinkTarget, setAssetLinkTarget] = useState<number | undefined>(undefined);
  const [assetLinkNote, setAssetLinkNote] = useState('');
  const [assetLinkCandidates, setAssetLinkCandidates] = useState<AssetLinkCandidate[]>([]);
  const [assetLinkCandidatesLoading, setAssetLinkCandidatesLoading] = useState(false);
  const [assetLinkSubmitting, setAssetLinkSubmitting] = useState(false);
  const assetLinkSearchTimer = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  const [milestoneModalOpen, setMilestoneModalOpen] = useState(false);
  const [editingMilestone, setEditingMilestone] = useState<ProjectMilestone | null>(null);
  const [milestoneForm] = Form.useForm();

  const [depModalOpen, setDepModalOpen] = useState(false);
  const [depTask, setDepTask] = useState<ProjectTask | null>(null);
  const [depForm] = Form.useForm();

  const [freezeModalOpen, setFreezeModalOpen] = useState(false);
  const [freezeForm] = Form.useForm();

  // P1（PE-11）：项目模板一键生成
  const [templates, setTemplates] = useState<ProjectTemplate[]>([]);
  const [applyModalOpen, setApplyModalOpen] = useState(false);
  const [applyTemplateId, setApplyTemplateId] = useState<number | undefined>(undefined);
  const [applySubmitting, setApplySubmitting] = useState(false);
  const [appliedTemplateName, setAppliedTemplateName] = useState<string>('');

  const fetchProject = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const data = await projectService.getProject(projectId);
      setProject(data);
    } catch (error) {
      console.error('Failed to fetch project:', error);
      message.error('项目不存在或无权访问');
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  const fetchTasks = useCallback(async () => {
    if (!projectId) return;
    try {
      const [taskList, summary] = await Promise.all([
        projectService.listTasks(projectId, { view: 'flat' }),
        projectService.hoursSummary(projectId).catch(() => null),
      ]);
      setTasks(taskList);
      if (summary) setHoursSummary(summary);
    } catch (error) {
      console.error('Failed to fetch tasks:', error);
    }
  }, [projectId]);

  const fetchMembers = useCallback(async () => {
    if (!projectId) return;
    try {
      setMembers(await projectService.listMembers(projectId));
    } catch (error) {
      console.error('Failed to fetch members:', error);
    }
  }, [projectId]);

  // 已应用模板名（PE-11；概览展示；模板已删时降级为 #ID）。
  useEffect(() => {
    const templateId = project?.templateId;
    if (!templateId) {
      setAppliedTemplateName('');
      return;
    }
    let cancelled = false;
    projectService
      .getTemplate(templateId)
      .then((tpl) => {
        if (!cancelled) setAppliedTemplateName(tpl.name);
      })
      .catch(() => {
        if (!cancelled) setAppliedTemplateName('');
      });
    return () => {
      cancelled = true;
    };
  }, [project?.templateId]);

  // PE-11：一键生成（仅 active 模板列出；一项目一次，重试失败不产生重复数据）。
  const openApplyModal = async () => {
    try {
      const data = await projectService.listTemplates({ status: 'active', pageSize: 100 });
      setTemplates(data.items ?? data.projectTemplates ?? []);
      setApplyTemplateId(undefined);
      setApplyModalOpen(true);
    } catch (error) {
      console.error('Failed to fetch project templates:', error);
      message.error('模板列表加载失败');
    }
  };

  const submitApplyTemplate = async () => {
    if (!applyTemplateId) {
      message.warning('请先选择模板');
      return;
    }
    setApplySubmitting(true);
    try {
      const result = await projectService.applyTemplate(projectId, applyTemplateId);
      message.success(
        `已生成：阶段 ${result.stages} / 任务 ${result.tasks} / 检查项 ${result.checklists} / 里程碑 ${result.milestones}`
      );
      setApplyModalOpen(false);
      setApplyTemplateId(undefined);
      fetchProject();
      fetchTasks();
      fetchMilestones();
    } catch (error) {
      console.error('Failed to apply project template:', error);
      message.error('应用失败：模板须处于已激活状态，且项目只能应用一次模板');
    } finally {
      setApplySubmitting(false);
    }
  };

  const fetchRecords = useCallback(
    async (type: RecordDomain | '' = recordType) => {
      if (!projectId) return;
      try {
        const data = await projectService.listRecords(projectId, { type, page: 1, pageSize: 50 });
        setRecords(data.items ?? data.records ?? []);
        setRecordTotal(data.total ?? 0);
      } catch (error) {
        console.error('Failed to fetch records:', error);
      }
    },
    [projectId, recordType]
  );

  const fetchMilestones = useCallback(async () => {
    if (!projectId) return;
    try {
      setMilestones(await projectService.listMilestones(projectId));
    } catch (error) {
      console.error('Failed to fetch milestones:', error);
    }
  }, [projectId]);

  const fetchDependencies = useCallback(async () => {
    if (!projectId) return;
    try {
      setDependencies(await projectService.listDependencies(projectId));
    } catch (error) {
      console.error('Failed to fetch dependencies:', error);
    }
  }, [projectId]);

  const fetchBaselines = useCallback(async () => {
    if (!projectId) return;
    setBaselineLoading(true);
    try {
      const list = await projectService.listBaselines(projectId);
      setBaselines(list);
      if (list.length > 0) {
        try {
          setBaselineDiff(await projectService.baselineDiff(projectId));
        } catch {
          setBaselineDiff(null);
        }
      } else {
        setBaselineDiff(null);
      }
    } catch (error) {
      console.error('Failed to fetch baselines:', error);
    } finally {
      setBaselineLoading(false);
    }
  }, [projectId]);

  const fetchGates = useCallback(async () => {
    if (!projectId) return;
    setGateLoading(true);
    try {
      const data = await projectService.listGates(projectId);
      setGates(data.items);
    } catch (error) {
      console.error('Failed to fetch gates:', error);
    } finally {
      setGateLoading(false);
    }
  }, [projectId]);

  const fetchClosure = useCallback(async () => {
    if (!projectId) return;
    setClosureLoading(true);
    try {
      const data = await projectService.getClosure(projectId);
      setClosure(data);
    } catch (error) {
      console.error('Failed to fetch closure checklist:', error);
    } finally {
      setClosureLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    fetchProject();
  }, [fetchProject]);

  useEffect(() => {
    if (project) {
      fetchTasks();
      fetchMembers();
      fetchRecords('');
      fetchMilestones();
      fetchDependencies();
      fetchBaselines();
      fetchGates();
      fetchClosure();
    }
  }, [project?.id, fetchGates, fetchClosure]);

  const memberNameById = useMemo(() => {
    const map: Record<number, string> = {};
    members.forEach((m) => {
      map[m.userId] = m.name || m.username;
    });
    return map;
  }, [members]);

  const taskNameById = useMemo(() => {
    const map: Record<number, string> = {};
    tasks.forEach((t) => {
      map[t.id] = t.name;
    });
    return map;
  }, [tasks]);

  // PE-12：门 key → 当前记录（未提交的门没有记录，前端显示「未提交」）。
  const gatesByKey = useMemo(() => {
    const map: Partial<Record<ProjectGateKey, ProjectGate>> = {};
    gates.forEach((g) => {
      map[g.gate] = g;
    });
    return map;
  }, [gates]);

  const handleSubmitGate = async (gate: ProjectGateKey) => {
    if (!project) return;
    setGateLoading(true);
    try {
      const updated = await projectService.submitGate(project.id, gate);
      if (updated.status === 'passed') {
        message.success(`${GATE_META[gate].label} 通过（未配置审批流程，已自动放行）`);
      } else if (updated.status === 'in_review') {
        message.success(`${GATE_META[gate].label} 已提交审批`);
      } else {
        message.success('已提交');
      }
      await fetchGates();
      if (gate === 'plan') {
        await fetchBaselines();
      }
    } catch (error) {
      console.error('Submit gate failed:', error);
      message.error(`${GATE_META[gate].label} 门禁未通过，请查看阻塞项`);
      await fetchGates();
    } finally {
      setGateLoading(false);
    }
  };

  const handleApproveGate = async (gate: ProjectGateKey) => {
    if (!project) return;
    setGateLoading(true);
    try {
      const updated = await projectService.decideGate(project.id, gate, 'approve');
      message.success(updated.status === 'passed' ? `${GATE_META[gate].label} 已放行` : '已提交本级审批');
      await fetchGates();
    } catch (error) {
      console.error('Approve gate failed:', error);
      message.error('裁决失败：请确认你是当前审批人');
    } finally {
      setGateLoading(false);
    }
  };

  const handleRejectGate = async () => {
    if (!project || !gateRejectTarget) return;
    if (!gateRejectComment.trim()) {
      message.warning('拒绝必须填写原因');
      return;
    }
    setGateLoading(true);
    try {
      await projectService.decideGate(project.id, gateRejectTarget, 'reject', gateRejectComment.trim());
      message.success(`${GATE_META[gateRejectTarget].label} 已拒绝（可修复后重新提交）`);
      setGateRejectTarget(null);
      setGateRejectComment('');
      await fetchGates();
    } catch (error) {
      console.error('Reject gate failed:', error);
      message.error('拒绝失败：请确认你是当前审批人');
    } finally {
      setGateLoading(false);
    }
  };

  // ---- P1（PE-15）结项清单动作 ----

  const applyClosureResult = (data: ProjectClosure) => {
    setClosure(data);
  };

  const handleGenerateRetro = async () => {
    if (!project) return;
    setClosureActionLoading(true);
    try {
      const data = await projectService.generateRetroDraft(project.id);
      applyClosureResult(data);
      message.success(`复盘草稿已生成（文章 #${data.retroKbArticleId}，未发布；请到知识库补充后发布）`);
    } catch (error) {
      console.error('Generate retro draft failed:', error);
      message.error('生成复盘草稿失败，请稍后重试');
    } finally {
      setClosureActionLoading(false);
    }
  };

  const handleRecycleMembers = () => {
    if (!project) return;
    Modal.confirm({
      title: '成员回收',
      content: '将回收本项目的「成员」角色（保留项目经理与干系人）。回收后这些成员不再可见该项目，是否继续？',
      okText: '执行回收',
      onOk: async () => {
        setClosureActionLoading(true);
        try {
          const data = await projectService.recycleClosureMembers(project.id);
          applyClosureResult(data);
          message.success(`成员回收完成（本次回收 ${data.membersRecycledCount} 人）`);
          await fetchMembers();
        } catch (error) {
          console.error('Recycle members failed:', error);
          message.error('成员回收失败，请稍后重试');
        } finally {
          setClosureActionLoading(false);
        }
      },
    });
  };

  const handleTriggerCSAT = async () => {
    if (!project) return;
    setClosureActionLoading(true);
    try {
      const data = await projectService.triggerClosureCSAT(project.id);
      applyClosureResult(data);
      message.success(`CSAT 调研已触发（调研 #${data.csatSurveyId}）`);
    } catch (error) {
      console.error('Trigger CSAT failed:', error);
      message.error('触发 CSAT 失败，请稍后重试');
    } finally {
      setClosureActionLoading(false);
    }
  };

  const openAssetsModal = (action: 'confirm' | 'waive') => {
    setAssetsAction(action);
    setAssetsNote(closure?.assetsNote ?? '');
    setAssetsReason(closure?.assetsWaiveReason ?? '');
    setAssetsModalOpen(true);
  };

  const submitClosureAssets = async () => {
    if (!project) return;
    if (assetsAction === 'waive' && !assetsReason.trim()) {
      message.warning('豁免必须填写原因');
      return;
    }
    setClosureActionLoading(true);
    try {
      const data = await projectService.confirmClosureAssets(project.id, assetsAction, {
        note: assetsAction === 'confirm' ? assetsNote.trim() : undefined,
        reason: assetsAction === 'waive' ? assetsReason.trim() : undefined,
      });
      applyClosureResult(data);
      message.success(assetsAction === 'confirm' ? '资产/许可登记已确认' : '已豁免资产/许可登记');
      setAssetsModalOpen(false);
    } catch (error) {
      console.error('Confirm closure assets failed:', error);
      message.error('登记确认失败，请稍后重试');
    } finally {
      setClosureActionLoading(false);
    }
  };

  // PE-26：登记明细——候选远程搜索（300ms 防抖）+ 新增 + 删除（成功后刷新结项清单）。
  const loadAssetLinkCandidates = useCallback(
    async (linkType: AssetLinkType, keyword: string) => {
      if (!project) return;
      setAssetLinkCandidatesLoading(true);
      try {
        const data = await projectService.listAssetLinkCandidates(project.id, linkType, {
          q: keyword.trim() || undefined,
          limit: 20,
        });
        setAssetLinkCandidates(data.items ?? []);
      } catch (error) {
        console.error('Load asset link candidates failed:', error);
        setAssetLinkCandidates([]);
      } finally {
        setAssetLinkCandidatesLoading(false);
      }
    },
    [project]
  );

  const scheduleAssetLinkCandidates = useCallback(
    (keyword: string) => {
      if (assetLinkSearchTimer.current) clearTimeout(assetLinkSearchTimer.current);
      assetLinkSearchTimer.current = setTimeout(() => {
        void loadAssetLinkCandidates(assetLinkType, keyword);
      }, 300);
    },
    [assetLinkType, loadAssetLinkCandidates]
  );

  const openAssetLinkModal = () => {
    setAssetLinkType('asset');
    setAssetLinkTarget(undefined);
    setAssetLinkNote('');
    setAssetLinkModalOpen(true);
    void loadAssetLinkCandidates('asset', '');
  };

  const submitAssetLink = async () => {
    if (!project) return;
    if (!assetLinkTarget) {
      message.warning('请选择登记目标');
      return;
    }
    setAssetLinkSubmitting(true);
    try {
      await projectService.createAssetLink(project.id, {
        linkType: assetLinkType,
        targetId: assetLinkTarget,
        note: assetLinkNote.trim() || undefined,
      });
      message.success('登记已添加');
      setAssetLinkModalOpen(false);
      await fetchClosure();
    } catch (error) {
      console.error('Create asset link failed:', error);
      message.error('登记失败：目标不存在、重复登记或无可写权限');
    } finally {
      setAssetLinkSubmitting(false);
    }
  };

  const removeAssetLink = async (linkId: number) => {
    if (!project) return;
    try {
      await projectService.deleteAssetLink(project.id, linkId);
      message.success('登记已删除');
      await fetchClosure();
    } catch (error) {
      console.error('Delete asset link failed:', error);
      message.error('删除失败，请稍后重试');
    }
  };

  const runTransition = async (action: string) => {
    if (!project) return;
    if (action === 'cancel') {
      Modal.confirm({
        title: '取消项目',
        content: '取消后不可恢复，是否继续？（原因留痕请到列表页操作）',
        onOk: async () => {
          await projectService.transitionProject(project.id, 'cancel', '前端取消', project.version);
          message.success('项目已取消');
          fetchProject();
        },
      });
      return;
    }
    try {
      await projectService.transitionProject(project.id, action, undefined, project.version);
      message.success('操作成功');
      fetchProject();
      if (action === 'complete') {
        // 结项成功：CSAT 已尽力自动触发，刷新清单查看结果。
        fetchClosure();
      }
    } catch (error) {
      console.error('Transition failed:', error);
      message.error('操作失败：请检查阶段门与结项门禁（未提交/未通过的阶段门、未终结任务或单据）');
    }
  };

  // ---- 任务 ----

  const openTaskCreate = () => {
    setEditingTask(null);
    taskForm.resetFields();
    taskForm.setFieldsValue({ priority: 'medium', parentTaskId: 0 });
    setTaskModalOpen(true);
  };

  const openTaskEdit = (task: ProjectTask) => {
    setEditingTask(task);
    taskForm.setFieldsValue({
      name: task.name,
      description: task.description,
      parentTaskId: task.parentTaskId ?? 0,
      priority: task.priority,
      assigneeId: task.assigneeId,
      planStart: dateValue(task.planStart),
      planEnd: dateValue(task.planEnd),
      estimateHours: task.estimateHours,
      actualHours: task.actualHours,
      progress: task.progress,
    });
    setTaskModalOpen(true);
  };

  const submitTask = async () => {
    try {
      const values = await taskForm.validateFields();
      const payload = {
        ...values,
        parentTaskId: values.parentTaskId || undefined,
        assigneeId: values.assigneeId || undefined,
        planStart: isoValue(values.planStart),
        planEnd: isoValue(values.planEnd),
      };
      if (editingTask) {
        await projectService.updateTask(projectId, editingTask.id, payload);
      } else {
        await projectService.createTask(projectId, payload);
      }
      message.success('保存成功');
      setTaskModalOpen(false);
      setEditingTask(null);
      fetchTasks();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Save task failed:', error);
      message.error('保存失败：负责人须为项目活跃成员，层级 ≤3 层');
    }
  };

  const runTaskAction = async (task: ProjectTask, action: string) => {
    try {
      await projectService.transitionTask(projectId, task.id, action, task.version);
      message.success('操作成功');
      fetchTasks();
    } catch (error) {
      console.error('Task transition failed:', error);
      message.error('非法状态流转或并发冲突');
    }
  };

  const deleteTask = async (task: ProjectTask) => {
    try {
      await projectService.deleteTask(projectId, task.id);
      message.success('已删除');
      fetchTasks();
    } catch (error) {
      console.error('Delete task failed:', error);
      message.error('存在子任务时不可删除');
    }
  };

  const generateTicket = async (task: ProjectTask) => {
    try {
      await projectService.generateTaskTicket(projectId, task.id, { title: task.name, priority: task.priority });
      message.success('已生成工单并关联');
      fetchTasks();
    } catch (error) {
      console.error('Generate ticket failed:', error);
      message.error('生成失败（任务已有工单或工单服务未启用）');
    }
  };

  const unbindTicket = async (task: ProjectTask) => {
    try {
      await projectService.unbindTaskTicket(projectId, task.id);
      message.success('已解绑');
      fetchTasks();
    } catch (error) {
      console.error('Unbind ticket failed:', error);
      message.error('解绑失败');
    }
  };

  // ---- 里程碑（PE-14） ----

  const openMilestoneCreate = () => {
    setEditingMilestone(null);
    milestoneForm.resetFields();
    setMilestoneModalOpen(true);
  };

  const openMilestoneEdit = (m: ProjectMilestone) => {
    setEditingMilestone(m);
    milestoneForm.setFieldsValue({
      name: m.name,
      description: m.description,
      dueDate: dateValue(m.dueDate),
      ownerId: m.ownerId,
    });
    setMilestoneModalOpen(true);
  };

  const submitMilestone = async () => {
    try {
      const values = await milestoneForm.validateFields();
      const payload = {
        ...values,
        dueDate: isoValue(values.dueDate),
        ownerId: values.ownerId || undefined,
      };
      if (editingMilestone) {
        await projectService.updateMilestone(projectId, editingMilestone.id, payload);
      } else {
        await projectService.createMilestone(projectId, payload);
      }
      message.success('保存成功');
      setMilestoneModalOpen(false);
      setEditingMilestone(null);
      fetchMilestones();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Save milestone failed:', error);
      message.error('保存失败：负责人须为项目活跃成员，终态项目不可新增');
    }
  };

  const runMilestoneAction = async (m: ProjectMilestone, action: 'reach' | 'miss') => {
    try {
      await projectService.transitionMilestone(projectId, m.id, action);
      message.success('操作成功');
      fetchMilestones();
    } catch (error) {
      console.error('Milestone transition failed:', error);
      message.error('非法状态流转');
    }
  };

  const deleteMilestone = async (m: ProjectMilestone) => {
    try {
      await projectService.deleteMilestone(projectId, m.id);
      message.success('已删除');
      fetchMilestones();
    } catch (error) {
      console.error('Delete milestone failed:', error);
      message.error('删除失败');
    }
  };

  // ---- 任务依赖（PE-14，v1 仅 FS） ----

  const openDepModal = (task: ProjectTask) => {
    setDepTask(task);
    depForm.resetFields();
    setDepModalOpen(true);
  };

  const submitDependency = async () => {
    if (!depTask) return;
    try {
      const values = await depForm.validateFields();
      await projectService.addDependency(projectId, depTask.id, values.dependsOnTaskId);
      message.success('依赖已添加');
      depForm.resetFields();
      fetchDependencies();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Add dependency failed:', error);
      message.error('添加失败：不可自依赖/重复依赖/形成环');
    }
  };

  const removeDependency = async (taskId: number, dependsOnTaskId: number) => {
    try {
      await projectService.removeDependency(projectId, taskId, dependsOnTaskId);
      message.success('依赖已移除');
      fetchDependencies();
    } catch (error) {
      console.error('Remove dependency failed:', error);
      message.error('移除失败');
    }
  };

  // ---- 计划基线（PE-13） ----

  const openFreezeModal = () => {
    freezeForm.resetFields();
    setFreezeModalOpen(true);
  };

  const submitFreeze = async () => {
    try {
      const values = await freezeForm.validateFields();
      await projectService.createBaseline(projectId, values.reason?.trim() || undefined);
      message.success('基线已冻结（不可变快照）');
      setFreezeModalOpen(false);
      fetchBaselines();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Freeze baseline failed:', error);
      message.error('冻结失败：终态项目不可冻结基线');
    }
  };

  const taskColumns: ColumnsType<ProjectTask> = [
    { title: '任务名称', dataIndex: 'name', key: 'name' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (s: string) => <Tag color={TASK_STATUS_META[s]?.color}>{TASK_STATUS_META[s]?.label ?? s}</Tag>,
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 90,
      render: (p: TaskPriority) => <Tag color={PRIORITY_META[p]?.color}>{PRIORITY_META[p]?.label ?? p}</Tag>,
    },
    {
      title: '负责人',
      dataIndex: 'assigneeId',
      key: 'assigneeId',
      width: 120,
      render: (uid?: number) => (uid ? memberNameById[uid] ?? `ID:${uid}` : '-'),
    },
    {
      title: '计划',
      key: 'plan',
      width: 200,
      render: (_: unknown, task) =>
        `${task.planStart?.slice(0, 10) ?? '-'} ~ ${task.planEnd?.slice(0, 10) ?? '-'}`,
    },
    { title: '进度', dataIndex: 'progress', key: 'progress', width: 120, render: (v: number) => `${v}%` },
    {
      // PE-22：工时（估算 / 实际；实际为空表示未填报）。
      title: '工时（估/实）',
      key: 'hours',
      width: 120,
      render: (_: unknown, task) => `${task.estimateHours ?? '-'} / ${task.actualHours ?? '-'}`,
    },
    {
      title: '工单',
      dataIndex: 'linkedTicketId',
      key: 'linkedTicketId',
      width: 90,
      render: (tid?: number) => (tid ? <Tag color="blue">#{tid}</Tag> : '-'),
    },
    {
      title: '操作',
      key: 'action',
      width: 320,
      render: (_: unknown, task) => (
        <Space size="small" wrap>
          {canWrite && task.status === 'todo' && <a onClick={() => runTaskAction(task, 'start')}>开始</a>}
          {canWrite && task.status === 'in_progress' && (
            <>
              <a onClick={() => runTaskAction(task, 'block')}>阻塞</a>
              <a onClick={() => runTaskAction(task, 'complete')}>完成</a>
            </>
          )}
          {canWrite && task.status === 'blocked' && (
            <>
              <a onClick={() => runTaskAction(task, 'unblock')}>解除阻塞</a>
              <a onClick={() => runTaskAction(task, 'complete')}>完成</a>
            </>
          )}
          {canWrite && (task.status === 'todo' || task.status === 'in_progress' || task.status === 'blocked') && (
            <a onClick={() => runTaskAction(task, 'cancel')}>取消</a>
          )}
          {canWrite && <a onClick={() => openTaskEdit(task)}>编辑</a>}
          {canWrite && <a onClick={() => openDepModal(task)}>依赖</a>}
          {canWrite &&
            (task.linkedTicketId ? (
              <a onClick={() => unbindTicket(task)}>解绑工单</a>
            ) : (
              <a onClick={() => generateTicket(task)}>生成工单</a>
            ))}
          {canWrite && (
            <Popconfirm title="确认删除该任务？" onConfirm={() => deleteTask(task)}>
              <a>删除</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  // ---- 里程碑 / 基线展示（P1：PE-14 / PE-13） ----

  const milestoneColumns: ColumnsType<ProjectMilestone> = [
    { title: '里程碑', dataIndex: 'name', key: 'name' },
    {
      title: '计划日期',
      dataIndex: 'dueDate',
      key: 'dueDate',
      width: 170,
      render: (v: string, m) => (
        <Space size={4}>
          {v?.slice(0, 10)}
          {milestoneOverdue(m) && <Tag color="error">已逾期</Tag>}
        </Space>
      ),
    },
    {
      title: '负责人',
      dataIndex: 'ownerId',
      key: 'ownerId',
      width: 120,
      render: (uid?: number) => (uid ? memberNameById[uid] ?? `ID:${uid}` : '-'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (s: MilestoneStatus) => (
        <Tag color={MILESTONE_STATUS_META[s]?.color}>{MILESTONE_STATUS_META[s]?.label ?? s}</Tag>
      ),
    },
    {
      title: '达成时间',
      dataIndex: 'reachedAt',
      key: 'reachedAt',
      width: 170,
      render: (v?: string) => v?.replace('T', ' ').slice(0, 19) ?? '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 240,
      render: (_: unknown, m) => (
        <Space size="small" wrap>
          {canWrite && (m.status === 'planned' || m.status === 'missed') && (
            <a onClick={() => runMilestoneAction(m, 'reach')}>达成</a>
          )}
          {canWrite && m.status === 'planned' && <a onClick={() => runMilestoneAction(m, 'miss')}>错过</a>}
          {canWrite && <a onClick={() => openMilestoneEdit(m)}>编辑</a>}
          {canWrite && (
            <Popconfirm title="确认删除该里程碑？" onConfirm={() => deleteMilestone(m)}>
              <a>删除</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  const baselineColumns: ColumnsType<ProjectBaseline> = [
    { title: '版本', dataIndex: 'version', key: 'version', width: 80, render: (v: number) => `v${v}` },
    { title: '变更原因', dataIndex: 'reason', key: 'reason', render: (v?: string) => v || '（首版）' },
    {
      title: '任务快照',
      key: 'tasks',
      width: 100,
      render: (_: unknown, b) => b.snapshot?.tasks.length ?? '-',
    },
    {
      title: '里程碑快照',
      key: 'milestones',
      width: 110,
      render: (_: unknown, b) => b.snapshot?.milestones.length ?? '-',
    },
    {
      title: '冻结时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 180,
      render: (v: string) => v?.replace('T', ' ').slice(0, 19),
    },
  ];

  // PE-12：阶段门表格（四道门固定顺序；无记录 = 未提交）。
  const gateRows: GateRow[] = GATE_ORDER.map((gate) => ({ gate, record: gatesByKey[gate] }));

  const gateColumns: ColumnsType<GateRow> = [
    {
      title: '阶段门',
      key: 'gate',
      width: 220,
      render: (_: unknown, row) => (
        <div>
          <div>{GATE_META[row.gate].label}</div>
          <div style={{ color: '#8c8c8c', fontSize: 12 }}>{GATE_META[row.gate].hint}</div>
        </div>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      render: (_: unknown, row) => {
        const status: ProjectGateStatus = row.record?.status ?? 'pending';
        return <Tag color={GATE_STATUS_META[status].color}>{GATE_STATUS_META[status].label}</Tag>;
      },
    },
    {
      title: '提交 / 裁决',
      key: 'decision',
      width: 240,
      render: (_: unknown, row) => {
        const record = row.record;
        if (!record) return '-';
        return (
          <div style={{ fontSize: 12, color: '#595959' }}>
            {record.submittedAt ? (
              <div>{`提交：${record.submittedAt.replace('T', ' ').slice(0, 19)}`}</div>
            ) : null}
            {record.processInstanceId ? <div>{`流程实例：#${record.processInstanceId}`}</div> : null}
            {record.decidedAt ? <div>{`裁决：${record.decidedAt.replace('T', ' ').slice(0, 19)}`}</div> : null}
            {record.decisionComment ? <div>{`意见：${record.decisionComment}`}</div> : null}
          </div>
        );
      },
    },
    {
      title: '门禁阻塞项',
      key: 'blockers',
      render: (_: unknown, row) => {
        const blockers = row.record?.blockers ?? [];
        if (blockers.length === 0) return '-';
        return (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {blockers.map((b, index) => (
              <Tag key={`${b.type}-${index}`} color="warning">
                {`${GATE_BLOCKER_META[b.type] ?? b.type}${b.total ? `（${b.total}）` : ''}${b.detail ? `：${b.detail}` : ''}`}
              </Tag>
            ))}
          </div>
        );
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      render: (_: unknown, row) => {
        const status = row.record?.status ?? 'pending';
        if (!canWrite || status === 'passed') return '-';
        if (status === 'in_review') {
          return (
            <Space size={4}>
              <Button size="small" type="primary" loading={gateLoading} onClick={() => handleApproveGate(row.gate)}>
                通过
              </Button>
              <Button size="small" danger loading={gateLoading} onClick={() => setGateRejectTarget(row.gate)}>
                拒绝
              </Button>
            </Space>
          );
        }
        return (
          <Button size="small" type="primary" loading={gateLoading} onClick={() => handleSubmitGate(row.gate)}>
            {status === 'rejected' ? '重新提交' : '提交'}
          </Button>
        );
      },
    },
  ];

  // PE-15：结项清单表格（自动项 + 人工/产物项；顺序与后端一致）。
  const closureRows = CLOSURE_ITEM_ORDER.map((key) => closure?.items.find((item) => item.key === key)).filter(
    (item): item is NonNullable<typeof item> => Boolean(item)
  );
  const closureBlockingTotal = closureRows.filter((item) => item.blocking).length;
  const closureBlockingDone = closureRows.filter((item) => item.blocking && item.status === 'done').length;

  const closureColumns: ColumnsType<(typeof closureRows)[number]> = [
    {
      title: '结项清单项',
      key: 'item',
      width: 220,
      render: (_: unknown, row) => (
        <div>
          <div>
            {CLOSURE_ITEM_META[row.key]?.label ?? row.title}
            {row.blocking ? (
              <Tag style={{ marginLeft: 6 }} color="default">
                阻塞项
              </Tag>
            ) : (
              <Tag style={{ marginLeft: 6 }} color="default">
                非阻塞
              </Tag>
            )}
          </div>
          <div style={{ color: '#8c8c8c', fontSize: 12 }}>{CLOSURE_ITEM_META[row.key]?.hint}</div>
        </div>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      render: (_: unknown, row) =>
        row.status === 'done' ? <Tag color="success">已通过</Tag> : <Tag color="warning">未通过</Tag>,
    },
    {
      title: '说明',
      key: 'detail',
      render: (_: unknown, row) => row.detail || '-',
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_: unknown, row) => {
        if (!canWrite || project?.status === 'completed' || project?.status === 'cancelled') return '-';
        if (row.key === 'retro') {
          return row.status === 'done' ? (
            <span style={{ color: '#8c8c8c', fontSize: 12 }}>{`草稿 #${closure?.retroKbArticleId}（未发布）`}</span>
          ) : (
            <Button size="small" type="primary" loading={closureActionLoading} onClick={handleGenerateRetro}>
              生成复盘草稿
            </Button>
          );
        }
        if (row.key === 'assets') {
          return (
            <Space size={4}>
              <Button size="small" type="primary" loading={closureActionLoading} onClick={() => openAssetsModal('confirm')}>
                确认登记
              </Button>
              <Button size="small" loading={closureActionLoading} onClick={() => openAssetsModal('waive')}>
                豁免
              </Button>
            </Space>
          );
        }
        if (row.key === 'members') {
          return (
            <Button size="small" type="primary" loading={closureActionLoading} onClick={handleRecycleMembers}>
              执行成员回收
            </Button>
          );
        }
        if (row.key === 'csat') {
          return row.status === 'done' ? (
            <span style={{ color: '#8c8c8c', fontSize: 12 }}>{`调研 #${closure?.csatSurveyId}`}</span>
          ) : (
            <Button size="small" loading={closureActionLoading} onClick={handleTriggerCSAT}>
              手动触发 CSAT
            </Button>
          );
        }
        return '-';
      },
    },
  ];

  // PE-26：采购/资产登记明细表格（目标缺失灰显 + 删除）。
  const assetLinkColumns: ColumnsType<AssetLinkItem> = [
    {
      title: '类型',
      key: 'linkType',
      width: 90,
      render: (_: unknown, row) => (
        <Tag color={ASSET_LINK_TYPE_META[row.linkType as AssetLinkType]?.color}>{assetLinkTypeLabel(row.linkType)}</Tag>
      ),
    },
    {
      title: '台账目标',
      key: 'target',
      render: (_: unknown, row) =>
        row.targetMissing ? (
          <span style={{ color: '#8c8c8c' }}>
            {`${assetLinkTypeLabel(row.linkType)} #${row.targetId}`}
            <Tag style={{ marginLeft: 6 }} color="default">
              目标已删除
            </Tag>
          </span>
        ) : (
          <div>
            <div>{row.label}</div>
            {row.sublabel ? <div style={{ color: '#8c8c8c', fontSize: 12 }}>{row.sublabel}</div> : null}
          </div>
        ),
    },
    { title: '备注', dataIndex: 'note', key: 'note', render: (v?: string) => v || '-' },
    {
      title: '登记时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 170,
      render: (v: string) => (v ? new Date(v).toLocaleString() : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_: unknown, row) =>
        canWrite && project?.status !== 'cancelled' ? (
          <Popconfirm title="删除该登记？" onConfirm={() => removeAssetLink(row.id)} okText="删除" cancelText="取消">
            <Button size="small" danger>
              删除
            </Button>
          </Popconfirm>
        ) : (
          '-'
        ),
    },
  ];

  const diffTaskColumns: ColumnsType<BaselineTaskDeviation> = [
    { title: '任务', dataIndex: 'name', key: 'name' },
    {
      title: '偏差',
      dataIndex: 'changeType',
      key: 'changeType',
      width: 90,
      render: (t: string) => <Tag color={CHANGE_TYPE_META[t]?.color}>{CHANGE_TYPE_META[t]?.label ?? t}</Tag>,
    },
    {
      title: '基线状态',
      dataIndex: 'baselineStatus',
      key: 'baselineStatus',
      width: 110,
      render: (v?: string) => TASK_STATUS_META[v ?? '']?.label ?? v ?? '-',
    },
    {
      title: '当前状态',
      dataIndex: 'currentStatus',
      key: 'currentStatus',
      width: 110,
      render: (v?: string) => TASK_STATUS_META[v ?? '']?.label ?? v ?? '-',
    },
    {
      title: '进度差',
      dataIndex: 'progressDelta',
      key: 'progressDelta',
      width: 90,
      render: (v: number) => (v > 0 ? `+${v}%` : `${v}%`),
    },
    {
      title: '计划结束偏移',
      dataIndex: 'planEndShiftDays',
      key: 'planEndShiftDays',
      width: 130,
      render: (v?: number) => (v === undefined ? '-' : v === 0 ? '0 天' : `${v > 0 ? '+' : ''}${v} 天`),
    },
  ];

  const diffMilestoneColumns: ColumnsType<BaselineMilestoneDeviation> = [
    { title: '里程碑', dataIndex: 'name', key: 'name' },
    {
      title: '偏差',
      dataIndex: 'changeType',
      key: 'changeType',
      width: 90,
      render: (t: string) => <Tag color={CHANGE_TYPE_META[t]?.color}>{CHANGE_TYPE_META[t]?.label ?? t}</Tag>,
    },
    {
      title: '基线状态',
      dataIndex: 'baselineStatus',
      key: 'baselineStatus',
      width: 110,
      render: (v?: string) => MILESTONE_STATUS_META[v as MilestoneStatus]?.label ?? v ?? '-',
    },
    {
      title: '当前状态',
      dataIndex: 'currentStatus',
      key: 'currentStatus',
      width: 110,
      render: (v?: string) => MILESTONE_STATUS_META[v as MilestoneStatus]?.label ?? v ?? '-',
    },
    {
      title: '计划日期偏移',
      dataIndex: 'dueDateShiftDays',
      key: 'dueDateShiftDays',
      width: 140,
      render: (v?: number) => (v === undefined ? '-' : v === 0 ? '0 天' : `${v > 0 ? '+' : ''}${v} 天`),
    },
  ];

  // 依赖弹窗数据：当前任务的前置任务 + 可选候选（排除自身与已添加）。
  const depRows = depTask ? dependencies.filter((d) => d.taskId === depTask.id) : [];
  const depPredecessorIds = depRows.map((d) => d.dependsOnTaskId);
  const depCandidates = tasks.filter((t) => t.id !== depTask?.id && !depPredecessorIds.includes(t.id));

  const depColumns: ColumnsType<TaskDependency> = [
    {
      title: '前置任务',
      dataIndex: 'dependsOnTaskId',
      key: 'dependsOnTaskId',
      render: (id: number) => taskNameById[id] ?? `ID:${id}`,
    },
    { title: '类型', dataIndex: 'depType', key: 'depType', width: 90, render: (t: string) => <Tag>{t}</Tag> },
    {
      title: '操作',
      key: 'action',
      width: 90,
      render: (_: unknown, d) =>
        canWrite ? <a onClick={() => removeDependency(d.taskId, d.dependsOnTaskId)}>移除</a> : null,
    },
  ];

  // ---- 成员 ----

  const searchCandidates = async (q?: string) => {
    setCandidateLoading(true);
    try {
      setCandidates(await projectService.memberCandidates(projectId, q));
    } catch (error) {
      console.error('Load candidates failed:', error);
    } finally {
      setCandidateLoading(false);
    }
  };

  const openMemberModal = () => {
    memberForm.resetFields();
    memberForm.setFieldsValue({ projectRole: 'member' });
    setMemberModalOpen(true);
    searchCandidates();
  };

  const submitMember = async () => {
    try {
      const values = await memberForm.validateFields();
      await projectService.addMember(projectId, values.userId, values.projectRole);
      message.success('成员已添加');
      setMemberModalOpen(false);
      fetchMembers();
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) return;
      console.error('Add member failed:', error);
      message.error('添加失败：该用户在本租户无有效成员身份或已在项目中');
    }
  };

  const memberColumns: ColumnsType<MemberView> = [
    { title: '姓名', dataIndex: 'name', key: 'name' },
    { title: '用户名', dataIndex: 'username', key: 'username', width: 140 },
    {
      title: '项目角色',
      dataIndex: 'projectRole',
      key: 'projectRole',
      width: 150,
      render: (role: ProjectRole, m) =>
        canWrite && m.status === 'active' ? (
          <Select
            size="small"
            value={role}
            style={{ width: 130 }}
            options={Object.entries(ROLE_META).map(([value, label]) => ({ value, label }))}
            onChange={async (value) => {
              try {
                await projectService.updateMemberRole(projectId, m.userId, value as ProjectRole);
                message.success('已更新');
                fetchMembers();
              } catch {
                message.error('更新失败');
              }
            }}
          />
        ) : (
          ROLE_META[role] ?? role
        ),
    },
    {
      title: '来源',
      dataIndex: 'memberOrigin',
      key: 'memberOrigin',
      width: 110,
      render: (origin: string) => (
        <Tag color={ORIGIN_META[origin]?.color}>{ORIGIN_META[origin]?.label ?? origin}</Tag>
      ),
    },
    {
      title: '到期',
      dataIndex: 'expiresAt',
      key: 'expiresAt',
      width: 150,
      render: (v?: string) =>
        v ? (
          <Space size={4}>
            {v.slice(0, 10)}
            {expiringSoon(v) && <Tag color="warning">即将到期</Tag>}
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 90,
      render: (s: string) => (s === 'active' ? <Tag color="success">在册</Tag> : <Tag>已移除</Tag>),
    },
    {
      title: '操作',
      key: 'action',
      width: 90,
      render: (_: unknown, m) =>
        canWrite && m.status === 'active' ? (
          <Popconfirm
            title="移除该成员？"
            description="项目经理需先变更项目经理后才能移除"
            onConfirm={async () => {
              try {
                await projectService.removeMember(projectId, m.userId);
                message.success('已移除');
                fetchMembers();
              } catch {
                message.error('移除失败：项目经理不可移除');
              }
            }}
          >
            <a>移除</a>
          </Popconfirm>
        ) : null,
    },
  ];

  // ---- 关联单据 ----

  const recordColumns: ColumnsType<RecordItem> = [
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      width: 100,
      render: (type: RecordDomain) => RECORD_TYPE_META[type] ?? type,
    },
    { title: '编号', dataIndex: 'number', key: 'number', width: 170, render: (v?: string) => v ?? '-' },
    { title: '标题', dataIndex: 'title', key: 'title' },
    { title: '状态', dataIndex: 'status', key: 'status', width: 120 },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 170,
      render: (v: string) => v?.replace('T', ' ').slice(0, 19),
    },
    {
      title: '操作',
      key: 'action',
      width: 80,
      render: (_: unknown, record) =>
        canWrite ? (
          <Popconfirm
            title="解除与项目的关联？"
            onConfirm={async () => {
              try {
                await projectService.detachRecord(record.type, record.id);
                message.success('已解除');
                fetchRecords();
              } catch {
                message.error('解除失败');
              }
            }}
          >
            <a>解除</a>
          </Popconfirm>
        ) : null,
    },
  ];

  if (!project) {
    return (
      <PageContainer header={{ title: '项目工作台' }}>
        <Button icon={<ArrowLeft />} onClick={() => navigate('/projects')}>
          返回项目列表
        </Button>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      header={{
        title: project.name,
        breadcrumb: {
          items: [
            { title: '首页' },
            { title: '项目管理', onClick: () => navigate('/projects') } as never,
            { title: project.code },
          ],
        },
      }}
      extra={[
        <Button key="back" icon={<ArrowLeft />} onClick={() => navigate('/projects')}>
          返回
        </Button>,
        <Button key="refresh" icon={<RefreshCw />} onClick={fetchProject} loading={loading}>
          刷新
        </Button>,
        canWrite && !project.templateId && project.status !== 'completed' && project.status !== 'cancelled' ? (
          <Button key="apply-template" icon={<FilePlus2 size={16} />} onClick={openApplyModal}>
            应用模板
          </Button>
        ) : null,
        canWrite && project.status === 'planning' ? (
          <Button key="activate" type="primary" onClick={() => runTransition('activate')}>
            激活
          </Button>
        ) : null,
        canWrite && project.status === 'active' ? (
          <Button key="suspend" onClick={() => runTransition('suspend')}>
            暂停
          </Button>
        ) : null,
        canWrite && project.status === 'suspended' ? (
          <Button key="resume" type="primary" onClick={() => runTransition('resume')}>
            恢复
          </Button>
        ) : null,
        canWrite && (project.status === 'active' || project.status === 'suspended') ? (
          <Button key="complete" onClick={() => runTransition('complete')}>
            结项
          </Button>
        ) : null,
      ]}
    >
      <Tabs
        items={[
          {
            key: 'overview',
            label: '概览',
            children: (
              <>
                <Progress percent={project.progress} status={project.status === 'cancelled' ? 'exception' : undefined} />
                <Descriptions bordered column={2} style={{ marginTop: 16 }}>
                  <Descriptions.Item label="项目编号">{project.code}</Descriptions.Item>
                  <Descriptions.Item label="状态">
                    <Tag color={STATUS_META[project.status]?.color}>{STATUS_META[project.status]?.label}</Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="健康度">
                    <Tag color={HEALTH_META[project.health]?.color}>{HEALTH_META[project.health]?.label}</Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="项目经理">
                    {project.managerId ? memberNameById[project.managerId] ?? `ID:${project.managerId}` : '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="计划起止">
                    {project.startDate?.slice(0, 10) ?? '-'} ~ {project.endDate?.slice(0, 10) ?? '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="实际起止">
                    {project.actualStart?.slice(0, 10) ?? '-'} ~ {project.actualEnd?.slice(0, 10) ?? '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="结项时间">{project.closedAt?.slice(0, 19) ?? '-'}</Descriptions.Item>
                  <Descriptions.Item label="已应用模板">
                    {project.templateId
                      ? `${appliedTemplateName || `模板 #${project.templateId}`}${
                          project.templateAppliedAt ? `（${project.templateAppliedAt.slice(0, 10)}）` : ''
                        }`
                      : '未应用'}
                  </Descriptions.Item>
                  {/* P2（PE-22）：轻量预算 + 任务工时汇总（估算/实际，口径见 plan/p2-budget-hours-design.md）。 */}
                  <Descriptions.Item label="预算">
                    {project.budgetAmount
                      ? `${project.budgetAmount.toLocaleString()} ${project.budgetCurrency ?? ''}`.trim()
                      : '未设置'}
                  </Descriptions.Item>
                  <Descriptions.Item label="工时汇总（估算/实际）">
                    {hoursSummary
                      ? `${hoursSummary.estimateHours} / ${hoursSummary.actualHours} 小时（实际已填报 ${hoursSummary.actualTaskCount}/${hoursSummary.taskCount} 任务）`
                      : '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="描述" span={2}>
                    {project.description || '-'}
                  </Descriptions.Item>
                </Descriptions>
              </>
            ),
          },
          {
            key: 'gates',
            label: `阶段门 (${GATE_ORDER.filter((g) => gatesByKey[g]?.status === 'passed').length}/4)`,
            children: (
              <>
                <Space style={{ marginBottom: 12 }}>
                  <Button icon={<RefreshCw />} onClick={fetchGates} loading={gateLoading}>
                    刷新
                  </Button>
                  <span style={{ color: '#8c8c8c' }}>
                    立项门通过后才能激活项目；结项门通过后才能结项（拒绝须填写原因，可修复后重新提交）。
                  </span>
                </Space>
                <Table
                  rowKey="gate"
                  columns={gateColumns}
                  dataSource={gateRows}
                  pagination={false}
                  size="small"
                  loading={gateLoading}
                />
              </>
            ),
          },
          {
            key: 'closure',
            label: `结项清单 (${closureBlockingDone}/${closureBlockingTotal})`,
            children: (
              <>
                <Space style={{ marginBottom: 12 }}>
                  <Button icon={<RefreshCw />} onClick={fetchClosure} loading={closureLoading}>
                    刷新
                  </Button>
                  {closure ? (
                    <Tag color={CLOSURE_STATUS_META[closure.status]?.color}>
                      {CLOSURE_STATUS_META[closure.status]?.label ?? closure.status}
                    </Tag>
                  ) : null}
                  <span style={{ color: '#8c8c8c' }}>
                    阻塞项全部通过后，用页头「结项」完成项目；复盘草稿不自动发布，CSAT 缺失不阻塞结项。
                  </span>
                </Space>
                {closure && closure.blockers.length > 0 && (
                  <Alert
                    type="warning"
                    showIcon
                    style={{ marginBottom: 12 }}
                    message={`存在 ${closure.blockers.length} 项结项阻塞`}
                    description={
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                        {closure.blockers.map((b, index) => (
                          <Tag key={`${b.type}-${index}`} color="warning">
                            {`${CLOSURE_ITEM_META[b.type]?.label ?? b.type}${b.title ? `：${b.title}` : ''}${
                              b.number ? `（${b.number}）` : ''
                            }`}
                          </Tag>
                        ))}
                      </div>
                    }
                  />
                )}
                <Table
                  rowKey="key"
                  columns={closureColumns}
                  dataSource={closureRows}
                  pagination={false}
                  size="small"
                  loading={closureLoading || closureActionLoading}
                />
                <Card
                  size="small"
                  style={{ marginTop: 16 }}
                  title={`采购/资产登记明细（${closure?.assetsLinks?.length ?? 0}）`}
                  extra={
                    canWrite && project?.status !== 'cancelled' ? (
                      <Button size="small" type="primary" onClick={openAssetLinkModal}>
                        添加登记
                      </Button>
                    ) : null
                  }
                >
                  <div style={{ color: '#8c8c8c', fontSize: 12, marginBottom: 8 }}>
                    链接既有台账（同租户）：资产 / 许可证 / 供应商 / 合同；不做采购流程，登记明细不改变结项门禁（仍需确认或豁免）。
                  </div>
                  <Table
                    rowKey="id"
                    columns={assetLinkColumns}
                    dataSource={closure?.assetsLinks ?? []}
                    pagination={false}
                    size="small"
                    loading={closureLoading}
                    locale={{ emptyText: '尚未登记台账目标' }}
                  />
                  {closure?.assetsLinksTruncated ? (
                    <Alert
                      style={{ marginTop: 8 }}
                      type="warning"
                      showIcon
                      message="登记明细超过 200 条，仅展示前 200 条"
                    />
                  ) : null}
                </Card>
              </>
            ),
          },
          {
            key: 'tasks',
            label: `任务 (${tasks.length})`,
            children: (
              <>
                {canWrite && (
                  <Button type="primary" icon={<Plus />} onClick={openTaskCreate} style={{ marginBottom: 12 }}>
                    新建任务
                  </Button>
                )}
                <Table rowKey="id" columns={taskColumns} dataSource={tasks} pagination={false} size="small" />
              </>
            ),
          },
          {
            key: 'milestones',
            label: `里程碑 (${milestones.length})`,
            children: (
              <>
                {canWrite && (
                  <Button type="primary" icon={<Plus />} onClick={openMilestoneCreate} style={{ marginBottom: 12 }}>
                    新建里程碑
                  </Button>
                )}
                <Table rowKey="id" columns={milestoneColumns} dataSource={milestones} pagination={false} size="small" />
              </>
            ),
          },
          {
            key: 'baselines',
            label: `计划基线 (${baselines.length})`,
            children: (
              <>
                <Space style={{ marginBottom: 12 }}>
                  {canWrite && (
                    <Button type="primary" icon={<Flag />} onClick={openFreezeModal} loading={baselineLoading}>
                      冻结基线
                    </Button>
                  )}
                  <Button icon={<RefreshCw />} onClick={fetchBaselines} loading={baselineLoading}>
                    刷新
                  </Button>
                </Space>
                <Table rowKey="id" columns={baselineColumns} dataSource={baselines} pagination={false} size="small" />
                {baselineDiff && (
                  <div style={{ marginTop: 16 }}>
                    <Space style={{ marginBottom: 8 }}>
                      <GitCompare size={16} />
                      <span>{`最新基线 v${baselineDiff.version} 偏差（vs 当前）`}</span>
                    </Space>
                    <Descriptions bordered size="small" column={2} style={{ marginBottom: 12 }}>
                      <Descriptions.Item label="任务 新增/删除/变更/未变">
                        {`${baselineDiff.taskSummary.added} / ${baselineDiff.taskSummary.removed} / ${baselineDiff.taskSummary.changed} / ${baselineDiff.taskSummary.unchanged}`}
                      </Descriptions.Item>
                      <Descriptions.Item label="里程碑 新增/删除/变更/未变">
                        {`${baselineDiff.milestoneSummary.added} / ${baselineDiff.milestoneSummary.removed} / ${baselineDiff.milestoneSummary.changed} / ${baselineDiff.milestoneSummary.unchanged}`}
                      </Descriptions.Item>
                    </Descriptions>
                    <Table
                      rowKey="taskId"
                      columns={diffTaskColumns}
                      dataSource={baselineDiff.taskChanges.filter((c) => c.changeType !== 'unchanged')}
                      pagination={false}
                      size="small"
                      locale={{ emptyText: '任务无偏差' }}
                      style={{ marginBottom: 12 }}
                    />
                    <Table
                      rowKey="milestoneId"
                      columns={diffMilestoneColumns}
                      dataSource={baselineDiff.milestoneChanges.filter((c) => c.changeType !== 'unchanged')}
                      pagination={false}
                      size="small"
                      locale={{ emptyText: '里程碑无偏差' }}
                    />
                  </div>
                )}
              </>
            ),
          },
          {
            key: 'records',
            label: `关联单据 (${recordTotal})`,
            children: (
              <>
                <Space style={{ marginBottom: 12 }}>
                  <Select
                    style={{ width: 160 }}
                    value={recordType}
                    options={[
                      { value: '', label: '全部类型' },
                      ...Object.entries(RECORD_TYPE_META).map(([value, label]) => ({ value, label })),
                    ]}
                    onChange={(value) => {
                      setRecordType(value);
                      fetchRecords(value);
                    }}
                  />
                </Space>
                <Table rowKey={(r) => `${r.type}-${r.id}`} columns={recordColumns} dataSource={records} pagination={false} size="small" />
              </>
            ),
          },
          {
            key: 'members',
            label: `成员 (${members.filter((m) => m.status === 'active').length})`,
            children: (
              <>
                {canWrite && (
                  <Button type="primary" icon={<Plus />} onClick={openMemberModal} style={{ marginBottom: 12 }}>
                    添加成员
                  </Button>
                )}
                <Table rowKey="userId" columns={memberColumns} dataSource={members} pagination={false} size="small" />
              </>
            ),
          },
          {
            key: 'report',
            label: '状态报告',
            children: <ProjectReportPanel projectId={project.id} />,
          },
          {
            key: 'risks',
            label: '风险/问题',
            children: (
              <ProjectRiskPanel
                projectId={project.id}
                canWrite={canWrite}
                memberOptions={members.map((m) => ({ value: m.userId, label: m.name || m.username }))}
              />
            ),
          },
          {
            key: 'resources',
            label: '资源负荷',
            children: <ProjectResourceHeatmap projectId={project.id} canWrite={canWrite} />,
          },
          {
            key: 'ai',
            label: 'AI 助手',
            children: (
              <ProjectAITab projectId={project.id} canWrite={canWrite} onTasksCreated={() => void fetchTasks()} />
            ),
          },
        ]}
      />

      <Modal
        title={editingTask ? '编辑任务' : '新建任务'}
        open={taskModalOpen}
        onOk={submitTask}
        onCancel={() => {
          setTaskModalOpen(false);
          setEditingTask(null);
        }}
        width={640}
      >
        <Form form={taskForm} layout="vertical">
          <Form.Item name="name" label="任务名称" rules={[{ required: true, message: '请输入任务名称' }]}>
            <Input />
          </Form.Item>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="parentTaskId" label="父任务（WBS ≤3 层）">
              <Select
                allowClear
                options={[
                  { value: 0, label: '（根任务）' },
                  ...tasks.filter((t2) => !editingTask || t2.id !== editingTask.id).map((t2) => ({ value: t2.id, label: t2.name })),
                ]}
              />
            </Form.Item>
            <Form.Item name="assigneeId" label="负责人（须为项目成员）">
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                options={members
                  .filter((m) => m.status === 'active')
                  .map((m) => ({ value: m.userId, label: m.name || m.username }))}
              />
            </Form.Item>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="priority" label="优先级">
              <Select options={Object.entries(PRIORITY_META).map(([value, meta]) => ({ value, label: meta.label }))} />
            </Form.Item>
            <Form.Item name="estimateHours" label="估算工时">
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="planStart" label="计划开始">
              <DatePicker style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="planEnd" label="计划结束">
              <DatePicker style={{ width: '100%' }} />
            </Form.Item>
          </div>
          {editingTask && (
            <div className="grid grid-cols-2 gap-4">
              <Form.Item name="progress" label="进度（0-100）">
                <InputNumber min={0} max={100} style={{ width: '100%' }} />
              </Form.Item>
              {/* PE-22：实际工时由执行者回填（空=未填报）。 */}
              <Form.Item name="actualHours" label="实际工时（回填）">
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </div>
          )}
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加成员"
        open={memberModalOpen}
        onOk={submitMember}
        onCancel={() => setMemberModalOpen(false)}
      >
        <Form form={memberForm} layout="vertical">
          <Form.Item name="userId" label="候选成员（目标租户活跃成员）" rules={[{ required: true, message: '请选择成员' }]}>
            <Select
              showSearch
              filterOption={false}
              loading={candidateLoading}
              placeholder="输入姓名/用户名搜索"
              onSearch={searchCandidates}
              options={candidates.map((c) => ({
                value: c.userId,
                label: `${c.name || c.username}${
                  c.memberOrigin === 'provider'
                    ? '（服务方）'
                    : c.memberOrigin === 'external'
                      ? '（外部）'
                      : ''
                }${c.expiresAt ? ` · ${c.expiresAt.slice(0, 10)} 到期` : ''}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="projectRole" label="项目角色">
            <Select options={Object.entries(ROLE_META).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={editingMilestone ? '编辑里程碑' : '新建里程碑'}
        open={milestoneModalOpen}
        onOk={submitMilestone}
        onCancel={() => {
          setMilestoneModalOpen(false);
          setEditingMilestone(null);
        }}
      >
        <Form form={milestoneForm} layout="vertical">
          <Form.Item name="name" label="里程碑名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="dueDate" label="计划日期" rules={[{ required: true, message: '请选择计划日期' }]}>
            <DatePicker style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="ownerId" label="负责人（须为项目成员）">
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              options={members
                .filter((m) => m.status === 'active')
                .map((m) => ({ value: m.userId, label: m.name || m.username }))}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={depTask ? `任务依赖：${depTask.name}` : '任务依赖'}
        open={depModalOpen}
        onOk={submitDependency}
        okText="添加依赖"
        onCancel={() => {
          setDepModalOpen(false);
          setDepTask(null);
        }}
      >
        <Form form={depForm} layout="vertical">
          <Form.Item
            name="dependsOnTaskId"
            label="前置任务（FS：当前任务在其完成后开始）"
            rules={[{ required: true, message: '请选择前置任务' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择前置任务"
              options={depCandidates.map((t) => ({ value: t.id, label: t.name }))}
            />
          </Form.Item>
        </Form>
        <Table
          rowKey="id"
          size="small"
          pagination={false}
          columns={depColumns}
          dataSource={depRows}
          locale={{ emptyText: '暂无前置任务' }}
        />
      </Modal>

      <Modal title="冻结计划基线" open={freezeModalOpen} onOk={submitFreeze} onCancel={() => setFreezeModalOpen(false)}>
        <p style={{ marginBottom: 12 }}>
          {`基线为不可变快照（任务计划/里程碑/项目计划面）。当前版本数：${baselines.length}；${
            baselines.length > 0 ? '再次冻结须填写计划变更原因。' : '首版无需填写原因。'
          }`}
        </p>
        <Form form={freezeForm} layout="vertical">
          <Form.Item
            name="reason"
            label="计划变更原因"
            rules={baselines.length > 0 ? [{ required: true, message: '后续版本须填写变更原因' }] : []}
          >
            <Input.TextArea rows={3} placeholder={baselines.length > 0 ? '例如：范围/日期调整' : '可选'} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="应用模板（一键生成计划）"
        open={applyModalOpen}
        onOk={submitApplyTemplate}
        confirmLoading={applySubmitting}
        okText="生成计划"
        onCancel={() => {
          setApplyModalOpen(false);
          setApplyTemplateId(undefined);
        }}
      >
        <p style={{ marginBottom: 12 }}>
          将按模板生成阶段/任务/检查项/里程碑；日期锚点 = 项目计划开始日期（未设置时为应用当日）。一个项目只能应用一次模板。
        </p>
        <Select
          style={{ width: '100%' }}
          placeholder="选择已激活的模板"
          value={applyTemplateId}
          onChange={(value) => setApplyTemplateId(value)}
          options={templates.map((t) => ({
            value: t.id,
            label: `${t.name}（阶段 ${t.stageCount} / 任务 ${t.taskCount} / 检查项 ${t.checklistCount} / 里程碑 ${t.milestoneCount}）`,
          }))}
          notFoundContent="暂无已激活模板，请先在「项目模板」页激活"
        />
      </Modal>

      <Modal
        title={gateRejectTarget ? `拒绝 ${GATE_META[gateRejectTarget].label}` : '拒绝阶段门'}
        open={Boolean(gateRejectTarget)}
        onOk={handleRejectGate}
        confirmLoading={gateLoading}
        okText="确认拒绝"
        okButtonProps={{ danger: true }}
        onCancel={() => {
          setGateRejectTarget(null);
          setGateRejectComment('');
        }}
      >
        <p style={{ marginBottom: 8, color: '#8c8c8c' }}>拒绝必须填写明确原因（写入审计），修复阻塞项后可重新提交。</p>
        <Input.TextArea
          rows={3}
          value={gateRejectComment}
          onChange={(e) => setGateRejectComment(e.target.value)}
          placeholder="如：预算未确认 / 交付物未签收"
        />
      </Modal>

      <Modal
        title={assetsAction === 'confirm' ? '确认资产/许可登记' : '豁免资产/许可登记'}
        open={assetsModalOpen}
        onOk={submitClosureAssets}
        confirmLoading={closureActionLoading}
        okText={assetsAction === 'confirm' ? '确认已登记' : '确认豁免'}
        okButtonProps={assetsAction === 'waive' ? { danger: true } : undefined}
        onCancel={() => setAssetsModalOpen(false)}
      >
        {assetsAction === 'confirm' ? (
          <>
            <p style={{ marginBottom: 8, color: '#8c8c8c' }}>
              确认本项目交付产生的资产/许可已登记（可先在「采购/资产登记明细」添加台账链接，再确认留痕；P2/PE-26）。
            </p>
            <Input.TextArea
              rows={3}
              value={assetsNote}
              onChange={(e) => setAssetsNote(e.target.value)}
              placeholder="登记说明（可选）：如 资产台账编号 / 许可清单位置"
            />
          </>
        ) : (
          <>
            <p style={{ marginBottom: 8, color: '#8c8c8c' }}>豁免须填写原因（写入审计与结项清单留痕）。</p>
            <Input.TextArea
              rows={3}
              value={assetsReason}
              onChange={(e) => setAssetsReason(e.target.value)}
              placeholder="如：本项目为咨询服务，无新增资产/许可"
            />
          </>
        )}
      </Modal>

      <Modal
        title="添加台账登记（采购/资产）"
        open={assetLinkModalOpen}
        onOk={submitAssetLink}
        confirmLoading={assetLinkSubmitting}
        okText="登记"
        onCancel={() => {
          setAssetLinkModalOpen(false);
          setAssetLinkTarget(undefined);
          setAssetLinkCandidates([]);
        }}
      >
        <p style={{ marginBottom: 12, color: '#8c8c8c' }}>
          链接既有台账（同租户弱引用）：资产 / 许可证 / 供应商 / 合同；不创建新台账记录，不做采购流程。
        </p>
        <Select
          style={{ width: '100%', marginBottom: 12 }}
          value={assetLinkType}
          onChange={(value: AssetLinkType) => {
            setAssetLinkType(value);
            setAssetLinkTarget(undefined);
            void loadAssetLinkCandidates(value, '');
          }}
          options={ASSET_LINK_TYPE_OPTIONS}
        />
        <Select
          showSearch
          style={{ width: '100%', marginBottom: 12 }}
          placeholder="搜索台账目标（编号/名称/标题）"
          value={assetLinkTarget}
          loading={assetLinkCandidatesLoading}
          filterOption={false}
          onSearch={(keyword) => scheduleAssetLinkCandidates(keyword)}
          onChange={(value: number) => setAssetLinkTarget(value)}
          notFoundContent={assetLinkCandidatesLoading ? '加载中…' : '无匹配台账'}
          options={assetLinkCandidates.map((c) => ({
            value: c.id,
            label: c.sublabel ? `${c.label} · ${c.sublabel}` : c.label,
          }))}
        />
        <Input.TextArea
          rows={2}
          value={assetLinkNote}
          onChange={(e) => setAssetLinkNote(e.target.value)}
          placeholder="备注（可选）：如 数量 / 台账位置"
        />
      </Modal>
    </PageContainer>
  );
}
