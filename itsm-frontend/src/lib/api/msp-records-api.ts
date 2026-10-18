/**
 * MSP 跨租户任务投影 + 单据（变更/问题/服务请求）API（跨租户单据流转 P2-②/P2-③）。
 *
 * 契约来源（已冻结）：
 * - itsm-backend/dto/msp_task_projection_dto.go、dto/msp_record_dto.go
 * - itsm-backend/handlers/msp/task_projection.go、records.go
 * - itsm-backend/router/msp_routes.go
 *
 * 授权与 UI 契约（XF5）：条目随响应下发 `allowedActions[]`，前端只按数组渲染、禁止推断；
 * 写操作一律携带 `customerTenantId`（资源租户声明），服务端校验「声明租户 == 资源租户」。
 */
import { httpClient } from './http-client';
import type { WorkbenchAllowedAction } from './msp-workbench-api';

// ==================== 任务投影（GET /api/v1/msp/workbench/tasks） ====================

export interface MSPTaskItem {
  taskId: string;
  taskName: string;
  taskDefinitionKey: string;
  processDefinitionKey: string;
  processInstanceId: number;
  status: string;
  priority: string;
  assignee?: string;
  dueDate?: string;
  createdTime: string;
  customerTenantId: number;
  customerTenantName?: string;
  /** 投影来源：assigned=显式指派/候选给我；queue=服务商待处理池（保留标记 msp_provider）。 */
  source: string;
  allowedActions: WorkbenchAllowedAction[];
}

export interface MSPTaskListResponse {
  items: MSPTaskItem[];
  total: number;
}

export interface MSPTaskQuery {
  customerTenantIds?: 'all' | number[];
  limit?: number;
}

function appendCustomerScope(params: URLSearchParams, scope?: 'all' | number[]): void {
  if (Array.isArray(scope) && scope.length > 0) {
    params.set('customerTenantIds', scope.join(','));
  }
}

/** GET /api/v1/msp/workbench/tasks —— 跨客户待办任务投影（按 allocation ∩ 客户租户 task 权限）。 */
export function listWorkbenchTasks(query: MSPTaskQuery = {}): Promise<MSPTaskListResponse> {
  const params = new URLSearchParams();
  appendCustomerScope(params, query.customerTenantIds);
  if (query.limit) params.set('limit', String(query.limit));
  const suffix = params.toString();
  return httpClient.get<MSPTaskListResponse>(
    `/api/v1/msp/workbench/tasks${suffix ? `?${suffix}` : ''}`
  );
}

/** POST /api/v1/msp/workbench/tasks/:taskId/claim —— 认领任务（客户租户内写回）。 */
export function claimWorkbenchTask(
  taskId: string,
  body: { customerTenantId?: number } = {}
): Promise<MSPTaskItem> {
  return httpClient.post<MSPTaskItem>(
    `/api/v1/msp/workbench/tasks/${encodeURIComponent(taskId)}/claim`,
    body
  );
}

/** POST /api/v1/msp/workbench/tasks/:taskId/complete —— 完成任务并推进客户流程。 */
export function completeWorkbenchTask(
  taskId: string,
  body: { customerTenantId?: number; variables?: Record<string, unknown> } = {}
): Promise<MSPTaskItem> {
  return httpClient.post<MSPTaskItem>(
    `/api/v1/msp/workbench/tasks/${encodeURIComponent(taskId)}/complete`,
    body
  );
}

// ==================== 三类单据读面（GET /api/v1/msp/workbench/{changes,problems,service-requests}） ====================

/** 记录类型（后端 recordType 枚举；project 为 P2+ MSP 客户交付只读面）。 */
export type MSPRecordType = 'change' | 'problem' | 'service_request' | 'project';

/** 记录类型 → 路径 slug（路由段）。 */
export const MSP_RECORD_SLUG: Record<MSPRecordType, string> = {
  change: 'changes',
  problem: 'problems',
  service_request: 'service-requests',
  project: 'projects',
};

export interface MSPRecordItem {
  recordType: MSPRecordType;
  id: number;
  customerTenantId: number;
  customerName?: string;
  number: string;
  title: string;
  status: string;
  priority?: string;
  assigneeId?: number;
  assigneeName?: string;
  requesterId?: number;
  requesterName?: string;
  updatedAt: string;
  allowedActions: WorkbenchAllowedAction[];
}

export interface MSPRecordListResponse {
  items: MSPRecordItem[];
  nextCursor?: string;
  total: number;
}

export interface MSPRecordQuery {
  customerTenantIds?: 'all' | number[];
  status?: string;
  q?: string;
  limit?: number;
  cursor?: string;
}

/** 详情 = 条目字段 + 三类并集字段（omitempty 按类型下发）。 */
export interface MSPRecordDetail extends MSPRecordItem {
  description?: string;
  createdAt: string;
  // 变更
  type?: string;
  riskLevel?: string;
  impactScope?: string;
  justification?: string;
  plannedStartDate?: string;
  plannedEndDate?: string;
  // 问题
  category?: string;
  rootCause?: string;
  workaround?: string;
  resolution?: string;
  impact?: string;
  resolvedAt?: string;
  closedAt?: string;
  // 服务请求
  catalogId?: number;
  currentLevel?: number;
  totalLevels?: number;
  currentApprover?: string;
  approverComment?: string;
  approvedAt?: string;
  startedAt?: string;
  completedAt?: string;
  completionNote?: string;
}

