/**
 * 工单 BPMN 流程状态类型定义
 *
 * 与后端 dto/ticket_workflow_dto.go 中的 BpmnProcessState / NextActivityInfo /
 * BpmnHistoryItem 严格 1:1 对齐（camelCase JSON 字段）。
 *
 * 工单三件套改造：详情页 / 流转卡片通过 GET /api/v1/tickets/:id/workflow/state-v2
 * 拉取真实 BPMN 节点状态，用于"当前节点 / 下一节点 / 已走节点 / 终态"展示。
 */

import type { TicketUser } from './ticket';

/**
 * BPMN 流程状态机取值。
 *
 * - not_started：工单存在但尚未关联任何运行中的 BPMN 流程实例（降级 UI）。
 * - running：流程进行中；currentActivity* 有值。
 * - suspended：流程被挂起（暂未触发生产路径）。
 * - completed / terminated：终态；currentActivity* 省略，nextActivities 为空。
 */
export type BpmnStatus =
  | 'not_started'
  | 'running'
  | 'suspended'
  | 'completed'
  | 'terminated';

/**
 * BPMN 节点类型，与后端 enrich 阶段对 XML 元素的归一化结果一致。
 */
export type BpmnActivityType =
  | 'startEvent'
  | 'endEvent'
  | 'userTask'
  | 'serviceTask'
  | 'exclusiveGateway'
  | 'parallelGateway'
  | 'inclusiveGateway'
  | string;

/**
 * BPMN 节点类型 → 中文标签（仅展示用，不参与逻辑）。
 * 词表单一源，覆盖 types/ticket-workflow-state.ts 的 BpmnActivityType。
 */
export const BPMN_ACTIVITY_TYPE_LABEL: Record<string, string> = {
  startEvent: '开始事件',
  endEvent: '结束事件',
  userTask: '用户任务',
  serviceTask: '服务任务',
  exclusiveGateway: '排他网关',
  parallelGateway: '并行网关',
  inclusiveGateway: '包容网关',
  scriptTask: '脚本任务',
  subProcess: '子流程',
  // 兼容后端 XML 原始写法（snake_case）
  start_event: '开始事件',
  end_event: '结束事件',
  user_task: '用户任务',
  service_task: '服务任务',
  exclusive_gateway: '排他网关',
  parallel_gateway: '并行网关',
  inclusive_gateway: '包容网关',
  script_task: '脚本任务',
  sub_process: '子流程',
};

/**
 * 把任意写法的节点类型归一化：`user_task` / `USER_TASK` / `usertask`
 * 统一折叠为 `userTask`，避免后端 XML 原始写法漏映射而外露原始标识。
 */
export function normalizeActivityType(type: string): string {
  if (!type) return '';
  if (type.includes('_') || type.includes('-')) {
    return type
      .split(/[_-]/)
      .filter(Boolean)
      .map((part, idx) =>
        idx === 0
          ? part.toLowerCase()
          : part.charAt(0).toUpperCase() + part.slice(1).toLowerCase()
      )
      .join('');
  }
  if (type === type.toUpperCase() || type === type.toLowerCase()) {
    // all-lower / all-upper：把首字母小写后按已知表命中
    const lower = type.toLowerCase();
    const camel = lower.charAt(0) + lower.slice(1);
    if (BPMN_ACTIVITY_TYPE_LABEL[camel]) return camel;
  }
  return type;
}

/** 将 BPMN 节点类型映射为中文标签（未命中也返回「节点」，绝不裸落原始 id）。 */
export function bpmnActivityTypeName(type?: BpmnActivityType | null): string {
  if (!type) return '节点';
  const normalized = normalizeActivityType(String(type));
  return BPMN_ACTIVITY_TYPE_LABEL[normalized] || BPMN_ACTIVITY_TYPE_LABEL[String(type)] || '节点';
}

/**
 * 节点类型 → 标签色的统一映射。
 *
 * 与 bpmnActivityTypeName 共用同一套归一化，避免 `user_task` / `usertask`
 * 这类写法在颜色表里 miss 而统一落到默认色（review round8 N6 的残留面）。
 */
const BPMN_ACTIVITY_TYPE_COLOR: Record<string, string> = {
  startEvent: 'green',
  endEvent: 'default',
  userTask: 'blue',
  serviceTask: 'purple',
  exclusiveGateway: 'orange',
  parallelGateway: 'cyan',
  inclusiveGateway: 'gold',
};

