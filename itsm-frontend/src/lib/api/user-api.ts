import { httpClient } from './http-client';

// 用户相关的接口定义

/**
 * 主角色词表。单一源 = 后端 domain/role 包（domain/role/role.go）；
 * security 为存量 legacy 值，新代码禁用。
 */
export type UserRole =
  | 'super_admin'
  | 'admin'
  | 'manager'
  | 'it_admin'
  | 'security_admin'
  | 'sysadmin'
  | 'agent'
  | 'technician'
  | 'security'
  | 'end_user';

/**
 * 主角色下拉选项（中文标签）。词表与后端 domain/role 包一一对应；
 * 组管理页成员选择器的角色筛选也复用此词表。
 */
export const PRIMARY_ROLE_OPTIONS: { value: UserRole; label: string }[] = [
  { value: 'end_user', label: '最终用户' },
  { value: 'agent', label: '服务台坐席' },
  { value: 'technician', label: '技术员' },
  { value: 'manager', label: '部门经理' },
  { value: 'it_admin', label: 'IT管理员' },
  { value: 'security_admin', label: '安全管理员' },
  { value: 'sysadmin', label: '系统运维' },
  { value: 'admin', label: '系统管理员' },
  { value: 'super_admin', label: '超级管理员' },
];

export const PRIMARY_ROLE_LABEL: Record<string, string> = Object.fromEntries(
  PRIMARY_ROLE_OPTIONS.map(o => [o.value, o.label])
);

/**
 * 统一角色 code→中文标签表（覆盖全部可见角色 code：主角色 / RBAC 角色码 /
 * MSP 身份角色码 / 平台·租户治理角色码）。
 * 词表单一源：与后端 domain/role、middleware.PlatformRoleCodes、
 * users.msp_role 枚举保持一致；PRIMARY_ROLE_LABEL 仅覆盖主角色子集。
 */
export const ROLE_LABEL_MAP: Record<string, string> = {
  // 主角色（与 PRIMARY_ROLE_LABEL 保持一致）
  ...PRIMARY_ROLE_LABEL,
  // 平台 / 服务商 / 租户治理角色
  platform_admin: '平台管理员',
  provider_admin: '服务商经理',
  provider_agent: '服务商坐席',
  provider_tenant_admin: '服务商租户管理员',
  tenant_admin: '租户管理员',
  customer_user: '客户用户',
  // 历史 / 兼容别名
  user: '用户',
  // MSP 内部角色别名（membership 映射：msp_manager / msp_tech / msp_admin …）
  msp_manager: '服务商经理',
  msp_tech: '服务商技术员',
  msp_viewer: '服务商观察员',
  msp_admin: '服务商管理员',
  msp_specialist: '服务商专员',
  msp_engineer: '服务商工程师',
  msp_delivery: '服务商交付专员',
  msp_support: '服务商支持工程师',
  // 通用岗位
  specialist: '专员',
  security: '安全人员',
};

/** 未知角色 code 的兜底中文标签。 */
export const DEFAULT_ROLE_LABEL = '用户';

/** 形如 customer_user / it_admin 的裸标识符正则（区分「未映射 code」与「自定义角色名」）。 */
const ROLE_CODE_RE = /^[a-z][a-zA-Z0-9_]*$/;

/**
 * 把单个角色 token（code 或角色名）映射为中文标签。
 * - 命中词表 → 中文标签；
 * - 形如裸 code（ASCII 标识符）→ 未映射 code，返回 null（不透出原始枚举）；
 * - 其余（含中文的自定义角色名）→ 原样返回。
 */
export function mapRoleToken(token?: string | null): string | null {
  if (!token) return null;
  if (ROLE_CODE_RE.test(token) && !ROLE_LABEL_MAP[token]) return null;
  return ROLE_LABEL_MAP[token] ?? token;
}

/** 取角色 code 的中文标签，未知时回退兜底（用于顶栏等「必须展示标签」的场景）。 */
export function getRoleLabel(code?: string | null): string {
  return mapRoleToken(code) ?? DEFAULT_ROLE_LABEL;
}

/**
 * 把「主角色 + MSP 角色 + RBAC 角色名」统一映射为去重的中文标签数组。
 * 用于用户/成员列表的角色列：未映射的裸 code 一律不透出；
 * 多角色由调用方以数量徽标统一呈现。
 */
export function getUserRoleLabels(input: {
  role?: string | null;
  mspRole?: string | null;
  roleNames?: string[] | null;
}): string[] {
  const labels = new Set<string>();
  const add = (token?: string | null) => {
    const label = mapRoleToken(token);
    if (label) labels.add(label);
  };
  add(input.role);
  add(input.mspRole);
  (input.roleNames ?? []).forEach(add);
  return Array.from(labels);
}

/**
 * 解析「有效角色」标签（顶栏等场景）：
 * - 优先 MSP 身份角色（服务商上下文，如 provider_admin）；
 * - 否则取主角色；
 * - 未知时回退兜底，绝不裸落原始 code。
 */
export function resolveEffectiveRoleLabel(input: {
  role?: string | null;
  mspRole?: string | null;
}): string {
  if (input.mspRole) {
    const label = mapRoleToken(input.mspRole);
    if (label) return label;
  }
  return getRoleLabel(input.role);
}

