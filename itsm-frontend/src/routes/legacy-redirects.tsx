import { Navigate, Outlet, useLocation } from 'react-router';
import { ROUTE_PATHS } from './route-paths';

/**
 * 历史遗留菜单路径 → 正确路由。
 * 原实现：`src/middleware.ts:65` 的 LEGACY_MENU_REDIRECTS（服务端 307）。
 * 迁移后必须保持 replace 语义（否则浏览器历史会被污染）。
 */
export const LEGACY_MENU_REDIRECTS: Record<string, string> = {
  // /list 后缀 → 模块根路径（App Router 下 xxx/page.tsx 即列表首页）
  '/service-requests/list': '/service-requests',
  '/incidents/list': '/incidents',
  '/problems/list': '/problems',
  '/changes/list': '/changes',
  '/knowledge/list': '/knowledge',
  '/service-catalog/list': '/service-catalog',
  '/assets/list': '/assets',
  '/workflow/list': '/workflow',
  '/ai/chat/list': '/ai/chat',
  '/msp/list': '/msp',
  '/releases/list': '/releases',
  // 命名错误：/admin/overview 页面加载后会客户端跳转到 /admin，直接指向 /admin 避免两跳
  '/admin/index': '/admin',
  '/knowledge/articles/create': '/knowledge/articles/new',
  // 缺少独立页面的入口（模块主页本身就是概览/会话首页）
  '/sla/overview': '/sla',
  '/email-intake/conversations': '/email-intake',
  '/knowledge/articles': '/knowledge',
};

/**
 * 兜底：对任意 /xxx/list 路径（且不在显式映射中、且剥离后不是真实路由）剥离 /list。
 * 等价原 `tryStripListSuffix`。
 */
export function legacyRedirectTarget(pathname: string): string | null {
  const exact = LEGACY_MENU_REDIRECTS[pathname];
  if (exact && exact !== pathname) {
    return exact;
  }
  if (pathname.endsWith('/list') && pathname.length > 6) {
    const stripped = pathname.slice(0, -5) || '/';
    return ROUTE_PATHS.has(stripped) ? stripped : null;
  }
  return null;
}

/**
 * 挂在路由树根部的守卫：命中历史路径时先 replace 到正确地址（保留 query），
 * 再做后续的认证保护 —— 与原中间件"先重定向、再鉴权"的顺序一致。
 */
export function LegacyRedirectGate() {
  const location = useLocation();
  const target = legacyRedirectTarget(location.pathname);
  if (target) {
    return <Navigate to={`${target}${location.search}`} replace />;
  }
  return <Outlet />;
}
