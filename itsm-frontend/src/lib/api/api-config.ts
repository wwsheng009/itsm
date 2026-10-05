// Browser requests default to same-origin so production traffic goes through the
// reverse proxy. Server-side requests are redirected to ITSM_BACKEND_URL by
// HttpClient without exposing the container hostname to the browser bundle.
export const API_BASE_URL = import.meta.env.VITE_API_URL || '';
export const API_VERSION = import.meta.env.VITE_API_VERSION || 'v1';
export const API_TIMEOUT = parseInt(import.meta.env.VITE_API_TIMEOUT || '30000');

// 通用 API 响应接口
export interface ApiResponse<T> {
  code: number;
  message: string;
  data: T;
}

// API 错误码定义
export const API_ERROR_CODES = {
  SUCCESS: 0,
  PARAM_ERROR: 1001,
  VALIDATION_ERROR: 1002,
  AUTH_FAILED: 2001,
  FORBIDDEN: 2003,
  NOT_FOUND: 4004,
  INTERNAL_ERROR: 5001,
} as const;

// 分页请求接口
export interface PaginationRequest {
  page?: number;
  pageSize?: number;
}

// 分页响应接口
export interface PaginationResponse<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

// 租户相关接口
// IP-P2-6 租户硬配额（limits）：>0 生效；缺省/0 = 不限。
export interface TenantQuota {
  maxUsers?: number;
  maxTicketsPerMonth?: number;
  maxStorageMB?: number;
}

// IP-P2-6 收尾：治理页“配额 vs 用量”（GET /api/v1/tenants/:id/usage，口径同写入校验）。
export interface TenantQuotaUsage {
  users: number;
  ticketsThisMonth: number;
  storageBytes: number;
}

export interface TenantQuotaUsageResponse {
  tenantId: number;
  limits: TenantQuota;
  used: TenantQuotaUsage;
}

export interface Tenant {
  id: number;
  name: string;
  code: string;
  domain?: string;
  /** 所属 MSP 服务商（仅 msp_customer 有值；msp_provider 自身为空）。 */
  mspProviderId?: number | null;
  /** 上级租户（MSP 客户指向服务商；平台租户为空）。 */
  parentTenantId?: number | null;
  /** 套餐编码（展示/筛选用，合同字段）。 */
  planCode?: string;
  type:
    | 'standard'
    | 'internal'
    | 'saas_customer'
    | 'msp_provider'
    | 'msp_customer'
    | 'msp'
    | 'customer'
    // Legacy frontend/session values kept for compatibility with existing auth state.
    | 'trial'
    | 'professional'
    | 'enterprise';
  status: 'active' | 'suspended' | 'expired' | 'deleted';
  createdAt: string;
  updatedAt: string;
  expiresAt?: string;
  settings?: Record<string, unknown>;
  quota?: TenantQuota;
}

export interface TenantListResponse {
  tenants: Tenant[];
  total: number;
  page: number;
  size: number;
}

export interface CreateTenantRequest {
  name: string;
  code: string;
  domain?: string;
  type: string;
  /** 仅 type='msp_customer' 时提交，指向 MSP 服务商租户。 */
  mspProviderId?: number;
  expiresAt?: string;
  settings?: Record<string, unknown>;
  quota?: TenantQuota;
}

export interface UpdateTenantRequest {
  name?: string;
  domain?: string;
  type?: string;
  /** 仅 type='msp_customer' 时提交，指向 MSP 服务商租户。 */
  mspProviderId?: number;
  status?: string;
  expiresAt?: string;
  settings?: Record<string, unknown>;
  quota?: TenantQuota;
}

export interface GetTenantsParams {
  page?: number;
  size?: number;
  status?: string;
  type?: string;
  search?: string;
}

