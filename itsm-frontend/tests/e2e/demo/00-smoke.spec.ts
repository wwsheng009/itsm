import { expect, test } from '@playwright/test';
import {
  getDemoRuntime,
  getDemoSession,
  newAuthenticatedContext,
} from './fixtures/demo-auth';
import { collectPageIssues, expectCleanPage, gotoAndReady } from './support/assertions';
import { apiGet } from './support/api';

test.describe.configure({ mode: 'serial' });

test.describe('Demo Round 0 · environment and login smoke', () => {
  test('238 环境闸门：health/ready/version/saas_msp', async () => {
    const manager = await getDemoSession('mspManager');
    const runtime = await getDemoRuntime(manager);

    expect(runtime.ready, 'readyz should be ready').toBe(true);
    expect(runtime.version, 'version should be a 1.8 dev build').toMatch(/^1\.8\.0-dev\./);
    expect(runtime.deploymentMode, 'deployment mode must be saas_msp').toBe('saas_msp');
  });

  test('多角色登录矩阵：平台/服务商/客户代表账号', async () => {
    const keys = ['admin', 'mspManager', 'mfgAdmin', 'mfgUser', 'medAdmin', 'eduAdmin'] as const;
    for (const key of keys) {
      const session = await getDemoSession(key);
      expect(session.token, `${key} should have an access token`).toBeTruthy();
      expect(session.user.username, `${key} username`).toBe(session.account.username);
      expect(session.user.tenantId, `${key} tenant id`).toBeTruthy();
    }

    const manager = await getDemoSession('mspManager');
    expect(manager.user.mspRole).toBe('provider_admin');
    expect(manager.account.username).toBe('demo.msp.manager');
  });

  test(
    'MSP 经理：登录角色与 /auth/me 的 effective role 一致',
    async () => {
      const manager = await getDemoSession('mspManager');
      expect(manager.loginUser.role, '登录响应应提级为 msp_manager').toBe('msp_manager');

      const me = await apiGet<{ role: string; mspRole?: string }>(manager, '/api/v1/auth/me');
      expect(me.mspRole).toBe('provider_admin');
      expect(me.role, '/auth/me 必须返回与登录相同的 effective role').toBe(
        manager.loginUser.role,
      );
    },
  );

  test('客户管理员关键页面 smoke：dashboard/tickets/problems/changes/service-catalog', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    const issues = collectPageIssues(page, [
      /\[antd:/i,
      /status of 404/i,
      /Failed to load resource: the server responded with a status of 404/i,
    ]);

    try {
      for (const path of ['/dashboard', '/tickets', '/problems', '/changes', '/service-catalog']) {
        await gotoAndReady(page, path);
        await expect(page.locator('body'), `${path} body`).toBeVisible();
        await expect(page.locator('body'), `${path} should not be empty`).not.toHaveText(/^\s*$/);
      }
      await expectCleanPage(page, issues);
    } finally {
      issues.stop();
      await context.close();
    }
  });

  test('MSP 工作台 smoke：跨客户列表接口 200 且页面可用', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mspManager');
    const issues = collectPageIssues(page, [/\[antd:/i, /WORKBENCH_VIEWS_ENABLED/i, /status of 404/i]);

    try {
      const listResponse = page.waitForResponse(
        response => response.url().includes('/api/v1/msp/workbench/tickets') && response.status() === 200,
        { timeout: 30_000 },
      );
      await gotoAndReady(page, '/msp/workbench');
      await expect(page.getByTestId('workbench-board-switch')).toBeVisible({ timeout: 15_000 });
      await expect(page.getByRole('heading', { name: '跨客户工作台' })).toBeVisible();
      const response = await listResponse;
      expect(response.status()).toBe(200);
      await expectCleanPage(page, issues, {
        allowFailedRequests: [/WORKBENCH_VIEWS_ENABLED/i, /\/msp\/workbench\/views(?:[?#]|$)/i],
      });
    } finally {
      issues.stop();
      await context.close();
    }
  });

  test('终端用户页面 smoke：dashboard 与我的工单入口', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgUser');
    const issues = collectPageIssues(page, [/\[antd:/i, /status of 404/i]);

    try {
      await gotoAndReady(page, '/dashboard');
      await expect(page.locator('body')).toBeVisible();
      await gotoAndReady(page, '/tickets');
      await expect(page.locator('body')).toBeVisible();
      // end_user 无 dashboard:read：Dashboard 不得请求 overview，也不应产生权限错误。
      await expectCleanPage(page, issues);
    } finally {
      issues.stop();
      await context.close();
    }
  });
});