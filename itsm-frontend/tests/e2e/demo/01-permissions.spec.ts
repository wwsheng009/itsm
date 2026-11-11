import { expect, test } from '@playwright/test';
import { getDemoSession, newAuthenticatedContext } from './fixtures/demo-auth';
import {
  apiGet,
  apiPost,
  authHeaders,
  callRaw,
  expectRejected,
  runId,
  title,
} from './support/api';

test.describe.configure({ mode: 'serial' });

async function rawGet(
  session: Awaited<ReturnType<typeof getDemoSession>>,
  path: string,
  headers: Record<string, string> = {},
) {
  return callRaw(session.api, 'GET', path, {
    headers: authHeaders(session, headers),
  });
}

test.describe('Demo Round 1 · permissions and tenant isolation', () => {
  test('end_user：权限边界与禁用能力', async () => {
    const user = await getDemoSession('mfgUser');
    const me = await apiGet<Record<string, unknown>>(user, '/api/v1/auth/me');
    const permissions = (me.permissions as string[]) ?? [];

    expect(permissions).toContain('ticket:read');
    expect(permissions).toContain('ticket:create');
    expect(permissions).toContain('service_catalog:read');
    expect(permissions).toContain('service_request:read');
    expect(permissions).toContain('knowledge:read');
    expect(permissions).not.toContain('dashboard:read');
    expect(permissions).not.toContain('problem:read');
    expect(permissions).not.toContain('msp:read');

    expect((await rawGet(user, '/api/v1/tickets?page=1&size=20')).status()).toBe(200);
    expect((await rawGet(user, '/api/v1/problems')).status()).toBe(403);
    expect((await rawGet(user, '/api/v1/tenants')).status()).toBe(403);
    expect((await rawGet(user, '/api/v1/msp/status')).status()).toBe(403);
    expect((await rawGet(user, '/api/v1/dashboard/overview')).status()).toBe(403);
  });

  test(
    'end_user Dashboard 不应请求 dashboard:read 专属接口',
    async ({ browser }) => {
      const { context, page } = await newAuthenticatedContext(browser, 'mfgUser');
      try {
        await page.goto('/dashboard', { waitUntil: 'domcontentloaded' });
        await page.waitForTimeout(1500);
        const failed = await page.evaluate(() =>
          performance
            .getEntriesByType('resource')
            .map(entry => entry.name)
            .filter(name => name.includes('/api/v1/dashboard/')),
        );
        expect(failed, 'end_user dashboard made no dashboard API calls').toEqual([]);
      } finally {
        await context.close();
      }
    },
  );

  test('客户管理员：本租户管理可用，但不能创建租户/使用 MSP', async () => {
    const admin = await getDemoSession('mfgAdmin');
    expect((await rawGet(admin, '/api/v1/users')).status()).toBe(200);
    expect((await rawGet(admin, '/api/v1/tenants')).status()).toBe(403);
    const mspStatus = await rawGet(admin, '/api/v1/msp/status');
    expect([200, 403]).toContain(mspStatus.status());
    if (mspStatus.status() === 200) {
      const body = (await mspStatus.json()) as { data?: { isMsp?: boolean } };
      expect(body.data?.isMsp).toBe(false);
    }

    const createTenant = await callRaw(admin.api, 'POST', '/api/v1/tenants', {
      headers: authHeaders(admin),
      data: { name: `[E2E] invalid tenant ${Date.now()}`, code: `e2e-invalid-${Date.now()}` },
    });
    await expectRejected(createTenant, [403]);
  });

  test('MSP 经理：客户目录/分配/工作台与未分配客户拒绝', async () => {
    const manager = await getDemoSession('mspManager');
    const status = await apiGet<Record<string, unknown>>(manager, '/api/v1/msp/status');
    expect(status.isMsp).toBe(true);
    expect(status.role).toBe('provider_admin');
    expect(status.deploymentMode).toBe('saas_msp');

    const context = await apiGet<{ allowedCustomers: number[] }>(manager, '/api/v1/msp/context');
    expect(context.allowedCustomers.sort((a, b) => a - b)).toEqual([3, 4, 5]);

    const customers = await apiGet<{ customers: Array<{ id: number; code: string }>; total: number }>(
      manager,
      '/api/v1/msp/customers',
    );
    expect(customers.total).toBe(4);
    expect(customers.customers.map(customer => customer.id)).toEqual(expect.arrayContaining([3, 4, 5, 6]));

    const allocations = await apiGet<{ allocations: unknown[]; total: number }>(
      manager,
      '/api/v1/msp/allocations',
    );
    expect(allocations.total).toBe(7);

    const demoMfg = await apiGet<{ total: number; items: Array<{ title: string; customerTenantId: number }> }>(
      manager,
      '/api/v1/msp/workbench/tickets',
      { params: { customerTenantIds: 3, q: '[DEMO]', limit: 50 } },
    );
    expect(demoMfg.total).toBe(12);
    expect(demoMfg.items.every(item => item.customerTenantId === 3)).toBe(true);
    expect(demoMfg.items.every(item => item.title.startsWith('[DEMO]'))).toBe(true);

    // 未分配客户 6（review-test）：工作台列表必须拒绝；详情/单请求头也必须拒绝。
    const unallocatedList = await callRaw(manager.api, 'GET', '/api/v1/msp/workbench/tickets', {
      headers: authHeaders(manager),
      params: { customerTenantIds: 6, limit: 20 },
    });
    await expectRejected(unallocatedList, [403]);

    const unallocatedTicket = await callRaw(manager.api, 'GET', '/api/v1/msp/tickets/7', {
      headers: authHeaders(manager),
      params: { customerTenantId: 6 },
    });
    await expectRejected(unallocatedTicket, [400, 403, 404]);
  });

  test('MSP 工程师：仅能访问已分配客户', async () => {
    const agent1 = await getDemoSession('mspAgent1');
    const context1 = await apiGet<{ allowedCustomers: number[] }>(agent1, '/api/v1/msp/context');
    expect(context1.allowedCustomers.sort((a, b) => a - b)).toEqual([3, 5]);

    const allowed = await callRaw(agent1.api, 'GET', '/api/v1/msp/workbench/tickets', {
      headers: authHeaders(agent1),
      params: { customerTenantIds: 3, limit: 10 },
    });
    expect(allowed.status()).toBe(200);

    const denied = await callRaw(agent1.api, 'GET', '/api/v1/msp/workbench/tickets', {
      headers: authHeaders(agent1),
      params: { customerTenantIds: 4, limit: 10 },
    });
    await expectRejected(denied, [403]);

    // 契约备注：X-Customer-Tenant-ID 的条目级校验挂在 /msp/*（MSPMiddleware）；
    // 对通用 /tickets 路由该头不会切换作用域，实测返回 home/provider 数据（当前为空）。
    // 这里断言它不会把客户 4 的数据带出来，而不是要求 403。
    const headerResponse = await rawGet(agent1, '/api/v1/tickets?page=1&size=10', {
      'X-Customer-Tenant-ID': '4',
    });
    expect([200, 401, 403]).toContain(headerResponse.status());
    if (headerResponse.status() === 200) {
      const bodyText = await headerResponse.text();
      expect(bodyText).not.toContain('HIS 医生站登录超时');
      expect(bodyText).not.toContain('PACS 影像调阅缓慢');
    }

    const agent2 = await getDemoSession('mspAgent2');
    const context2 = await apiGet<{ allowedCustomers: number[] }>(agent2, '/api/v1/msp/context');
    expect(context2.allowedCustomers.sort((a, b) => a - b)).toEqual([3, 4]);
  });

  test('跨租户：制造管理员创建的工单不得被医疗管理员读取/更新/删除', async () => {
    const mfgAdmin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const rid = runId('E2E-XTENANT');
    const ticketTitle = title(rid, '跨租户隔离验证工单');

    const created = await apiPost<{ id: number; ticketNumber: string }>(mfgAdmin, '/api/v1/tickets', {
      title: ticketTitle,
      description: '跨租户 E2E 隔离验证，仅用于测试，结束后删除。',
      priority: 'low',
      type: 'incident',
    });

    try {
      const crossGet = await rawGet(medAdmin, `/api/v1/tickets/${created.id}`);
      await expectRejected(crossGet, [403, 404], { forbiddenText: ticketTitle });

      const crossList = await rawGet(medAdmin, '/api/v1/tickets?page=1&size=100');
      expect(crossList.status()).toBe(200);
      const listText = await crossList.text();
      expect(listText).not.toContain(ticketTitle);

      const crossPut = await callRaw(medAdmin.api, 'PUT', `/api/v1/tickets/${created.id}`, {
        headers: authHeaders(medAdmin),
        data: { title: `${ticketTitle} HACKED` },
      });
      await expectRejected(crossPut, [403, 404, 409]);

      const crossDelete = await callRaw(medAdmin.api, 'DELETE', `/api/v1/tickets/${created.id}`, {
        headers: authHeaders(medAdmin),
      });
      await expectRejected(crossDelete, [403, 404]);
      const stillExists = await rawGet(mfgAdmin, `/api/v1/tickets/${created.id}`);
      expect(stillExists.status(), 'cross-tenant delete must not remove the source ticket').toBe(200);
    } finally {
      await callRaw(mfgAdmin.api, 'DELETE', `/api/v1/tickets/${created.id}`, {
        headers: authHeaders(mfgAdmin),
      });
    }
  });

  test('未认证/无效 token：读接口必须 401', async ({ request }) => {
    expect((await request.get('/api/v1/tickets')).status()).toBe(401);
    expect(
      (
        await request.get('/api/v1/tickets', {
          headers: { Authorization: 'Bearer invalid-token-12345' },
        })
      ).status(),
    ).toBe(401);
  });

  test('跨租户 DELETE 必须返回 404', async () => {
    const mfgAdmin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const created = await apiPost<{ id: number }>(mfgAdmin, '/api/v1/tickets', {
      title: title(runId('E2E-XDELETE'), '跨租户删除 404'),
      description: 'cross-tenant delete must be hidden as 404',
      priority: 'low',
      type: 'incident',
    });
    try {
      const response = await callRaw(medAdmin.api, 'DELETE', `/api/v1/tickets/${created.id}`, {
        headers: authHeaders(medAdmin),
      });
      expect(response.status()).toBe(404);
    } finally {
      await callRaw(mfgAdmin.api, 'DELETE', `/api/v1/tickets/${created.id}`, {
        headers: authHeaders(mfgAdmin),
      });
    }
  });
});