/**
 * 全路由可达性冒烟 —— docs/plan/vite-migration-plan.md §8.1
 *
 * ⚠️ 本文件由 `.dev/vite-migration/gen_reachability_spec.py` 依据
 * `docs/plan/_data/vite-routes.json`（FE-V1-02 路由清单）生成，请勿手工编辑；
 * 路由变更后重新生成即可：
 *
 *     python .dev/vite-migration/gen_reachability_spec.py
 *
 * 覆盖范围：
 *  - 未登录：每条路由都必须能返回（无 404/5xx）；受保护路由由 RouteGuard 跳
 *    `/login?redirect=<原地址>`，公开路由直接渲染；
 *  - 已登录（admin，复用一次 UI 登录）：每条路由都能挂载出应用根节点，
 *    redirect-only 路由必须落到目标地址。
 *
 * 只做「可达 / 挂载」断言，业务断言见 §8.2 手工回归与 business-flows 用例。
 */
import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { loginAs } from './utils/test-utils';

interface RouteSample {
  /** 路由表里的路径模板（动态段写成 :param） */
  path: string;
  /** 可直接访问的示例地址（动态段已替换为真实示例值） */
  sample: string;
  /** 非空表示该路由只做重定向，值为目标地址 */
  redirect: string;
}

/** src/components/layout/RouteGuard.tsx 中 publicRoutes 的同一份清单 */
const PUBLIC_ROUTES = new Set(['/login', '/register', '/forgot-password']);

const APP_ROOT = '#root';

