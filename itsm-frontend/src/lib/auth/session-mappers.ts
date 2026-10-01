import type { Tenant, User } from '@/lib/api/api-config';

/**
 * 服务端用户对象 → 前端 User（宽松映射，仅保留稳定字段）。
 * 用于 /auth/me 与 switch-tenant 后的会话重建，避免各处手写映射漂移。
 */
export function mapServerUser(raw: unknown): User {
  const src = (raw ?? {}) as Record<string, unknown>;
  return {
    id: Number(src.id || 0),
    username: String(src.username || ''),
    email: String(src.email || ''),
    name: String(src.name || ''),
    tenantId: src.tenantId ? Number(src.tenantId) : undefined,
    role: String(src.role || 'end_user'),
    mustChangePassword: Boolean(src.mustChangePassword),
    mspRole: typeof src.mspRole === 'string' ? src.mspRole : undefined,
    department: typeof src.department === 'string' ? src.department : undefined,
    permissions: Array.isArray(src.permissions) ? (src.permissions as string[]) : undefined,
    createdAt: (src.createdAt as string) ?? new Date().toISOString(),
    updatedAt: (src.updatedAt as string) ?? new Date().toISOString(),
  };
}

/** 服务端租户对象 → 前端 Tenant（tenant-context / 持久化所需字段）。 */
export function mapServerTenant(raw: unknown): Tenant | null {
  if (!raw || typeof raw !== 'object') return null;
  const src = raw as Record<string, unknown>;
  if (src.id === undefined || src.id === null) return null;
  return {
    id: Number(src.id),
    name: String(src.name ?? ''),
    code: String(src.code ?? ''),
    domain: typeof src.domain === 'string' ? src.domain : undefined,
    type: (src.type || 'standard') as Tenant['type'],
    status: (src.status || 'active') as Tenant['status'],
    createdAt: (src.createdAt as string) ?? new Date().toISOString(),
    updatedAt: (src.updatedAt as string) ?? new Date().toISOString(),
  };
}
