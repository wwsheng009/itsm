import { MSPAPI } from '@/lib/api/msp-api';
import type {
  MSPAllocation,
  CreateAllocationRequest,
  MSPCustomersResponse,
  MSPCustomerTicketsResponse,
  MSPCustomerReport,
  MSPAllocationHistory,
  MSPContext,
  TicketMSPInfo,
} from '@/types/msp';
import type {
  ProvisionRoleOption,
  ProvisionUserRequest,
  ProvisionUserResponse,
} from '@/lib/api/api-config';

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
  ): Promise<{
    tickets: Array<TicketMSPInfo & {
      id: number;
      title: string;
      status: string;
      assigneeId?: number;
      assigneeName?: string;
      createdAt: string;
      tenant: { id: number; code: string; name: string };
    }>;
    total: number;
  }> {
    const res = await MSPAPI.getCustomerTickets(customerTenantId, params);
    const raw = res?.tickets;
    const tickets = Array.isArray(raw) ? raw : raw ? [raw] : [];
    return {
      tickets,
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
