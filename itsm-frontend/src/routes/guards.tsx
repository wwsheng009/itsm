import { useEffect, useState, type ReactElement } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router';
import { useAuthStore } from '@/lib/store/auth-store';
import { bootstrapSession, type SessionStatus } from '@/lib/auth/session-bootstrap';
import RouteLoading from '@/components/common/RouteLoading';

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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (status === 'pending') {
    return <RouteLoading />;
  }

  if (status === 'anonymous') {
    const redirect = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirect}`} replace />;
  }

  return <Outlet />;
}
