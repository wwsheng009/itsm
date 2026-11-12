import { expect, test } from '@playwright/test';

import {
  getDemoSession,
  newAuthenticatedContext,
  switchDemoTenant,
} from './fixtures/demo-auth';
import { apiDelete, apiPost, apiPut, authHeaders, callRaw, runId, title } from './support/api';
import { gotoAndReady } from './support/assertions';

test.describe.configure({ mode: 'serial' });

/**
 * Round 7 · membership 身份显式展示。
 *
 * 固定使用 demo 种子/回归数据：
 * - ticket 71：客户管理员（membership 4）的公开评论；
 * - ticket 545：provider 深度态（membership 22）的公开评论；
 * - ticket 61：客户管理员上传的附件（membership 4）；
 * - notifications：当前客户管理员的历史通知（均已回填 membership）。
 */
test.describe('Demo Round 7 · membership identity display', () => {
  test('工单评论展示作者 membership 身份', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    try {
      await gotoAndReady(page, '/tickets/71');
      await page.getByRole('tab', { name: /评论|Comments/ }).click();
      const activePane = page.locator('.ant-tabs-tabpane-active');
      await expect(activePane.getByTestId('membership-badge-4').first()).toBeVisible();
      await expect(activePane.getByText('租户身份 #4').first()).toBeVisible();
    } finally {
      await context.close();
    }
  });

  test('provider 客户租户评论展示客户租户 membership', async ({ browser }) => {
    const admin = await getDemoSession('mfgAdmin');
    const manager = await getDemoSession('mspManager');
    const managerHome = Number(manager.user.homeTenantId ?? 2);
    const run = runId('E2E-MEM');
    let ticketId = 0;

    try {
      const ticket = await apiPost<{ id: number }>(admin, '/api/v1/tickets', {
        title: title(run, 'provider membership comment'),
        description: 'membership identity display E2E; safe to delete.',
        priority: 'low',
        type: 'incident',
      });
      ticketId = ticket.id;

      await switchDemoTenant(manager, 3);
      await apiPost(manager, `/api/v1/tickets/${ticketId}/comments`, {
        content: `[E2E-MEM] provider comment ${run}`,
        isInternal: false,
      });

      const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
      try {
        await gotoAndReady(page, `/tickets/${ticketId}`);
        await page.getByRole('tab', { name: /评论|Comments/ }).click();
        await expect(
          page.locator('.ant-tabs-tabpane-active').getByTestId('membership-badge-22').first(),
        ).toBeVisible();
      } finally {
        await context.close();
      }
    } finally {
      await switchDemoTenant(manager, managerHome).catch(() => undefined);
      if (ticketId > 0) {
        await callRaw(admin.api, 'DELETE', `/api/v1/tickets/${ticketId}`, {
          headers: authHeaders(admin),
        }).catch(() => undefined);
      }
    }
  });

  test('附件上传人展示 membership 身份', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    try {
      await gotoAndReady(page, '/tickets/61');
      await page.getByRole('tab', { name: /附件|Attachments/ }).click();
      await expect(
        page.locator('.ant-tabs-tabpane-active').getByTestId('membership-badge-4').first(),
      ).toBeVisible();
    } finally {
      await context.close();
    }
  });

  test('通知接收人展示 membership 身份', async ({ browser }) => {
    const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
    try {
      await gotoAndReady(page, '/notifications');
      await expect(page.locator('[data-testid^="membership-badge-"]').first()).toBeVisible();
    } finally {
      await context.close();
    }
  });

  test('服务请求审批人展示 membership 身份', async ({ browser }) => {
    const user = await getDemoSession('mfgUser');
    const lead = await getDemoSession('mfgLead');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-MEM-SR');
    let requestId = 0;

    try {
      const request = await apiPost<{ id: number }>(user, '/api/v1/service-requests', {
        catalogId: 9,
        title: title(run, 'approval membership'),
        reason: 'service request approval membership badge E2E; safe to cancel.',
        dataClassification: 'internal',
        complianceAck: true,
      });
      requestId = request.id;

      await apiPost(lead, `/api/v1/service-requests/${requestId}/approval`, {
        action: 'approve',
        comment: 'membership badge verification',
      });

      const { context, page } = await newAuthenticatedContext(browser, 'mfgAdmin');
      try {
        await gotoAndReady(page, `/service-requests/${requestId}`);
        await expect(page.getByTestId('membership-badge-6').first()).toBeVisible();
      } finally {
        await context.close();
      }
    } finally {
      if (requestId > 0) {
        await apiPut(admin, `/api/v1/service-requests/${requestId}/status`, {
          status: 'cancelled',
        }).catch(() => undefined);
        await apiDelete(admin, `/api/v1/service-requests/${requestId}`).catch(() => undefined);
      }
    }
  });
});
