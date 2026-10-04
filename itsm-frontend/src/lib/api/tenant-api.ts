import { httpClient } from './http-client';
import type {
  Tenant,
  TenantListResponse,
  CreateTenantRequest,
  UpdateTenantRequest,
  GetTenantsParams,
  TenantQuotaUsageResponse,
  TenantReadinessResponse,
  BootstrapAdminRequest,
  BootstrapAdminResponse,
} from './api-config';

/** 模板供给是逐项落库的幂等长事务，默认 30s 不够，单独放宽到 120s。 */
const PROVISION_TIMEOUT_MS = 120_000;
/** 首管创建含密码策略与默认角色装配，放宽到 60s。 */
const BOOTSTRAP_ADMIN_TIMEOUT_MS = 60_000;

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

  // 租户硬配额用量（IP-P2-6 收尾；上限与用量同源，口径与写入校验一致）。
  static async getTenantUsage(id: number): Promise<TenantQuotaUsageResponse> {
    return httpClient.get<TenantQuotaUsageResponse>(`/api/v1/tenants/${id}/usage`);
  }

  // 租户开通闭环：供给就绪度（7 项 required 模板资源 + 首管数量）。
  static async getTenantReadiness(id: number): Promise<TenantReadinessResponse> {
    return httpClient.get<TenantReadinessResponse>(`/api/v1/tenants/${id}/readiness`);
  }

  // 模板供给（幂等；重复调用返回同一 readiness 结构）。request 通道才能携带 timeout。
  static async provisionTenant(
    id: number,
    templateVersion?: string
  ): Promise<TenantReadinessResponse> {
    return httpClient.request<TenantReadinessResponse>(`/api/v1/tenants/${id}/provision`, {
      method: 'POST',
      body: JSON.stringify({ templateVersion }),
      timeout: PROVISION_TIMEOUT_MS,
    });
  }

  // 创建首个管理员；已存在时后端返回 HTTP 409 / envelope code 4090（由调用方降级为“已创建”）。
  static async createBootstrapAdmin(
    id: number,
    payload: BootstrapAdminRequest = {}
  ): Promise<BootstrapAdminResponse> {
    return httpClient.request<BootstrapAdminResponse>(`/api/v1/tenants/${id}/bootstrap-admin`, {
      method: 'POST',
      body: JSON.stringify(payload),
      timeout: BOOTSTRAP_ADMIN_TIMEOUT_MS,
    });
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
