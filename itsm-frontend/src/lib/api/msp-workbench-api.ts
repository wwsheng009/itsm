/**
 * MSP 跨客户工作台 API（IP-P0-8 / WB-A2、WB-A6）。
 *
 * 契约来源（已冻结，后端 IP-P0-7）：
 * - itsm-backend/dto/msp_workbench_dto.go
 * - itsm-backend/handlers/msp/workbench.go
 * - itsm-backend/router/msp_routes.go
 *
 * httpClient 自动解包响应包络 `data` 并做 snake_case → camelCase 归一化，
 * 因此本模块直接返回业务结构。
 */
import { httpClient } from './http-client';

/** 工作台路由（父会话负责在 routes/** 注册；此处仅作为导航目标常量）。 */
export const MSP_WORKBENCH_PATH = '/msp/workbench';

/** URL query / 请求 query 中客户范围的参数名。 */
export const CUSTOMER_TENANT_IDS_PARAM = 'customerTenantIds';

/** 服务端暂停/过期客户的只读原因码（WB-A5 / §3.4）。 */
export const CUSTOMER_INACTIVE_REASON = 'CUSTOMER_INACTIVE';

// ==================== 客户列表（GET /api/v1/msp/customers） ====================

export interface MspCustomer {
  id: number;
  code: string;
  name: string;
}

export interface MspCustomersResponse {
  customers: MspCustomer[];
  total: number;
}

// ==================== 工作台计数（GET /api/v1/msp/workbench/summary） ====================

export interface WorkbenchSummaryCustomer {
  customerTenantId: number;
  customerName: string;
  /** 未关闭工单数 */
  open: number;
  /** 已超 SLA 截止的未关闭工单数 */
  slaRisk: number;
  /** 未指派工单数 */
  unassigned: number;
}

export interface WorkbenchSummary {
  generatedAt: string;
  ttlSeconds: number;
  customers: WorkbenchSummaryCustomer[];
}

// ==================== 跨客户工单列表（GET /api/v1/msp/workbench/tickets） ====================

/** 条目级动作（前端只按该数组渲染，不自行推断权限）。 */
export interface WorkbenchAllowedAction {
  action: string;
  allowed: boolean;
  reasonCode?: string;
  reasonText?: string;
}

export interface WorkbenchTicketItem {
  id: number;
  customerTenantId: number;
  customerName: string;
  ticketNumber: string;
  title: string;
  status: string;
  priority: string;
  assigneeId?: number;
  assigneeName?: string;
  updatedAt: string;
  slaDeadline?: string;
  allowedActions: WorkbenchAllowedAction[];
}

export interface WorkbenchTicketListResponse {
  items: WorkbenchTicketItem[];
  nextCursor?: string;
  total: number;
}

export type CustomerTenantIdsFilter = 'all' | number[];

export interface WorkbenchTicketQuery {
  customerTenantIds?: CustomerTenantIdsFilter;
  status?: string;
  priority?: string;
  assigneeId?: number;
  q?: string;
  /** RFC3339 */
  updatedAfter?: string;
  sort?: 'updated' | 'sla';
  cursor?: string;
  limit?: number;
}

// ==================== 契约解析助手（URL query ↔ API query） ====================

/**
 * 解析 URL query 中的 customerTenantIds：`all`/空 → 'all'；非法内容回退 'all'。
 * 与服务端 parseCustomerTenantIDs 语义保持一致。
 */
export function parseCustomerTenantIds(raw: string | null | undefined): CustomerTenantIdsFilter {
  const value = (raw ?? '').trim();
  if (!value || value.toLowerCase() === 'all') return 'all';
  const ids = value
    .split(',')
    .map(part => Number(part.trim()))
    .filter(id => Number.isFinite(id) && id > 0);
  const unique = Array.from(new Set(ids));
  return unique.length > 0 ? unique : 'all';
}

/** 序列化客户端选择：空数组 = 全部客户（服务端展开为允许集合）。 */
export function serializeCustomerTenantIds(filter?: CustomerTenantIdsFilter): string {
  if (!filter || filter === 'all') return 'all';
  const ids = filter.filter(id => Number.isFinite(id) && id > 0);
  return ids.length > 0 ? ids.join(',') : 'all';
}

// ==================== allowedActions 判定（WB-A5/B2：后端计算、前端只读消费） ====================

export function findAllowedAction(
  ticket: Pick<WorkbenchTicketItem, 'allowedActions'>,
  action: string
): WorkbenchAllowedAction | undefined {
  return (ticket.allowedActions ?? []).find(item => item.action === action);
}

/** 客户租户暂停/过期：后端对每个动作下发 CUSTOMER_INACTIVE，条目整体只读。 */
export function isCustomerInactive(ticket: Pick<WorkbenchTicketItem, 'allowedActions'>): boolean {
  return (ticket.allowedActions ?? []).some(item => item.reasonCode === CUSTOMER_INACTIVE_REASON);
}

/**
 * 只读态判定：allowedActions 为空（客户端无法自行判定动作）或客户已暂停/过期。
 * 此时不渲染任何行内操作。
 */
export function isTicketReadOnly(ticket: Pick<WorkbenchTicketItem, 'allowedActions'>): boolean {
  const actions = ticket.allowedActions ?? [];
  return actions.length === 0 || isCustomerInactive(ticket);
}

