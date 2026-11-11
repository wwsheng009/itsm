import { expect, test } from '@playwright/test';
import { getDemoSession, switchDemoTenant } from './fixtures/demo-auth';
import type { DemoRoleKey } from './fixtures/demo-accounts';
import {
  apiGet,
  apiPost,
  apiPut,
  authHeaders,
  callRaw,
  expectRejected,
  runId,
  title,
} from './support/api';

test.describe.configure({ mode: 'serial' });

const CUSTOMERS = [
  { id: 3, adminKey: 'mfgAdmin' as DemoRoleKey, code: 'mfg' },
  { id: 4, adminKey: 'medAdmin' as DemoRoleKey, code: 'med' },
  { id: 5, adminKey: 'eduAdmin' as DemoRoleKey, code: 'edu' },
] as const;

const PROVIDER_TENANT_ID = 2;

async function commentText(session: Awaited<ReturnType<typeof getDemoSession>>, ticketId: number) {
  const comments = await apiGet<{
    comments?: Array<{ content: string }>;
    items?: Array<{ content: string }>;
  }>(session, `/api/v1/tickets/${ticketId}/comments`);
  return (comments.comments ?? comments.items ?? []).map(item => item.content).join('\n');
}

async function safeDeleteTicket(
  session: Awaited<ReturnType<typeof getDemoSession>>,
  ticketId: number,
): Promise<void> {
  await callRaw(session.api, 'DELETE', `/api/v1/tickets/${ticketId}`, {
    headers: authHeaders(session),
  }).catch(() => undefined);
}