/** 取节点类型的标签色；未命中等价回落 `blue`，不因写法差异变色。 */
export function bpmnActivityTypeColor(type?: BpmnActivityType | null, fallback = 'blue'): string {
  if (!type) return fallback;
  const normalized = normalizeActivityType(String(type));
  return BPMN_ACTIVITY_TYPE_COLOR[normalized] || BPMN_ACTIVITY_TYPE_COLOR[String(type)] || fallback;
}

/** 已知的「裸原始 id」集合（currentActivityName / activityName 偶尔返回技术 id 而非业务名）。 */
const BPMN_RAW_NAME_TOKENS = new Set<string>([
  'user_task',
  'userTask',
  'exclusiveGateway',
  'parallelGateway',
  'inclusiveGateway',
  'serviceTask',
  'scriptTask',
  'startEvent',
  'endEvent',
  'task',
  'gateway',
  'subprocess',
  'subProcess',
]);

/**
 * 解析节点「业务显示名」：
 * - 已有业务名（非裸原始 id）→ 原样展示；
 * - 缺失或命中裸原始 id → 降级为「处理中（第 N 步）」，不回落到原始 id。
 */
export function bpmnActivityDisplayName(name?: string | null, step?: number): string {
  if (name && !BPMN_RAW_NAME_TOKENS.has(name)) return name;
  return `处理中（第 ${step ?? '?'} 步）`;
}

/**
 * 判断某节点名是否为「裸原始标识」（`user_task` / `userTask` / `exclusiveGateway` 等）。
 * 供流转进度、审批链等展示层统一判定，避免各组件各自维护一份 token 集合。
 */
export function isRawActivityName(name?: string | null): boolean {
  if (!name) return false;
  return BPMN_RAW_NAME_TOKENS.has(name);
}

/**
 * 流程当前节点的处理人列表。
 * 与后端 WorkflowUserInfo 字段一一对应；前端可直接复用 TicketUser。
 */
export type WorkflowAssignee = TicketUser;

/**
 * 单个"下一步候选活动"。
 * IsGateway=true 时表示该活动为网关节点（exclusive / parallel / inclusive），
 * 调用方应据此决定是否展开分支说明（例如"如审批通过则进入 X，否则进入 Y"）。
 */
export interface NextActivityInfo {
  activityId: string;
  activityName: string;
  activityType: BpmnActivityType;
  assignees?: WorkflowAssignee[];
  isGateway: boolean;
}

/**
 * 流程历史快照中的一项。
 *
 * - Outcome 取自 process_tasks 的 status + 业务变量（approvalResult / approvalAction），
 *   例如 approved / rejected / completed / skipped。
 * - Assignee 可能为 null（系统任务、网关等无处理人）。
 */
export interface BpmnHistoryItem {
  activityId: string;
  activityName: string;
  activityType: BpmnActivityType;
  startTime: string; // ISO8601
  endTime?: string | null;
  assignee?: WorkflowAssignee | null;
  outcome?: string;
}

/**
 * 工单对应的 BPMN 流程状态聚合。
 *
 * 单结构体覆盖全部状态：调用方按 bpmnStatus 分支：
 * - 'not_started' → 显示降级 Alert
 * - 'running'     → 渲染 current / next / history 三段
 * - 'completed' / 'terminated' → 显示终态 Tag，仅展示 history
 * - 'suspended'   → 保留扩展（暂未在生产触发）
 */
export interface BpmnProcessState {
  processInstanceId: string;
  processDefinitionKey: string;
  processDefinitionName: string;
  bpmnStatus: BpmnStatus;
  currentActivityId?: string;
  currentActivityName?: string;
  currentActivityType?: BpmnActivityType;
  currentAssignees?: WorkflowAssignee[];
  nextActivities?: NextActivityInfo[];
  history?: BpmnHistoryItem[];
  startedAt?: string | null;
  endedAt?: string | null;
}

/**
 * 进度卡片 / 当前节点渲染所需的精简视图。
 *
 * ApprovalWorkflowPanel 与 WorkflowProgressCard 共用，便于上层避免重复解析
 * NextActivities / History（按需计算即可）。
 */
export interface WorkflowProgressView {
  state: BpmnProcessState;
  totalActivities: number;     // 已走节点 + 当前节点 + 下一节点的去重数（估算）
  completedActivities: number; // 历史节点数
}