export function isActionAllowed(
  ticket: Pick<WorkbenchTicketItem, 'allowedActions'>,
  action: string
): boolean {
  if (isTicketReadOnly(ticket)) return false;
  return findAllowedAction(ticket, action)?.allowed === true;
}

// ==================== API 调用 ====================

/** GET /api/v1/msp/customers —— 当前 MSP 员工可访问的客户（服务端 allocation 收口）。 */
export async function listMspCustomers(): Promise<MspCustomersResponse> {
  return httpClient.get<MspCustomersResponse>('/api/v1/msp/customers');
}

/** GET /api/v1/msp/workbench/summary —— 每客户计数徽标（服务端 30s 内存缓存）。 */
export async function getWorkbenchSummary(): Promise<WorkbenchSummary> {
  return httpClient.get<WorkbenchSummary>('/api/v1/msp/workbench/summary');
}

/** GET /api/v1/msp/workbench/tickets —— 跨客户工单列表（per-tenant 复合游标）。 */
export async function listWorkbenchTickets(
  query: WorkbenchTicketQuery = {}
): Promise<WorkbenchTicketListResponse> {
  const params = new URLSearchParams();
  params.set('customerTenantIds', serializeCustomerTenantIds(query.customerTenantIds));
  if (query.status) params.set('status', query.status);
  if (query.priority) params.set('priority', query.priority);
  if (query.assigneeId !== undefined && query.assigneeId > 0) {
    params.set('assigneeId', String(query.assigneeId));
  }
  if (query.q) params.set('q', query.q);
  if (query.updatedAfter) params.set('updatedAfter', query.updatedAfter);
  if (query.sort) params.set('sort', query.sort);
  if (query.cursor) params.set('cursor', query.cursor);
  if (query.limit !== undefined && query.limit > 0) params.set('limit', String(query.limit));

  return httpClient.get<WorkbenchTicketListResponse>(
    `/api/v1/msp/workbench/tickets?${params.toString()}`
  );
}

export interface WorkbenchReplyBody {
  customerTenantId: number;
  content: string;
}

/** POST /api/v1/msp/tickets/:id/reply —— 条目级回复（服务端按单据租户授权）。 */
export async function replyWorkbenchTicket(
  ticketId: number,
  body: WorkbenchReplyBody
): Promise<unknown> {
  return httpClient.post<unknown>(`/api/v1/msp/tickets/${ticketId}/reply`, body);
}

export interface WorkbenchStatusBody {
  customerTenantId: number;
  status: string;
}

/** POST /api/v1/msp/tickets/:id/status —— 条目级改状态（状态机校验）。 */
export async function changeWorkbenchTicketStatus(
  ticketId: number,
  body: WorkbenchStatusBody
): Promise<unknown> {
  return httpClient.post<unknown>(`/api/v1/msp/tickets/${ticketId}/status`, body);
}

/**
 * POST /api/v1/msp/tickets/:id/assign —— 行内指派（IP-P1 / assign 动作全量接入）。
 *
 * 语义（后端 ticket_service.AssignMSPTechnician）：把工单指派给**当前登录的 MSP 技术员**
 * （即提交人），同步写 `managed_by_user_id` 与客户快照；不接受自定义 assigneeId。
 */
export async function assignWorkbenchTicket(
  ticketId: number,
  body: { customerTenantId: number }
): Promise<unknown> {
  return httpClient.post<unknown>(`/api/v1/msp/tickets/${ticketId}/assign`, body);
}

/**
 * POST /api/v1/auth/switch-tenant —— 深度操作入口（会话切换）。
 * 成功后调用方需整页 `window.location.assign('/')` 重新引导会话（父会话负责 store 刷新链路）。
 */
export async function switchTenantScope(tenantId: number): Promise<unknown> {
  return httpClient.post<unknown>('/api/v1/auth/switch-tenant', { tenantId });
}

// ==================== 批量操作（POST /api/v1/msp/workbench/batch，IP-P1-6a/6b） ====================

/** 单次批量上限（与后端 dto 校验一致）。 */
export const MAX_BATCH_ITEMS = 100;

/** 低危批量动作白名单（后端同口径）。 */
export type WorkbenchBatchAction = 'reply' | 'status' | 'assign';

export interface WorkbenchBatchItem {
  ticketId: number;
  /** 显式声明目标租户；服务端逐条校验资源租户一致。 */
  customerTenantId: number;
}

export interface WorkbenchBatchPayload {
  content?: string;
  status?: string;
  assigneeId?: number;
}

export interface WorkbenchBatchRequest {
  action: WorkbenchBatchAction;
  items: WorkbenchBatchItem[];
  payload?: WorkbenchBatchPayload;
}

export interface WorkbenchBatchItemResult {
  ticketId: number;
  customerTenantId: number;
  ok: boolean;
  reasonCode?: string;
  message?: string;
}

export interface WorkbenchBatchResponse {
  /** 整批审计回溯 id。 */
  batchId: string;
  succeeded: number;
  failed: number;
  results: WorkbenchBatchItemResult[];
}

/** POST /api/v1/msp/workbench/batch —— 批量低危操作（部分失败不影响其余条目）。 */
export async function batchWorkbenchItems(
  body: WorkbenchBatchRequest
): Promise<WorkbenchBatchResponse> {
  return httpClient.post<WorkbenchBatchResponse>('/api/v1/msp/workbench/batch', body);
}
