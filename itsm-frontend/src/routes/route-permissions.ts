/**
 * 路由级权限门禁的单一事实源（前端 fail-closed）。
 *
 * 背景：此前权限只在「提交后拿到 403」时才体现（如 /knowledge/articles/new 填完表单才报
 * 权限不足），入口按钮也无条件渲染。本模块把「写入口路由 → 所需权限码」集中声明，
 * 由 RequireAuth（见 guards.tsx）在进入页面前判定，不通过直接落 /403。
 *
 * 约定：
 * - 权限码与后端路由上的 `middleware.RequirePermission(resource, action)` 逐条对齐
 *   （核对点：itsm-backend/router/*_routes.go 与 handlers/<domain>/routes.go）；
 * - 判定语义与后端 `middleware/rbac.go checkPermissionMatch` 一致（见 permissionGranted）；
 * - 新增写入口路由（路径含 new/create/edit）必须在 ROUTE_PERMISSIONS 登记，否则
 *   `__tests__/route-permissions.test.tsx` 的守卫测试会失败（对标后端
 *   router/route_permission_guard_test.go）。
 */
import { matchPath } from 'react-router';

export interface RoutePermissionRule {
  /** react-router matchPath 模式（如 /knowledge/articles/:id/edit）。 */
  pattern: string;
  /** 命中任意一个权限码即放行（任一命中，语义同 RequireCapability.anyOf）。 */
  anyOf: string[];
  /** 规则依据（对应后端路由注册点），便于评审与防漂移。 */
  reason: string;
}

/**
 * 写入口路由 → 权限码映射（21 条写入口 + 知识库复核面）。
 *
 * 说明：
 * - 区域级守卫（/admin、租户管理、/msp）仍由 routes/index.tsx 的 RequireCapability 承担，
 *   本表不重复声明；
 * - `/improvements/new` 前端复用 TicketApi.createTicket（ticket:create）；
 * - `/tickets/ai-create` 走 A2UI 生成面（handlers/a2ui：ai:write）。
 */
export const ROUTE_PERMISSIONS: RoutePermissionRule[] = [
  { pattern: '/tickets/create', anyOf: ['ticket:create'], reason: 'router/ticket_routes.go POST /tickets' },
  { pattern: '/tickets/ai-create', anyOf: ['ai:write'], reason: 'handlers/a2ui/handler.go POST /a2ui/ticket/*' },
  { pattern: '/incidents/create', anyOf: ['incident:write'], reason: 'router/incident_routes.go POST /incidents' },
  { pattern: '/incidents/:id/edit', anyOf: ['incident:write'], reason: 'router/incident_routes.go PUT /incidents/:id' },
  { pattern: '/changes/new', anyOf: ['change:write'], reason: 'router/change_routes.go POST /changes' },
  { pattern: '/changes/:id/edit', anyOf: ['change:write'], reason: 'router/change_routes.go PUT /changes/:id' },
  { pattern: '/problems/new', anyOf: ['problem:write'], reason: 'router/problem_routes.go POST /problems' },
  { pattern: '/problems/:id/edit', anyOf: ['problem:write'], reason: 'router/problem_routes.go PUT /problems/:id' },
  { pattern: '/releases/new', anyOf: ['release:write'], reason: 'router/release_routes.go POST /releases' },
  { pattern: '/releases/:id/edit', anyOf: ['release:write'], reason: 'router/release_routes.go PUT /releases/:id' },
  { pattern: '/improvements/new', anyOf: ['ticket:create'], reason: '页面复用 TicketApi.createTicket（POST /tickets）' },
  { pattern: '/knowledge/articles/new', anyOf: ['knowledge:write'], reason: 'router/knowledge_routes.go POST /knowledge/articles' },
  { pattern: '/knowledge/articles/:id/edit', anyOf: ['knowledge:write'], reason: 'router/knowledge_routes.go PUT /knowledge/articles/:id' },
  { pattern: '/knowledge/reviews', anyOf: ['knowledge:write'], reason: 'router/knowledge_routes.go POST /knowledge/articles/:id/review' },
  { pattern: '/assets/new', anyOf: ['asset:write'], reason: 'handlers/asset/routes.go POST /assets' },
  { pattern: '/assets/:id/edit', anyOf: ['asset:write'], reason: 'handlers/asset/routes.go PUT /assets/:id' },
  { pattern: '/licenses/new', anyOf: ['license:write'], reason: 'handlers/asset/routes.go POST /licenses' },
  { pattern: '/licenses/:id/edit', anyOf: ['license:write'], reason: 'handlers/asset/routes.go PUT /licenses/:id' },
  { pattern: '/cmdb/cis/create', anyOf: ['cmdb:write'], reason: 'router/cmdb_routes.go POST /cmdb/configuration-items' },
  { pattern: '/cmdb/cis/:id/edit', anyOf: ['cmdb:write'], reason: 'router/cmdb_routes.go PUT /cmdb/configuration-items/:id' },
  { pattern: '/service-catalog/edit/:id', anyOf: ['service_catalog:write'], reason: 'router/service_catalog_routes.go PUT /service-catalogs/:id' },
  { pattern: '/service-requests/new', anyOf: ['service_request:write'], reason: 'router/service_request_routes.go POST /service-requests' },
];

export type RoutePermissionDecision = 'allow' | 'deny' | 'none';

/** 命中当前路径的规则（无规则返回 null）。 */
export function matchRoutePermission(pathname: string): RoutePermissionRule | null {
  for (const rule of ROUTE_PERMISSIONS) {
    if (matchPath({ path: rule.pattern, end: true }, pathname)) return rule;
  }
  return null;
}

/**
 * 单权限码判定，语义与后端 `middleware/rbac.go checkPermissionMatch` 对齐：
 * - `*`（super_admin 下发）直通；
 * - `resource:*` / `*:action` 通配；
 * - `resource:admin` 蕴含该资源下的具体业务动作（MCP 例外：mcp:admin 不蕴含 mcp:read/write）。
 */
export function permissionGranted(permissions: string[] | undefined, required: string): boolean {
  if (!permissions || permissions.length === 0) return false;

  const separator = required.indexOf(':');
  if (separator <= 0 || separator === required.length - 1) return false;
  const resource = required.slice(0, separator);
  const action = required.slice(separator + 1);

  for (const raw of permissions) {
    if (raw === '*') return true;
    const idx = raw.indexOf(':');
    if (idx <= 0 || idx === raw.length - 1) continue;
    const r = raw.slice(0, idx);
    const a = raw.slice(idx + 1);

    if (r === '*' && (a === '*' || a === action)) return true;
    if (r === resource && (a === '*' || a === action)) return true;
    // 资源管理员蕴含（与后端一致；MCP 治理与使用严格分离，不蕴含）。
    if (r === resource && a === 'admin' && resource !== 'mcp') return true;
  }
  return false;
}

/** 路由级判定：`none` = 无需额外权限（放行）；`allow` = 命中且具备权限；`deny` = 命中但缺权限。 */
export function resolveRoutePermission(
  pathname: string,
  permissions: string[] | undefined
): RoutePermissionDecision {
  const rule = matchRoutePermission(pathname);
  if (!rule) return 'none';
  return rule.anyOf.some(code => permissionGranted(permissions, code)) ? 'allow' : 'deny';
}
