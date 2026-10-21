import { httpClient } from '@/lib/api/http-client';

// ---------------------------------------------------------------------------
// 项目管理 P0 API 客户端（权威定义：docs/project-management/plan/p0-api-and-pages.md）
// ---------------------------------------------------------------------------

export type ProjectType = 'internal' | 'improvement' | 'delivery';
export type ProjectStatus = 'planning' | 'active' | 'suspended' | 'completed' | 'cancelled';
export type ProjectHealth = 'green' | 'yellow' | 'red';
/** PE-27 健康度解释项：rule 为命中规则（进度偏差/预算偏差/风险/逾期任务等），count 为明细数。 */
export interface HealthReason {
  rule: string;
  level: ProjectHealth;
  message: string;
  count?: number;
}
export type ProjectRole = 'pm' | 'member' | 'stakeholder';
export type TaskStatus = 'todo' | 'in_progress' | 'blocked' | 'done' | 'cancelled';
export type TaskPriority = 'low' | 'medium' | 'high' | 'critical';
export type RecordDomain = 'ticket' | 'incident' | 'problem' | 'change' | 'service_request';

export interface Project {
  id: number;
  name: string;
  code: string;
  description: string;
  type: ProjectType;
  status: ProjectStatus;
  /** PE-27：事实派生值（读时计算，不可写；写面已移除 health）。 */
  health: ProjectHealth;
  healthReasons?: HealthReason[];
  /** A7：健康度数据完备度；insufficient 表示缺基线/缺归属/无任务，结论仅供参考。 */
  healthDataQuality?: 'sufficient' | 'insufficient';
  /** A7：数据缺口说明（≤4 条）。 */
  healthDataGaps?: string[];
  progress: number;
  managerId?: number;
  /** 展示层人员名称（后端富化；缺失时前端回退 ID，禁止裸 ID 直接展示）。 */
  managerName?: string;
  departmentId?: number;
  /** 展示层部门名称（A2：归口/部门维度可读化）。 */
  departmentName?: string;
  ownerTeamId?: number;
  /** PE-17 归口展示：团队名（团队缺失/软删时为空）+ 降级标记（UI 提示重新归口）。 */
  ownerTeamName?: string;
  ownerTeamDeleted?: boolean;
  parentProjectId?: number;
  startDate?: string;
  endDate?: string;
  actualStart?: string;
  actualEnd?: string;
  closedAt?: string;
  /** P1/PE-11：已应用模板（一项目一模板；非空即不可重复应用）。 */
  templateId?: number;
  templateAppliedAt?: string;
  /** P2/PE-22 轻量预算（未设置时省略；不做财务级核算）。 */
  budgetAmount?: number;
  budgetCurrency?: string;
  version: number;
  tenantId: number;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectListQuery {
  page?: number;
  pageSize?: number;
  status?: string;
  type?: string;
  managerId?: number;
  ownerTeamId?: number;
  q?: string;
}

/** 列表响应：标准契约 items/total + 领域别名 projects（后端 ListResponse）。 */
export interface ProjectListResult {
  items: Project[];
  projects?: Project[];
  total: number;
  page: number;
  pageSize: number;
  totalPages?: number;
}

export interface CreateProjectRequest {
  name: string;
  code?: string;
  description?: string;
  type?: ProjectType;
  managerId?: number;
  departmentId?: number;
  ownerTeamId?: number;
  startDate?: string;
  endDate?: string;
  /** P2/PE-22 轻量预算：金额与币种须成对提供；amount<=0 表示清空。 */
  budgetAmount?: number;
  budgetCurrency?: string;
}

export interface UpdateProjectRequest extends Partial<Omit<CreateProjectRequest, 'code'>> {
  /** PE-27：health 已移出写面（后端派生）；此处不再接受 health。 */
  progress?: number;
  version?: number;
}

export interface Blocker {
  type: string;
  id: number;
  number?: string;
  title?: string;
  status: string;
}

export interface MemberView {
  userId: number;
  name: string;
  username: string;
  projectRole: ProjectRole;
  status: 'active' | 'removed';
  memberOrigin: 'internal' | 'external' | 'provider' | 'platform' | 'unknown';
  expiresAt?: string;
  joinedAt: string;
  /** 成员周容量覆盖（P2/PE-23；null/缺省=继承项目默认，见 p2-resource-heatmap-design.md §3.2）。 */
  weeklyCapacityHours?: number | null;
}

export interface MemberCandidate {
  userId: number;
  name: string;
  username: string;
  /** B6：同名消歧信息（部门/邮箱）。 */
  department?: string;
  email?: string;
  memberOrigin: string;
  expiresAt?: string;
}

/** 归口团队候选（A2：项目「归口」维护入口）。 */
export interface OwnerTeamCandidate {
  id: number;
  name: string;
  code?: string;
}

export interface ProjectTask {
  id: number;
  projectId: number;
  parentTaskId?: number;
  name: string;
  description: string;
  assigneeId?: number;
  status: TaskStatus;
  priority: TaskPriority;
  planStart?: string;
  planEnd?: string;
  actualStart?: string;
  actualEnd?: string;
  progress: number;
  estimateHours?: number;
  actualHours?: number;
  linkedTicketId?: number;
  sortOrder: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  children?: ProjectTask[];
}

export interface RecordItem {
  type: RecordDomain;
  id: number;
  number?: string;
  title: string;
  status: string;
  priority?: string;
  updatedAt: string;
}

export interface RecordListResult {
  items: RecordItem[];
  records?: RecordItem[];
  total: number;
}

/** 单据→项目反向查询视图（PE-5 增补）：project=null 表示未挂接或项目对当前用户不可见。 */
export interface RecordProjectView {
  id: number;
  name: string;
  code: string;
  status: ProjectStatus;
}

export interface RecordProjectResult {
  project: RecordProjectView | null;
}

export interface TaskTicketRequest {
  ticketTypeId?: number;
  title?: string;
  priority?: TaskPriority;
}

// ---- P1（PE-14）：里程碑与任务依赖 ----

export type MilestoneStatus = 'planned' | 'reached' | 'missed';

export interface ProjectMilestone {
  id: number;
  projectId: number;
  name: string;
  description: string;
  dueDate: string;
  status: MilestoneStatus;
  ownerId?: number;
  reachedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface MilestoneCreateRequest {
  name: string;
  description?: string;
  dueDate?: string;
  ownerId?: number;
}

export interface MilestoneUpdateRequest extends Partial<Omit<MilestoneCreateRequest, 'name'>> {
  name?: string;
}

/** 我的项目任务（跨可见项目；含项目名，供个人工作台聚合；PE-16）。 */
export interface MyProjectTask extends ProjectTask {
  projectName: string;
}

/** 我的里程碑到期（含项目名/编号与逾期相对天数；PE-16）。 */
export interface MyMilestone extends ProjectMilestone {
  projectName: string;
  projectCode?: string;
  overdue: boolean;
  daysLeft: number;
}

/** 任务依赖（v1 仅 FS：前置任务完成后当前任务才可开始）。 */
export interface TaskDependency {
  id: number;
  taskId: number;
  dependsOnTaskId: number;
  depType: string;
  createdAt: string;
}

// ---- P1（PE-13）：计划基线 ----

export interface BaselineTaskSnapshot {
  id: number;
  name: string;
  status: string;
  priority: string;
  assigneeId?: number;
  planStart?: string;
  planEnd?: string;
  progress: number;
}

export interface BaselineMilestoneSnapshot {
  id: number;
  name: string;
  status: string;
  dueDate: string;
  ownerId?: number;
  reachedAt?: string;
}

export interface BaselineSnapshot {
  schemaVersion: number;
  frozenAt: string;
  project: {
    code: string;
    name: string;
    status: string;
    progress: number;
    startDate?: string;
    endDate?: string;
    /** P2/PE-22（快照 v2）：冻结预算；v1 旧快照缺省。 */
    budgetAmount?: number;
    budgetCurrency?: string;
  };
  tasks: BaselineTaskSnapshot[];
  milestones: BaselineMilestoneSnapshot[];
}

export interface ProjectBaseline {
  id: number;
  projectId: number;
  version: number;
  reason?: string;
  createdBy?: number;
  createdAt: string;
  snapshot?: BaselineSnapshot;
}

export interface BaselineDiffSummary {
  added: number;
  removed: number;
  changed: number;
  unchanged: number;
}

/** 任务偏差（基线 vs 当前）。changeType: added|removed|changed|unchanged。 */
export interface BaselineTaskDeviation {
  taskId: number;
  name: string;
  changeType: 'added' | 'removed' | 'changed' | 'unchanged';
  baselineStatus?: string;
  currentStatus?: string;
  statusChanged: boolean;
  progressDelta: number;
  planEndShiftDays?: number;
}

/** 里程碑偏差（基线 vs 当前）。 */
export interface BaselineMilestoneDeviation {
  milestoneId: number;
  name: string;
  changeType: 'added' | 'removed' | 'changed' | 'unchanged';
  baselineStatus?: string;
  currentStatus?: string;
  statusChanged: boolean;
  dueDateShiftDays?: number;
}

export interface BaselineDiff {
  baselineId: number;
  version: number;
  frozenAt: string;
  createdAt: string;
  taskSummary: BaselineDiffSummary;
  milestoneSummary: BaselineDiffSummary;
  taskChanges: BaselineTaskDeviation[];
  milestoneChanges: BaselineMilestoneDeviation[];
  /** P2/PE-22：项目级预算偏差（v1 旧快照 frozen=false，不参与变更判定）。 */
  budget?: BaselineBudgetDeviation;
}

/** 项目级预算偏差（基线 vs 当前；PE-22/D-34）。 */
export interface BaselineBudgetDeviation {
  frozen: boolean;
  baseAmount?: number;
  baseCurrency?: string;
  currentAmount?: number;
  currentCurrency?: string;
  changed: boolean;
}

// ---- 阶段门（P1/PE-12）----

/** 四道阶段门固定顺序（与后端 GateOrder 一致）。 */
export const GATE_ORDER = ['initiation', 'plan', 'acceptance', 'closure'] as const;

export type ProjectGateKey = (typeof GATE_ORDER)[number];

export type ProjectGateStatus = 'pending' | 'in_review' | 'passed' | 'rejected';

/** 门禁阻塞项（type 见后端 GateBlocker；total > 0 表示明细截断时的总数）。 */
export interface ProjectGateBlocker {
  type: string;
  detail?: string;
  refId?: number;
  number?: string;
  status?: string;
  total?: number;
}

export interface ProjectGate {
  id: number;
  projectId: number;
  gate: ProjectGateKey;
  status: ProjectGateStatus;
  processInstanceId: number;
  approvalChainId?: string;
  /** 审批方式（A4）：manual=人工审批；auto=未配置流程自动放行；pending=尚未提交。 */
  approvalMode?: 'manual' | 'auto' | 'pending';
  /** 未配置审批流程的自动放行（B12：不计入一次通过率）。 */
  autoApproved?: boolean;
  submittedBy: number;
  submittedAt?: string;
  decidedBy: number;
  decidedAt?: string;
  decisionComment?: string;
  blockers: ProjectGateBlocker[];
  version: number;
  createdAt: string;
  updatedAt: string;
}

/** 阶段门实时预检（只读；后端与提交共用同一套规则）。 */
export interface ProjectGateReadiness {
  gate: ProjectGateKey;
  ready: boolean;
  blockers: ProjectGateBlocker[];
  /** 提交后的审批方式预判（A4）。 */
  approvalMode?: 'manual' | 'auto';
  /** 自动放行且未配置审批流程（UI 需显式告警与配置引导）。 */
  autoApprovalConfigMissing?: boolean;
}

export interface GateListResult {
  items: ProjectGate[];
  gates: string[];
  readiness: ProjectGateReadiness[];
}

// ---- P1（PE-15）：结项清单 ----

/** 清单项顺序（自动项 → 人工/产物项 → 非阻塞 CSAT）。 */
export const CLOSURE_ITEM_ORDER = ['tasks', 'records', 'gate', 'retro', 'assets', 'members', 'csat'] as const;

export type ClosureItemKey = (typeof CLOSURE_ITEM_ORDER)[number];

export type ClosureItemStatus = 'done' | 'pending';

export type ProjectClosureStatus = 'pending' | 'ready' | 'completed' | 'cancelled';

export type ClosureActionKey = 'generate_retro' | 'confirm_assets' | 'recycle_members' | 'trigger_csat';

export interface ClosureItem {
  key: ClosureItemKey | string;
  title: string;
  status: ClosureItemStatus;
  /** 是否阻塞结项（CSAT 为非阻塞项）。 */
  blocking: boolean;
  detail?: string;
  action?: ClosureActionKey | string;
}

// ---- 采购/资产结项登记（P2/PE-26）----

/** 登记类型词表（与后端 D-49 一致）。 */
export type AssetLinkType = 'asset' | 'asset_license' | 'vendor' | 'contract';

/** 登记行（结项清单 assetsLinks 与登记列表共用形状）。 */
export interface AssetLinkItem {
  id: number;
  linkType: AssetLinkType | string;
  targetId: number;
  /** 读时富化标签；targetMissing=true 时为空。 */
  label: string;
  sublabel?: string;
  note?: string;
  /** 目标已删除/不存在（登记仍保留）。 */
  targetMissing: boolean;
  createdBy: number;
  createdAt: string;
}

/** 登记列表响应（新增/删除后与列表同构，一次刷新）。 */
export interface AssetLinkList {
  items: AssetLinkItem[];
  summary: Record<AssetLinkType, number>;
  total: number;
  /** 超出硬上限（200）截断标记。 */
  truncated: boolean;
}

/** 候选行（最小字段）。 */
export interface AssetLinkCandidate {
  id: number;
  label: string;
  sublabel?: string;
}

/** 候选列表响应。 */
export interface AssetLinkCandidateList {
  items: AssetLinkCandidate[];
  truncated: boolean;
}

export interface ProjectClosure {
  id: number;
  projectId: number;
  status: ProjectClosureStatus;
  items: ClosureItem[];
  blockers: Blocker[];
  /** A8：阻塞项总数（blockers 明细超过 10 条时被截断，计数以本字段为准）。 */
  blockerTotal?: number;
  /** 复盘 KB 草稿文章ID（0 = 未生成；草稿不自动发布）。 */
  retroKbArticleId: number;
  retroGeneratedAt?: string | null;
  assetsNote?: string;
  assetsWaived: boolean;
  assetsWaiveReason?: string;
  assetsConfirmedAt?: string | null;
  /** 采购/资产结项登记明细（PE-26；读时富化，上限 200）。 */
  assetsLinks: AssetLinkItem[];
  /** 按类型计数（恒含四键，缺省 0）。 */
  assetsLinkSummary: Record<AssetLinkType, number>;
  /** 登记明细超出上限（200）时为 true。 */
  assetsLinksTruncated: boolean;
  membersRecycledAt?: string | null;
  /** 最近一次成员回收数量。 */
  membersRecycledCount: number;
  /** CSAT 调研ID（0 = 未触发）。 */
  csatSurveyId: number;
  csatTriggeredAt?: string | null;
  version: number;
  updatedAt: string;
}

// ---- 项目模板（P1/PE-11）----

export type TemplateStatus = 'draft' | 'active' | 'archived';

/** 模板里程碑定义（offsetDays 相对锚点 = 项目开始日期）。 */
export interface TemplateMilestoneSpec {
  name: string;
  offsetDays: number;
}

/** 模板任务定义（stage 相同归入同一阶段 L1；checklist → L3 检查项子任务）。 */
export interface TemplateTaskSpec {
  stage?: string;
  name: string;
  description?: string;
  priority?: TaskPriority;
  startOffsetDays?: number;
  durationDays?: number;
  estimateHours?: number;
  checklist?: string[];
}

export interface TemplatePayload {
  milestones?: TemplateMilestoneSpec[];
  tasks?: TemplateTaskSpec[];
}

export interface ProjectTemplate {
  id: number;
  name: string;
  description: string;
  type: ProjectType;
  status: TemplateStatus;
  /** 列表响应省略定义体（仅计数）；详情响应带回。 */
  payload?: TemplatePayload;
  stageCount: number;
  taskCount: number;
  checklistCount: number;
  milestoneCount: number;
  version: number;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
}

export interface TemplateListQuery {
  page?: number;
  pageSize?: number;
  status?: TemplateStatus | '';
  type?: ProjectType | '';
  q?: string;
}

/** 列表响应：标准契约 items/total + 领域别名 projectTemplates（后端 ListResponse）。 */
export interface TemplateListResult {
  items: ProjectTemplate[];
  projectTemplates?: ProjectTemplate[];
  total: number;
  page: number;
  pageSize: number;
  totalPages?: number;
}

export interface TemplateCreateRequest {
  name: string;
  description?: string;
  type?: ProjectType;
  status?: TemplateStatus;
  payload: TemplatePayload;
}

export interface TemplateUpdateRequest {
  name?: string;
  description?: string;
  type?: ProjectType;
  status?: TemplateStatus;
  payload?: TemplatePayload;
  version?: number;
}

/** 一键生成结果（计数 + 锚点）。 */
export interface TemplateApplyResult {
  projectId: number;
  templateId: number;
  templateName: string;
  appliedAt: string;
  anchorDate: string;
  stages: number;
  tasks: number;
  checklists: number;
  milestones: number;
}

// ---- 状态报告（PE-17）----

export type ReportTemplateStatus = 'draft' | 'active' | 'archived';
export type ReportCadence = 'weekly' | 'biweekly' | 'monthly';
export type ReportSection = 'overview' | 'tasks' | 'milestones' | 'gates' | 'variance' | 'team';

export interface ReportTemplate {
  id: number;
  name: string;
  description: string;
  cadence: ReportCadence;
  sections: ReportSection[];
  status: ReportTemplateStatus;
  version: number;
  tenantId: number;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
}

export interface ReportTemplateListQuery {
  status?: ReportTemplateStatus | '';
  q?: string;
  page?: number;
  pageSize?: number;
}

export interface ReportTemplateListResult {
  items: ReportTemplate[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ReportTemplateCreateRequest {
  name: string;
  description?: string;
  cadence?: ReportCadence;
  sections?: ReportSection[];
  status?: ReportTemplateStatus;
}

export interface ReportTemplateUpdateRequest {
  name?: string;
  description?: string;
  cadence?: ReportCadence;
  sections?: ReportSection[];
  status?: ReportTemplateStatus;
  version?: number;
}

export interface ReportTaskStats {
  total: number;
  done: number;
  open: number;
  cancelled: number;
  overdue: number;
  dueSoon: number;
}

export interface ReportMilestoneStats {
  total: number;
  reached: number;
  planned: number;
  missed: number;
  overdue: number;
  onTime: number;
  rate: number;
  overdueRate: number;
}

export interface ReportGateItem {
  gate: ProjectGateKey;
  status: ProjectGateStatus;
  updatedAt?: string;
}

export interface ReportVarianceSummary {
  baselineVersion: number;
  baselineCreatedAt?: string;
  added: number;
  removed: number;
  changed: number;
  unchanged: number;
}

export interface ProjectReportData {
  projectId: number;
  projectName: string;
  projectCode: string;
  status: ProjectStatus;
  health: ProjectHealth;
  /** PE-27：报告 data 同步携带派生解释项（与列表同源）。 */
  healthReasons?: HealthReason[];
  /** A7：报告数据完备度与缺口（与列表同源）。 */
  healthDataQuality?: 'sufficient' | 'insufficient';
  healthDataGaps?: string[];
  progress: number;
  managerId?: number;
  /** B3：报告人员展示名（后端富化；缺失回退 ID 前缀）。 */
  managerName?: string;
  /** A2：报告部门展示名（归口/部门维度可读化）。 */
  departmentName?: string;
  departmentId?: number;
  ownerTeam?: { id: number; name: string; deleted: boolean };
  generatedAt: string;
  template?: { id: number; name: string; cadence: ReportCadence };
  sections: ReportSection[];
  taskStats: ReportTaskStats;
  milestoneStats: ReportMilestoneStats;
  gates: ReportGateItem[];
  variance?: ReportVarianceSummary;
}

export interface ProjectReport {
  markdown: string;
  data: ProjectReportData;
}

export interface ReportGroup {
  ownerTeamId?: number;
  ownerTeamName: string;
  ownerTeamDeleted: boolean;
  /** A2（回归遗留）：部门维度与归口并列输出（未归口时仍可按部门归因）。 */
  departmentId?: number;
  departmentName?: string;
  projectCount: number;
  greenCount: number;
  yellowCount: number;
  redCount: number;
}

const BASE_URL = '/api/v1/projects';

const DOMAIN_PATH: Record<RecordDomain, string> = {
  ticket: 'tickets',
  incident: 'incidents',
  problem: 'problems',
  change: 'changes',
  service_request: 'service-requests',
};

// ---- P2（PE-21）：风险/问题台账 ----

export type RiskKind = 'risk' | 'issue';
export type RiskLevel = 'low' | 'medium' | 'high';
export type RiskStatus = 'open' | 'mitigating' | 'closed';
export type RiskConvertTarget = 'problem' | 'change';

export interface ProjectRisk {
  id: number;
  projectId: number;
  kind: RiskKind;
  title: string;
  description: string;
  level: RiskLevel;
  probability?: RiskLevel;
  impact?: RiskLevel;
  ownerId?: number;
  ownerName?: string;
  status: RiskStatus;
  resolution?: string;
  relatedTicketId?: number;
  relatedProblemId?: number;
  relatedChangeId?: number;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface RiskCreateRequest {
  kind?: RiskKind;
  title: string;
  description?: string;
  level?: RiskLevel;
  probability?: RiskLevel;
  impact?: RiskLevel;
  ownerId?: number;
  relatedTicketId?: number;
}

/** 更新请求（字段缺省 = 不修改；ownerId/relatedTicketId 传 0 清空）。 */
export interface RiskUpdateRequest {
  kind?: RiskKind;
  title?: string;
  description?: string;
  level?: RiskLevel;
  probability?: RiskLevel;
  impact?: RiskLevel;
  ownerId?: number;
  relatedTicketId?: number;
  version?: number;
}

/** 转换响应：产物ID + 更新后的风险（同目标二次转换 409，data.existingId 给出既有产物）。 */
export interface RiskConvertResult {
  target: RiskConvertTarget;
  createdId: number;
  risk: ProjectRisk;
}

// ---- P2/PE-23：资源负荷（周容量 + 只读热图） ----

/** 日历口径标注（v1 自然日 + 明确标注；B7）。 */
export interface HeatmapCalendar {
  mode: 'natural_days';
  businessDaysOnly: boolean;
  note: string;
}

/** 容量默认解析结果（source=project 已配置 | fallback 内置 40h）。 */
export interface HeatmapDefaults {
  defaultWeeklyCapacityHours: number;
  source: 'project' | 'fallback';
}

export interface HeatmapRange {
  start: string;
  end: string;
}

/** 未排期提示（有估算但无计划日期，无法摊分）。 */
export interface HeatmapUnplanned {
  taskCount: number;
  estimateHours: number;
}

/** 单元格：某成员某周的计划/剩余负荷 vs 容量。 */
export interface HeatmapMemberWeek {
  userId: number;
  name: string;
  username: string;
  projectRole: ProjectRole;
  memberStatus: 'active' | 'removed';
  capacityHours: number;
  plannedHours: number;
  remainingHours: number;
  plannedRatio: number;
  remainingRatio: number;
}

export interface HeatmapWeek {
  weekStart: string;
  weekEnd: string;
  members: HeatmapMemberWeek[];
}

export interface ResourceHeatmap {
  projectId: number;
  range: HeatmapRange;
  calendar: HeatmapCalendar;
  defaults: HeatmapDefaults;
  weeks: HeatmapWeek[];
  unplanned: HeatmapUnplanned;
}

/** 成员容量项（weeklyCapacityHours 缺省=不修改；null=清空回继承）。 */
export interface CapacityMemberInput {
  userId: number;
  weeklyCapacityHours?: number | null;
}

/** 容量配置请求（字段缺省=不修改；null=清空：项目默认回内置 40，成员回继承）。 */
export interface CapacityUpdateRequest {
  defaultWeeklyCapacityHours?: number | null;
  members?: CapacityMemberInput[];
}

export interface CapacityMemberView {
  userId: number;
  weeklyCapacityHours: number | null;
}

/** 容量配置响应（members=存活成员全量）。 */
export interface CapacityResponse {
  defaultWeeklyCapacityHours: number | null;
  members: CapacityMemberView[];
}

// ---- P2/PE-24：组合视图与度量 ----

/** 组合/度量共用的项目过滤（与 GET /projects 同参数，口径同源）。 */
export interface PortfolioQuery {
  status?: string;
  type?: string;
  managerId?: number;
  ownerTeamId?: number;
  q?: string;
}

/** 度量查询（from/to 口径日期；start/weeks 资源窗口；项目过滤同列表）。 */
export interface MetricsQuery extends PortfolioQuery {
  from?: string;
  to?: string;
  start?: string;
  weeks?: number;
}

/** 聚合桶（key 为维度值；management 桶带姓名，department 桶名称由前端映射）。 */
export interface PortfolioBucket {
  key: string;
  count: number;
  name?: string;
}

export interface PortfolioSummary {
  total: number;
  byStatus: PortfolioBucket[];
  byHealth: PortfolioBucket[];
  byType: PortfolioBucket[];
  byManager: PortfolioBucket[];
  byDepartment: PortfolioBucket[];
}

/** 组合视图响应（projects 行与列表同结构同序，可直接对账）。 */
export interface PortfolioResponse {
  generatedAt: string;
  truncated: boolean;
  summary: PortfolioSummary;
  projects: Project[];
}

export interface DeliveryDetail {
  projectId: number;
  code?: string;
  name: string;
  planEnd: string;
  actualEnd: string;
  onTime: boolean;
}

/** 按时交付率（rate=null 表示分母为 0；noPlanDate 单列不掩盖）。 */
export interface DeliveryMetrics {
  denominator: number;
  numerator: number;
  rate: number | null;
  noPlanDate: number;
  detailsTotal: number;
  details: DeliveryDetail[];
}

export interface MilestoneDetail {
  projectId: number;
  milestoneId: number;
  name: string;
  status: 'planned' | 'reached' | 'missed';
  dueDate: string;
  reachedAt?: string;
  achieved: boolean;
  onTime: boolean;
  dateUsed: string;
}

/** 里程碑达成率（judged=已判定；onTimeRate 以 reached 为分母）。 */
export interface MilestoneMetrics {
  judged: number;
  reached: number;
  missed: number;
  onTimeReached: number;
  rate: number | null;
  onTimeRate: number | null;
  detailsTotal: number;
  details: MilestoneDetail[];
}

export interface GateDetail {
  projectId: number;
  gate: ProjectGateKey;
  status: ProjectGateStatus;
  rejectCount: number;
  firstPass: boolean;
  /** 自动放行（B12：剔除出一次通过率）。 */
  autoApproved?: boolean;
  decidedAt?: string;
}

/** 阶段门一次通过率（人工审批 passed 且 rejectCount=0 记为一次通过；自动放行单独计数）。 */
export interface GateMetrics {
  passed: number;
  firstPass: number;
  autoApproved?: number;
  rate: number | null;
  detailsTotal: number;
  details: GateDetail[];
}

export interface ResourceUnplanned {
  taskCount: number;
  estimateHours: number;
}

export interface ResourceWindow {
  start: string;
  end: string;
  weeks: number;
}

export interface ResourceProjectRow {
  projectId: number;
  code?: string;
  name: string;
  plannedHours: number;
  capacityHours: number;
  rate: number | null;
}

/** 资源利用率（复用 PE-23 热图口径：planned/capacity；rate=null 表示容量为 0）。 */
export interface ResourceUtilization {
  plannedHours: number;
  capacityHours: number;
  rate: number | null;
  unplanned: ResourceUnplanned;
  calendar: HeatmapCalendar;
  window: ResourceWindow;
  projects: ResourceProjectRow[];
}

/** 组合度量响应（四项指标 + 明细，支撑抽样对账）。 */
export interface MetricsResponse {
  generatedAt: string;
  projectScope: {
    total: number;
    truncated: boolean;
    filters: { status?: string; type?: string; managerId?: number; ownerTeamId?: number; q?: string };
  };
  window: { from?: string; to?: string };
  onTimeDelivery: DeliveryMetrics;
  milestoneAchievement: MilestoneMetrics;
  gateFirstPass: GateMetrics;
  resourceUtilization: ResourceUtilization;
}

// ---- P2/PE-22：预算与工时 ----

/** 任务工时汇总（口径：项目全部未删任务；不做状态过滤）。 */
export interface HoursSummary {
  taskCount: number;
  estimatedTaskCount: number;
  actualTaskCount: number;
  estimateHours: number;
  actualHours: number;
}

// ---- P2/PE-25：项目 AI（状态摘要 / 风险预警 / 纪要转任务） ----

/** 摘要周期（与报告模板 cadence 同词表）。 */
export type AIReportCadence = 'weekly' | 'biweekly' | 'monthly';

/** 结果来源：ai=LLM；deterministic=事实派生降级；rules=规则信号降级；none=纯 AI 能力无结果。 */
export type AIGeneratedBy = 'ai' | 'deterministic' | 'rules' | 'none';

/** 降级原因（受控词表；空串/缺省=未降级）。 */
export type AIDegradeReason = '' | 'ai_unavailable' | 'llm_error' | 'parse_error';

export type AISeverity = 'info' | 'warning' | 'critical';

/** AI 状态摘要响应（降级时 summary 为事实派生文本、generatedBy=deterministic）。 */
export interface AISummaryResponse {
  projectId: number;
  summary: string;
  cadence: AIReportCadence;
  generatedBy: AIGeneratedBy;
  degraded: boolean;
  degradeReason?: AIDegradeReason;
  model?: string;
  promptChars: number;
  generatedAt: string;
}

/** 规则风险信号（确定性派生；AI 不可用时同样返回）。 */
export interface AIRiskSignal {
  type: string;
  severity: AISeverity;
  refType?: string;
  refId?: number;
  title: string;
  detail: string;
}

/** AI 判断的风险预警条目。 */
export interface AIRiskWarning {
  severity: AISeverity;
  title: string;
  detail: string;
  suggestion?: string;
}

export interface AIRiskWarningsResponse {
  projectId: number;
  signals: AIRiskSignal[];
  signalTotal: number;
  truncated: boolean;
  warnings: AIRiskWarning[];
  generatedBy: AIGeneratedBy;
  degraded: boolean;
  degradeReason?: AIDegradeReason;
  model?: string;
  generatedAt: string;
}

/** 纪要抽取的任务建议（不落库；用户勾选后走 createTask）。 */
export interface AITaskProposal {
  name: string;
  description?: string;
  priority?: string;
  estimateHours?: number;
  ownerHint?: string;
}

export interface AIMinutesTasksResponse {
  projectId: number;
  proposals: AITaskProposal[];
  generatedBy: AIGeneratedBy;
  degraded: boolean;
  degradeReason?: AIDegradeReason;
  model?: string;
  contentChars: number;
  generatedAt: string;
}

class ProjectService {
  // ---- 项目 ----

  listProjects(params: ProjectListQuery = {}): Promise<ProjectListResult> {
    return httpClient.get<ProjectListResult>(BASE_URL, params);
  }

  getProject(id: number): Promise<Project> {
    return httpClient.get<Project>(`${BASE_URL}/${id}`);
  }

  /** 项目任务工时汇总（估算/实际合计 + 已填报任务数；PE-22）。 */
  hoursSummary(projectId: number): Promise<HoursSummary> {
    return httpClient.get<HoursSummary>(`${BASE_URL}/${projectId}/hours-summary`);
  }

  /**
   * 归口团队候选（A2 回归遗留：项目「归口」维护入口的取数来源）。
   * 只返回同租户、未删除且 active 的团队；创建/编辑项目共用。
   */
  ownerTeamCandidates(params: { q?: string; limit?: number } = {}): Promise<OwnerTeamCandidate[]> {
    return httpClient.get<OwnerTeamCandidate[]>(`${BASE_URL}/owner-team-candidates`, params);
  }

  createProject(data: CreateProjectRequest): Promise<Project> {
    return httpClient.post<Project>(BASE_URL, data);
  }

  updateProject(id: number, data: UpdateProjectRequest): Promise<Project> {
    return httpClient.put<Project>(`${BASE_URL}/${id}`, data);
  }

  deleteProject(id: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${id}`);
  }

  /** 状态流转：activate|suspend|resume|complete|cancel（complete 门禁阻塞时返回 409+blockers）。 */
  transitionProject(id: number, action: string, reason?: string, version?: number): Promise<Project> {
    return httpClient.post<Project>(`${BASE_URL}/${id}/status`, { action, reason, version });
  }

  // ---- 成员 ----

  listMembers(projectId: number): Promise<MemberView[]> {
    return httpClient.get<MemberView[]>(`${BASE_URL}/${projectId}/members`);
  }

  addMember(projectId: number, userId: number, projectRole: ProjectRole = 'member'): Promise<MemberView> {
    return httpClient.post<MemberView>(`${BASE_URL}/${projectId}/members`, { userId, projectRole });
  }

  updateMemberRole(projectId: number, userId: number, projectRole: ProjectRole): Promise<MemberView> {
    return httpClient.patch<MemberView>(`${BASE_URL}/${projectId}/members/${userId}`, { projectRole });
  }

  removeMember(projectId: number, userId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${projectId}/members/${userId}`);
  }

  memberCandidates(projectId: number, q?: string): Promise<MemberCandidate[]> {
    return httpClient.get<MemberCandidate[]>(`${BASE_URL}/${projectId}/member-candidates`, { q });
  }

  // ---- 任务 ----

  listTasks(projectId: number, params: { view?: 'tree' | 'flat'; status?: string; assigneeId?: number } = {}): Promise<ProjectTask[]> {
    return httpClient.get<ProjectTask[]>(`${BASE_URL}/${projectId}/tasks`, params);
  }

  createTask(projectId: number, data: Partial<ProjectTask> & { name: string }): Promise<ProjectTask> {
    return httpClient.post<ProjectTask>(`${BASE_URL}/${projectId}/tasks`, data);
  }

  updateTask(projectId: number, taskId: number, data: Partial<ProjectTask>): Promise<ProjectTask> {
    return httpClient.put<ProjectTask>(`${BASE_URL}/${projectId}/tasks/${taskId}`, data);
  }

  deleteTask(projectId: number, taskId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${projectId}/tasks/${taskId}`);
  }

  /** 任务状态流转：start|block|unblock|complete|cancel。 */
  transitionTask(projectId: number, taskId: number, action: string, version?: number): Promise<ProjectTask> {
    return httpClient.post<ProjectTask>(`${BASE_URL}/${projectId}/tasks/${taskId}/status`, { action, version });
  }

  generateTaskTicket(projectId: number, taskId: number, data: TaskTicketRequest = {}): Promise<ProjectTask> {
    return httpClient.post<ProjectTask>(`${BASE_URL}/${projectId}/tasks/${taskId}/tickets`, data);
  }

  unbindTaskTicket(projectId: number, taskId: number): Promise<ProjectTask> {
    return httpClient.delete<ProjectTask>(`${BASE_URL}/${projectId}/tasks/${taskId}/tickets`);
  }

  myTasks(params: { status?: string; limit?: number } = {}): Promise<MyProjectTask[]> {
    return httpClient.get<MyProjectTask[]>(`${BASE_URL}/my-tasks`, params);
  }

  /** 我的里程碑到期（跨可见项目；仅 planned；默认 30 天内含逾期；PE-16）。 */
  myMilestones(params: { dueWithinDays?: number; limit?: number } = {}): Promise<MyMilestone[]> {
    return httpClient.get<MyMilestone[]>(`${BASE_URL}/my-milestones`, params);
  }

  // ---- 里程碑（P1/PE-14） ----

  listMilestones(projectId: number, params: { status?: MilestoneStatus } = {}): Promise<ProjectMilestone[]> {
    return httpClient.get<ProjectMilestone[]>(`${BASE_URL}/${projectId}/milestones`, params);
  }

  createMilestone(projectId: number, data: MilestoneCreateRequest): Promise<ProjectMilestone> {
    return httpClient.post<ProjectMilestone>(`${BASE_URL}/${projectId}/milestones`, data);
  }

  updateMilestone(projectId: number, milestoneId: number, data: MilestoneUpdateRequest): Promise<ProjectMilestone> {
    return httpClient.put<ProjectMilestone>(`${BASE_URL}/${projectId}/milestones/${milestoneId}`, data);
  }

  /** 里程碑状态流转：reach（planned|missed → reached）| miss（planned → missed）。 */
  transitionMilestone(projectId: number, milestoneId: number, action: 'reach' | 'miss'): Promise<ProjectMilestone> {
    return httpClient.post<ProjectMilestone>(`${BASE_URL}/${projectId}/milestones/${milestoneId}/status`, { action });
  }

  deleteMilestone(projectId: number, milestoneId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${projectId}/milestones/${milestoneId}`);
  }

  // ---- 任务依赖（P1/PE-14，v1 仅 FS） ----

  listDependencies(projectId: number): Promise<TaskDependency[]> {
    return httpClient.get<TaskDependency[]>(`${BASE_URL}/${projectId}/dependencies`);
  }

  addDependency(projectId: number, taskId: number, dependsOnTaskId: number, depType = 'FS'): Promise<TaskDependency> {
    return httpClient.post<TaskDependency>(`${BASE_URL}/${projectId}/tasks/${taskId}/dependencies`, {
      dependsOnTaskId,
      depType,
    });
  }

  removeDependency(projectId: number, taskId: number, dependsOnTaskId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${projectId}/tasks/${taskId}/dependencies/${dependsOnTaskId}`);
  }

  // ---- 计划基线（P1/PE-13） ----

  listBaselines(projectId: number): Promise<ProjectBaseline[]> {
    return httpClient.get<ProjectBaseline[]>(`${BASE_URL}/${projectId}/baselines`);
  }

  /** 冻结基线快照（不可变）。后续版本（≥2）须提供 reason（计划变更原因）。 */
  createBaseline(projectId: number, reason?: string): Promise<ProjectBaseline> {
    return httpClient.post<ProjectBaseline>(`${BASE_URL}/${projectId}/baselines`, { reason });
  }

  /** 最新基线 vs 当前实时状态的偏差（无基线时 404）。 */
  baselineDiff(projectId: number): Promise<BaselineDiff> {
    return httpClient.get<BaselineDiff>(`${BASE_URL}/${projectId}/baselines/diff`);
  }

  // ---- 阶段门（P1/PE-12） ----

  /** 阶段门列表（四道门状态 + 阻塞明细 + 门顺序）。 */
  listGates(projectId: number): Promise<GateListResult> {
    return httpClient
      .get<{
        items?: ProjectGate[];
        gates?: string[];
        projectGates?: ProjectGate[];
        readiness?: ProjectGateReadiness[];
      }>(`${BASE_URL}/${projectId}/gates`)
      .then((data) => ({
        items: data.items ?? data.projectGates ?? [],
        gates: data.gates ?? [...GATE_ORDER],
        readiness: data.readiness ?? [],
      }));
  }

  /**
   * 提交阶段门：先跑门禁规则（不通过 → 409 + blockers），
   * 通过后触发 BPMN 审批（in_review）；租户未配置流程时降级自动通过（passed）。
   */
  submitGate(projectId: number, gate: ProjectGateKey): Promise<ProjectGate> {
    return httpClient.post<ProjectGate>(`${BASE_URL}/${projectId}/gates/${gate}/submit`, {});
  }

  /** 裁决阶段门：approve 在流程走完（或无流程）后放行；reject 必须带原因（400 语义）。 */
  decideGate(projectId: number, gate: ProjectGateKey, decision: 'approve' | 'reject', comment?: string): Promise<ProjectGate> {
    return httpClient.post<ProjectGate>(`${BASE_URL}/${projectId}/gates/${gate}/decision`, { decision, comment });
  }

  // ---- 结项清单（P1/PE-15） ----

  /** 结项清单：自动项（任务/单据/结项门）实时派生 + 人工项（复盘/资产登记/成员回收/CSAT）。 */
  getClosure(projectId: number): Promise<ProjectClosure> {
    return httpClient.get<ProjectClosure>(`${BASE_URL}/${projectId}/closure`);
  }

  /** 生成复盘 KB 草稿（幂等；草稿态不自动发布）。 */
  generateRetroDraft(projectId: number): Promise<ProjectClosure> {
    return httpClient.post<ProjectClosure>(`${BASE_URL}/${projectId}/closure/retro`, {});
  }

  /** 资产/许可登记确认（confirm）或豁免（waive，必填原因）。 */
  confirmClosureAssets(
    projectId: number,
    action: 'confirm' | 'waive',
    payload: { note?: string; reason?: string } = {}
  ): Promise<ProjectClosure> {
    return httpClient.post<ProjectClosure>(`${BASE_URL}/${projectId}/closure/assets`, { action, ...payload });
  }

  /** 采购/资产结项登记列表（PE-26；可见性同详情）。 */
  listAssetLinks(projectId: number): Promise<AssetLinkList> {
    return httpClient.get<AssetLinkList>(`${BASE_URL}/${projectId}/asset-links`);
  }

  /** 新增登记（同租户弱引用；重复 409 / 目标无效 400）。 */
  createAssetLink(
    projectId: number,
    payload: { linkType: AssetLinkType; targetId: number; note?: string }
  ): Promise<AssetLinkList> {
    return httpClient.post<AssetLinkList>(`${BASE_URL}/${projectId}/asset-links`, payload);
  }

  /** 删除登记（软删；已删/不存在 404）。 */
  deleteAssetLink(projectId: number, linkId: number): Promise<AssetLinkList> {
    return httpClient.delete<AssetLinkList>(`${BASE_URL}/${projectId}/asset-links/${linkId}`);
  }

  /** 候选选择器（type 四词表；q 关键词；limit 1..50）。 */
  listAssetLinkCandidates(
    projectId: number,
    linkType: AssetLinkType,
    params: { q?: string; limit?: number } = {}
  ): Promise<AssetLinkCandidateList> {
    const query = new URLSearchParams({ type: linkType });
    if (params.q) query.set('q', params.q);
    if (params.limit) query.set('limit', String(params.limit));
    return httpClient.get<AssetLinkCandidateList>(
      `${BASE_URL}/${projectId}/asset-link-candidates?${query.toString()}`
    );
  }

  /** 成员回收：回收 member 角色活跃成员（保留 PM 与干系人）；可重复执行。 */
  recycleClosureMembers(projectId: number): Promise<ProjectClosure> {
    return httpClient.post<ProjectClosure>(`${BASE_URL}/${projectId}/closure/members/recycle`, {});
  }

  /** 触发结项 CSAT 调研（幂等；结项时自动触发，失败可手动重试）。 */
  triggerClosureCSAT(projectId: number): Promise<ProjectClosure> {
    return httpClient.post<ProjectClosure>(`${BASE_URL}/${projectId}/closure/csat`, {});
  }

  // ---- 项目模板（P1/PE-11） ----

  listTemplates(params: TemplateListQuery = {}): Promise<TemplateListResult> {
    return httpClient.get<TemplateListResult>(`${BASE_URL}/templates`, params);
  }

  getTemplate(templateId: number): Promise<ProjectTemplate> {
    return httpClient.get<ProjectTemplate>(`${BASE_URL}/templates/${templateId}`);
  }

  createTemplate(data: TemplateCreateRequest): Promise<ProjectTemplate> {
    return httpClient.post<ProjectTemplate>(`${BASE_URL}/templates`, data);
  }

  /** 更新模板（version 可选乐观锁；status 显式切换 draft|active|archived）。 */
  updateTemplate(templateId: number, data: TemplateUpdateRequest): Promise<ProjectTemplate> {
    return httpClient.put<ProjectTemplate>(`${BASE_URL}/templates/${templateId}`, data);
  }

  deleteTemplate(templateId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/templates/${templateId}`);
  }

