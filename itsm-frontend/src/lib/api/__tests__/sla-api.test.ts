import { SLAApi } from '../sla-api';
import { httpClient } from '../http-client';

jest.mock('../http-client', () => ({
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

describe('SLAApi', () => {
  beforeEach(() => { jest.clearAllMocks(); });

  describe('getSLADefinitions', () => {
    it('should get definitions with params', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 10 });
      await SLAApi.getSLADefinitions({ page: 1, pageSize: 10, isActive: true, name: 'test' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions', { page: '1', pageSize: '10', isActive: 'true', name: 'test' });
    });

    it('should get definitions without params', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0 });
      await SLAApi.getSLADefinitions();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions', {});
    });
  });

  describe('getSLADefinition', () => {
    it('should get definition by id', async () => {
      mockGet.mockResolvedValue({ id: 1, name: 'Gold SLA' });
      const result = await SLAApi.getSLADefinition(1);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions/1');
      expect(result.name).toBe('Gold SLA');
    });
  });

  describe('createSLADefinition', () => {
    it('should create definition', async () => {
      const data = { name: 'New SLA', priority: 'high', responseTime: 60, resolutionTime: 240 };
      mockPost.mockResolvedValue({ id: 1, ...data });
      const result = await SLAApi.createSLADefinition(data);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/definitions', data);
      expect(result.name).toBe('New SLA');
    });
  });

  describe('updateSLADefinition', () => {
    it('should update definition', async () => {
      mockPut.mockResolvedValue({ id: 1, name: 'Updated' });
      await SLAApi.updateSLADefinition(1, { name: 'Updated' });
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/definitions/1', { name: 'Updated' });
    });
  });

  describe('deleteSLADefinition', () => {
    it('should delete definition', async () => {
      mockDelete.mockResolvedValue(undefined);
      await SLAApi.deleteSLADefinition(1);
      expect(mockDelete).toHaveBeenCalledWith('/api/v1/sla/definitions/1');
    });
  });

  describe('disputeSLAViolation', () => {
    it('should PUT resolutionState=disputed with reason（D-SLA-8）', async () => {
      mockPut.mockResolvedValue({ id: 3, resolutionState: 'disputed' });
      const result = await SLAApi.disputeSLAViolation(3, '客户认为已按时响应');
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/violations/3', {
        resolutionState: 'disputed',
        disputeReason: '客户认为已按时响应',
      });
      expect(result.resolutionState).toBe('disputed');
    });
  });

  describe('settleSLAViolation', () => {
    it('should PUT /settle with decision and reason', async () => {
      mockPut.mockResolvedValue({ id: 3, resolutionState: 'waived' });
      await SLAApi.settleSLAViolation(3, 'waived', '客户侧停电');
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/violations/3/settle', {
        decision: 'waived',
        reason: '客户侧停电',
      });
    });
  });

  describe('settlement report（provider 面）', () => {
    it('should GET settlement-report with customer/from/to', async () => {
      mockGet.mockResolvedValue({ coveredTickets: 0, rows: [] });
      await SLAApi.getSettlementReport({ customerTenantId: 6, from: 'F', to: 'T' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/settlement-report', {
        customerTenantId: '6',
        from: 'F',
        to: 'T',
      });
    });

    it('should POST lock with query string（锁定快照 §13.3）', async () => {
      mockPost.mockResolvedValue({ lockedViolations: 2 });
      const res = await SLAApi.lockSettlementSnapshot({ customerTenantId: 6, from: 'F', to: 'T' });
      expect(mockPost).toHaveBeenCalledWith(
        '/api/v1/sla/settlement-report/lock?customerTenantId=6&from=F&to=T',
        {}
      );
      expect(res.lockedViolations).toBe(2);
    });
  });

  // ==================== D-SLA-4：MSP SLA 合同契约 ====================

  describe('listContracts', () => {
    it('should list contracts with filters', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0 });
      const result = await SLAApi.listContracts({ customerTenantId: 5, status: 'active' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/contracts', {
        customerTenantId: '5',
        status: 'active',
      });
      expect(result.items).toEqual([]);
    });

    it('should tolerate missing envelope fields', async () => {
      mockGet.mockResolvedValue(undefined);
      const result = await SLAApi.listContracts();
      expect(result).toEqual({ items: [], total: 0 });
    });
  });

  describe('createContract', () => {
    it('should create contract with bindings', async () => {
      const payload = {
        contractNo: 'HT-1',
        name: '年度合同',
        customerTenantId: 5,
        bindings: [{ slaDefinitionId: 1, ticketTypeId: 0 }],
      };
      mockPost.mockResolvedValue({ id: 9, ...payload, status: 'active' });
      const result = await SLAApi.createContract(payload);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/contracts', payload);
      expect(result.id).toBe(9);
    });
  });

  describe('getContract', () => {
    it('should get contract by id', async () => {
      mockGet.mockResolvedValue({ id: 9, contractNo: 'HT-1' });
      const result = await SLAApi.getContract(9);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/contracts/9');
      expect(result.contractNo).toBe('HT-1');
    });
  });

  describe('updateContract', () => {
    it('should update status', async () => {
      mockPut.mockResolvedValue({ id: 9, status: 'terminated' });
      await SLAApi.updateContract(9, { status: 'terminated' });
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/contracts/9', { status: 'terminated' });
    });

    it('should replace bindings when provided', async () => {
      mockPut.mockResolvedValue({ id: 9 });
      await SLAApi.updateContract(9, { bindings: [{ slaDefinitionId: 2, ticketTypeId: 7 }] });
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/contracts/9', {
        bindings: [{ slaDefinitionId: 2, ticketTypeId: 7 }],
      });
    });
  });

  describe('getProvidedDefinitions', () => {
    it('should read provided definitions (customer read-only view)', async () => {
      mockGet.mockResolvedValue({ items: [{ contractId: 1, slaDefinitionId: 2, name: 'Contract SLA' }], total: 1 });
      const result = await SLAApi.getProvidedDefinitions();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions/provided');
      expect(result.items[0].name).toBe('Contract SLA');
    });
  });

  describe('checkTicketCompliance', () => {
    it('should check compliance', async () => {
      mockPost.mockResolvedValue({ isCompliant: true, violations: [] });
      const result = await SLAApi.checkTicketCompliance(1);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/check-compliance/1');
      expect(result.isCompliant).toBe(true);
    });
  });

  describe('getSLAViolations', () => {
    it('should get violations with params', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0 });
      await SLAApi.getSLAViolations({ page: 1, pageSize: 10, severity: 'high' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/violations', expect.objectContaining({ page: '1', pageSize: '10', severity: 'high' }));
    });
  });

  describe('updateSLAViolationStatus', () => {
    it('should update violation status', async () => {
      mockPut.mockResolvedValue(undefined);
      await SLAApi.updateSLAViolationStatus(1, true, 'resolved');
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/violations/1', { isResolved: true, notes: 'resolved' });
    });
  });

  describe('getSLAComplianceReport', () => {
    it('should get compliance report and transform data', async () => {
      mockGet.mockResolvedValue({ totalTickets: 100, metSla: 90, violatedSla: 10, complianceRate: 90, avgResponseTime: 30, avgResolutionTime: 120, reportPeriod: { startDate: '2024-01-01', endDate: '2024-01-31' } });
      const result = await SLAApi.getSLAComplianceReport({ startDate: '2024-01-01', endDate: '2024-01-31' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/compliance-report', { startDate: '2024-01-01', endDate: '2024-01-31' });
      expect(result.totalTickets).toBe(100);
      expect(result.complianceRate).toBe(90);
    });

    it('should handle missing fields with defaults', async () => {
      mockGet.mockResolvedValue({});
      const result = await SLAApi.getSLAComplianceReport({ startDate: '2024-01-01', endDate: '2024-01-31' });
      expect(result.totalTickets).toBe(0);
      expect(result.reportPeriod.startDate).toBe('2024-01-01');
    });
  });

  describe('getSLAMonitoring', () => {
    it('should post the canonical monitoring contract without fabricating fields', async () => {
      mockPost.mockResolvedValue({
        startTime: '2026-08-22T00:00:00Z',
        endTime: '2026-08-29T00:00:00Z',
        truncated: false,
        totalTickets: 10,
        resolvedTickets: 2,
        resolutionRate: 20,
        complianceRate: 75.5,
        activeAlerts: 3,
        alerts: [],
      });
      const result = await SLAApi.getSLAMonitoring({
        startTime: '2026-08-22T00:00:00Z',
        endTime: '2026-08-29T00:00:00Z',
      });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/monitoring', {
        startTime: '2026-08-22T00:00:00Z',
        endTime: '2026-08-29T00:00:00Z',
      });
      // 百分数原样透传，客户端禁止做 *100 之类的二次换算或字段改名。
      expect(result.complianceRate).toBe(75.5);
      expect(result.totalTickets).toBe(10);
      expect(result.activeAlerts).toBe(3);
    });

    it('should send an empty body when no window is given', async () => {
      mockPost.mockResolvedValue({ alerts: null });
      const result = await SLAApi.getSLAMonitoring();
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/monitoring', {});
      expect(result.alerts).toEqual([]);
    });

    it('should throw on empty response instead of faking success', async () => {
      mockPost.mockResolvedValue(undefined);
      await expect(SLAApi.getSLAMonitoring()).rejects.toThrow('SLA 监控数据为空');
    });
  });

  describe('getSLAPerformance', () => {
    it('should query the performance endpoint with camelCase params', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20, totalPages: 0 });
      await SLAApi.getSLAPerformance({
        dimension: 'serviceType',
        startDate: '2026-08-01T00:00:00Z',
        serviceType: 'incident',
        page: 2,
        pageSize: 10,
      });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/performance', {
        dimension: 'serviceType',
        startDate: '2026-08-01T00:00:00Z',
        serviceType: 'incident',
        page: '2',
        pageSize: '10',
      });
    });
  });

  describe('getSLAMetrics', () => {
    it('should get metric records with camelCase query', async () => {
      mockGet.mockResolvedValue({ metrics: [{ id: 1 }], count: 1 });
      const result = await SLAApi.getSLAMetrics({ slaDefinitionId: 3, metricType: 'response_time' });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/metrics', {
        slaDefinitionId: '3',
        metricType: 'response_time',
      });
      expect(result.count).toBe(1);
    });
  });

  describe('getSLAAlerts', () => {
    it('should return real alert items from monitoring', async () => {
      mockPost.mockResolvedValue({
        alerts: [{
          id: 5,
          ticketId: 1,
          ticketNumber: 'TK-1',
          ticketTitle: 'Test',
          priority: 'high',
          alertLevel: 'warning',
          alertRuleName: 'Gold rule',
          thresholdPercentage: 80,
          actualPercentage: 90,
          createdAt: '2026-01-01T00:00:00Z',
        }],
      });
      const result = await SLAApi.getSLAAlerts();
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/monitoring', {});
      expect(result).toHaveLength(1);
      expect(result[0].alertRuleName).toBe('Gold rule');
    });

    it('should return empty array when monitoring has no alerts', async () => {
      mockPost.mockResolvedValue({});
      const result = await SLAApi.getSLAAlerts();
      expect(result).toEqual([]);
    });
  });

  describe('getSLAStats', () => {
    it('should get stats', async () => {
      mockGet.mockResolvedValue({ totalDefinitions: 5, activeDefinitions: 3 });
      await SLAApi.getSLAStats();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/stats');
    });
  });

  describe('createAlertRule', () => {
    it('should create alert rule', async () => {
      const data = { name: 'Rule1', slaDefinitionId: 1, alertLevel: 'warning' as const, thresholdPercentage: 80, notificationChannels: ['email'], isActive: true };
      mockPost.mockResolvedValue({ id: 1 });
      await SLAApi.createAlertRule(data);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/sla/alert-rules', data);
    });
  });

  describe('updateAlertRule', () => {
    it('should update alert rule', async () => {
      mockPut.mockResolvedValue({ id: 1 });
      await SLAApi.updateAlertRule(1, { name: 'Updated' });
      expect(mockPut).toHaveBeenCalledWith('/api/v1/sla/alert-rules/1', { name: 'Updated' });
    });
  });

  describe('deleteAlertRule', () => {
    it('should delete alert rule', async () => {
      mockDelete.mockResolvedValue(undefined);
      await SLAApi.deleteAlertRule(1);
      expect(mockDelete).toHaveBeenCalledWith('/api/v1/sla/alert-rules/1');
    });
  });

  describe('getAlertRules', () => {
    it('should get alert rules', async () => {
      mockGet.mockResolvedValue([]);
      await SLAApi.getAlertRules({ slaDefinitionId: 1 });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/alert-rules', { slaDefinitionId: 1 });
    });
  });

  describe('getAlertRule', () => {
    it('should get alert rule by id', async () => {
      mockGet.mockResolvedValue({ id: 1, name: 'Rule1' });
      await SLAApi.getAlertRule(1);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/alert-rules/1');
    });
  });

  describe('getAlertHistory', () => {
    it('should get alert history', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 10 });
      await SLAApi.getAlertHistory({ page: 1, pageSize: 10 });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/alert-history', { page: 1, pageSize: 10 });
    });
  });

  describe('deprecated aliases', () => {
    it('getDefinitions should call getSLADefinitions', async () => {
      mockGet.mockResolvedValue({ items: [] });
      await SLAApi.getDefinitions({ page: 1 });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions', expect.any(Object));
    });

    it('getDefinition should call getSLADefinition', async () => {
      mockGet.mockResolvedValue({ id: 1 });
      await SLAApi.getDefinition(1);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/sla/definitions/1');
    });
  });

  describe('error propagation', () => {
    it('should propagate errors', async () => {
      mockGet.mockRejectedValue(new Error('Server error'));
      await expect(SLAApi.getSLADefinition(999)).rejects.toThrow('Server error');
    });
  });
});
