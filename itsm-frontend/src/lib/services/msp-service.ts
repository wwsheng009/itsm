import { MSPAPI } from '@/lib/api/msp-api';
import type {
  MSPAllocation,
  CreateAllocationRequest,
  MSPCustomersResponse,
  MSPCustomerTicketsResponse,
  MSPCustomerTicket,
  MSPCustomerReport,
  MSPAllocationHistory,
  MSPContext,
} from '@/types/msp';
import type {
  ProvisionRoleOption,
  ProvisionUserRequest,
  ProvisionUserResponse,
} from '@/lib/api/api-config';

/**
 * 兼容读取形状：旧后端（领域模型直出）的 PascalCase 键。
 * 契约收口后新响应为 camelCase（types/msp.ts 的 MSPCustomerTicket）。
 */
type RawCustomerTicket = Partial<MSPCustomerTicket> & {
  ID?: number;
  TicketNumber?: string;
  Title?: string;
  Status?: string;
  Priority?: string;
  Type?: string;
  AssigneeID?: number;
  AssigneeName?: string;
  CreatedAt?: string;
  UpdatedAt?: string;
  TenantID?: number;
};

function pick<T>(...values: (T | undefined)[]): T | undefined {
  return values.find(value => value !== undefined);
}

/**
 * 归一化客户工单行：新契约已是 camelCase；PascalCase 兜底保证
 * 前后端版本偏差窗口内列表标题/状态不空白。
 */
function normalizeCustomerTicket(raw: RawCustomerTicket): MSPCustomerTicket {
  return {
    id: pick(raw.id, raw.ID) ?? 0,
    title: pick(raw.title, raw.Title) ?? '',
    status: pick(raw.status, raw.Status) ?? '',
    ticketNumber: pick(raw.ticketNumber, raw.TicketNumber),
    priority: pick(raw.priority, raw.Priority),
    type: pick(raw.type, raw.Type),
    assigneeId: pick(raw.assigneeId, raw.AssigneeID),
    assigneeName: pick(raw.assigneeName, raw.AssigneeName),
    createdAt: pick(raw.createdAt, raw.CreatedAt),
    updatedAt: pick(raw.updatedAt, raw.UpdatedAt),
    tenantId: pick(raw.tenantId, raw.TenantID),
    tenant: raw.tenant,
  };
}

export class MSPService {
  // 缓存当前用户 MSP 状态
  private static _isMSPUser: { isMSP: boolean; isAdmin: boolean } | null = null;
  private static mspContext: MSPContext | null = null;

  /**
   * 获取所有分配（MSP 员工）
   */
  static async getAllocations(params?: { page?: number; pageSize?: number }): Promise<{
    allocations: MSPAllocation[];
    total: number;
  }> {
    const res = await MSPAPI.getAllocations(params);
    return {
      allocations: res?.allocations ?? [],
      total: res?.total ?? 0,
    };
  }

  /**
   * 创建分配（MSP Manager）
   */
  static async createAllocation(data: CreateAllocationRequest): Promise<MSPAllocation> {
    // 业务失败由 httpClient 直接 throw（含后端 message），此处只做透传。
    return MSPAPI.createAllocation(data);
  }

  /**
   * 解除分配
   */
  static async deallocate(mspUserId: number, customerTenantId: number, reason?: string): Promise<void> {
    await MSPAPI.deallocate(mspUserId, customerTenantId, reason);
  }

  /**
   * 获取客户列表（MSP 视角）
   */
  static async getCustomers(): Promise<{
    customers: { id: number; code: string; name: string }[];
    total: number;
  }> {
    const res = await MSPAPI.getCustomers();
    return {
      customers: res?.customers ?? [],
      total: res?.total ?? 0,
    };
  }

  /**
   * 为客户建号（MSP 通道；业务失败由 httpClient 直接 throw）。
   */
  static async provisionCustomerUser(
    customerTenantId: number,
    payload: ProvisionUserRequest
  ): Promise<ProvisionUserResponse> {
    return MSPAPI.provisionCustomerUser(customerTenantId, payload);
  }

  /**
   * 为客户建号表单的角色候选（IP-P2-7；候选范围由服务端按通道/allocation 收口）。
   */
  static async listCustomerGrantableRoles(customerTenantId: number): Promise<ProvisionRoleOption[]> {
    return MSPAPI.listCustomerGrantableRoles(customerTenantId);
  }

  /**
   * 获取指定客户的工单
   */
  static async getCustomerTickets(
    customerTenantId: number,
    params?: { status?: string; page?: number; pageSize?: number }
  ): Promise<{ tickets: MSPCustomerTicket[]; total: number }> {
    const res = await MSPAPI.getCustomerTickets(customerTenantId, params);
    const raw: unknown = res?.tickets;
    const list: RawCustomerTicket[] = Array.isArray(raw)
      ? (raw as RawCustomerTicket[])
      : raw
        ? [raw as RawCustomerTicket]
        : [];
    return {
      tickets: list.map(normalizeCustomerTicket),
      total: res?.total || 0,
    };
  }

  /**
   * 为工单分配 MSP 技术员
   */
  static async assignTechnician(
    ticketId: number,
    customerTenantId: number,
    assignerUserId?: number
  ): Promise<{ id: number; status: string }> {
    return MSPAPI.assignTechnician(ticketId, customerTenantId, assignerUserId);
  }

  /**
   * 获取客户服务报表
   */
  static async getCustomerReports(params: {
    startDate: string;
    endDate: string;
    customerTenantId?: number;
  }): Promise<MSPCustomerReport[]> {
    const res = await MSPAPI.getCustomerReports(params);
    // OB-15：后端契约为 {reports, total}（逐客户行）；兼容历史裸数组返回，异常形状一律收敛为空数组。
    const payload: unknown = res;
    if (Array.isArray(payload)) {
      return payload as MSPCustomerReport[];
    }
    const raw = (payload as { reports?: MSPCustomerReport[] } | null | undefined)?.reports;
    return Array.isArray(raw) ? raw : [];
  }

  /**
   * 检查当前用户是否是 MSP 员工或管理员（带缓存）
   */
  static async isMSPUser(): Promise<{ isMSP: boolean; isAdmin: boolean }> {
    if (this._isMSPUser !== null) {
      return this._isMSPUser;
    }
    this._isMSPUser = await MSPAPI.isMSPUser();
    return this._isMSPUser;
  }

  /**
   * 获取当前 MSP 上下文（带缓存）
   */
  static async getMSPContext(): Promise<MSPContext | null> {
    if (this.mspContext !== null) {
      return this.mspContext;
    }
    const res = await MSPAPI.getMSPContext();
    if (res) {
      this.mspContext = res;
      return this.mspContext;
    }
    return null;
  }

  /**
   * 刷新 MSP 缓存
   */
  static refreshCache(): void {
    this._isMSPUser = null;
    this.mspContext = null;
  }

  /**
   * 获取分配历史
   */
  static async getAllocationHistory(params: {
    mspUserId?: number;
    customerTenantId?: number;
    startDate?: string;
    endDate?: string;
  }): Promise<MSPAllocationHistory[]> {
    const res = await MSPAPI.getAllocationHistory(params);
    return res || [];
  }
}

export default MSPService;