const ROUTE_SAMPLES: RouteSample[] = [
  { path: "/forgot-password", sample: "/forgot-password", redirect: "" },
  { path: "/login", sample: "/login", redirect: "" },
  { path: "/register", sample: "/register", redirect: "" },
  { path: "/reset-password", sample: "/reset-password", redirect: "" },
  { path: "/sso/callback", sample: "/sso/callback", redirect: "" },
  { path: "/admin/approval-chains", sample: "/admin/approval-chains", redirect: "" },
  { path: "/admin/approvals", sample: "/admin/approvals", redirect: "" },
  { path: "/admin/cab", sample: "/admin/cab", redirect: "" },
  { path: "/admin/cmdb-types", sample: "/admin/cmdb-types", redirect: "" },
  { path: "/admin/config-inheritance", sample: "/admin/config-inheritance", redirect: "" },
  { path: "/admin/connectors", sample: "/admin/connectors", redirect: "" },
  { path: "/admin/department-processes", sample: "/admin/department-processes", redirect: "" },
  { path: "/admin/departments", sample: "/admin/departments", redirect: "" },
  { path: "/admin/escalation-matrices", sample: "/admin/escalation-matrices", redirect: "" },
  { path: "/admin/escalation-rules", sample: "/admin/escalation-rules", redirect: "" },
  { path: "/admin/groups", sample: "/admin/groups", redirect: "" },
  { path: "/admin/menus", sample: "/admin/menus", redirect: "" },
  { path: "/admin/overview", sample: "/admin/overview", redirect: "/admin" },
  { path: "/admin", sample: "/admin", redirect: "" },
  { path: "/admin/permissions", sample: "/admin/permissions", redirect: "" },
  { path: "/admin/process-routing", sample: "/admin/process-routing", redirect: "" },
  { path: "/admin/roles", sample: "/admin/roles", redirect: "" },
  { path: "/admin/service-catalogs", sample: "/admin/service-catalogs", redirect: "" },
  { path: "/admin/sla-definitions", sample: "/admin/sla-definitions", redirect: "" },
  { path: "/admin/sla-templates", sample: "/admin/sla-templates", redirect: "" },
  { path: "/admin/system-config", sample: "/admin/system-config", redirect: "" },
  { path: "/admin/teams", sample: "/admin/teams", redirect: "" },
  { path: "/admin/tenants", sample: "/admin/tenants", redirect: "" },
  { path: "/admin/ticket-categories", sample: "/admin/ticket-categories", redirect: "" },
  { path: "/admin/tickets/assignment-rules", sample: "/admin/tickets/assignment-rules", redirect: "" },
  { path: "/admin/tickets/automation-rules", sample: "/admin/tickets/automation-rules", redirect: "" },
  { path: "/admin/users", sample: "/admin/users", redirect: "" },
  { path: "/admin/vector-store", sample: "/admin/vector-store", redirect: "" },
  { path: "/admin/workflows", sample: "/admin/workflows", redirect: "" },
  { path: "/ai/approval", sample: "/ai/approval", redirect: "" },
  { path: "/ai/audit", sample: "/ai/audit", redirect: "" },
  { path: "/ai/chat", sample: "/ai/chat", redirect: "" },
  { path: "/applications", sample: "/applications", redirect: "" },
  { path: "/approvals", sample: "/approvals", redirect: "" },
  { path: "/approvals/pending", sample: "/approvals/pending", redirect: "/approvals" },
  { path: "/assets/:id/edit", sample: "/assets/1/edit", redirect: "" },
  { path: "/assets/:id", sample: "/assets/1", redirect: "" },
  { path: "/assets/new", sample: "/assets/new", redirect: "" },
  { path: "/assets", sample: "/assets", redirect: "" },
  { path: "/audit-logs", sample: "/audit-logs", redirect: "" },
  { path: "/changes/:id/edit", sample: "/changes/1/edit", redirect: "" },
  { path: "/changes/:id", sample: "/changes/1", redirect: "" },
  { path: "/changes/:id/pir", sample: "/changes/1/pir", redirect: "" },
  { path: "/changes/new", sample: "/changes/new", redirect: "" },
  { path: "/changes", sample: "/changes", redirect: "" },
  { path: "/changes/pirs", sample: "/changes/pirs", redirect: "" },
  { path: "/cmdb/ci", sample: "/cmdb/ci", redirect: "/cmdb/cis" },
  { path: "/cmdb/ci-types", sample: "/cmdb/ci-types", redirect: "/admin/cmdb-types" },
  { path: "/cmdb/cis/:id/edit", sample: "/cmdb/cis/1/edit", redirect: "" },
  { path: "/cmdb/cis/:id", sample: "/cmdb/cis/1", redirect: "" },
  { path: "/cmdb/cis/create", sample: "/cmdb/cis/create", redirect: "" },
  { path: "/cmdb/cis", sample: "/cmdb/cis", redirect: "" },
  { path: "/cmdb/cloud-accounts", sample: "/cmdb/cloud-accounts", redirect: "" },
  { path: "/cmdb/cloud-resources", sample: "/cmdb/cloud-resources", redirect: "" },
  { path: "/cmdb/cloud-services", sample: "/cmdb/cloud-services", redirect: "" },
  { path: "/cmdb", sample: "/cmdb", redirect: "" },
  { path: "/cmdb/reconciliation", sample: "/cmdb/reconciliation", redirect: "" },
  { path: "/cmdb/registry", sample: "/cmdb/registry", redirect: "" },
  { path: "/cmdb/relationships", sample: "/cmdb/relationships", redirect: "" },
  { path: "/cmdb/topology", sample: "/cmdb/topology", redirect: "" },
  { path: "/dashboard", sample: "/dashboard", redirect: "" },
  { path: "/email-intake/contracts", sample: "/email-intake/contracts", redirect: "" },
  { path: "/email-intake/customers", sample: "/email-intake/customers", redirect: "" },
  { path: "/email-intake/on-call", sample: "/email-intake/on-call", redirect: "" },
  { path: "/email-intake", sample: "/email-intake", redirect: "" },
  { path: "/email-intake/sources", sample: "/email-intake/sources", redirect: "" },
  { path: "/enterprise/departments", sample: "/enterprise/departments", redirect: "" },
  { path: "/enterprise/teams", sample: "/enterprise/teams", redirect: "" },
  { path: "/improvements/:id", sample: "/improvements/1", redirect: "" },
  { path: "/improvements/new", sample: "/improvements/new", redirect: "" },
  { path: "/improvements", sample: "/improvements", redirect: "" },
  { path: "/incidents/:id/edit", sample: "/incidents/1/edit", redirect: "" },
  { path: "/incidents/:id", sample: "/incidents/1", redirect: "" },
  { path: "/incidents/create", sample: "/incidents/create", redirect: "" },
  { path: "/incidents", sample: "/incidents", redirect: "" },
  { path: "/installations", sample: "/installations", redirect: "" },
  { path: "/knowledge/:id", sample: "/knowledge/1", redirect: "" },
  { path: "/knowledge/articles/:id/edit", sample: "/knowledge/articles/1/edit", redirect: "" },
  { path: "/knowledge/articles/:id", sample: "/knowledge/articles/1", redirect: "" },
  { path: "/knowledge/articles/new", sample: "/knowledge/articles/new", redirect: "" },
  { path: "/knowledge", sample: "/knowledge", redirect: "" },
  { path: "/knowledge/reviews", sample: "/knowledge/reviews", redirect: "" },
  { path: "/licenses/:id/edit", sample: "/licenses/1/edit", redirect: "" },
  { path: "/licenses/:id", sample: "/licenses/1", redirect: "" },
  { path: "/licenses/new", sample: "/licenses/new", redirect: "" },
  { path: "/licenses", sample: "/licenses", redirect: "" },
  { path: "/marketplace/:id", sample: "/marketplace/1", redirect: "" },
  { path: "/marketplace", sample: "/marketplace", redirect: "" },
  { path: "/msp/management", sample: "/msp/management", redirect: "" },
  { path: "/msp", sample: "/msp", redirect: "" },
  { path: "/my-requests/:requestId", sample: "/my-requests/1", redirect: "" },
  { path: "/my-requests", sample: "/my-requests", redirect: "" },
  { path: "/noc", sample: "/noc", redirect: "" },
  { path: "/notifications", sample: "/notifications", redirect: "" },
  { path: "/problems/:id/edit", sample: "/problems/1/edit", redirect: "" },
  { path: "/problems/:id", sample: "/problems/1", redirect: "" },
  { path: "/problems/known-errors", sample: "/problems/known-errors", redirect: "" },
  { path: "/problems/new", sample: "/problems/new", redirect: "" },
  { path: "/problems", sample: "/problems", redirect: "" },
  { path: "/problems/trends", sample: "/problems/trends", redirect: "" },
  { path: "/profile", sample: "/profile", redirect: "" },
  { path: "/projects", sample: "/projects", redirect: "" },
  { path: "/releases/:id/edit", sample: "/releases/1/edit", redirect: "" },
  { path: "/releases/:id", sample: "/releases/1", redirect: "" },
  { path: "/releases/new", sample: "/releases/new", redirect: "" },
  { path: "/releases", sample: "/releases", redirect: "" },
  { path: "/reports/catalog-usage", sample: "/reports/catalog-usage", redirect: "/reports/service-catalog-usage" },
  { path: "/reports/change-success", sample: "/reports/change-success", redirect: "" },
  { path: "/reports/changes", sample: "/reports/changes", redirect: "/reports/change-success" },
  { path: "/reports/cmdb-quality", sample: "/reports/cmdb-quality", redirect: "" },
  { path: "/reports/incident-trends", sample: "/reports/incident-trends", redirect: "" },
  { path: "/reports/incidents", sample: "/reports/incidents", redirect: "/reports/incident-trends" },
  { path: "/reports", sample: "/reports", redirect: "" },
  { path: "/reports/problem-efficiency", sample: "/reports/problem-efficiency", redirect: "" },
  { path: "/reports/problems", sample: "/reports/problems", redirect: "/reports/problem-efficiency" },
  { path: "/reports/service-catalog-usage", sample: "/reports/service-catalog-usage", redirect: "" },
  { path: "/reports/sla", sample: "/reports/sla", redirect: "/reports/sla-performance" },
  { path: "/reports/sla-performance", sample: "/reports/sla-performance", redirect: "" },
  { path: "/reports/tickets", sample: "/reports/tickets", redirect: "" },
  { path: "/service-catalog/approvals", sample: "/service-catalog/approvals", redirect: "" },
  { path: "/service-catalog/detail/:id", sample: "/service-catalog/detail/1", redirect: "" },
  { path: "/service-catalog/edit/:id", sample: "/service-catalog/edit/1", redirect: "" },
  { path: "/service-catalog", sample: "/service-catalog", redirect: "" },
  { path: "/service-catalog/request/:id", sample: "/service-catalog/request/1", redirect: "" },
  { path: "/service-requests/:id", sample: "/service-requests/1", redirect: "" },
  { path: "/service-requests", sample: "/service-requests", redirect: "" },
  { path: "/settings/approvals", sample: "/settings/approvals", redirect: "" },
  { path: "/sla/definitions/:id", sample: "/sla/definitions/1", redirect: "" },
  { path: "/sla", sample: "/sla", redirect: "" },
  { path: "/sla-dashboard", sample: "/sla-dashboard", redirect: "/sla" },
  { path: "/sla-monitor", sample: "/sla-monitor", redirect: "" },
  { path: "/standard-changes", sample: "/standard-changes", redirect: "" },
  { path: "/system/organization", sample: "/system/organization", redirect: "" },
  { path: "/system/users", sample: "/system/users", redirect: "" },
  { path: "/tags", sample: "/tags", redirect: "" },
  { path: "/teams", sample: "/teams", redirect: "" },
  { path: "/templates", sample: "/templates", redirect: "/tickets/templates" },
  { path: "/tickets/:ticketId", sample: "/tickets/1", redirect: "" },
  { path: "/tickets/ai-create", sample: "/tickets/ai-create", redirect: "" },
  { path: "/tickets/analytics", sample: "/tickets/analytics", redirect: "" },
  { path: "/tickets/cc", sample: "/tickets/cc", redirect: "" },
  { path: "/tickets/create", sample: "/tickets/create", redirect: "" },
  { path: "/tickets/dashboard", sample: "/tickets/dashboard", redirect: "" },
  { path: "/tickets", sample: "/tickets", redirect: "" },
  { path: "/tickets/templates/:id", sample: "/tickets/templates/1", redirect: "" },
  { path: "/tickets/templates", sample: "/tickets/templates", redirect: "" },
  { path: "/tickets/types", sample: "/tickets/types", redirect: "" },
  { path: "/workflow/audit", sample: "/workflow/audit", redirect: "" },
  { path: "/workflow/automation", sample: "/workflow/automation", redirect: "" },
  { path: "/workflow/bottlenecks", sample: "/workflow/bottlenecks", redirect: "" },
  { path: "/workflow/dashboard", sample: "/workflow/dashboard", redirect: "" },
  { path: "/workflow/designer", sample: "/workflow/designer", redirect: "" },
  { path: "/workflow/instances/:id", sample: "/workflow/instances/1", redirect: "" },
  { path: "/workflow/instances", sample: "/workflow/instances", redirect: "" },
  { path: "/workflow", sample: "/workflow", redirect: "" },
  { path: "/workflow/sla", sample: "/workflow/sla", redirect: "" },
  { path: "/workflow/ticket-approval", sample: "/workflow/ticket-approval", redirect: "/approvals" },
  { path: "/workflow/versions", sample: "/workflow/versions", redirect: "" },
  { path: "/workflows", sample: "/workflows", redirect: "/workflow" },
  { path: "/agent-ops-demo", sample: "/agent-ops-demo", redirect: "" },
  { path: "/auth/callback/:provider", sample: "/auth/callback/sso", redirect: "" },
  { path: "/onboarding/wizard", sample: "/onboarding/wizard", redirect: "" },
  { path: "/", sample: "/", redirect: "" },
];

