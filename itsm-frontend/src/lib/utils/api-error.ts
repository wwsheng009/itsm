import type { HttpClientError } from '@/lib/api/http-client';

/**
 * 403（HTTP 403 / 业务码 2003「权限不足」）判定。
 *
 * 路由级（routes/route-permissions.ts + RequireAuth）与入口级（PermissionGate）门禁
 * 已把绝大多数无权限场景挡在提交前；本判定用于**兜底**：会话权限被服务端收回、
 * 跨租户切换后作用域变化等竞态下，把"事后错误"提示语义化，而不是裸错误串。
 */
export function isForbiddenError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false;
  const e = error as Partial<HttpClientError>;
  return e.httpStatus === 403 || e.code === 2003;
}