// ---------------------------------------------------------------------------
// 租户开通闭环（模板供给 / 首个管理员）契约。
// GET  /api/v1/tenants/:id/readiness       → TenantReadinessResponse
// POST /api/v1/tenants/:id/provision       → TenantReadinessResponse（幂等）
// POST /api/v1/tenants/:id/bootstrap-admin → BootstrapAdminResponse
// ---------------------------------------------------------------------------

/** readiness.items 单项：required=true 且 count=0 视为模板缺失。 */
export interface TenantReadinessItem {
  key: string;
  label: string;
  count: number;
  required: boolean;
}

export interface TenantReadinessResponse {
  tenantId: number;
  templateVersion?: string;
  ready: boolean;
  /** 已存在首个管理员的数量（0 表示未创建）。 */
  bootstrapAdmins: number;
  items: TenantReadinessItem[];
}

export interface BootstrapAdminRequest {
  /** 留空则服务端生成（响应中一次性回传）。 */
  password?: string;
  /** 留空则使用服务端默认（admin-<code>）。 */
  username?: string;
  email?: string;
}

export interface BootstrapAdminResponse {
  userId: number;
  username: string;
  email: string;
  /** 仅服务端生成密码时回传一次（generated=true）。 */
  password?: string;
  generated: boolean;
  mustChangePassword: boolean;
}

/** 建号通道（平台 / MSP 共用）请求体：CreateUserRequest 的 UI 子集。 */
export interface ProvisionUserRequest {
  username: string;
  name: string;
  email: string;
  password: string;
  /** MSP 角色（users.msp_role 词表：provider_admin|provider_agent|customer_user）；
   * 服务商租户建号必须携带，否则员工无法进入服务商工作台。 */
  mspRole?: string;
}

/** 建号响应（UserDetailResponse 的最小子集）。 */
export interface ProvisionUserResponse {
  id: number;
  username: string;
  email: string;
  name?: string;
}

// 重新导出标准Ticket类型，并扩展租户相关字段
import type { Ticket as BaseTicket } from './types';

export interface Ticket extends BaseTicket {
  tenantId?: number;
  templateId?: number;
  tenant?: Tenant;
  /** 富文本 HTML（服务端已二次清洗）；为空时按纯文本 description 渲染 */
  descriptionHtml?: string;
  /** 描述格式：plain / html，用于灰度回退与兼容旧数据 */
  descriptionFormat?: string;
  // 扩展字段
  subcategory?: string;
  impact?: string;
  urgency?: string;
  workNotes?: string;
  dueDate?: string; // 兼容旧字段名
  escalationLevel?: number;
  source?: string;
  businessValue?: string;
  customFields?: Record<string, unknown>;
}

// 附件接口
export interface Attachment {
  id: number;
  filename: string;
  originalName: string;
  fileSize: number;
  mimeType: string;
  url: string;
  uploadedBy: number;
  uploadedAt: string;
  uploader?: User;
}

// 工作流步骤接口
export interface WorkflowStep {
  id: number;
  stepName: string;
  stepOrder: number;
  status: 'pending' | 'in_progress' | 'completed' | 'skipped';
  assigneeId?: number;
  assignee?: User;
  startedAt?: string;
  completedAt?: string;
  comments?: string;
  requiredApproval: boolean;
  approvalStatus?: 'pending' | 'approved' | 'rejected';
  approvalComments?: string;
}

// 评论接口
export interface Comment {
  id: number;
  ticketId?: number;
  content: string;
  type?: 'comment' | 'work_note' | 'system';
  createdBy?: number;
  userId?: number;
  createdAt: string;
  updatedAt?: string;
  author?: User;
  isInternal: boolean;
  mentions?: number[];
  attachments?: number[];
  user?: {
    id: number;
    username: string;
    name: string;
    email: string;
    role?: string;
    department?: string;
    tenantId?: number;
  };
}

// SLA信息接口
export interface SLAInfo {
  slaId: number;
  slaName: string;
  responseTime: number; // 分钟
  resolutionTime: number; // 分钟
  startTime: string;
  dueTime: string;
  breachTime?: string;
  status: 'active' | 'breached' | 'completed';
}

