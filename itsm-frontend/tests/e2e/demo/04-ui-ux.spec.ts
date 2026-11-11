import { expect, test, type Response } from '@playwright/test';
import {
  getDemoSession,
  newAuthenticatedContext,
  switchDemoTenant,
} from './fixtures/demo-auth';
import { apiDelete, apiGet, apiPost, runId, title } from './support/api';
import { deleteChangeAsPlatformAdmin, deleteReleaseAsPlatformAdmin } from './support/cleanup';
import {
  collectPageIssues,
  expectCleanPage,
  expectNoHorizontalScroll,
  expectNoRawBpmnIds,
  expectNoRawI18nKeys,
  expectNoRawRoleCodes,
  gotoAndReady,
} from './support/assertions';

test.describe.configure({ mode: 'serial' });

const KNOWN_UI_NOISE = [
  /\[antd:/i,
  /WORKBENCH_VIEWS_ENABLED/i,
  /status of 404/i,
  /Failed to load resource: the server responded with a status of 404/i,
];

test.describe('Demo Round 4 · frontend UI/UX', () => {
  test('客户管理员核心页面：无裸 key/角色码、无未解释 console/API 失败', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    const issues = collectPageIssues(page, KNOWN_UI_NOISE);

    try {
      const paths = [
        '/dashboard',
        '/tickets',
        '/problems',
        '/changes',
        '/service-catalog',
        '/service-requests',
        '/cmdb',
        '/knowledge',
        '/tickets/7',
      ];
      for (const path of paths) {
        await test.step(`page ${path}`, async () => {
          await gotoAndReady(page, path);
          await expect(page.locator('body'), `${path} body`).toBeVisible();
          await expectNoRawI18nKeys(page);
          await expectNoRawRoleCodes(page);
        });
      }
      await expectCleanPage(page, issues, {
        allowConsoleErrors: [
          // 连续 goto 时前一个页面尚有 React Query 请求在途，导航会 abort 并打印
          // TypeError: Failed to fetch；该请求未出现在 API 4xx/5xx 收集中。
          /TypeError: Failed to fetch/,
        ],
      });
    } finally {
      issues.stop();
      await context.close();
    }
  });

  test(
    'D11 工单详情：无 ai:read 不请求 AI 接口',
    async ({ browser }) => {
      const { context, page, session } = await newAuthenticatedContext(browser, 'mfgUser');
      expect(((session.user.permissions as string[]) ?? [])).not.toContain('ai:read');
      const aiRequests: string[] = [];
      const onResponse = (response: Response) => {
        if (response.url().includes('/api/v1/ai/')) {
          aiRequests.push(`${response.status()} ${response.url()}`);
        }
      };
      page.on('response', onResponse);

      try {
        await gotoAndReady(page, '/tickets/7');
        await page.waitForTimeout(1500);
        await expect(page.getByTestId('ticket-ai-inline')).toHaveCount(0);
        expect(aiRequests, '无 ai:read 的工单详情不应发起 AI 请求').toEqual([]);
      } finally {
        page.off('response', onResponse);
        await context.close();
      }
    },
  );

  test(
    'D14 发布创建/详情：可选择并展示关联变更',
    async ({ browser }) => {
      const admin = await getDemoSession('mfgAdmin');
      const run = runId('E2E-RELEASE-UI');
      const change = await apiPost<{ id: number; title: string }>(admin, '/api/v1/changes', {
        title: title(run, '关联变更'),
        description: 'release UI association E2E; safe to delete.',
        justification: '验证发布表单可选择关联变更',
        type: 'normal',
        priority: 'low',
        impactScope: 'low',
        riskLevel: 'low',
        implementationPlan: '仅用于 E2E UI 验证。',
        rollbackPlan: '删除测试记录。',
        relatedTickets: [],
      });

      let releaseId = 0;
      const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
      try {
        await gotoAndReady(page, '/releases/new');
        const changeLabel = page.getByText('关联变更', { exact: true });
        await expect(changeLabel, '发布表单应提供关联变更入口').toBeVisible();

        await page.getByTestId('release-number-input').fill(`E2E-${run}`);
        await page.getByTestId('release-title-input').fill(title(run, '发布关联变更'));

        const changeCombobox = page.locator('#changeId');
        await changeCombobox.click();
        await changeCombobox.fill(change.title);
        const changeOption = page
          .locator('.ant-select-dropdown')
          .getByText(change.title, { exact: false })
          .first();
        await expect(changeOption).toBeVisible();
        await changeOption.click();

        const createResponsePromise = page.waitForResponse(
          response =>
            response.url().endsWith('/api/v1/releases') &&
            response.request().method() === 'POST',
          { timeout: 20_000 },
        );
        await page.getByTestId('release-submit-button').click();
        const createResponse = await createResponsePromise;
        expect(createResponse.status()).toBe(200);
        const createBody = (await createResponse.json()) as { data?: { id?: number } };
        releaseId = Number(createBody.data?.id ?? 0);
        expect(releaseId).toBeGreaterThan(0);
        const release = await apiGet<{ changeId?: number | null }>(
          admin,
          `/api/v1/releases/${releaseId}`,
        );
        expect(release.changeId).toBe(change.id);

        await gotoAndReady(page, `/releases/${releaseId}`);
        await expect(page.getByText('关联变更', { exact: true })).toBeVisible();
        // 详情页会异步补变更标题；命中限流时降级为“变更 #ID”。两种形态都证明关联已持久化并可见。
        const linkedChange = page
          .getByText(change.title, { exact: false })
          .or(page.getByText(`变更 #${change.id}`, { exact: false }));
        await expect(linkedChange.first()).toBeVisible();
      } finally {
        await context.close();
        if (releaseId > 0) {
          await deleteReleaseAsPlatformAdmin(3, releaseId).catch(() => undefined);
        }
        await deleteChangeAsPlatformAdmin(3, change.id).catch(() => undefined);
        await apiDelete(admin, `/api/v1/changes/${change.id}`).catch(() => undefined);
      }
    },
  );

  test(
    'D12 变更详情：无 ai:read 不展示 AI 影响分析卡',
    async ({ browser }) => {
      const admin = await getDemoSession('mfgAdmin');
      const lead = await getDemoSession('mfgLead');
      const run = runId('E2E-CHANGE-AI');
      const leadId = Number(lead.user.id);
      const change = await apiPost<{ id: number }>(admin, '/api/v1/changes', {
        title: title(run, '无 AI 权限的变更详情'),
        description: 'change detail AI gate E2E; safe to delete.',
        justification: '验证无 ai:read 时不展示 AI 影响分析入口',
        type: 'normal',
        priority: 'low',
        impactScope: 'low',
        riskLevel: 'low',
        implementationPlan: '仅用于 E2E UI 验证。',
        rollbackPlan: '删除测试记录。',
        relatedTickets: [],
      });
      await apiPost(admin, `/api/v1/changes/${change.id}/assign`, { assigneeId: leadId });

      const { context, page } = await newAuthenticatedContext(browser, 'mfgLead');
      const aiRequests: string[] = [];
      const onResponse = (response: Response) => {
        if (response.url().includes('/api/v1/ai/')) {
          aiRequests.push(`${response.status()} ${response.url()}`);
        }
      };
      page.on('response', onResponse);
      try {
        await gotoAndReady(page, `/changes/${change.id}`);
        await page.waitForTimeout(1200);
        await expect(page.getByText('AI 变更影响分析（plan 产物）')).toHaveCount(0);
        expect(aiRequests, '无 ai:read 的变更详情不应发起 AI 请求').toEqual([]);
      } finally {
        page.off('response', onResponse);
        await context.close();
        await deleteChangeAsPlatformAdmin(3, change.id).catch(() => undefined);
        await apiDelete(admin, `/api/v1/changes/${change.id}`).catch(() => undefined);
      }
    },
  );

  test('工单详情：审批链/流转进度不暴露 BPMN 原始标识和 i18n key', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    const issues = collectPageIssues(page, KNOWN_UI_NOISE);
    try {
      await gotoAndReady(page, '/tickets/7');
      await expect(page.locator('body')).toBeVisible();
      await expectNoRawI18nKeys(page);
      await expectNoRawBpmnIds(page);
      await expectCleanPage(page, issues);
    } finally {
      issues.stop();
      await context.close();
    }
  });

  test('MSP 工作台：客户/SLA 列可见，无裸角色码，列表 200', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mspManager');
    const issues = collectPageIssues(page, KNOWN_UI_NOISE);
    try {
      const listResponse = page.waitForResponse(
        response => response.url().includes('/api/v1/msp/workbench/tickets') && response.status() === 200,
        { timeout: 30_000 },
      );
      await gotoAndReady(page, '/msp/workbench');
      await expect(page.getByTestId('workbench-board-switch')).toBeVisible({ timeout: 15_000 });
      const headers = page.locator('.ant-table-thead th');
      await expect(headers.filter({ hasText: '客户' }).first()).toBeVisible();
      await expect(headers.filter({ hasText: 'SLA 截止' }).first()).toBeVisible();
      await expectNoRawRoleCodes(page);
      await expectCleanPage(page, issues, {
        allowFailedRequests: [/\/msp\/workbench\/views(?:[?#]|$)/i],
        allowConsoleErrors: [/WORKBENCH_VIEWS_ENABLED/i],
      });
      expect((await listResponse).status()).toBe(200);
    } finally {
      issues.stop();
      await context.close();
    }
  });

  test('租户 readiness 向导：9 项（7 required + 2 hint）可读', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'admin');
    try {
      await gotoAndReady(page, '/admin/tenants');
      const row = page.locator('tr').filter({ hasText: '华辰智能制造' }).first();
      await expect(row).toBeVisible({ timeout: 15_000 });
      await row.getByRole('button', { name: /开通/ }).first().click();
      await expect(page.getByTestId('readiness-items')).toBeVisible({ timeout: 15_000 });
      for (const key of [
        'roles',
        'permissions',
        'role_permissions',
        'menus',
        'groups',
        'sla_definitions',
        'ci_types',
        'service_catalog_items',
        'ticket_types',
      ]) {
        await expect(page.getByTestId(`readiness-item-${key}`)).toBeVisible();
      }
      await expect(page.getByTestId('readiness-onboarding-nudge')).toBeVisible();
      await page.keyboard.press('Escape');
    } finally {
      await context.close();
    }
  });

  test('响应式：客户管理员核心页面 360/1440 无 body 横向溢出', async ({ browser }) => {
    for (const viewport of [
      { width: 360, height: 800 },
      { width: 1440, height: 900 },
    ]) {
      await test.step(`viewport ${viewport.width}x${viewport.height}`, async () => {
        const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin', { viewport });
        try {
          for (const path of ['/dashboard', '/tickets', '/service-catalog']) {
            await gotoAndReady(page, path);
            try {
              await expectNoHorizontalScroll(page);
            } catch (error) {
              throw new Error(`${path} @ ${viewport.width}x${viewport.height}: ${(error as Error).message}`);
            }
          }
        } finally {
          await context.close();
        }
      });
    }
  });

  test('响应式：平板 768 宽度下 Dashboard 不应 body 横向溢出', async ({
    browser,
  }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin', {
      viewport: { width: 768, height: 900 },
    });
    try {
      await gotoAndReady(page, '/dashboard');
      await expectNoHorizontalScroll(page);
    } finally {
      await context.close();
    }
  });

  test('服务目录：顶部分类计数与标签页一致', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    try {
      await gotoAndReady(page, '/service-catalog');
      const cards = await page.locator('.ant-statistic').evaluateAll(nodes =>
        nodes.map(node => ({
          title: node.querySelector('.ant-statistic-title')?.textContent?.trim() ?? '',
          value: Number(
            (node.querySelector('.ant-statistic-content-value')?.textContent ?? '').replace(/\D/g, ''),
          ),
        })),
      );
      const total = cards.find(card => card.title === '服务总数')?.value ?? NaN;
      expect(total).toBeGreaterThan(0);

      for (const category of ['终端支持', '网络与访问', '账号与权限']) {
        const card = cards.find(item => item.title === category);
        expect(card, `top stat card for ${category}`).toBeTruthy();
        const tab = await page.getByText(new RegExp(`${category}\\s*\\(\\d+\\)`)).first().innerText();
        const tabCount = Number(tab.match(/\((\d+)\)/)?.[1] ?? NaN);
        expect(card!.value, `${category} top card vs tab`).toBe(tabCount);
      }
    } finally {
      await context.close();
    }
  });

  test('多租户跳转：provider 进入客户后 Header 保留返回工作台入口', async ({ browser }) => {
    const session = await getDemoSession('mspManager');
    const homeTenantId = Number(session.user.homeTenantId ?? session.user.tenantId ?? 0);
    expect(homeTenantId, 'mspManager 必须有归属 provider 租户').toBeGreaterThan(0);

    let context: Awaited<ReturnType<typeof newAuthenticatedContext>>['context'] | undefined;
    try {
      await switchDemoTenant(session, 3);
      const authenticated = await newAuthenticatedContext(browser, 'mspManager');
      context = authenticated.context;
      const { page } = authenticated;

      await gotoAndReady(page, '/dashboard');
      await expect(page.getByTestId('deep-tenant-indicator')).toContainText('华辰智能制造');
      const backButton = page.getByTestId('back-to-workbench');
      await expect(backButton, '客户作用域下必须保留返回工作台入口').toBeVisible();

      await backButton.click();
      await expect(page).toHaveURL(/\/msp\/workbench/);
      await expect(page.getByTestId('deep-tenant-indicator')).toHaveCount(0);
    } finally {
      await context?.close();
      await switchDemoTenant(session, homeTenantId).catch(() => undefined);
    }
  });
});