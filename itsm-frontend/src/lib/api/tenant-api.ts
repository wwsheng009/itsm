import { httpClient } from './http-client';
import type {
  Tenant,
  TenantListResponse,
  CreateTenantRequest,
  UpdateTenantRequest,
  GetTenantsParams,
} from './api-config';

/** POST /api/v1/auth/switch-tenant 响应（与登录同构，仅取会话所需字段）。 */
export interface SwitchTenantResponse {
  accessToken?: string;
  refreshToken?: string;
  user?: Record<string, unknown>;
  tenant?: Tenant;
  tenantSelection?: { mode?: string; autoSelected?: boolean; reason?: string };
}

export class TenantAPI {
  // 获取租户列表
  static async getTenants(params?: GetTenantsParams): Promise<TenantListResponse> {
    return httpClient.get<TenantListResponse>('/api/v1/tenants', params);
  }

  // 获取单个租户
  static async getTenant(id: number): Promise<Tenant> {
    return httpClient.get<Tenant>(`/api/v1/tenants/${id}`);
  }

  // 创建租户
  static async createTenant(data: CreateTenantRequest): Promise<Tenant> {
    return httpClient.post<Tenant>('/api/v1/tenants', data);
  }

  // 更新租户
  static async updateTenant(id: number, data: UpdateTenantRequest): Promise<Tenant> {
    return httpClient.put<Tenant>(`/api/v1/tenants/${id}`, data);
  }

  // 删除租户
  static async deleteTenant(id: number): Promise<void> {
    return httpClient.delete<void>(`/api/v1/tenants/${id}`);
  }

  // 获取当前用户的租户信息
  static async getCurrentTenant(): Promise<Tenant> {
    return httpClient.get<Tenant>('/api/v1/tenants/current');
  }

  // 深度切换作用域（IP-P0-6 契约端点）：POST /api/v1/auth/switch-tenant。
  static async switchTenant(tenantId: number): Promise<SwitchTenantResponse> {
    return httpClient.post<SwitchTenantResponse>('/api/v1/auth/switch-tenant', { tenantId });
  }

  // 认证后的可访问租户候选（home ∪ allocation ∪ 平台全量；仅用于深度切换入口）。
  static async getMyTenants(): Promise<{ tenants: Tenant[] }> {
    return httpClient.get<{ tenants: Tenant[] }>('/api/v1/auth/tenants');
  }
}
