/**
 * 路由级权限门禁守卫测试（对标后端 router/route_permission_guard_test.go）。
 *
 * 规则：
 * - A（fail-closed）：routes/index.tsx 中所有写入口路径（new/create/edit，含 ai-create）
 *   必须命中 ROUTE_PERMISSIONS；新增未声明的写入口 → 本测试红；
 * - B：ROUTE_PERMISSIONS 每条规则必须匹配到真实路由（防僵尸规则/路径漂移）；
 * - 判定语义：permissionGranted 与后端 middleware/rbac.go checkPermissionMatch 对齐
 *   （`*` / `resource:*` / `resource:admin` 蕴含；MCP 例外）。
 */
// routes/index.tsx 顶层 import 了 RootProviders（@/App → GlobalShortcutProvider →
// react-hotkeys-hook，ESM 未进 jest transform 白名单）。本测试只枚举路由表、不渲染，
// 故替身掉 App，避免加载 UI 依赖。
jest.mock('@/App', () => ({ __esModule: true, default: () => null }));

import type { RouteObject } from 'react-router';
import { matchPath } from 'react-router';
import {
  ROUTE_PERMISSIONS,
  matchRoutePermission,
  permissionGranted,
  resolveRoutePermission,
} from '../route-permissions';
import { canUseMspWorkbench } from '../area-permissions';
import { routes } from '../index';

/** 递归收集路由表全部路径（拼接父子段；无 path 的布局路由只递归不产出）。 */
function collectRoutePaths(routeObjects: RouteObject[], parent = ''): string[] {
  const out: string[] = [];
  for (const route of routeObjects) {
    const segment = route.path ?? '';
    const full = segment.startsWith('/')
      ? segment.slice(1)
      : [parent, segment].filter(Boolean).join('/');
    if (segment) out.push(full);
    if (route.children?.length) out.push(...collectRoutePaths(route.children, full));
  }
  return out;
}

/** 写入口路径：任一路径段为 new/create/edit，或以 -create 结尾（如 ai-create）。 */
function isWriteEntryPath(p: string): boolean {
  return p
    .split('/')
    .filter(Boolean)
    .some(s => s === 'new' || s === 'create' || s === 'edit' || s.endsWith('-create'));
}

/** 显式豁免（必须写明理由）；当前为空——所有写入口均已登记权限码。 */
const WRITE_ENTRY_EXEMPTIONS: Record<string, string> = {};

describe('路由级权限门禁（route-permissions 守卫）', () => {
  const routePaths = collectRoutePaths(routes);

  it('路由表可枚举（防扫描失效）', () => {
    expect(routePaths.length).toBeGreaterThan(100);
    expect(routePaths).toContain('knowledge/articles/new');
    expect(routePaths).toContain('tickets/create');
  });

  it('规则 A：所有写入口路径必须命中权限映射（fail-closed）', () => {
    const missing = routePaths
      .filter(isWriteEntryPath)
      .filter(p => !WRITE_ENTRY_EXEMPTIONS[p])
      .filter(p => !matchRoutePermission(`/${p}`));
    expect(missing).toEqual([]);
  });

  it('规则 B：映射表每条规则必须匹配到真实路由（防僵尸规则）', () => {
    const stale = ROUTE_PERMISSIONS.filter(
      rule => !routePaths.some(p => matchPath({ path: rule.pattern, end: true }, `/${p}`))
    ).map(rule => rule.pattern);
    expect(stale).toEqual([]);
  });
});

