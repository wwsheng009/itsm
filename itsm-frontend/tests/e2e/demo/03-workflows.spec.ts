import { expect, test } from '@playwright/test';
import { getDemoSession, type DemoSession } from './fixtures/demo-auth';
import {
  apiDelete,
  apiGet,
  apiPost,
  apiPut,
  callRaw,
  authHeaders,
  runId,
  title,
} from './support/api';
import { deleteServiceRequestAsPlatformAdmin } from './support/cleanup';

test.describe.configure({ mode: 'serial' });

async function createTicket(session: DemoSession, run: string, suffix: string) {
  return apiPost<{ id: number; ticketNumber: string }>(session, '/api/v1/tickets', {
    title: title(run, suffix),
    description: `${suffix} workflow E2E; safe to delete.`,
    priority: 'low',
    type: 'incident',
  });
}

async function safeDelete(session: DemoSession, path: string): Promise<void> {
  await callRaw(session.api, 'DELETE', path, { headers: authHeaders(session) }).catch(() => undefined);
}

async function cleanupServiceRequest(session: DemoSession, tenantId: number, id: number) {
  // 审批完成后的服务请求不能直接删除；先取消再删，平台管理员仅作为授权回归兜底。
  await callRaw(session.api, 'PUT', `/api/v1/service-requests/${id}/status`, {
    headers: authHeaders(session),
    data: { status: 'cancelled' },
  }).catch(() => undefined);
  const deleted = await callRaw(session.api, 'DELETE', `/api/v1/service-requests/${id}`, {
    headers: authHeaders(session),
  });
  if (deleted.status() !== 200) {
    await deleteServiceRequestAsPlatformAdmin(tenantId, id).catch(() => undefined);
  }
  const gone = await callRaw(session.api, 'GET', `/api/v1/service-requests/${id}`, {
    headers: authHeaders(session),
  });
  expect([403, 404], 'service request cleanup should leave no readable record').toContain(gone.status());
}