  /** 一键生成阶段/任务/检查单/里程碑（一项目一模板；重复应用 409 且不产生重复数据）。 */
  applyTemplate(projectId: number, templateId: number): Promise<TemplateApplyResult> {
    return httpClient.post<TemplateApplyResult>(`${BASE_URL}/${projectId}/templates/apply`, { templateId });
  }

  // ---- 状态报告（PE-17）----

  /** 报告模板列表（status/q 过滤 + 服务端分页）。 */
  listReportTemplates(params: ReportTemplateListQuery = {}): Promise<ReportTemplateListResult> {
    return httpClient.get<ReportTemplateListResult>(`${BASE_URL}/report-templates`, params);
  }

  getReportTemplate(templateId: number): Promise<ReportTemplate> {
    return httpClient.get<ReportTemplate>(`${BASE_URL}/report-templates/${templateId}`);
  }

  createReportTemplate(data: ReportTemplateCreateRequest): Promise<ReportTemplate> {
    return httpClient.post<ReportTemplate>(`${BASE_URL}/report-templates`, data);
  }

  /** 更新报告模板（version 乐观锁；status 显式切换 draft|active|archived）。 */
  updateReportTemplate(templateId: number, data: ReportTemplateUpdateRequest): Promise<ReportTemplate> {
    return httpClient.put<ReportTemplate>(`${BASE_URL}/report-templates/${templateId}`, data);
  }

