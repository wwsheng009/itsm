import { useEffect, useState, type ReactElement } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router';
import { useAuthStore } from '@/lib/store/auth-store';
import { bootstrapSession, type SessionStatus } from '@/lib/auth/session-bootstrap';
import RouteLoading from '@/components/common/RouteLoading';
import { resolveRoutePermission } from './route-permissions';

/**
 * 受保护路由守卫（替代 Next 中间件 `src/middleware.ts:6-48` 的 protectedRoutes 服务端 307）。
 *
 * 行为对齐迁移前：
 *  - 会话探活期间渲染骨架屏（不闪烁、不误踢）；
 *  - 未登录 → `/login?redirect=<原路径+query>`（replace，避免污染历史）；
 *  - 已登录 → 渲染子路由。
 *
 * 注意：原中间件的 protectedRoutes 前缀表（'/dashboard'、'/tickets' …）在路由树里已由
 * `(main)` 分组与 `RequireAuth` 包裹表达，语义一致（见 src/routes/index.tsx）。
 */
export function RequireAuth(): ReactElement {
  const location = useLocation();
  const [status, setStatus] = useState<SessionStatus>(() =>
    useAuthStore.getState().isAuthenticated ? 'authenticated' : 'pending'
  );

  useEffect(() => {
    if (status === 'authenticated') return;
    let alive = true;
    bootstrapSession().then(next => {
      if (alive) setStatus(next);
    });
    return () => {
      alive = false;
    };
    // 仅在首次挂载时探活；SPA 内部导航不重复检查，避免瞬时故障误踢用户
  }, []);

  if (status === 'pending') {
    return <RouteLoading />;
  }

  if (status === 'anonymous') {
    const redirect = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirect}`} replace />;
  }

  // IP-P1-5：首登强制改密——bootstrap 管理员（或任何 must_change_password 用户）
  // 在改密完成前只能停留在 /change-password，其余受保护路由一律收敛回改密页。
  const mustChangePassword = useAuthStore.getState().user?.mustChangePassword === true;
  if (mustChangePassword && location.pathname !== '/change-password') {
    return <Navigate to='/change-password' replace />;
  }

  // 系统性权限门禁（FE 路由级，fail-closed）：写入口路由（new/create/edit 等）在进入页面前
  // 按 route-permissions.ts 的映射表判定（权限码与后端 RequirePermission 逐条对齐），不通过
  // 直接落 /403——避免「填完表单才 403」。判定语义与后端 checkPermissionMatch 一致
  //（含 resource:admin 蕴含；super_admin 下发 `*` 直通）。
  const permissions = useAuthStore.getState().user?.permissions;
  if (resolveRoutePermission(location.pathname, permissions) === 'deny') {
    return <Navigate to="/403" replace />;
  }

  return <Outlet />;
}

interface RequireCapabilityProps {
  /** 命中任意一个权限码即放行（`*` / `resource:*` 由 store.hasPermission 统一匹配）。 */
  anyOf: string[];
  children?: ReactElement;
}

/**
 * 分组能力守卫（IP-P0-8 / FE-A7-A8；2026-10-10 治理域收口）：
 * - 单一权限模型：hasPermission()（store 单源，后端权限集下发）；
 * - **无角色名旁路**：此前 admin/super_admin 直接放行（isAdmin()）会掩盖"码不对齐"的页面
 *   （如租户管理员缺 department:write 仍可进，进页后接口 403）；super_admin 的 `*`
 *   由 hasPermission 全局通配覆盖，租户 admin 走显式码——前端门禁与后端 L2 同源；
 * - 不满足 → 独立 /403 路由（不再内联 Result 白屏）。
 *
 * 作为 pathless layout route 使用时可不传 children，命中后渲染 <Outlet />。
 */
export function RequireCapability({ anyOf, children }: RequireCapabilityProps): ReactElement {
  const hasPermission = useAuthStore(state => state.hasPermission);

  const allowed = anyOf.some(permission => hasPermission(permission));
  if (!allowed) {
    return <Navigate to="/403" replace />;
  }
  return children ?? <Outlet />;
}

/**
 * 平台治理面守卫（2026-10-10 收口）：租户管理（/admin/tenants）为平台专属面，
 *
 * 判定依据 = 后端 /auth/me 下发的 `isPlatformRole` 标记（词表单一源=middleware.PlatformRoleCodes，
 * 与后端 RequirePlatformAdmin 同词表）——前端不再手写角色名比对，避免双份词表漂移；
 * 标记缺失一律 /403（fail-closed；后端另有平台身份硬校验兜底）。
 */
export function RequirePlatformRole({ children }: { children?: ReactElement }): ReactElement {
  const isPlatformRole = useAuthStore(state => state.user?.isPlatformRole);
  if (!isPlatformRole) {
    return <Navigate to="/403" replace />;
  }
  return children ?? <Outlet />;
}