describe('permissionGranted（与后端 checkPermissionMatch 同语义）', () => {
  it('精确命中 / 资源通配 / 全局通配', () => {
    expect(permissionGranted(['knowledge:write'], 'knowledge:write')).toBe(true);
    expect(permissionGranted(['knowledge:*'], 'knowledge:write')).toBe(true);
    expect(permissionGranted(['*'], 'knowledge:write')).toBe(true);
    expect(permissionGranted(['knowledge:read'], 'knowledge:write')).toBe(false);
  });

  it('resource:admin 蕴含具体业务动作；MCP 例外（治理不蕴含使用）', () => {
    expect(permissionGranted(['ticket:admin'], 'ticket:create')).toBe(true);
    expect(permissionGranted(['mcp:admin'], 'mcp:write')).toBe(false);
    expect(permissionGranted(['mcp:admin'], 'mcp:read')).toBe(false);
  });

  it('空权限 fail-closed；非法码不误判', () => {
    expect(permissionGranted([], 'knowledge:write')).toBe(false);
    expect(permissionGranted(undefined, 'knowledge:write')).toBe(false);
    expect(permissionGranted(['knowledge:write'], 'knowledge')).toBe(false);
  });
});

describe('resolveRoutePermission', () => {
  it('未登记路径返回 none（不拦截）', () => {
    expect(resolveRoutePermission('/dashboard', ['ticket:read'])).toBe('none');
    expect(resolveRoutePermission('/service-catalog', [])).toBe('none');
  });

  it('写入口缺权限 → deny；具备 → allow；admin 蕴含 → allow', () => {
    expect(resolveRoutePermission('/knowledge/articles/new', ['knowledge:read'])).toBe('deny');
    expect(resolveRoutePermission('/knowledge/articles/new', ['knowledge:write'])).toBe('allow');
    expect(resolveRoutePermission('/knowledge/articles/42/edit', ['knowledge:admin'])).toBe('allow');
    expect(resolveRoutePermission('/tickets/create', ['ticket:admin'])).toBe('allow');
  });

  it('端到端口径：technician（无 knowledge:write）被拦，admin 放行', () => {
    const technician = [
      'ticket:read',
      'ticket:write',
      'knowledge:read',
      'service_request:read',
      'service_request:write',
    ];
    expect(resolveRoutePermission('/knowledge/articles/new', technician)).toBe('deny');
    const admin = ['knowledge:write', 'ticket:admin'];
    expect(resolveRoutePermission('/knowledge/articles/new', admin)).toBe('allow');
  });
});

describe('canUseMspWorkbench（顶栏客户过滤器门禁）', () => {
  const allowAll = () => true;

  it('服务商租户 + msp_ticket:read → 渲染', () => {
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp_provider',
        mspRole: 'msp_manager',
        hasPermission: allowAll,
      })
    ).toBe(true);
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp',
        mspRole: 'provider_agent',
        hasPermission: allowAll,
      })
    ).toBe(true);
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp_provider',
        mspRole: 'msp_manager',
        hasPermission: allowAll,
      })
    ).toBe(true);
  });

  it('平台超级管理员在 provider 租户但 mspRole 为空 → 不渲染（本次 403 根因）', () => {
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp_provider',
        mspRole: undefined,
        hasPermission: allowAll,
      })
    ).toBe(false);
  });

  it('客户租户即使有 * 通配权限也不渲染', () => {
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'customer',
        mspRole: undefined,
        hasPermission: allowAll,
      })
    ).toBe(false);
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'internal',
        mspRole: 'msp_tech',
        hasPermission: allowAll,
      })
    ).toBe(false);
  });

  it('客户角色 / 无权限 / 未登录 → 不渲染（fail-closed）', () => {
    const denyAll = () => false;
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp_provider',
        mspRole: 'customer_user',
        hasPermission: allowAll,
      })
    ).toBe(false);
    expect(
      canUseMspWorkbench({
        userId: 1,
        tenantType: 'msp_provider',
        mspRole: 'msp_viewer',
        hasPermission: denyAll,
      })
    ).toBe(false);
    expect(
      canUseMspWorkbench({
        userId: undefined,
        tenantType: 'msp_provider',
        mspRole: 'msp_manager',
        hasPermission: allowAll,
      })
    ).toBe(false);
  });
});