  deleteReportTemplate(templateId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/report-templates/${templateId}`);
  }

  /** 生成项目状态报告（数据派生：markdown + data；templateId 缺省用默认章节）。 */
  generateProjectReport(projectId: number, templateId?: number): Promise<ProjectReport> {
    return httpClient.post<ProjectReport>(`${BASE_URL}/${projectId}/reports/generate`, {
      templateId: templateId ?? 0,
    });
  }

  /** 报表分组：按归口团队聚合项目数与健康度分布（可见性谓词收窄）。 */
  reportGroups(): Promise<ReportGroup[]> {
    return httpClient.get<ReportGroup[]>(`${BASE_URL}/report-groups`);
  }

  // ---- 风险/问题台账（P2/PE-21） ----

  listRisks(
    projectId: number,
    params: { kind?: RiskKind | ''; status?: RiskStatus | ''; level?: RiskLevel | '' } = {}
  ): Promise<ProjectRisk[]> {
    return httpClient.get<ProjectRisk[]>(`${BASE_URL}/${projectId}/risks`, params);
  }

  getRisk(projectId: number, riskId: number): Promise<ProjectRisk> {
    return httpClient.get<ProjectRisk>(`${BASE_URL}/${projectId}/risks/${riskId}`);
  }

  createRisk(projectId: number, data: RiskCreateRequest): Promise<ProjectRisk> {
    return httpClient.post<ProjectRisk>(`${BASE_URL}/${projectId}/risks`, data);
  }

  /** 更新风险（version 乐观锁；ownerId/relatedTicketId 传 0 清空）。 */
  updateRisk(projectId: number, riskId: number, data: RiskUpdateRequest): Promise<ProjectRisk> {
    return httpClient.put<ProjectRisk>(`${BASE_URL}/${projectId}/risks/${riskId}`, data);
  }

  /** 状态流转：mitigate（open→mitigating）| close（要求 resolution）| reopen（closed→open）。 */
  transitionRisk(
    projectId: number,
    riskId: number,
    action: 'mitigate' | 'close' | 'reopen',
    resolution?: string
  ): Promise<ProjectRisk> {
    return httpClient.post<ProjectRisk>(`${BASE_URL}/${projectId}/risks/${riskId}/status`, {
      action,
      resolution,
    });
  }

  /** 转问题单/变更单（同目标二次转换 409：data.existingId 为既有产物ID）。 */
  convertRisk(projectId: number, riskId: number, target: RiskConvertTarget): Promise<RiskConvertResult> {
    return httpClient.post<RiskConvertResult>(`${BASE_URL}/${projectId}/risks/${riskId}/convert`, { target });
  }

  deleteRisk(projectId: number, riskId: number): Promise<void> {
    return httpClient.delete<void>(`${BASE_URL}/${projectId}/risks/${riskId}`);
  }

  // ---- 资源负荷（P2/PE-23） ----

  /** 资源负荷只读热图（周维度；start 缺省=本周并 snap 到周一，weeks 1..12 缺省 6）。 */
  resourceHeatmap(projectId: number, params: { start?: string; weeks?: number } = {}): Promise<ResourceHeatmap> {
    return httpClient.get<ResourceHeatmap>(`${BASE_URL}/${projectId}/resource-heatmap`, params);
  }

  /** 周容量配置（项目默认 + 成员覆盖；缺省=不修改，null=清空回继承/回内置）。 */
  updateCapacity(projectId: number, data: CapacityUpdateRequest): Promise<CapacityResponse> {
    return httpClient.put<CapacityResponse>(`${BASE_URL}/${projectId}/capacity`, data);
  }

  // ---- 组合视图与度量（P2/PE-24） ----

  /** 项目组合视图（按状态/健康度/负责人/部门/类型聚合 + 与列表一致的项目行）。 */
  portfolio(params: PortfolioQuery = {}): Promise<PortfolioResponse> {
    return httpClient.get<PortfolioResponse>(`${BASE_URL}/portfolio`, params);
  }

  /** 组合度量（按时交付率/里程碑达成率/阶段门一次通过率/资源利用率 + 明细）。 */
  metrics(params: MetricsQuery = {}): Promise<MetricsResponse> {
    return httpClient.get<MetricsResponse>(`${BASE_URL}/metrics`, params);
  }

  // ---- 项目 AI（P2/PE-25） ----

  /** 生成 AI 状态摘要（cadence 缺省 weekly；降级时 generatedBy=deterministic 且 summary 为事实派生）。 */
  aiSummary(projectId: number, cadence: AIReportCadence = 'weekly'): Promise<AISummaryResponse> {
    return httpClient.post<AISummaryResponse>(`${BASE_URL}/${projectId}/ai/summary`, { cadence });
  }

  /** 生成风险预警（规则信号恒返回；warnings 为 AI 判断增强段，降级时空）。 */
  aiRiskWarnings(projectId: number): Promise<AIRiskWarningsResponse> {
    return httpClient.post<AIRiskWarningsResponse>(`${BASE_URL}/${projectId}/ai/risk-warnings`, {});
  }

  /** 会议纪要转任务建议（只出建议不落库；用户勾选后走 createTask 逐条创建）。 */
  aiMinutesTasks(projectId: number, content: string, limit?: number): Promise<AIMinutesTasksResponse> {
    return httpClient.post<AIMinutesTasksResponse>(`${BASE_URL}/${projectId}/ai/minutes/tasks`, {
      content,
      ...(limit ? { limit } : {}),
    });
  }

  // ---- 关联单据 ----

  /** 单据→项目反向查询（详情页展示"所属项目"；project=null = 未挂接或项目对当前用户不可见）。 */
  getRecordProject(domain: RecordDomain, recordId: number): Promise<RecordProjectResult> {
    return httpClient.get<RecordProjectResult>(`/api/v1/${DOMAIN_PATH[domain]}/${recordId}/project`);
  }

  listRecords(
    projectId: number,
    params: { type?: RecordDomain | ''; status?: string; page?: number; pageSize?: number } = {}
  ): Promise<RecordListResult> {
    return httpClient.get<RecordListResult>(`${BASE_URL}/${projectId}/records`, params);
  }

  attachRecord(domain: RecordDomain, recordId: number, projectId: number): Promise<unknown> {
    return httpClient.post(`/api/v1/${DOMAIN_PATH[domain]}/${recordId}/project`, { projectId });
  }

  detachRecord(domain: RecordDomain, recordId: number): Promise<unknown> {
    return httpClient.delete(`/api/v1/${DOMAIN_PATH[domain]}/${recordId}/project`);
  }
}

export const projectService = new ProjectService();
export default projectService;