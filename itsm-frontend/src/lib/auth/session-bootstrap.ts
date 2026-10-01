import { httpClient } from '@/lib/api/http-client';
import { useAuthStore } from '@/lib/store/auth-store';
import { mapServerTenant, mapServerUser } from '@/lib/auth/session-mappers';

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

  // IP-P0-8：作用域以服务端为准 —— /auth/me 的 tenantId 即当前 JWT 作用域。
  // 候选列表（/auth/tenants = home ∪ allocation ∪ 平台全量）仅用于深度切换入口，
  // 绝不参与自动选租户（移除历史 tenants[0] 强制）。
  const tenants: unknown[] = Array.isArray(tenantInfo?.tenants) ? tenantInfo.tenants : [];
  const serverTenantId = Number(userInfo?.tenantId || 0);
  const matchedTenantRaw =
    serverTenantId > 0
      ? tenants.find(t => Number((t as Record<string, unknown>)?.id) === serverTenantId)
      : undefined;
  const currentTenant = mapServerTenant(matchedTenantRaw);

  const { login, setCurrentTenant } = useAuthStore.getState();
  login(
    mapServerUser(userInfo),
    'authenticated',
    currentTenant ?? undefined
  );

  if (currentTenant) {
    setCurrentTenant(currentTenant);
  }

  return 'authenticated';
}