test.describe('全路由可达性冒烟 · 未登录', () => {
  for (const route of ROUTE_SAMPLES) {
    test(`未登录可达：${route.path}`, async ({ page }) => {
      const response = await page.goto(route.sample, { waitUntil: 'domcontentloaded' });
      expect(response?.status() ?? 0, `${route.sample} 响应状态`).toBeLessThan(400);

      const landed = new URL(page.url());
      if (PUBLIC_ROUTES.has(route.path)) {
        // 公开路由不得被守卫拦截（否则登录页/注册页会死循环）
        expect(landed.pathname, `${route.sample} 应停留在公开路由`).toBe(route.path);
      } else {
        // 受保护路由：必须带上原始地址，登录后才能回跳
        expect(landed.pathname, `${route.sample} 应跳转登录页`).toBe('/login');
        expect(landed.searchParams.get('redirect'), `${route.sample} 的 redirect 参数`).toBe(
          route.sample
        );
      }

      await expect(page.locator(APP_ROOT)).not.toBeEmpty();
    });
  }
});

test.describe('全路由可达性冒烟 · 已登录 admin', () => {
  test.describe.configure({ mode: 'serial' });

  let context: BrowserContext;
  let page: Page;

  test.beforeAll(async ({ browser }) => {
    context = await browser.newContext();
    page = await context.newPage();
    await loginAs(page, 'admin');
  });

  test.afterAll(async () => {
    await context?.close();
  });

  test('每条路由都能渲染，redirect-only 路由落到目标地址', async () => {
    const failures: string[] = [];

    for (const route of ROUTE_SAMPLES) {
      try {
        const response = await page.goto(route.sample, { waitUntil: 'domcontentloaded' });
        const status = response?.status() ?? 0;
        if (status >= 400) {
          failures.push(`${route.sample}: HTTP ${status}`);
          continue;
        }

        const landed = new URL(page.url()).pathname;
        if (route.redirect && landed !== route.redirect) {
          failures.push(`${route.sample}: 期望重定向到 ${route.redirect}，实际 ${landed}`);
          continue;
        }

        await expect(page.locator(APP_ROOT)).not.toBeEmpty();
      } catch (error) {
        failures.push(`${route.sample}: ${(error as Error).message.split('\n')[0]}`);
      }
    }

    expect(failures, `不可达路由：\n${failures.join('\n')}`).toEqual([]);
  });
});
