import { expect, test } from '@playwright/test';
import { getDemoSession, switchDemoTenant } from './fixtures/demo-auth';
import { apiPost, authHeaders, callRaw, runId, title } from './support/api';

test.describe.configure({ mode: 'serial' });

test.describe('Demo Round 6 · attachment access', () => {
  test('ticket 74 类场景：非 admin/agent 客户角色与 provider 深度态均可列出附件', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const user = await getDemoSession('mfgUser');
    const itAdmin = await getDemoSession('mfgItAdmin');
    const secAdmin = await getDemoSession('mfgSecAdmin');
    const manager = await getDemoSession('mspManager');
    const managerHome = Number(manager.user.homeTenantId ?? 2);
    const run = runId('E2E-ATT-ACCESS');
    let ticketId = 0;
    let attachmentId = 0;

    try {
      const ticket = await apiPost<{ id: number }>(admin, '/api/v1/tickets', {
        title: title(run, '附件访问回归'),
        description: 'attachment access regression; safe to delete.',
        priority: 'low',
        type: 'incident',
      });
      ticketId = ticket.id;

      const upload = await admin.api.post(`/api/v1/tickets/${ticketId}/attachments`, {
        headers: authHeaders(admin),
        multipart: {
          file: {
            name: 'attachment-access.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('attachment access regression'),
          },
        },
      });
      const uploadText = await upload.text();
      expect(upload.status(), uploadText).toBe(200);
      const uploadBody = JSON.parse(uploadText) as {
        data?: { id?: number; uploadedByMembershipId?: number };
      };
      attachmentId = Number(uploadBody.data?.id ?? 0);
      expect(attachmentId).toBeGreaterThan(0);
      expect(
        uploadBody.data?.uploadedByMembershipId,
        'attachment uploader must carry tenant membership',
      ).toBeGreaterThan(0);

      for (const [label, session] of [
        ['end_user', user],
        ['it_admin', itAdmin],
        ['security_admin', secAdmin],
      ] as const) {
        const listed = await callRaw(session.api, 'GET', `/api/v1/tickets/${ticketId}/attachments`, {
          headers: authHeaders(session),
        });
        expect(listed.status(), `${label} attachment list: ${await listed.text()}`).toBe(200);
      }

      try {
        await switchDemoTenant(manager, 3);
        const providerListed = await callRaw(
          manager.api,
          'GET',
          `/api/v1/tickets/${ticketId}/attachments`,
          { headers: authHeaders(manager) },
        );
        expect(
          providerListed.status(),
          `provider customer-context attachment list: ${await providerListed.text()}`,
        ).toBe(200);
      } finally {
        await switchDemoTenant(manager, managerHome).catch(() => undefined);
      }
    } finally {
      if (ticketId > 0 && attachmentId > 0) {
        await callRaw(admin.api, 'DELETE', `/api/v1/tickets/${ticketId}/attachments/${attachmentId}`, {
          headers: authHeaders(admin),
        }).catch(() => undefined);
      }
      if (ticketId > 0) {
        await callRaw(admin.api, 'DELETE', `/api/v1/tickets/${ticketId}`, {
          headers: authHeaders(admin),
        }).catch(() => undefined);
      }
    }
  });
});