test.describe('Demo Round 5 · multi-tenant business operations', () => {
  test('MT-1 客户建单 → provider 深度切换三客户：处理/回读/隔离/返回', async () => {
    const manager = await getDemoSession('mspManager');
    const managerHome = Number(manager.user.homeTenantId ?? PROVIDER_TENANT_ID);
    const run = runId('E2E-MT-DEEP');
    const createdTickets: Array<{ id: number; adminKey: DemoRoleKey }> = [];

    try {
      for (let index = 0; index < CUSTOMERS.length; index += 1) {
        const customer = CUSTOMERS[index];
        const next = CUSTOMERS[(index + 1) % CUSTOMERS.length];
        const ticketTitle = title(run, `${customer.code} 深度处理工单`);
        const customerComment = `[E2E-MT] ${customer.code} 客户作用域评论`;
        const providerReply = `[E2E-MT] provider 回复 ${customer.code}`;
        const admin = await getDemoSession(customer.adminKey);

        // 客户侧先建单；本用例验证 provider 深度切换后的“处理”操作，
        // 不把 provider 用户误当作客户租户内的申请人。
        // 已知产品缺口（待决策）：provider 在客户作用域直接 POST /tickets 时，
        // 权限含 ticket:create，但 handler 强制 requester=当前 provider 用户，
        // 服务端按目标租户校验 requester → 404「申请人不存在或不可用」。
        // 修复方向需产品确认：客户 requester 选择器 / 允许 provider requester / 禁用建单。
        const created = await apiPost<{ id: number }>(admin, '/api/v1/tickets', {
          title: ticketTitle,
          description: 'multi-tenant business operation E2E; safe to delete.',
          priority: 'low',
          type: 'incident',
        });
        const ticketId = created.id;
        createdTickets.push({ id: ticketId, adminKey: customer.adminKey });

        await test.step(`${customer.code}: 进入客户并验证作用域`, async () => {
          await switchDemoTenant(manager, customer.id);
          const me = await apiGet<{
            tenantId: number;
            homeTenantId: number;
            mspRole?: string;
            permissions?: string[];
          }>(manager, '/api/v1/auth/me');
          expect(me.tenantId).toBe(customer.id);
          expect(me.homeTenantId).toBe(managerHome);
          expect(me.mspRole).toBe('provider_admin');
          expect(me.permissions ?? []).toContain('ticket:create');

          // 客户作用域不能使用服务商数据面。
          const mspDenied = await callRaw(manager.api, 'GET', '/api/v1/msp/workbench/summary', {
            headers: authHeaders(manager),
          });
          await expectRejected(mspDenied, [403]);
        });

        await test.step(`${customer.code}: 客户作用域读取+评论+改状态`, async () => {
          const detail = await apiGet<{ id: number }>(manager, `/api/v1/tickets/${ticketId}`);
          expect(detail.id).toBe(ticketId);
          await apiPost(manager, `/api/v1/tickets/${ticketId}/comments`, {
            content: customerComment,
            isInternal: false,
          });
          const updated = await apiPut<{ status: string }>(
            manager,
            `/api/v1/tickets/${ticketId}/status`,
            { status: 'in_progress' },
          );
          expect(updated.status).toBe('in_progress');

          const ownList = await callRaw(manager.api, 'GET', '/api/v1/tickets?page=1&size=100', {
            headers: authHeaders(manager),
          });
          expect(ownList.status()).toBe(200);
          expect(await ownList.text()).toContain(ticketTitle);
        });

        await test.step(`${next.code}: 其他客户不可见`, async () => {
          await switchDemoTenant(manager, next.id);
          const crossGet = await callRaw(manager.api, 'GET', `/api/v1/tickets/${ticketId}`, {
            headers: authHeaders(manager),
          });
          await expectRejected(crossGet, [403, 404], { forbiddenText: ticketTitle });

          const crossList = await callRaw(manager.api, 'GET', '/api/v1/tickets?page=1&size=100', {
            headers: authHeaders(manager),
          });
          expect(crossList.status()).toBe(200);
          expect(await crossList.text()).not.toContain(ticketTitle);
        });

        await test.step(`${customer.code}: 返回 provider 后工作台可见并可操作`, async () => {
          await switchDemoTenant(manager, managerHome);
          const providerDetail = await apiGet<{ id: number; status?: string }>(
            manager,
            `/api/v1/msp/tickets/${ticketId}`,
            { params: { customerTenantId: customer.id } },
          );
          expect(providerDetail.id).toBe(ticketId);
          const providerComments = await apiGet<{
            comments?: Array<{ content: string }>;
            items?: Array<{ content: string }>;
          }>(manager, `/api/v1/msp/tickets/${ticketId}/comments`, {
            params: { customerTenantId: customer.id },
          });
          expect(
            (providerComments.comments ?? providerComments.items ?? [])
              .map(item => item.content)
              .join('\n'),
          ).toContain(customerComment);

          await apiPost(manager, `/api/v1/msp/tickets/${ticketId}/assign`, {
            customerTenantId: customer.id,
          });
          await apiPost(manager, `/api/v1/msp/tickets/${ticketId}/reply`, {
            customerTenantId: customer.id,
            content: providerReply,
          });
          const updated = await apiPost<{ status?: string; Status?: string }>(
            manager,
            `/api/v1/msp/tickets/${ticketId}/status`,
            { customerTenantId: customer.id, status: 'in_progress' },
          );
          expect(updated.status ?? updated.Status).toBe('in_progress');
        });

        await test.step(`${customer.code}: 客户作用域看到 provider 操作结果`, async () => {
          await switchDemoTenant(manager, customer.id);
          const customerDetail = await apiGet<{ status: string }>(
            manager,
            `/api/v1/tickets/${ticketId}`,
          );
          expect(customerDetail.status).toBe('in_progress');
          expect(await commentText(manager, ticketId)).toContain(providerReply);
        });

      }
    } finally {
      for (const ticket of createdTickets) {
        const admin = await getDemoSession(ticket.adminKey);
        await safeDeleteTicket(admin, ticket.id);
      }
      await switchDemoTenant(manager, managerHome).catch(() => undefined);
    }
  });

  test('MT-2 provider 员工分配边界：允许客户可处理，未分配客户切换 403', async () => {
    const manager = await getDemoSession('mspManager');
    const agent1 = await getDemoSession('mspAgent1');
    const agent2 = await getDemoSession('mspAgent2');
    const managerHome = Number(manager.user.homeTenantId ?? PROVIDER_TENANT_ID);
    const agentHome = Number(agent1.user.homeTenantId ?? PROVIDER_TENANT_ID);
    const run = runId('E2E-MT-AGENT');
    const admin = await getDemoSession('mfgAdmin');
    let ticketId = 0;

    try {
      const created = await apiPost<{ id: number }>(admin, '/api/v1/tickets', {
        title: title(run, 'agent1 分配客户处理'),
        description: 'allocated provider agent customer operation E2E; safe to delete.',
        priority: 'low',
        type: 'incident',
      });
      ticketId = created.id;

      // manager 未分配 review-test(6)；agent1 未分配医疗(4)；agent2 未分配教育(5)。
      const managerDenied = await callRaw(manager.api, 'POST', '/api/v1/auth/switch-tenant', {
        headers: authHeaders(manager),
        data: { tenantId: 6 },
      });
      await expectRejected(managerDenied, [403]);
      const managerMe = await apiGet<{ tenantId: number }>(manager, '/api/v1/auth/me');
      expect(managerMe.tenantId).toBe(managerHome);

      const agent1Denied = await callRaw(agent1.api, 'POST', '/api/v1/auth/switch-tenant', {
        headers: authHeaders(agent1),
        data: { tenantId: 4 },
      });
      await expectRejected(agent1Denied, [403]);
      expect((await apiGet<{ tenantId: number }>(agent1, '/api/v1/auth/me')).tenantId).toBe(agentHome);

      const agent2Denied = await callRaw(agent2.api, 'POST', '/api/v1/auth/switch-tenant', {
        headers: authHeaders(agent2),
        data: { tenantId: 5 },
      });
      await expectRejected(agent2Denied, [403]);

      await switchDemoTenant(agent1, 3);
      expect((await apiGet<{ id: number }>(agent1, `/api/v1/tickets/${ticketId}`)).id).toBe(
        ticketId,
      );
      await apiPost(agent1, `/api/v1/tickets/${ticketId}/comments`, {
        content: '[E2E-MT] agent1 在制造租户写入',
        isInternal: false,
      });
      const updated = await apiPut<{ status: string }>(
        agent1,
        `/api/v1/tickets/${ticketId}/status`,
        { status: 'in_progress' },
      );
      expect(updated.status).toBe('in_progress');

      await switchDemoTenant(agent1, agentHome);
      const providerDetail = await apiGet<{ id: number }>(
        agent1,
        `/api/v1/msp/tickets/${ticketId}`,
        { params: { customerTenantId: 3 } },
      );
      expect(providerDetail.id).toBe(ticketId);
    } finally {
      if (ticketId > 0) {
        await safeDeleteTicket(admin, ticketId);
      }
      await switchDemoTenant(agent1, agentHome).catch(() => undefined);
      await switchDemoTenant(manager, managerHome).catch(() => undefined);
    }
  });

  test('MT-3 跨单据跨租户：制造问题可由 provider 工作台读取，医疗不可见且客户态不可走 MSP', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const manager = await getDemoSession('mspManager');
    const managerHome = Number(manager.user.homeTenantId ?? PROVIDER_TENANT_ID);
    const run = runId('E2E-MT-PROB');
    const problemTitle = title(run, '多租户问题读取');
    const problem = await apiPost<{ id: number }>(admin, '/api/v1/problems', {
      title: problemTitle,
      description: 'multi-tenant problem visibility E2E; safe to delete.',
      priority: 'medium',
    });

    try {
      const providerView = await apiGet<{
        id: number;
        title: string;
        allowedActions?: Array<{ action: string; allowed: boolean; reasonCode?: string }>;
      }>(
        manager,
        `/api/v1/msp/problems/${problem.id}`,
        { params: { customerTenantId: 3 } },
      );
      expect(providerView.id).toBe(problem.id);
      expect(providerView.title).toBe(problemTitle);
      const statusAction = providerView.allowedActions?.find(
        action => action.action === 'status',
      );
      expect(
        statusAction?.allowed,
        `provider 写门禁通过时 allowedActions 不得按家租户 users.role 误判为不可写: ${JSON.stringify(
          providerView.allowedActions,
        )}`,
      ).toBe(true);

      const wrongTenant = await callRaw(manager.api, 'GET', `/api/v1/msp/problems/${problem.id}`, {
        headers: authHeaders(manager),
        params: { customerTenantId: 4 },
      });
      await expectRejected(wrongTenant, [400, 403, 404]);

      const medView = await callRaw(medAdmin.api, 'GET', `/api/v1/problems/${problem.id}`, {
        headers: authHeaders(medAdmin),
      });
      await expectRejected(medView, [403, 404], { forbiddenText: problemTitle });

      await switchDemoTenant(manager, 3);
      const customerContextMsp = await callRaw(
        manager.api,
        'GET',
        `/api/v1/msp/problems/${problem.id}`,
        { headers: authHeaders(manager), params: { customerTenantId: 3 } },
      );
      await expectRejected(customerContextMsp, [403]);
    } finally {
      await switchDemoTenant(manager, managerHome).catch(() => undefined);
      await callRaw(admin.api, 'DELETE', `/api/v1/problems/${problem.id}`, {
        headers: authHeaders(admin),
      }).catch(() => undefined);
    }
  });
});