/** GET /api/v1/msp/workbench/{slug} —— 聚合列表（逐租户查询 + 内存合并；nextCursor 逐租户位置编码）。 */
export function listWorkbenchRecords(
  recordType: MSPRecordType,
  query: MSPRecordQuery = {}
): Promise<MSPRecordListResponse> {
  const params = new URLSearchParams();
  appendCustomerScope(params, query.customerTenantIds);
  if (query.status) params.set('status', query.status);
  if (query.q) params.set('q', query.q);
  if (query.limit) params.set('limit', String(query.limit));
  if (query.cursor) params.set('cursor', query.cursor);
  const suffix = params.toString();
  return httpClient.get<MSPRecordListResponse>(
    `/api/v1/msp/workbench/${MSP_RECORD_SLUG[recordType]}${suffix ? `?${suffix}` : ''}`
  );
}

/** GET /api/v1/msp/{slug}/:id?customerTenantId=… —— 单条详情（声明租户绑定 RLS + allocation 二次校验）。 */
export function getWorkbenchRecord(
  recordType: MSPRecordType,
  id: number,
  customerTenantId: number
): Promise<MSPRecordDetail> {
  return httpClient.get<MSPRecordDetail>(
    `/api/v1/msp/${MSP_RECORD_SLUG[recordType]}/${id}?customerTenantId=${customerTenantId}`
  );
}

// ==================== 写面：状态推进（P2-③） ====================

/**
 * 状态推进请求体（按类型取用）：
 * - 变更（change）：status ∈ scheduled|in_progress|completed|closed|cancelled；comment 可选（approve/reject/rollback 不在 MSP 面）；
 * - 问题（problem）：status ∈ investigating|identified|resolved|closed；resolution 可选；
 * - 服务请求（service_request）：status ∈ provisioning|delivered|failed（approval 保留客户治理面）。
 */
export interface UpdateMSPRecordStatusRequest {
  customerTenantId: number;
  status: string;
  comment?: string;
  resolution?: string;
}

/** POST /api/v1/msp/{slug}/:id/status —— 非审批状态推进（服务端 7 步授权链 + 域状态机裁决）。 */
export function updateWorkbenchRecordStatus(
  recordType: MSPRecordType,
  id: number,
  body: UpdateMSPRecordStatusRequest
): Promise<MSPRecordItem> {
  return httpClient.post<MSPRecordItem>(
    `/api/v1/msp/${MSP_RECORD_SLUG[recordType]}/${id}/status`,
    body
  );
}

// ==================== 展示口径（与后端域白名单镜像；仅用于 UI 选项，不用于授权） ====================

export interface MSPStatusOption {
  value: string;
  label: string;
}

/** 变更非审批流转目标状态（镜像后端 handlers/change 白名单；审批类不在此列）。 */
export const CHANGE_STATUS_OPTIONS: MSPStatusOption[] = [
  { value: 'scheduled', label: '已排期' },
  { value: 'in_progress', label: '实施中' },
  { value: 'completed', label: '已完成' },
  { value: 'closed', label: '已关闭' },
  { value: 'cancelled', label: '已取消' },
];

/** 问题状态推进目标状态（镜像后端 handlers/problem 白名单）。 */
export const PROBLEM_STATUS_OPTIONS: MSPStatusOption[] = [
  { value: 'investigating', label: '调查中' },
  { value: 'identified', label: '已定位' },
  { value: 'resolved', label: '已解决' },
  { value: 'closed', label: '已关闭' },
];

/** 服务请求履约状态（镜像后端 handlers/service_request 操作面白名单；审批类不在此列）。 */
export const SERVICE_REQUEST_STATUS_OPTIONS: MSPStatusOption[] = [
  { value: 'provisioning', label: '履约中' },
  { value: 'delivered', label: '已交付' },
  { value: 'failed', label: '履约失败' },
];

export const MSP_STATUS_OPTIONS: Record<MSPRecordType, MSPStatusOption[]> = {
  change: CHANGE_STATUS_OPTIONS,
  problem: PROBLEM_STATUS_OPTIONS,
  service_request: SERVICE_REQUEST_STATUS_OPTIONS,
  // 项目只读：审批/状态推进保留客户治理面（MSP 面不提供写端点）。
  project: [],
};

export const MSP_RECORD_TYPE_LABEL: Record<MSPRecordType, string> = {
  change: '变更',
  problem: '问题',
  service_request: '服务请求',
  project: '项目',
};

/** 全量状态展示标签（含流程中/终态；详情与列表当前状态回显用）。 */
const STATUS_LABELS: Record<string, string> = {
  // 变更
  draft: '草稿',
  pending_approval: '待审批',
  approved: '已批准',
  rejected: '已拒绝',
  scheduled: '已排期',
  in_progress: '实施中',
  completed: '已完成',
  closed: '已关闭',
  cancelled: '已取消',
  rolled_back: '已回滚',
  // 问题
  new: '新建',
  investigating: '调查中',
  identified: '已定位',
  resolved: '已解决',
  // 项目（只读展示）
  planning: '规划中',
  active: '进行中',
  suspended: '已暂停',
  // 服务请求
  pending: '待提交',
  pending_approval_sr: '待审批',
  security_approved: '安全审批通过',
  provisioning: '履约中',
  delivered: '已交付',
  failed: '履约失败',
  // 通用
  open: '待处理',
  assigned: '已指派',
};

export function recordStatusLabel(status?: string): string {
  if (!status) return '—';
  return STATUS_LABELS[status] ?? status;
}

/** 条目级动作判定（XF5：只读消费 allowedActions；与工单 helper 同语义）。 */
export function findRecordAction(
  item: { allowedActions?: WorkbenchAllowedAction[] },
  action: string
): WorkbenchAllowedAction | undefined {
  return (item.allowedActions ?? []).find(entry => entry.action === action);
}
