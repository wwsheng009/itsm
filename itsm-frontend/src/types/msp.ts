/**
 * MSP (Managed Service Provider) 类型定义
 */

// 租户类型
export type TenantType = 'internal' | 'customer' | 'partner' | 'standard';

// MSP 角色
export type MSPRole = 'msp_tech' | 'msp_specialist' | 'msp_manager' | 'msp_viewer';

// 分配角色
export type AllocationRole = 'primary' | 'backup' | 'specialist';

// MSP 分配 DTO
export interface MSPAllocation {
  id: number;
  mspUserId: number;
  mspUsername?: string;
  customerTenantId: number;
  // 后端 DTO 字段为 customerTenantName（customerName 为历史遗留，兼容读取）。
  customerTenantName?: string;
  customerName?: string;
  role: AllocationRole;
  assignedAt: string;
  deassignedAt?: string;
}

// 创建分配请求
export interface CreateAllocationRequest {
  mspUserId: number;
  customerTenantId: number;
  role?: AllocationRole;
}

// 工单的 MSP 信息
export interface TicketMSPInfo {
  isManagedByMsp: boolean;
  mspProviderId?: number;
  mspProviderName?: string;
  managedByUserId?: number;
  managedByUsername?: string;
  mspTicketId?: string;
}

// 工作流节点的 MSP 配置
export interface WorkflowNodeMSPConfig {
  enableMspSupport: boolean;
  allowedMspRoles?: MSPRole[];
  requireMspApproval: boolean;
}

// MSP 上下文（用于中间件）
export interface MSPContext {
  isMsp: boolean;
  mspUserId: number;
  customerTenantId?: number;
  role?: string;
  allowedCustomers: number[];
}

// MSP 报表数据
export interface MSPCustomerReport {
  customerTenantId: number;
  customerName: string;
  period: string;
  totalTickets: number;
  resolvedTickets: number;
  mspHandlingTimeAvg: number; // 小时
  slaComplianceRate?: number | null; // 0-1；缺省/nil=暂无 SLA 样本（页面展示 '-'）
}

// MSP 分配历史
export interface MSPAllocationHistory {
  id: number;
  mspUserId: number;
  mspUsername: string;
  customerTenantId: number;
  customerName: string;
  role: AllocationRole;
  assignedAt: string;
  deassignedAt?: string;
  deallocationReason?: string;
  createdBy: number;
  createdByName?: string;
}

// API 响应类型
export interface MSPAllocationListResponse {
  allocations: MSPAllocation[];
  total: number;
}

export interface MSPCustomersResponse {
  customers: {
    id: number;
    code: string;
    name: string;
  }[];
  total: number;
}

// MSP 视角客户工单列表项（GET /msp/customers/:customer_tenant_id/tickets，camelCase 契约）。
// 历史缺陷：后端直出领域模型（PascalCase 键）导致 title/status 恒为空，列表标题与状态不显示。
export interface MSPCustomerTicket {
  id: number;
  ticketNumber?: string;
  title: string;
  status: string;
  priority?: string;
  type?: string;
  assigneeId?: number;
  assigneeName?: string;
  createdAt?: string;
  updatedAt?: string;
  tenantId?: number;
  tenant?: {
    id: number;
    code: string;
    name: string;
  };
}

export interface MSPCustomerTicketsResponse {
  tickets: MSPCustomerTicket[];
  total: number;
}

// 扩展 Tenant 类型
export interface Tenant extends Record<string, unknown> {
  id: number;
  name: string;
  code: string;
  domain?: string;
  type: TenantType;
  parentTenantId?: number;
  mspProviderId?: number;
  status: 'active' | 'suspended' | 'expired';
  expiresAt?: string;
  createdAt: string;
  updatedAt: string;

  // 关联关系
  parent?: Tenant;
  children?: Tenant[];
  mspProvider?: Tenant;
  mspCustomers?: Tenant[];
}

// 扩展 User 类型
export interface User extends Record<string, unknown> {
  id: number;
  username: string;
  email: string;
  tenantId: number;
  mspRole?: MSPRole;
  assignedByMspId?: number;
  createdAt: string;
  updatedAt: string;

  // 关联关系
  tenant?: Tenant;
  mspAllocations?: MSPAllocation[];
}

// 扩展 Ticket 类型
export interface Ticket extends Record<string, unknown> {
  id: number;
  tenantId: number;
  title: string;
  description?: string;
  status: string;
  assigneeId?: number;
  isManagedByMsp: boolean;
  mspProviderId?: number;
  managedByUserId?: number;
  mspTicketId?: string;
  createdAt: string;
  updatedAt: string;

  // 关联关系
  tenant?: Tenant;
  assignee?: User;
  mspProvider?: Tenant;
  mspUser?: User;
  mspInfo?: TicketMSPInfo;
}
