import { httpClient } from './http-client';
import type { HttpClientError } from './http-client';

/**
 * 平台侧租户用户管理 API（TUM-1/TUM-2 契约）。
 * 单一源：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §3.1/§3.5
 * 与 itsm-backend/dto/tenant_user_admin_dto.go；URL/载荷变更必须同步两侧。
 */

export type TenantUserResetMode = 'generated' | 'specified';

/** 平台视角租户用户条目（不含敏感字段）。 */
export interface TenantUserItem {
  id: number;
  username: string;
  name: string;
  email: string;
  role: string;
  mspRole?: string;
  department?: string;
  active: boolean;
  mustChangePassword: boolean;
  isBootstrapAdmin: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface TenantUserListParams {
  page?: number;
  pageSize?: number;
  /** 用户名/姓名/邮箱模糊匹配（后端 form 名 keyword） */
  keyword?: string;
  status?: 'active' | 'inactive';
  role?: string;
}

export interface TenantUserListResponse {
  items: TenantUserItem[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ResetTenantUserPasswordRequest {
  mode: TenantUserResetMode;
  /** mode=specified 时必填；generated 忽略 */
  newPassword?: string;
}

export interface ResetTenantUserPasswordResponse {
  userId: number;
  mode: TenantUserResetMode;
  /** 一次性口令明文，仅本次响应回传；关闭卡片后不可再取 */
  generatedPassword?: string;
  mustChangePassword: boolean;
}

export interface SetTenantUserStatusRequest {
  active: boolean;
  /** default 平台租户停用时必填租户编码（防误操作） */
  confirmCode?: string;
}

export interface ForceLogoutTenantUserResponse {
  userId: number;
  revokedAt: string;
}

/** IP-P2-8 D3=B：平台侧离职编排结果（与 provider 自服务面同形）。 */
export interface OffboardTenantUserResponse {
  userId: number;
  deactivated: boolean;
  alreadyInactive: boolean;
  allocationsRevoked: number;
  allocationsPending: number;
}

/** §3.5 稳定错误码 → 可读文案；未知码回落后端 message。 */
export const TENANT_USER_ADMIN_ERROR_MESSAGES: Record<string, string> = {
  PLATFORM_SCOPE_REQUIRED: '仅平台管理员可执行该操作',
  TENANT_USER_ADMIN_DISABLED: '租户用户管理写通道未开启，请联系平台运维开启 TENANT_USER_ADMIN_ENABLED',
  TENANT_NOT_FOUND: '目标租户不存在',
  TENANT_USER_NOT_FOUND: '目标用户不存在或不属于该租户',
  INVALID_PARAM: '参数不合法，请检查后重试',
  PASSWORD_POLICY_VIOLATION: '新密码不满足该租户的密码策略',
  INVALID_RESET_MODE: '重置模式不合法',
  LAST_ADMIN_PROTECTED: '该租户最后一个管理员不可停用，请先指派新管理员',
  SELF_OPERATION_FORBIDDEN: '不能停用当前登录账号',
  TENANT_CONFIRM_REQUIRED: '该租户为平台默认租户，停用需输入租户编码确认',
};

export function tenantUserAdminErrorMessage(error: unknown): string {
  const e = error as HttpClientError | undefined;
  const code = e?.errorCode;
  if (code && TENANT_USER_ADMIN_ERROR_MESSAGES[code]) {
    return TENANT_USER_ADMIN_ERROR_MESSAGES[code];
  }
  return e?.message || '操作失败，请稍后重试';
}

export class TenantUserAdminApi {
  private static base(tenantId: number): string {
    return `/api/v1/tenants/${tenantId}/users`;
  }

  /** GET /tenants/:id/users（分页/关键字/状态/角色过滤） */
  static async listUsers(
    tenantId: number,
    params: TenantUserListParams = {}
  ): Promise<TenantUserListResponse> {
    return httpClient.get<TenantUserListResponse>(this.base(tenantId), params);
  }

  /** GET /tenants/:id/users/:userId（(tenant_id,user_id) 双键） */
  static async getUser(tenantId: number, userId: number): Promise<TenantUserItem> {
    return httpClient.get<TenantUserItem>(`${this.base(tenantId)}/${userId}`);
  }

  /** POST /tenants/:id/users/:userId/reset-password（generated | specified） */
  static async resetPassword(
    tenantId: number,
    userId: number,
    body: ResetTenantUserPasswordRequest
  ): Promise<ResetTenantUserPasswordResponse> {
    return httpClient.post<ResetTenantUserPasswordResponse>(
      `${this.base(tenantId)}/${userId}/reset-password`,
      body
    );
  }

  /** PUT /tenants/:id/users/:userId/status（停用=吊销存量 token；default 需 confirmCode） */
  static async setStatus(
    tenantId: number,
    userId: number,
    body: SetTenantUserStatusRequest
  ): Promise<void> {
    await httpClient.put<void>(`${this.base(tenantId)}/${userId}/status`, body);
  }

  /** POST /tenants/:id/users/:userId/force-logout（仅吊销会话） */
  static async forceLogout(
    tenantId: number,
    userId: number
  ): Promise<ForceLogoutTenantUserResponse> {
    return httpClient.post<ForceLogoutTenantUserResponse>(
      `${this.base(tenantId)}/${userId}/force-logout`,
      {}
    );
  }

  /** POST /tenants/:id/users/:userId/offboard（停用 + 全量会话失效 + 撤销全部分配；幂等可重跑） */
  static async offboardUser(
    tenantId: number,
    userId: number
  ): Promise<OffboardTenantUserResponse> {
    return httpClient.post<OffboardTenantUserResponse>(
      `${this.base(tenantId)}/${userId}/offboard`,
      {}
    );
  }
}
