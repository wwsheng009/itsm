import { expect, test } from '@playwright/test';
import { getDemoSession, type DemoSession } from './fixtures/demo-auth';
import {
  apiDelete,
  apiGet,
  apiPost,
  authHeaders,
  callRaw,
  expectRejected,
  runId,
  title,
} from './support/api';
import { deleteChangeAsPlatformAdmin, deleteReleaseAsPlatformAdmin } from './support/cleanup';

test.describe.configure({ mode: 'serial' });

async function createTicket(session: DemoSession, run: string, suffix: string) {
  return apiPost<{ id: number; ticketNumber: string; title: string }>(session, '/api/v1/tickets', {
    title: title(run, suffix),
    description: `${suffix} for demo association E2E; safe to delete.`,
    priority: 'low',
    type: 'incident',
  });
}

async function safeDelete(session: DemoSession, path: string): Promise<void> {
  await callRaw(session.api, 'DELETE', path, { headers: authHeaders(session) }).catch(() => undefined);
}

async function relationsOf(session: DemoSession, ticketId: number) {
  const data = await apiGet<{
    relatedTickets?: Array<{ id: number; title: string }> | null;
    parentChain?: unknown[] | null;
    childrenTree?: unknown[] | null;
  }>(session, `/api/v1/tickets/${ticketId}/relations`);
  return data.relatedTickets ?? [];
}

