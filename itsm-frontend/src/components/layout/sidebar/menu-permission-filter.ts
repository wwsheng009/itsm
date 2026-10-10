/**
 * 菜单挂载的权限复核（2026-10-10 菜单挂载收口）。
 *
 * 背景：后端 /api/v1/auth/menus 已按权限过滤，但菜单匹配曾用 action/resource 别名放宽
 * （如 system:write → system_config:write），使未授予入口权限的菜单仍被挂载
 * （tenant 管理员看到 /admin/tenants，点进去 403）。后端已收紧为严格匹配；
 * 前端在挂载点再用与路由守卫同一判定（permissionGranted）复核一次：
 *
 * - 有 permission_code 的菜单项：未命中即不挂载（fail-closed）；
 * - 无 permission_code 的公共项（如 /dashboard）：保留；
 * - 容器节点（有子节点）：自身未命中但任一子项存活时保留，避免子项孤儿化。
 */
import { permissionGranted } from '@/routes/route-permissions';
import type { MenuItem } from './menu-config';

export function filterMenusByPermission(
  items: MenuItem[],
  permissions: string[] | undefined
): MenuItem[] {
  return items.flatMap(item => {
    const children = item.children
      ? filterMenusByPermission(item.children, permissions)
      : undefined;
    const selfGranted = !item.permission || permissionGranted(permissions, item.permission);
    const hadChildren = !!(item.children && item.children.length > 0);
    const containerAlive = hadChildren && (children?.length ?? 0) > 0;

    if (!selfGranted && !containerAlive) {
      return [];
    }
    return [{ ...item, children }];
  });
}