export interface User {
  id: number;
  username: string;
  email: string;
  name: string;
  tenantId?: number;
  role?: string;
  /** MSP 身份角色（provider_admin/provider_agent；客户账号为空）。 */
  mspRole?: string;
  department?: string;
  permissions?: string[];
  /** IP-P1-5：首登强制改密标志（登录/`/auth/me` 下发；改密成功后清除）。 */
  mustChangePassword?: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface TicketListResponse {
  tickets: Ticket[];
  total: number;
  page: number;
  pageSize?: number;
  size: number;
  totalPages?: number;
}

export interface CreateTicketRequest {
  title: string;
  description: string;
  /** 富文本 HTML（提交前前端已净化，服务端再次白名单清洗） */
  descriptionHtml?: string;
  /** 描述格式：plain / html，默认 plain */
  descriptionFormat?: 'plain' | 'html';
  priority: string;
  type?: 'incident' | 'service_request' | 'change' | 'problem' | string;
	/** Tenant-scoped configured TicketType code. `type` remains the ITIL lifecycle domain. */
	typeId?: string;
	ticketTypeId?: number;
  category?: string;
  categoryId?: number;
  formFields?: Record<string, unknown>;
  assigneeId?: number;
  workflowDefinitionKey?: string;
}

export interface UpdateStatusRequest {
  status: string;
}

export interface GetTicketsParams {
  page?: number;
  pageSize?: number;
  size?: number;
  status?: string;
  priority?: string;
  tenantId?: number;
  templateId?: number;
}

// 服务目录相关接口（添加租户支持）
export interface ServiceCatalog {
  id: number;
  name: string;
  description: string;
  category: string;
  price?: number;
  tenantId: number;
  isActive: boolean;
  formSchema?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  tenant?: Tenant;
}

export interface ServiceRequest {
  id: number;
  catalogId: number;
  requesterId: number;
  tenantId: number;
  status: string;
  reason: string;
  formData?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  catalog?: ServiceCatalog;
  requester?: User;
  tenant?: Tenant;
}

// 角色相关接口
// 与后端 dto.RoleDTO 逐字段对齐
export interface Role {
  id: number;
  name: string;
  code?: string;
  description: string;
  permissions: string[];
  status?: 'active' | 'inactive';
  isSystem?: boolean;
  userCount?: number;
  /** 数据范围：all / department / owner */
  dataScope?: string;
  tenantId?: number;
  createdAt: string;
  updatedAt: string;
}

// 与后端 dto.RoleListResponse 逐字段对齐
export interface RoleListResponse {
  roles: Role[];
  total: number;
  page: number;
  pageSize: number;
  totalPages?: number;
}

export interface CreateRoleRequest {
  name: string;
  code?: string;
  description: string;
  permissions: string[];
  status: 'active' | 'inactive';
}

export interface UpdateRoleRequest {
  name?: string;
  code?: string;
  description?: string;
  permissions?: string[];
  status?: 'active' | 'inactive';
}

export interface GetRolesParams {
  page?: number;
  pageSize?: number;
  status?: string;
  search?: string;
}

export interface PermissionCatalogItem {
  id: number;
  code: string;
  name: string;
  description?: string;
  resource: string;
  action: string;
  tenantId?: number;
  createdAt?: string;
  updatedAt?: string;
}

// 系统配置相关接口
export interface SystemConfig {
  id: number;
  key: string;
  value: string;
  description?: string;
  category: string;
  createdAt: string;
  updatedAt: string;
}

export interface SystemConfigListResponse {
  items: SystemConfig[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface UpdateSystemConfigRequest {
  key: string;
  value: string;
  description?: string;
}

export interface GetSystemConfigsParams {
  category?: string;
  page?: number;
  pageSize?: number;
}