test.describe('Demo Round 2 · cross-record associations', () => {
  test('A1 工单↔工单：创建、双向可见、删除、自关联/跨租户拒绝', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const run = runId('E2E-REL');
    const source = await createTicket(admin, run, 'relation source');
    const target = await createTicket(admin, run, 'relation target');
    const medTicket = (await apiGet<{ items?: Array<{ id: number }>; tickets?: Array<{ id: number }> }>(
      medAdmin,
      '/api/v1/tickets?page=1&size=10',
    )).items ??
      (await apiGet<{ tickets?: Array<{ id: number }> }>(medAdmin, '/api/v1/tickets?page=1&size=10')).tickets ??
      [];

    try {
      const created = await apiPost<{
        id: number;
        sourceTicketId: number;
        targetTicketId: number;
        relationType: string;
      }>(admin, '/api/v1/tickets/relations', {
        sourceTicketId: source.id,
        targetTicketId: target.id,
        relationType: 'relates_to',
        description: `${run} relation`,
      });
      expect(created.sourceTicketId).toBe(source.id);
      expect(created.targetTicketId).toBe(target.id);
      expect(created.relationType).toBe('relates_to');

      expect((await relationsOf(admin, source.id)).some(item => item.id === target.id)).toBe(true);
      expect((await relationsOf(admin, target.id)).some(item => item.id === source.id)).toBe(true);

      const self = await callRaw(admin.api, 'POST', '/api/v1/tickets/relations', {
        headers: authHeaders(admin),
        data: { sourceTicketId: source.id, targetTicketId: source.id, relationType: 'relates_to' },
      });
      expect([400, 409, 422]).toContain(self.status());

      if (medTicket.length > 0) {
        const cross = await callRaw(admin.api, 'POST', '/api/v1/tickets/relations', {
          headers: authHeaders(admin),
          data: { sourceTicketId: source.id, targetTicketId: medTicket[0].id, relationType: 'relates_to' },
        });
        await expectRejected(cross, [400, 403, 404]);
      }

      const removed = await apiDelete<{ removed: boolean }>(
        admin,
        `/api/v1/tickets/relations?sourceTicketId=${source.id}&targetTicketId=${target.id}`,
      );
      expect(removed.removed).toBe(true);
      expect((await relationsOf(admin, source.id)).some(item => item.id === target.id)).toBe(false);
      expect((await relationsOf(admin, target.id)).some(item => item.id === source.id)).toBe(false);
    } finally {
      await safeDelete(admin, `/api/v1/tickets/${source.id}`);
      await safeDelete(admin, `/api/v1/tickets/${target.id}`);
    }
  });

  test('A2 问题↔工单/事件/变更：添加、读取、移除、跨租户拒绝', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const run = runId('E2E-PROB');
    const ticket = await createTicket(admin, run, 'problem-linked ticket');
    const problem = await apiPost<{ id: number }>(admin, '/api/v1/problems', {
      title: title(run, 'association problem'),
      description: 'Problem association E2E; safe to delete.',
      priority: 'medium',
    });

    const incidents = await apiGet<{ incidents?: Array<{ id: number }> }>(admin, '/api/v1/incidents?page=1&size=5');
    const changes = await apiGet<{ changes?: Array<{ id: number }> }>(admin, '/api/v1/changes?page=1&size=5');
    const medTickets = await apiGet<{ items?: Array<{ id: number }>; tickets?: Array<{ id: number }> }>(
      medAdmin,
      '/api/v1/tickets?page=1&size=10',
    );

    try {
      await apiPost(admin, `/api/v1/problems/${problem.id}/associations`, {
        relatedType: 'ticket',
        relatedIds: [ticket.id],
      });
      if (incidents.incidents?.[0]) {
        await apiPost(admin, `/api/v1/problems/${problem.id}/associations`, {
          relatedType: 'incident',
          relatedIds: [incidents.incidents[0].id],
        });
      }
      if (changes.changes?.[0]) {
        await apiPost(admin, `/api/v1/problems/${problem.id}/associations`, {
          relatedType: 'change',
          relatedIds: [changes.changes[0].id],
        });
      }

      const associations = await apiGet<{
        tickets: Array<{ id: number }>;
        incidents: Array<{ id: number }>;
        changes: Array<{ id: number }>;
      }>(admin, `/api/v1/problems/${problem.id}/associations`);
      expect(associations.tickets.some(item => item.id === ticket.id)).toBe(true);
      if (incidents.incidents?.[0]) {
        expect(associations.incidents.some(item => item.id === incidents.incidents[0].id)).toBe(true);
      }
      if (changes.changes?.[0]) {
        expect(associations.changes.some(item => item.id === changes.changes[0].id)).toBe(true);
      }

      const medTicketId = medTickets.items?.[0]?.id ?? medTickets.tickets?.[0]?.id;
      if (medTicketId) {
        const cross = await callRaw(admin.api, 'POST', `/api/v1/problems/${problem.id}/associations`, {
          headers: authHeaders(admin),
          data: { relatedType: 'ticket', relatedIds: [medTicketId] },
        });
        await expectRejected(cross, [400, 403, 404]);
      }

      await apiDelete(admin, `/api/v1/problems/${problem.id}/associations`, {
        headers: authHeaders(admin),
        data: { relatedType: 'ticket', relatedId: ticket.id },
      });
      const afterRemove = await apiGet<{ tickets: Array<{ id: number }> }>(
        admin,
        `/api/v1/problems/${problem.id}/associations`,
      );
      expect(afterRemove.tickets.some(item => item.id === ticket.id)).toBe(false);
    } finally {
      await safeDelete(admin, `/api/v1/problems/${problem.id}`);
      await safeDelete(admin, `/api/v1/tickets/${ticket.id}`);
    }
  });

  test('A3 事件→问题：转换生成问题并建立关联', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-CONV');
    const incident = await apiPost<{ id: number; incidentNumber: string }>(admin, '/api/v1/incidents', {
      title: title(run, 'convert incident'),
      description: 'Incident to problem conversion E2E; safe to delete.',
      priority: 'medium',
      type: 'incident',
    });
    let problemId = 0;

    try {
      const problem = await apiPost<{ id: number; title: string }>(
        admin,
        `/api/v1/incidents/${incident.id}/convert-to-problem`,
        {
          title: title(run, 'converted problem'),
          description: 'Converted problem from incident E2E; safe to delete.',
          rootCause: 'E2E root cause',
        },
      );
      problemId = problem.id;
      expect(problem.id).toBeGreaterThan(0);
      expect(problem.title).toContain('converted problem');

      const associations = await apiGet<{ incidents: Array<{ id: number }> }>(
        admin,
        `/api/v1/problems/${problemId}/associations`,
      );
      expect(
        associations.incidents.some(item => item.id === incident.id),
        'converted problem should be associated with source incident',
      ).toBe(true);

      const convertedIncident = await apiGet<{ problemId?: number; problem?: { id: number } }>(
        admin,
        `/api/v1/incidents/${incident.id}`,
      );
      const exposedProblemId = Number(
        convertedIncident.problemId ?? convertedIncident.problem?.id ?? 0,
      );
      expect(exposedProblemId, 'incident detail should expose the converted problemId').toBe(problemId);
    } finally {
      if (problemId > 0) await safeDelete(admin, `/api/v1/problems/${problemId}`);
      await safeDelete(admin, `/api/v1/incidents/${incident.id}`);
    }
  });

  test('A3-D2 事件详情转换后应返回 problemId', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-CONV-ID');
    const incident = await apiPost<{ id: number }>(admin, '/api/v1/incidents', {
      title: title(run, 'convert incident problemId'),
      description: 'Incident conversion problemId regression; safe to delete.',
      priority: 'low',
      type: 'incident',
    });
    let problemId = 0;
    try {
      const problem = await apiPost<{ id: number }>(
        admin,
        `/api/v1/incidents/${incident.id}/convert-to-problem`,
        {
          title: title(run, 'converted problem problemId'),
          description: 'Converted problem problemId regression; safe to delete.',
        },
      );
      problemId = problem.id;
      const reread = await apiGet<{ problemId?: number }>(admin, `/api/v1/incidents/${incident.id}`);
      expect(reread.problemId).toBe(problemId);
    } finally {
      if (problemId > 0) await safeDelete(admin, `/api/v1/problems/${problemId}`);
      await safeDelete(admin, `/api/v1/incidents/${incident.id}`);
    }
  });

  test('A4 变更↔CMDB：数字 ID 命中影响分析；关联工单可回读', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-CHG-ID');
    const ticket = await createTicket(admin, run, 'change linked ticket');
    const ciList = await apiGet<{ items: Array<{ id: number; ciNumber: string; name: string }> }>(
      admin,
      '/api/v1/cmdb/cis?size=5',
    );
    const ci = ciList.items[0];
    expect(ci, 'demo tenant should have at least one CI').toBeTruthy();

    const change = await apiPost<{ id: number; affectedCis: string[]; relatedTickets: string[] }>(
      admin,
      '/api/v1/changes',
      {
        title: title(run, 'change by ci id'),
        description: 'Change affectedCis by numeric id E2E.',
        type: 'normal',
        priority: 'medium',
        affectedCis: [String(ci.id)],
        relatedTickets: [ticket.ticketNumber],
      },
    );

    try {
      expect(change.affectedCis).toEqual([String(ci.id)]);
      expect(change.relatedTickets).toEqual([ticket.ticketNumber]);

      const impact = await apiGet<{
        totalAffectedCIs: number;
        criticalCICount: number;
        affectedCis: number[];
        requiresBackoutPlan: boolean;
      }>(admin, `/api/v1/changes/${change.id}/cmdb-impact`);
      expect(impact.totalAffectedCIs).toBe(1);
      expect(impact.affectedCis).toEqual([ci.id]);
      expect(impact.requiresBackoutPlan).toBe(true);

      const reread = await apiGet<{ affectedCis: string[]; relatedTickets: string[] }>(
        admin,
        `/api/v1/changes/${change.id}`,
      );
      expect(reread.affectedCis).toEqual([String(ci.id)]);
      expect(reread.relatedTickets).toEqual([ticket.ticketNumber]);
    } finally {
      await safeDelete(admin, `/api/v1/changes/${change.id}`);
      await safeDelete(admin, `/api/v1/tickets/${ticket.id}`);
    }
  });

  test('A4-D1 变更受影响 CI 使用 ciNumber 时应同样命中影响分析', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-CHG-NUM');
    const ciList = await apiGet<{ items: Array<{ id: number; ciNumber: string; name: string }> }>(
      admin,
      '/api/v1/cmdb/cis?size=5',
    );
    const ci = ciList.items[0];
    const change = await apiPost<{ id: number }>(admin, '/api/v1/changes', {
      title: title(run, 'change by ci number'),
      description: 'Change affectedCis by ciNumber E2E (known defect).',
      type: 'normal',
      priority: 'medium',
      affectedCis: [ci.ciNumber],
    });

    try {
      const impact = await apiGet<{ totalAffectedCIs: number; affectedCis: number[] }>(
        admin,
        `/api/v1/changes/${change.id}/cmdb-impact`,
      );
      expect(
        impact.totalAffectedCIs,
        `affectedCis=${ci.ciNumber}, impact=${JSON.stringify(impact)}`,
      ).toBeGreaterThan(0);
    } finally {
      await safeDelete(admin, `/api/v1/changes/${change.id}`);
    }
  });

  test('A5 工单↔CMDB：配置项反向查询返回数组', async () => {
    const admin = await getDemoSession('mfgAdmin');
    const tickets = await apiGet<{ items: Array<{ id: number }>; tickets: Array<{ id: number }> }>(
      admin,
      '/api/v1/tickets?page=1&size=5',
    );
    const ticketId = tickets.items?.[0]?.id ?? tickets.tickets?.[0]?.id;
    expect(ticketId, 'demo tenant should have at least one ticket').toBeTruthy();
    const items = await apiGet<unknown[]>(admin, `/api/v1/tickets/${ticketId}/configuration-items`);
    expect(Array.isArray(items)).toBe(true);
  });

  test('A6 发布↔变更：跨租户 changeId 必须拒绝', async () => {
    const mfgAdmin = await getDemoSession('mfgAdmin');
    const medAdmin = await getDemoSession('medAdmin');
    const run = runId('E2E-RELEASE-XT');

    const medChange = await apiPost<{ id: number; title: string }>(medAdmin, '/api/v1/changes', {
      title: title(run, '医疗租户变更（不可被制造发布引用）'),
      description: 'cross-tenant release association E2E; safe to delete.',
      justification: '验证发布↔变更租户边界',
      type: 'normal',
      priority: 'low',
      impactScope: 'low',
      riskLevel: 'low',
      implementationPlan: '仅用于 E2E 边界验证。',
      rollbackPlan: '删除测试记录。',
      relatedTickets: [],
    });

    let releaseId = 0;
    try {
      const response = await callRaw(mfgAdmin.api, 'POST', '/api/v1/releases', {
        headers: authHeaders(mfgAdmin),
        data: {
          releaseNumber: run,
          title: title(run, '跨租户发布关联'),
          description: 'cross-tenant changeId must be rejected',
          type: 'minor',
          environment: 'staging',
          severity: 'low',
          changeId: medChange.id,
          requiresApproval: false,
        },
      });
      const bodyText = await response.text();
      try {
        releaseId = Number((JSON.parse(bodyText) as { data?: { id?: number } })?.data?.id) || 0;
      } catch {
        releaseId = 0;
      }

      await expectRejected(response, [400, 403, 404], { forbiddenText: medChange.title });
    } finally {
      if (releaseId > 0) {
        await deleteReleaseAsPlatformAdmin(3, releaseId).catch(() => undefined);
      }
      await deleteChangeAsPlatformAdmin(4, medChange.id).catch(() => undefined);
      const releases = await apiGet<{ releases: Array<{ id: number; title: string }> }>(
        mfgAdmin,
        '/api/v1/releases',
        { params: { page: 1, pageSize: 100 } },
      );
      expect(releases.releases.some(item => item.title.includes(run)), 'E2E release must be cleaned').toBe(
        false,
      );
      const changes = await apiGet<{ changes: Array<{ id: number; title: string }> }>(
        medAdmin,
        '/api/v1/changes',
        { params: { page: 1, pageSize: 100 } },
      );
      expect(changes.changes.some(item => item.title.includes(run)), 'E2E change must be cleaned').toBe(
        false,
      );
    }
  });
});