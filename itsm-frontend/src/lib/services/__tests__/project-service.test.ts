import { projectService } from '../project-service';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
    patch: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;
const mockPut = httpClient.put as jest.Mock;
const mockDelete = httpClient.delete as jest.Mock;

describe('ProjectService', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('listProjects', () => {
    it('should fetch projects with pagination envelope', async () => {
      const items = [
        { id: 1, name: 'Project Alpha', code: 'PA', status: 'active' },
        { id: 2, name: 'Project Beta', code: 'PB', status: 'completed' },
      ];
      mockGet.mockResolvedValue({ items, total: 2, page: 1, pageSize: 20 });

      const result = await projectService.listProjects();

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects', {});
      expect(result.items).toHaveLength(2);
      expect(result.items[0].name).toBe('Project Alpha');
      expect(result.total).toBe(2);
    });

    it('should support query filters and empty envelope', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 });

      const result = await projectService.listProjects({ page: 1, pageSize: 20, status: 'active', q: 'PRJ' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects', { page: 1, pageSize: 20, status: 'active', q: 'PRJ' });
      expect(result.items).toEqual([]);
    });
  });

  describe('createProject', () => {
    it('should create a project', async () => {
      const request = { name: 'New Project', code: 'NP', departmentId: 1, managerId: 5 };
      const response = { id: 3, ...request };
      mockPost.mockResolvedValue(response);

      const result = await projectService.createProject(request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects', request);
      expect(result).toEqual(response);
    });

    it('should create a project with minimal fields', async () => {
      const request = { name: 'Minimal', code: 'MIN' };
      const response = { id: 4, ...request };
      mockPost.mockResolvedValue(response);

      const result = await projectService.createProject(request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects', request);
      expect(result.id).toBe(4);
    });
  });

  describe('updateProject', () => {
    it('should update a project by id', async () => {
      const updateData = { name: 'Updated Project' };
      const response = { id: 1, name: 'Updated Project', code: 'PA' };
      mockPut.mockResolvedValue(response);

      const result = await projectService.updateProject(1, updateData);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/1', updateData);
      expect(result.name).toBe('Updated Project');
    });
  });

  describe('ownerTeamCandidates (A2 回归遗留)', () => {
    it('should fetch owner team candidates with optional keyword and limit', async () => {
      const items = [
        { id: 1, name: '交付部', code: 'TEAM-DELIVERY' },
        { id: 2, name: '运维部', code: 'TEAM-OPS' },
      ];
      mockGet.mockResolvedValue(items);

      const result = await projectService.ownerTeamCandidates({ q: '部', limit: 20 });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/owner-team-candidates', {
        q: '部',
        limit: 20,
      });
      expect(result).toEqual(items);
    });

    it('should default to empty query', async () => {
      mockGet.mockResolvedValue([]);

      const result = await projectService.ownerTeamCandidates();

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/owner-team-candidates', {});
      expect(result).toEqual([]);
    });
  });

  describe('deleteProject', () => {
    it('should delete a project by id', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.deleteProject(3);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/3');
    });
  });

  describe('milestones (P1/PE-14)', () => {
    it('should list milestones with optional status filter', async () => {
      const items = [{ id: 11, projectId: 1, name: 'M1', status: 'planned' }];
      mockGet.mockResolvedValue(items);

      const result = await projectService.listMilestones(1, { status: 'planned' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/1/milestones', { status: 'planned' });
      expect(result).toEqual(items);
    });

    it('should create a milestone', async () => {
      const request = { name: '上线', dueDate: '2026-11-30' };
      mockPost.mockResolvedValue({ id: 12, projectId: 1, status: 'planned', ...request });

      const result = await projectService.createMilestone(1, request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/1/milestones', request);
      expect(result.status).toBe('planned');
    });

    it('should transition milestone status via dedicated action endpoint', async () => {
      mockPost.mockResolvedValue({ id: 12, status: 'reached' });

      await projectService.transitionMilestone(1, 12, 'reach');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/1/milestones/12/status', { action: 'reach' });
    });

    it('should delete a milestone', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.deleteMilestone(1, 12);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/1/milestones/12');
    });
  });

  describe('task dependencies (P1/PE-14, FS only)', () => {
    it('should list project dependencies in one call', async () => {
      const items = [{ id: 1, taskId: 7, dependsOnTaskId: 5, depType: 'FS' }];
      mockGet.mockResolvedValue(items);

      const result = await projectService.listDependencies(1);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/1/dependencies');
      expect(result).toEqual(items);
    });

    it('should add a FS dependency by default', async () => {
      mockPost.mockResolvedValue({ id: 2, taskId: 7, dependsOnTaskId: 5, depType: 'FS' });

      await projectService.addDependency(1, 7, 5);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/1/tasks/7/dependencies', {
        dependsOnTaskId: 5,
        depType: 'FS',
      });
    });

    it('should remove a dependency by predecessor task id', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.removeDependency(1, 7, 5);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/1/tasks/7/dependencies/5');
    });
  });

  describe('baselines (P1/PE-13)', () => {
    it('should list baseline versions', async () => {
      const items = [{ id: 1, projectId: 3, version: 2, reason: '范围变更', createdAt: '2026-10-17T00:00:00Z' }];
      mockGet.mockResolvedValue(items);

      const result = await projectService.listBaselines(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/baselines');
      expect(result[0].version).toBe(2);
    });

    it('should freeze a new baseline with reason', async () => {
      mockPost.mockResolvedValue({ id: 4, projectId: 3, version: 1 });

      const result = await projectService.createBaseline(3, '首次立项');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/baselines', { reason: '首次立项' });
      expect(result.version).toBe(1);
    });

    it('should fetch latest baseline diff', async () => {
      const diff = {
        baselineId: 1,
        version: 1,
        frozenAt: '2026-10-01T00:00:00Z',
        createdAt: '2026-10-01T00:00:00Z',
        taskSummary: { added: 1, removed: 0, changed: 2, unchanged: 3 },
        milestoneSummary: { added: 0, removed: 0, changed: 1, unchanged: 0 },
        taskChanges: [],
        milestoneChanges: [],
      };
      mockGet.mockResolvedValue(diff);

      const result = await projectService.baselineDiff(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/baselines/diff');
      expect(result.taskSummary.changed).toBe(2);
    });
  });

  describe('gates (P1/PE-12)', () => {
    it('should list gates with envelope and order', async () => {
      const initiation = { id: 11, projectId: 3, gate: 'initiation', status: 'passed', blockers: [] };
      mockGet.mockResolvedValue({
        items: [initiation],
        gates: ['initiation', 'plan', 'acceptance', 'closure'],
        projectGates: [initiation],
      });

      const result = await projectService.listGates(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/gates');
      expect(result.items).toHaveLength(1);
      expect(result.items[0].status).toBe('passed');
      expect(result.gates).toEqual(['initiation', 'plan', 'acceptance', 'closure']);
    });

    it('should fallback to projectGates alias and default order', async () => {
      mockGet.mockResolvedValue({ projectGates: [{ id: 12, gate: 'plan', status: 'in_review', blockers: [] }] });

      const result = await projectService.listGates(3);

      expect(result.items).toHaveLength(1);
      expect(result.gates).toEqual(['initiation', 'plan', 'acceptance', 'closure']);
    });

    it('should submit a gate', async () => {
      mockPost.mockResolvedValue({ id: 13, gate: 'initiation', status: 'passed', processInstanceId: 0, blockers: [] });

      const result = await projectService.submitGate(3, 'initiation');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/gates/initiation/submit', {});
      expect(result.status).toBe('passed');
    });

    it('should decide a gate with reason on reject', async () => {
      mockPost.mockResolvedValue({ id: 14, gate: 'closure', status: 'rejected', decisionComment: '单据未关闭' });

      const result = await projectService.decideGate(3, 'closure', 'reject', '单据未关闭');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/gates/closure/decision', {
        decision: 'reject',
        comment: '单据未关闭',
      });
      expect(result.status).toBe('rejected');
    });
  });

  describe('closure checklist (P1/PE-15)', () => {
    it('should fetch closure checklist with items and blockers', async () => {
      mockGet.mockResolvedValue({
        id: 21,
        projectId: 3,
        status: 'pending',
        items: [
          { key: 'tasks', title: '任务清零', status: 'done', blocking: true },
          { key: 'retro', title: '复盘 KB 草稿', status: 'pending', blocking: true, action: 'generate_retro' },
          { key: 'csat', title: 'CSAT 调研', status: 'pending', blocking: false, action: 'trigger_csat' },
        ],
        blockers: [{ type: 'retro', id: 0, title: '复盘 KB 草稿未生成', status: 'pending' }],
        retroKbArticleId: 0,
        membersRecycledCount: 0,
        csatSurveyId: 0,
        version: 1,
      });

      const result = await projectService.getClosure(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/closure');
      expect(result.items).toHaveLength(3);
      expect(result.blockers[0].type).toBe('retro');
      expect(result.status).toBe('pending');
    });

    it('should generate retro draft and return article id', async () => {
      mockPost.mockResolvedValue({ retroKbArticleId: 7001, status: 'pending' });

      const result = await projectService.generateRetroDraft(3);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/closure/retro', {});
      expect(result.retroKbArticleId).toBe(7001);
    });

    it('should confirm or waive assets registration', async () => {
      mockPost.mockResolvedValueOnce({ assetsConfirmedAt: '2026-10-21T00:00:00Z', assetsWaived: false });
      const confirmed = await projectService.confirmClosureAssets(3, 'confirm', { note: '台账 A-12' });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/closure/assets', {
        action: 'confirm',
        note: '台账 A-12',
      });
      expect(confirmed.assetsConfirmedAt).toBeTruthy();

      mockPost.mockResolvedValueOnce({ assetsWaived: true, assetsWaiveReason: '无新增资产' });
      const waived = await projectService.confirmClosureAssets(3, 'waive', { reason: '无新增资产' });
      expect(mockPost).toHaveBeenLastCalledWith('/api/v1/projects/3/closure/assets', {
        action: 'waive',
        reason: '无新增资产',
      });
      expect(waived.assetsWaived).toBe(true);
    });

    it('should recycle members and report count', async () => {
      mockPost.mockResolvedValue({ membersRecycledCount: 3, membersRecycledAt: '2026-10-21T00:00:00Z' });

      const result = await projectService.recycleClosureMembers(3);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/closure/members/recycle', {});
      expect(result.membersRecycledCount).toBe(3);
    });

    it('should trigger CSAT survey idempotently', async () => {
      mockPost.mockResolvedValue({ csatSurveyId: 8001, csatTriggeredAt: '2026-10-21T00:00:00Z' });

      const result = await projectService.triggerClosureCSAT(3);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/closure/csat', {});
      expect(result.csatSurveyId).toBe(8001);
    });
  });

  describe('asset links (P2/PE-26)', () => {
    it('should list asset links with summary', async () => {
      mockGet.mockResolvedValue({
        items: [
          {
            id: 1,
            linkType: 'asset',
            targetId: 9,
            label: 'AST-1',
            targetMissing: false,
            createdBy: 1,
            createdAt: '2026-10-28T00:00:00Z',
          },
        ],
        summary: { asset: 1, asset_license: 0, vendor: 0, contract: 0 },
        total: 1,
        truncated: false,
      });

      const result = await projectService.listAssetLinks(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/asset-links');
      expect(result.items[0].label).toBe('AST-1');
      expect(result.summary.asset).toBe(1);
    });

    it('should create an asset link', async () => {
      mockPost.mockResolvedValue({
        items: [],
        summary: { asset: 0, asset_license: 0, vendor: 1, contract: 0 },
        total: 1,
        truncated: false,
      });

      await projectService.createAssetLink(3, { linkType: 'vendor', targetId: 7, note: '独家' });

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/asset-links', {
        linkType: 'vendor',
        targetId: 7,
        note: '独家',
      });
    });

    it('should delete an asset link', async () => {
      mockDelete.mockResolvedValue({
        items: [],
        summary: { asset: 0, asset_license: 0, vendor: 0, contract: 0 },
        total: 0,
        truncated: false,
      });

      await projectService.deleteAssetLink(3, 12);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/3/asset-links/12');
    });

    it('should search asset link candidates with query params', async () => {
      mockGet.mockResolvedValue({
        items: [{ id: 5, label: 'Office 365', sublabel: 'subscription' }],
        truncated: false,
      });

      const result = await projectService.listAssetLinkCandidates(3, 'asset_license', { q: 'office', limit: 10 });

      expect(mockGet).toHaveBeenCalledWith(
        '/api/v1/projects/3/asset-link-candidates?type=asset_license&q=office&limit=10'
      );
      expect(result.items[0].label).toBe('Office 365');
    });
  });

  describe('templates (P1/PE-11)', () => {
    it('should list templates with filters and envelope alias', async () => {
      const items = [{ id: 1, name: '交付标准模板', status: 'active', taskCount: 3 }];
      mockGet.mockResolvedValue({ items, projectTemplates: items, total: 1, page: 1, pageSize: 20 });

      const result = await projectService.listTemplates({ status: 'active', q: '交付' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/templates', { status: 'active', q: '交付' });
      expect(result.items).toHaveLength(1);
      expect(result.items[0].taskCount).toBe(3);
    });

    it('should fetch template detail with payload', async () => {
      const payload = { tasks: [{ stage: '启动', name: '项目章程', checklist: ['干系人登记'] }] };
      mockGet.mockResolvedValue({ id: 5, name: 'T5', payload });

      const result = await projectService.getTemplate(5);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/templates/5');
      expect(result.payload?.tasks?.[0].name).toBe('项目章程');
    });

    it('should create a template with payload', async () => {
      const request = {
        name: '标准交付',
        type: 'delivery' as const,
        payload: { milestones: [{ name: '验收', offsetDays: 30 }], tasks: [{ name: '部署' }] },
      };
      mockPost.mockResolvedValue({ id: 6, status: 'draft', ...request });

      const result = await projectService.createTemplate(request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/templates', request);
      expect(result.status).toBe('draft');
    });

    it('should update a template with optimistic lock version', async () => {
      mockPut.mockResolvedValue({ id: 6, status: 'active', version: 2 });

      const result = await projectService.updateTemplate(6, { status: 'active', version: 1 });

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/templates/6', { status: 'active', version: 1 });
      expect(result.version).toBe(2);
    });

    it('should delete a template by id', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.deleteTemplate(6);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/templates/6');
    });

    it('should apply a template to a project', async () => {
      mockPost.mockResolvedValue({
        projectId: 3,
        templateId: 6,
        templateName: '标准交付',
        stages: 1,
        tasks: 2,
        checklists: 2,
        milestones: 1,
      });

      const result = await projectService.applyTemplate(3, 6);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/templates/apply', { templateId: 6 });
      expect(result.stages).toBe(1);
      expect(result.checklists).toBe(2);
    });
  });

  describe('report（PE-17：状态报告模板 + 报表分组）', () => {
    it('should list report templates with filters', async () => {
      mockGet.mockResolvedValue({ items: [{ id: 1, name: '周报模板' }], total: 1, page: 1, pageSize: 10 });

      const result = await projectService.listReportTemplates({ status: 'active', q: '周报', page: 1, pageSize: 10 });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/report-templates', {
        status: 'active',
        q: '周报',
        page: 1,
        pageSize: 10,
      });
      expect(result.items).toHaveLength(1);
    });

    it('should create a report template', async () => {
      const request = {
        name: '双周报模板',
        cadence: 'biweekly' as const,
        sections: ['overview', 'variance'] as const,
      };
      mockPost.mockResolvedValue({ id: 2, ...request, version: 1, status: 'draft' });

      const result = await projectService.createReportTemplate({
        name: request.name,
        cadence: request.cadence,
        sections: [...request.sections],
      });

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/report-templates', {
        name: '双周报模板',
        cadence: 'biweekly',
        sections: ['overview', 'variance'],
      });
      expect(result.id).toBe(2);
    });

    it('should update a report template with version CAS', async () => {
      mockPut.mockResolvedValue({ id: 2, status: 'active', version: 2 });

      await projectService.updateReportTemplate(2, { status: 'active', version: 1 });

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/report-templates/2', {
        status: 'active',
        version: 1,
      });
    });

    it('should delete a report template by id', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.deleteReportTemplate(2);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/report-templates/2');
    });

    it('should generate a project report with template id', async () => {
      mockPost.mockResolvedValue({
        markdown: '# 项目状态报告',
        data: { projectId: 3, taskStats: { total: 0 }, milestoneStats: { total: 0 } },
      });

      const result = await projectService.generateProjectReport(3, 9);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/reports/generate', { templateId: 9 });
      expect(result.markdown).toContain('项目状态报告');
    });

    it('should default templateId to 0 when generating with default sections', async () => {
      mockPost.mockResolvedValue({ markdown: '', data: { projectId: 3 } });

      await projectService.generateProjectReport(3);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/reports/generate', { templateId: 0 });
    });

    it('should fetch report groups by owner team', async () => {
      mockGet.mockResolvedValue([{ ownerTeamName: '未归口', projectCount: 2, greenCount: 2 }]);

      const result = await projectService.reportGroups();

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/report-groups');
      expect(result[0].projectCount).toBe(2);
    });
  });

  describe('risks (P2/PE-21)', () => {
    it('should list risks with filters', async () => {
      const rows = [{ id: 1, projectId: 3, kind: 'risk', title: '延期', level: 'high', status: 'open' }];
      mockGet.mockResolvedValue(rows);

      const result = await projectService.listRisks(3, { status: 'open', level: 'high' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/risks', { status: 'open', level: 'high' });
      expect(result).toHaveLength(1);
      expect(result[0].level).toBe('high');
    });

    it('should create a risk', async () => {
      const request = { title: '机房风险', level: 'medium' as const };
      mockPost.mockResolvedValue({ id: 11, ...request, status: 'open', version: 1 });

      const result = await projectService.createRisk(3, request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/risks', request);
      expect(result.id).toBe(11);
    });

    it('should update a risk with optimistic lock version', async () => {
      const request = { title: '改名', ownerId: 0, version: 2 };
      mockPut.mockResolvedValue({ id: 11, title: '改名', version: 3 });

      const result = await projectService.updateRisk(3, 11, request);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/3/risks/11', request);
      expect(result.version).toBe(3);
    });

    it('should transition risk status with resolution', async () => {
      mockPost.mockResolvedValue({ id: 11, status: 'closed' });

      await projectService.transitionRisk(3, 11, 'close', '已处置');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/risks/11/status', {
        action: 'close',
        resolution: '已处置',
      });
    });

    it('should convert a risk to a problem', async () => {
      mockPost.mockResolvedValue({ target: 'problem', createdId: 501, risk: { id: 11, relatedProblemId: 501 } });

      const result = await projectService.convertRisk(3, 11, 'problem');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/risks/11/convert', { target: 'problem' });
      expect(result.createdId).toBe(501);
      expect(result.risk.relatedProblemId).toBe(501);
    });

    it('should delete a risk by id', async () => {
      mockDelete.mockResolvedValue(undefined);

      await projectService.deleteRisk(3, 11);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/3/risks/11');
    });
  });

  describe('budget & hours (P2/PE-22)', () => {
    it('should create a project with budget pair', async () => {
      const request = { name: '预算项目', budgetAmount: 120000, budgetCurrency: 'CNY' };
      mockPost.mockResolvedValue({ id: 9, ...request });

      const result = await projectService.createProject(request);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects', request);
      expect(result.budgetAmount).toBe(120000);
      expect(result.budgetCurrency).toBe('CNY');
    });

    it('should update budget with optimistic lock version', async () => {
      const request = { budgetAmount: 88000, budgetCurrency: 'USD', version: 2 };
      mockPut.mockResolvedValue({ id: 9, budgetAmount: 88000, budgetCurrency: 'USD', version: 3 });

      const result = await projectService.updateProject(9, request);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/9', request);
      expect(result.budgetCurrency).toBe('USD');
    });

    it('should fetch project hours summary', async () => {
      const summary = { taskCount: 2, estimatedTaskCount: 2, actualTaskCount: 1, estimateHours: 10, actualHours: 2.5 };
      mockGet.mockResolvedValue(summary);

      const result = await projectService.hoursSummary(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/hours-summary');
      expect(result.estimateHours).toBe(10);
      expect(result.actualHours).toBe(2.5);
    });

    it('should carry task actual hours in update payload', async () => {
      const request = { actualHours: 5.5, version: 1 };
      mockPut.mockResolvedValue({ id: 4, actualHours: 5.5, version: 2 });

      const result = await projectService.updateTask(3, 4, request);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/3/tasks/4', request);
      expect(result.actualHours).toBe(5.5);
    });

    it('should expose baseline budget deviation in diff', async () => {
      const diff = {
        baselineId: 1,
        version: 2,
        taskSummary: { added: 0, removed: 0, changed: 0, unchanged: 0 },
        milestoneSummary: { added: 0, removed: 0, changed: 0, unchanged: 0 },
        taskChanges: [],
        milestoneChanges: [],
        budget: { frozen: true, baseAmount: 100000, currentAmount: 50000, changed: true },
      };
      mockGet.mockResolvedValue(diff);

      const result = await projectService.baselineDiff(3);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/baselines/diff');
      expect(result.budget?.changed).toBe(true);
      expect(result.budget?.baseAmount).toBe(100000);
    });
  });

  describe('resource heatmap & capacity (P2/PE-23)', () => {
    it('should fetch resource heatmap with window params', async () => {
      const heatmap = {
        projectId: 3,
        range: { start: '2026-10-19', end: '2026-11-01' },
        calendar: { mode: 'natural_days', businessDaysOnly: false, note: 'v1 自然日' },
        defaults: { defaultWeeklyCapacityHours: 40, source: 'fallback' },
        weeks: [],
        unplanned: { taskCount: 1, estimateHours: 5 },
      };
      mockGet.mockResolvedValue(heatmap);

      const result = await projectService.resourceHeatmap(3, { weeks: 2 });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/resource-heatmap', { weeks: 2 });
      expect(result.calendar.mode).toBe('natural_days');
      expect(result.defaults.source).toBe('fallback');
      expect(result.unplanned.taskCount).toBe(1);
    });

    it('should update capacity with default and member override', async () => {
      const request = { defaultWeeklyCapacityHours: 30, members: [{ userId: 5, weeklyCapacityHours: 18 }] };
      mockPut.mockResolvedValue({ defaultWeeklyCapacityHours: 30, members: [{ userId: 5, weeklyCapacityHours: 18 }] });

      const result = await projectService.updateCapacity(3, request);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/3/capacity', request);
      expect(result.defaultWeeklyCapacityHours).toBe(30);
      expect(result.members[0].weeklyCapacityHours).toBe(18);
    });

    it('should send explicit null to clear capacity override', async () => {
      const request = { defaultWeeklyCapacityHours: null, members: [{ userId: 5, weeklyCapacityHours: null }] };
      mockPut.mockResolvedValue({ defaultWeeklyCapacityHours: null, members: [{ userId: 5, weeklyCapacityHours: null }] });

      const result = await projectService.updateCapacity(3, request);

      expect(mockPut).toHaveBeenCalledWith('/api/v1/projects/3/capacity', request);
      expect(result.defaultWeeklyCapacityHours).toBeNull();
      expect(result.members[0].weeklyCapacityHours).toBeNull();
    });
  });

  describe('portfolio & metrics (PE-24)', () => {
    it('should fetch portfolio aggregation with list filters', async () => {
      const portfolio = {
        generatedAt: '2026-10-27T00:00:00Z',
        truncated: false,
        summary: {
          total: 2,
          byStatus: [{ key: 'active', count: 2 }],
          byHealth: [{ key: 'green', count: 2 }],
          byType: [{ key: 'delivery', count: 1 }],
          byManager: [{ key: '9', count: 1, name: '张三' }],
          byDepartment: [{ key: '0', count: 2 }],
        },
        projects: [],
      };
      mockGet.mockResolvedValue(portfolio);

      const result = await projectService.portfolio({ status: 'active' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/portfolio', { status: 'active' });
      expect(result.summary.total).toBe(2);
      expect(result.summary.byManager[0].name).toBe('张三');
      expect(result.truncated).toBe(false);
    });

    it('should fetch metrics with date window and resource weeks', async () => {
      const metrics = {
        generatedAt: '2026-10-27T00:00:00Z',
        projectScope: { total: 2, truncated: false, filters: {} },
        window: { from: '2026-10-01', to: '2026-10-31' },
        onTimeDelivery: { denominator: 2, numerator: 1, rate: 0.5, noPlanDate: 0, detailsTotal: 1, details: [] },
        milestoneAchievement: {
          judged: 1,
          reached: 1,
          missed: 0,
          onTimeReached: 1,
          rate: 1,
          onTimeRate: 1,
          detailsTotal: 1,
          details: [],
        },
        gateFirstPass: { passed: 4, firstPass: 3, rate: 0.75, detailsTotal: 4, details: [] },
        resourceUtilization: {
          plannedHours: 21,
          capacityHours: 40,
          rate: 0.525,
          unplanned: { taskCount: 1, estimateHours: 5 },
          calendar: { mode: 'natural_days', businessDaysOnly: false, note: 'v1 自然日' },
          window: { start: '2026-10-26', end: '2026-11-08', weeks: 2 },
          projects: [],
        },
      };
      mockGet.mockResolvedValue(metrics);

      const result = await projectService.metrics({ from: '2026-10-01', to: '2026-10-31', weeks: 2 });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/metrics', {
        from: '2026-10-01',
        to: '2026-10-31',
        weeks: 2,
      });
      expect(result.onTimeDelivery.rate).toBe(0.5);
      expect(result.gateFirstPass.firstPass).toBe(3);
      expect(result.resourceUtilization.window.weeks).toBe(2);
    });
  });

  describe('AI 能力（PE-25）', () => {
    it('should generate AI summary with default weekly cadence (degraded envelope)', async () => {
      const summary = {
        projectId: 12,
        summary: '## 项目状态（事实派生）',
        cadence: 'weekly',
        generatedBy: 'deterministic',
        degraded: true,
        degradeReason: 'ai_unavailable',
        promptChars: 320,
        generatedAt: '2026-10-28T02:00:00Z',
      };
      mockPost.mockResolvedValue(summary);

      const result = await projectService.aiSummary(12);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/12/ai/summary', { cadence: 'weekly' });
      expect(result.degraded).toBe(true);
      expect(result.generatedBy).toBe('deterministic');
      expect(result.degradeReason).toBe('ai_unavailable');
    });

    it('should pass biweekly cadence when requested', async () => {
      mockPost.mockResolvedValue({ projectId: 12, summary: '摘要', cadence: 'biweekly', generatedBy: 'ai', degraded: false });

      await projectService.aiSummary(12, 'biweekly');

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/12/ai/summary', { cadence: 'biweekly' });
    });

    it('should fetch AI risk warnings with rule signals and AI warnings', async () => {
      const resp = {
        projectId: 7,
        signals: [{ type: 'overdue_task', severity: 'warning', refType: 'task', refId: 3, title: '任务逾期', detail: '计划结束已过' }],
        signalTotal: 4,
        truncated: false,
        warnings: [{ severity: 'critical', title: '缓冲不足', detail: '依赖链无缓冲', suggestion: '提前联调' }],
        generatedBy: 'ai',
        degraded: false,
        model: 'gpt-4o-mini',
        generatedAt: '2026-10-28T02:00:00Z',
      };
      mockPost.mockResolvedValue(resp);

      const result = await projectService.aiRiskWarnings(7);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/7/ai/risk-warnings', {});
      expect(result.signals[0].refId).toBe(3);
      expect(result.warnings[0].severity).toBe('critical');
      expect(result.signalTotal).toBe(4);
    });

    it('should extract task proposals from minutes and omit empty limit', async () => {
      const resp = {
        projectId: 5,
        proposals: [
          { name: '补充压测', description: '由张三负责', priority: 'high', estimateHours: 8, ownerHint: '张三' },
        ],
        generatedBy: 'ai',
        degraded: false,
        contentChars: 12,
        generatedAt: '2026-10-28T02:00:00Z',
      };
      mockPost.mockResolvedValue(resp);

      const result = await projectService.aiMinutesTasks(5, '会议纪要正文', 5);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/5/ai/minutes/tasks', { content: '会议纪要正文', limit: 5 });
      expect(result.proposals[0].priority).toBe('high');
      expect(result.proposals[0].estimateHours).toBe(8);

      await projectService.aiMinutesTasks(5, '会议纪要正文');
      expect(mockPost).toHaveBeenLastCalledWith('/api/v1/projects/5/ai/minutes/tasks', { content: '会议纪要正文' });
    });
  });
});