test.describe('Demo Round 3 · multi-user business workflows', () => {
  test('B1 事件→问题→变更：多角色处理 + 变更生命周期', async () => {
    const user = await getDemoSession('mfgUser');
    const agent = await getDemoSession('mfgAgent');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-FLOW1');

    const incident = await apiPost<{ id: number; incidentNumber: string }>(agent, '/api/v1/incidents', {
      title: title(run, '事件到变更源事件'),
      description: 'Incident to problem to change E2E; safe to delete.',
      priority: 'medium',
      type: 'incident',
    });
    let problemId = 0;
    let changeId = 0;

    try {
      // 事件域写操作由租户管理员执行；用户负责报障，形成多角色链路。
      await apiPost(admin, `/api/v1/incidents/${incident.id}/acknowledge`);
      const acknowledged = await apiGet<{ version: number }>(admin, `/api/v1/incidents/${incident.id}`);
      await apiPut(admin, `/api/v1/incidents/${incident.id}/status`, {
        status: 'in_progress',
        version: acknowledged.version,
      });
      await apiPost(admin, `/api/v1/incidents/${incident.id}/resolve`, {
        resolution: 'E2E 已定位并恢复',
        rootCause: 'E2E 根因：连接池不足',
      });
      const resolved = await apiGet<{ status: string }>(admin, `/api/v1/incidents/${incident.id}`);
      expect(resolved.status).toBe('resolved');

      const problem = await apiPost<{ id: number; status: string }>(
        admin,
        `/api/v1/incidents/${incident.id}/convert-to-problem`,
        {
          title: title(run, '事件转问题'),
          description: 'Converted problem from incident workflow E2E; safe to delete.',
          rootCause: 'E2E 根因：连接池不足',
        },
      );
      problemId = problem.id;
      expect(problem.status).toBe('open');

      const change = await apiPost<{ id: number; status: string }>(admin, '/api/v1/changes', {
        title: title(run, '预防性变更'),
        description: 'Preventive change from problem E2E; safe to delete.',
        justification: '验证事件→问题→变更闭环',
        type: 'normal',
        priority: 'medium',
        impactScope: 'low',
        riskLevel: 'medium',
        implementationPlan: '按窗口实施并保留回滚点。',
        rollbackPlan: '回滚至上一版本并复核核心链路。',
        relatedTickets: [],
      });
      changeId = change.id;
      expect(change.status).toBe('draft');

      await apiPost(admin, `/api/v1/problems/${problemId}/associations`, {
        relatedType: 'change',
        relatedIds: [changeId],
      });
      const associations = await apiGet<{ changes: Array<{ id: number }> }>(
        admin,
        `/api/v1/problems/${problemId}/associations`,
      );
      expect(associations.changes.some(item => item.id === changeId)).toBe(true);

      // 变更生命周期按种子脚本同口径由租户管理员执行。
      await apiPost(admin, `/api/v1/changes/${changeId}/submit`, {});
      await apiPost(admin, `/api/v1/changes/${changeId}/approve`, { comment: 'E2E 审批通过' });
      await apiPost(admin, `/api/v1/changes/${changeId}/schedule`, { comment: 'E2E 排期' });
      await apiPost(admin, `/api/v1/changes/${changeId}/start`, {});
      await apiPost(admin, `/api/v1/changes/${changeId}/complete`, {});
      const completed = await apiGet<{ status: string }>(admin, `/api/v1/changes/${changeId}`);
      expect(completed.status).toBe('completed');

      await apiPost(admin, `/api/v1/incidents/${incident.id}/close`, {
        closeNotes: 'E2E 闭环',
      });
      const closed = await apiGet<{ status: string }>(admin, `/api/v1/incidents/${incident.id}`);
      expect(closed.status).toBe('closed');
    } finally {
      if (changeId > 0) await safeDelete(admin, `/api/v1/changes/${changeId}`);
      if (problemId > 0) await safeDelete(admin, `/api/v1/problems/${problemId}`);
      await safeDelete(admin, `/api/v1/incidents/${incident.id}`);
    }
  });

  test('B2 服务请求：目录下单 → L1/L2/L3 全链路审批', async () => {
    const user = await getDemoSession('mfgUser');
    const lead = await getDemoSession('mfgLead');
    const itadmin = await getDemoSession('mfgItAdmin');
    const secadmin = await getDemoSession('mfgSecAdmin');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-SR');

    const catalogs = await apiGet<{ catalogs: Array<{ id: number; name: string; status: string }> }>(
      user,
      '/api/v1/service-catalogs',
      { params: { page: 1, size: 10 } },
    );
    const catalog = catalogs.catalogs.find(item => item.status === 'enabled');
    expect(catalog, 'demo tenant should have an enabled service catalog item').toBeTruthy();

    const request = await apiPost<{
      id: number;
      status: string;
      currentLevel: number;
      approvals: Array<{ level: number; step: string; status: string }>;
    }>(user, '/api/v1/service-requests', {
      catalogId: catalog!.id,
      title: title(run, '多级审批服务请求'),
      reason: 'Service request multi-level approval E2E; safe to cancel.',
      dataClassification: 'internal',
      complianceAck: true,
    });

    try {
      expect(request.status).toBe('submitted');
      expect(request.approvals.map(item => item.step)).toEqual(['manager', 'it', 'security']);

      const approvals: Array<{ level: string; actor: DemoSession; expected: string; nextLevel: number }> = [
        { level: 'L1', actor: lead, expected: 'manager_approved', nextLevel: 2 },
        { level: 'L2', actor: itadmin, expected: 'it_approved', nextLevel: 3 },
        { level: 'L3', actor: secadmin, expected: 'security_approved', nextLevel: 3 },
      ];
      for (const step of approvals) {
        const response = await callRaw(
          step.actor.api,
          'POST',
          `/api/v1/service-requests/${request.id}/approval`,
          {
            headers: authHeaders(step.actor),
            data: { action: 'approve', comment: `E2E ${step.level} 通过` },
          },
        );
        expect(response.status(), `${step.level} approval HTTP`).toBe(200);
        const body = (await response.json()) as { data?: { status?: string; currentLevel?: number } };
        expect(body.data?.status).toBe(step.expected);
        expect(body.data?.currentLevel).toBe(step.nextLevel);
      }
    } finally {
      await cleanupServiceRequest(admin, 3, request.id);
    }
  });

  test('B2-D2 服务请求 L3 审批在非审批任务待办时仍应成功', async () => {
    const user = await getDemoSession('mfgUser');
    const lead = await getDemoSession('mfgLead');
    const itadmin = await getDemoSession('mfgItAdmin');
    const secadmin = await getDemoSession('mfgSecAdmin');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-SR-L3');
    const catalogs = await apiGet<{ catalogs: Array<{ id: number; status: string }> }>(
      user,
      '/api/v1/service-catalogs',
      { params: { page: 1, size: 10 } },
    );
    const catalog = catalogs.catalogs.find(item => item.status === 'enabled')!;
    const request = await apiPost<{ id: number }>(user, '/api/v1/service-requests', {
      catalogId: catalog.id,
      title: title(run, 'L3 审批回归'),
      reason: 'Service request L3 regression E2E; safe to cancel.',
      dataClassification: 'internal',
      complianceAck: true,
    });
    try {
      await apiPost(lead, `/api/v1/service-requests/${request.id}/approval`, {
        action: 'approve',
        comment: 'L1',
      });
      await apiPost(itadmin, `/api/v1/service-requests/${request.id}/approval`, {
        action: 'approve',
        comment: 'L2',
      });

      // 稳定复现 D2：等 BPMN 实例走到「请求受理」这类非审批用户任务后，
      // 再触发 L3。旧桥接会把该任务当成审批任务而 500；修复后应跳过并成功。
      const deadline = Date.now() + 10_000;
      let activityName = '';
      let pendingNonApprovalTask = false;
      while (Date.now() < deadline) {
        const instances = await apiGet<{
          bPMNProcessInstanceResponse?: Array<{ currentActivityName: string; currentActivityId: string }>;
        }>(admin, '/api/v1/bpmn/process-instances', {
          params: { businessKey: `service_request:${request.id}`, page: 1, pageSize: 20 },
        });
        activityName = instances.bPMNProcessInstanceResponse?.[0]?.currentActivityName ?? '';
        const tasks = await apiGet<{
          bPMNTaskResponse?: Array<{
            businessKey: string;
            status: string;
            taskVariables?: { taskPurpose?: string };
          }>;
        }>(admin, '/api/v1/bpmn/tasks/all', { params: { page: 1, pageSize: 200 } });
        pendingNonApprovalTask = (tasks.bPMNTaskResponse ?? []).some(
          task =>
            task.businessKey === `service_request:${request.id}` &&
            ['created', 'assigned', 'started', 'delegated'].includes(task.status) &&
            !task.taskVariables?.taskPurpose,
        );
        if (activityName === '请求受理' && pendingNonApprovalTask) break;
        await new Promise(resolve => setTimeout(resolve, 500));
      }
      expect(activityName, 'BPMN instance should reach non-approval task 请求受理').toBe('请求受理');
      expect(pendingNonApprovalTask, 'BPMN non-approval task should be pending before L3').toBe(true);

      const l3 = await apiPost<{ status: string; currentLevel: number }>(
        secadmin,
        `/api/v1/service-requests/${request.id}/approval`,
        { action: 'approve', comment: 'L3' },
      );
      expect(l3.status).toBe('security_approved');
      expect(l3.currentLevel).toBe(3);
    } finally {
      await cleanupServiceRequest(admin, 3, request.id);
    }
  });

  test('B3 工单协作：用户提单 → 指派 → 处理/解决/关闭 + 非法流转拒绝', async () => {
    const user = await getDemoSession('mfgUser');
    const agent = await getDemoSession('mfgAgent');
    const itadmin = await getDemoSession('mfgItAdmin');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-TKT');
    const ticket = await createTicket(user, run, '工单协作闭环');

    try {
      const agentId = Number(agent.user.id);
      const assign = await apiPost<{ assigneeId?: number; assigneeMembershipId?: number }>(
        admin,
        `/api/v1/tickets/${ticket.id}/assign`,
        {
        assigneeId: agentId,
        },
      );
      expect(assign.assigneeId).toBe(agentId);
      expect(assign.assigneeMembershipId).toBeGreaterThan(0);

      await apiPost(agent, `/api/v1/tickets/${ticket.id}/comments`, {
        content: '[E2E] 已受理，排查中',
        isInternal: false,
      });

      await apiPost(admin, '/api/v1/tickets/workflow/cc', {
        ticketId: ticket.id,
        ccUsers: [Number(itadmin.user.id)],
        notifyChannels: [],
      });
      const cc = await apiGet<{ records?: Array<{ user?: { id?: number } }> }>(
        admin,
        `/api/v1/tickets/${ticket.id}/cc`,
      );
      expect(cc.records?.some(record => Number(record.user?.id) === Number(itadmin.user.id))).toBe(true);

      const illegal = await callRaw(agent.api, 'PUT', `/api/v1/tickets/${ticket.id}/status`, {
        headers: authHeaders(agent),
        data: { status: 'closed' },
      });
      expect([400, 409, 422]).toContain(illegal.status());

      const inProgress = await apiPut<{ status: string }>(agent, `/api/v1/tickets/${ticket.id}/status`, {
        status: 'in_progress',
      });
      expect(inProgress.status).toBe('in_progress');

      const resolved = await apiPost<{ status: string }>(agent, `/api/v1/tickets/${ticket.id}/resolve`, {
        resolution: 'E2E 已恢复',
      });
      expect(resolved.status).toBe('resolved');

      const closed = await apiPost<{ status: string }>(agent, `/api/v1/tickets/${ticket.id}/close`, {
        closeReason: 'E2E 用户确认',
      });
      expect(closed.status).toBe('closed');

      const comments = await apiGet<{
        comments?: Array<{ content: string; userMembershipId?: number }>;
        items?: Array<{ content: string; userMembershipId?: number }>;
      }>(
        admin,
        `/api/v1/tickets/${ticket.id}/comments`,
      );
      const commentList = comments.comments ?? comments.items ?? [];
      const staffComment = commentList.find(item => item.content.includes('[E2E] 已受理'));
      expect(staffComment, 'agent comment should be visible').toBeTruthy();
      expect(staffComment!.userMembershipId, 'agent comment must carry tenant membership').toBeGreaterThan(0);

    } finally {
      await safeDelete(admin, `/api/v1/tickets/${ticket.id}`);
    }
  });

  test('B4 MSP 工作台：指派托管 → 回复 → 改状态 → 客户侧可见', async () => {
    const user = await getDemoSession('mfgUser');
    const agent = await getDemoSession('mspAgent1');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-MSP');
    const ticket = await createTicket(user, run, 'MSP 跨租户协作');

    try {
      const assigned = await apiPost<{ ID?: number; id?: number; IsManagedByMSP?: boolean }>(
        agent,
        `/api/v1/msp/tickets/${ticket.id}/assign`,
        { customerTenantId: 3 },
      );
      // MSP assign 端点直接返回 ent.Ticket（PascalCase），详情端点才是 camelCase DTO。
      expect(Number(assigned.ID ?? assigned.id)).toBe(ticket.id);

      const detail = await apiGet<{ isManagedByMsp: boolean; assigneeId: number; assigneeName?: string }>(
        agent,
        `/api/v1/msp/tickets/${ticket.id}`,
        { params: { customerTenantId: 3 } },
      );
      expect(detail.isManagedByMsp).toBe(true);
      expect(detail.assigneeId).toBe(Number(agent.user.id));

      const replyContent = `[E2E] 服务商已受理 ${run}`;
      await apiPost(agent, `/api/v1/msp/tickets/${ticket.id}/reply`, {
        customerTenantId: 3,
        content: replyContent,
      });
      const comments = await apiGet<{
        comments?: Array<{ content: string; userMembershipId?: number }>;
        items?: Array<{ content: string; userMembershipId?: number }>;
      }>(agent, `/api/v1/msp/tickets/${ticket.id}/comments`, {
        params: { customerTenantId: 3 },
      });
      const commentList = comments.comments ?? comments.items ?? [];
      const providerReply = commentList.find(item => item.content.includes(replyContent));
      expect(providerReply, 'provider reply should be visible in workbench').toBeTruthy();
      expect(providerReply!.userMembershipId, 'provider reply must carry customer-tenant membership').toBeGreaterThan(0);

      const updated = await apiPost<{ Status?: string; status?: string }>(
        agent,
        `/api/v1/msp/tickets/${ticket.id}/status`,
        { customerTenantId: 3, status: 'in_progress' },
      );
      expect(updated.Status ?? updated.status).toBe('in_progress');

      const customerView = await apiGet<{ status: string }>(user, `/api/v1/tickets/${ticket.id}`);
      expect(customerView.status).toBe('in_progress');

      const customerComments = await apiGet<{
        comments?: Array<{ content: string }>;
        items?: Array<{ content: string }>;
      }>(user, `/api/v1/tickets/${ticket.id}/comments`);
      const customerCommentList = customerComments.comments ?? customerComments.items ?? [];
      expect(customerCommentList.some(item => item.content.includes(replyContent))).toBe(true);
    } finally {
      await safeDelete(admin, `/api/v1/tickets/${ticket.id}`);
    }
  });

  test('B5 平台治理：租户 readiness 9 项（7 required + 2 hint）', async () => {
    const admin = await getDemoSession('admin');
    const readiness = await apiGet<{
      ready: boolean;
      items: Array<{ key: string; required: boolean; count: number }>;
    }>(admin, '/api/v1/tenants/3/readiness');

    expect(readiness.ready).toBe(true);
    expect(readiness.items).toHaveLength(9);
    expect(readiness.items.filter(item => item.required)).toHaveLength(7);
    expect(readiness.items.map(item => item.key)).toEqual(
      expect.arrayContaining(['service_catalog_items', 'ticket_types']),
    );
    expect(readiness.items.find(item => item.key === 'service_catalog_items')?.required).toBe(false);
    expect(readiness.items.find(item => item.key === 'ticket_types')?.required).toBe(false);
  });

  test('D10 租户管理员可删除服务请求', async () => {
    const user = await getDemoSession('mfgUser');
    const admin = await getDemoSession('mfgAdmin');
    const run = runId('E2E-SR-DELETE');
    const catalogs = await apiGet<{ catalogs: Array<{ id: number; status: string }> }>(
      user,
      '/api/v1/service-catalogs',
      { params: { page: 1, size: 10 } },
    );
    const catalog = catalogs.catalogs.find(item => item.status === 'enabled');
    expect(catalog, 'demo tenant should have an enabled service catalog item').toBeTruthy();
    const request = await apiPost<{ id: number }>(user, '/api/v1/service-requests', {
      catalogId: catalog!.id,
      title: title(run, '租户管理员删除权限'),
      reason: 'D10 tenant admin delete E2E; safe to delete.',
      dataClassification: 'internal',
      complianceAck: true,
    });

    let deleted = false;
    try {
      const response = await callRaw(admin.api, 'DELETE', `/api/v1/service-requests/${request.id}`, {
        headers: authHeaders(admin),
      });
      expect(response.status(), `tenant admin DELETE returned HTTP ${response.status()}`).toBe(200);
      deleted = true;
      const gone = await callRaw(admin.api, 'GET', `/api/v1/service-requests/${request.id}`, {
        headers: authHeaders(admin),
      });
      expect(gone.status(), 'soft-deleted service request GET must be HTTP 404').toBe(404);
    } finally {
      if (!deleted) {
        await cleanupServiceRequest(admin, 3, request.id);
      }
    }
  });
});