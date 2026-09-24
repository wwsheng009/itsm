import { httpClient } from '@/lib/api/http-client';
import { useAuthStore } from '@/lib/store/auth-store';
import type { Tenant } from '@/lib/api/api-config';

export type SessionStatus = 'pending' | 'authenticated' | 'anonymous';

/**
 * 客户端会话探活（替代 Next 中间件的服务端 JWT 校验）。
 *
 * 逻辑与迁移前 `src/app/(main)/layout.tsx` 内的 checkAuth 完全一致：
 *  - 并行/依次请求 `/api/v1/auth/me` 与 `/api/v1/auth/tenants`；
 *  - 两者都失败且 store 也没有会话时才判定为匿名（接口瞬时故障不踢人）；
 *  - 任一成功则写入 auth store（login + 租户上下文）。
 *
 * 结果在模块级缓存（in-flight promise），避免守卫与布局重复请求。
 */
let inFlight: Promise<SessionStatus> | null = null;

export function bootstrapSession(force = false): Promise<SessionStatus> {
  if (force) inFlight = null;
  if (!inFlight) {
    inFlight = run().catch(error => {
      console.error('Session bootstrap failed:', error);
      inFlight = null;
      return useAuthStore.getState().isAuthenticated ? 'authenticated' : 'anonymous';
    });
  }
  return inFlight;
}

/** 测试/登出后可重置缓存 */
export function resetSessionBootstrap(): void {
  inFlight = null;
}

async function run(): Promise<SessionStatus> {
  let userInfo: any = null;
  let tenantInfo: any = null;

  try {
    userInfo = await httpClient.get<any>('/api/v1/auth/me');
  } catch (e) {
    console.error('Failed to fetch user info:', e);
  }

  try {
    tenantInfo = await httpClient.get<any>('/api/v1/auth/tenants');
  } catch (e) {
    console.error('Failed to fetch tenant info:', e);
  }

  if (!userInfo && !tenantInfo) {
    const { isAuthenticated: storeIsAuth } = useAuthStore.getState();
    if (!storeIsAuth) {
      return 'anonymous';
    }
    console.warn('Auth check endpoints failed but store session exists; keeping session');
    return 'authenticated';
  }

  const tenants = Array.isArray(tenantInfo?.tenants) ? tenantInfo.tenants : [];
  const currentTenant = tenants[0];

  const { login, setCurrentTenant } = useAuthStore.getState();
  login(
    {
      id: Number(userInfo?.id || 0),
      username: String(userInfo?.username || ''),
      email: String(userInfo?.email || ''),
      name: String(userInfo?.name || ''),
      role: String(userInfo?.role || 'end_user'),
      department: userInfo?.department,
      tenantId: userInfo?.tenantId ? Number(userInfo.tenantId) : undefined,
      permissions: userInfo?.permissions,
      createdAt: userInfo?.createdAt ?? new Date().toISOString(),
      updatedAt: userInfo?.updatedAt ?? new Date().toISOString(),
    },
    'authenticated',
    currentTenant
      ? {
          id: Number(currentTenant.id),
          name: String(currentTenant.name),
          code: String(currentTenant.code),
          type: currentTenant.type,
          status: currentTenant.status,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        }
      : undefined
  );

  if (currentTenant) {
    const tenantData: Tenant = {
      id: Number(currentTenant.id),
      name: String(currentTenant.name),
      code: String(currentTenant.code),
      type: currentTenant.type || 'standard',
      status: currentTenant.status || 'active',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    setCurrentTenant(tenantData);
  }

  return 'authenticated';
}