export interface User {
  id: number;
  username: string;
  email: string;
  name: string;
  department: string;
  phone: string;
  active: boolean;
  tenantId: number;
  /** 主角色（users.role 枚举，词表单一源=后端 domain/role） */
  role: string;
  /** RBAC 多角色 ID（user_roles M2M 边） */
  roleIds?: number[];
  /** RBAC 多角色名称（仅展示用） */
  roleNames?: string[];
  /** MSP 身份角色（provider_admin / msp_specialist 等；客户账号为空）。 */
  mspRole?: string | null;
  createdAt: string;
  updatedAt: string;
  /** Q2=B / MSW-2：客户侧成员列表的服务方成员标记（provider + readOnly=true）。 */
  accountKind?: string;
  readOnly?: boolean;
}

export interface CreateUserRequest {
  username: string;
  email: string;
  name: string;
  department: string;
  phone: string;
  password: string;
  tenantId: number;
  /** 主角色；不传时后端默认 end_user */
  role?: string;
  /** RBAC 多角色 ID（写入 user_roles 边） */
  roleIds?: number[];
}

export interface UpdateUserRequest {
  username?: string;
  email?: string;
  name?: string;
  department?: string;
  phone?: string;
  /** 主角色变更（后端会吊销存量 token） */
  role?: string;
  /** RBAC 多角色替换集：非 undefined 时整体替换；[] 表示清空 */
  roleIds?: number[];
}

export interface ListUsersParams {
  page?: number;
  pageSize?: number;
  tenantId?: number;
  status?: string;
  department?: string;
  search?: string;
}

export interface PagedUsersResponse {
  users: User[];
  /** 客户租户内服务方成员（只读区块；独立字段，不计入分页 total）。 */
  providerMembers?: User[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
    totalPage: number;
  };
}

export interface UserStatsResponse {
  total: number;
  active: number;
  inactive: number;
}

export interface BatchUpdateUsersRequest {
  userIds: number[];
  action: 'activate' | 'deactivate' | 'department';
  department?: string;
}

export interface SearchUsersParams {
  keyword: string;
  tenantId?: number;
  limit?: number;
}

/** IP-P2-8：离职编排结果（停用 + 全量会话失效 + 撤销全部分配计数）。 */
export interface OffboardUserResponse {
  userId: number;
  deactivated: boolean;
  alreadyInactive: boolean;
  allocationsRevoked: number;
  allocationsPending: number;
}

export class UserApi {
  private static baseURL = '/api/v1/users';

  // 获取用户列表
  static async getUsers(params: ListUsersParams = {}): Promise<PagedUsersResponse> {
    return httpClient.get<PagedUsersResponse>(this.baseURL, params);
  }

  // 创建用户
  static async createUser(userData: CreateUserRequest): Promise<User> {
    return httpClient.post<User>(this.baseURL, userData);
  }

  // 获取用户详情
  static async getUserById(id: number): Promise<User> {
    return httpClient.get<User>(`${this.baseURL}/${id}`);
  }

  // 更新用户信息
  static async updateUser(id: number, userData: UpdateUserRequest): Promise<User> {
    return httpClient.put<User>(`${this.baseURL}/${id}`, userData);
  }

  // 自助资料更新（剩余缺口治理 B3）：PUT /api/v1/users/me —— 仅本人；
  // 字段白名单 name/email/phone（部门/角色等治理字段由管理员维护），无需 user:write。
  static async updateMyProfile(data: {
    name?: string;
    email?: string;
    phone?: string;
  }): Promise<{ id: number; username: string; name: string; email: string; phone: string }> {
    return httpClient.put(`${this.baseURL}/me`, data);
  }

  // 删除用户（软删除）
  static async deleteUser(id: number): Promise<void> {
    await httpClient.delete(`${this.baseURL}/${id}`);
  }

  // 更改用户状态
  static async changeUserStatus(id: number, active: boolean): Promise<void> {
    await httpClient.put(`${this.baseURL}/${id}/status`, { active });
  }

  // 重置用户密码
  static async resetPassword(id: number, newPassword: string): Promise<void> {
    await httpClient.put(`${this.baseURL}/${id}/reset-password`, { newPassword: newPassword });
  }

  // 离职编排（IP-P2-8）：停用账号 + 全量会话失效 + 撤销全部分配（幂等可重跑）。
  static async offboardUser(id: number): Promise<OffboardUserResponse> {
    return httpClient.post<OffboardUserResponse>(`${this.baseURL}/${id}/offboard`);
  }

  // 获取用户统计
  static async getUserStats(tenantId?: number): Promise<UserStatsResponse> {
    const params = tenantId ? { tenantId: tenantId } : {};
    return httpClient.get<UserStatsResponse>(`${this.baseURL}/stats`, params);
  }

  // 批量更新用户
  static async batchUpdateUsers(request: BatchUpdateUsersRequest): Promise<void> {
    await httpClient.put(`${this.baseURL}/batch`, request);
  }

  // 搜索用户
  static async searchUsers(params: SearchUsersParams): Promise<User[]> {
    return httpClient.get<User[]>(`${this.baseURL}/search`, params);
  }

  // 导出用户
  static async exportUsers(filters?: Record<string, unknown>): Promise<Blob> {
    const blob = await httpClient.request<Blob>({
      method: 'GET',
      url: `${this.baseURL}/export`,
      params: filters,
      responseType: 'blob',
    });
    return blob;
  }

  // 导入用户
  static async importUsers(file: File): Promise<{
    success: User[];
    failed: Array<{ index: number; user: CreateUserRequest; error: string }>;
    total: number;
    processed: number;
  }> {
    const formData = new FormData();
    formData.append('file', file);

    return httpClient.post(`${this.baseURL}/import`, formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    });
  }
}
