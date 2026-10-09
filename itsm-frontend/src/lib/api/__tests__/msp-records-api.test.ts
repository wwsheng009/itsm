/**
 * MSP 任务投影 + 跨租户单据 API 契约测试（P2-②/P2-③ 前端入口）。
 *
 * 锁定 URL 形状与声明租户参数（后端以 customerTenantId 绑定 RLS GUC 并二次校验 allocation）：
 * - 任务投影：GET /msp/workbench/tasks（customerTenantIds/limit）+ claim/complete 写端点；
 * - 单据读面：GET /msp/workbench/{changes,problems,service-requests} + 单条详情（声明租户必带）；
 * - 写面：POST /msp/{slug}/:id/status（审批类不在 MSP 面，目标状态由调用方按域白名单选择）。
 */
import {
  MSP_RECORD_SLUG,
  claimWorkbenchTask,
  completeWorkbenchTask,
  getWorkbenchRecord,
  listWorkbenchRecords,
  listWorkbenchTasks,
  updateWorkbenchRecordStatus,
} from '../msp-records-api';
import { httpClient } from '../http-client';

jest.mock('../http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
    patch: jest.fn(),
    request: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;

describe('msp-records-api · 任务投影', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('listWorkbenchTasks 序列化客户范围与 limit', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    await listWorkbenchTasks({ customerTenantIds: [11, 12], limit: 50 });
    expect(mockGet).toHaveBeenCalledWith('/api/v1/msp/workbench/tasks?customerTenantIds=11%2C12&limit=50');
  });

  it('listWorkbenchTasks 无客户范围时不带参数', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    await listWorkbenchTasks();
    expect(mockGet).toHaveBeenCalledWith('/api/v1/msp/workbench/tasks');
  });

  it('claimWorkbenchTask 携带声明租户', async () => {
    mockPost.mockResolvedValue({ taskId: 'TASK-1' });
    await claimWorkbenchTask('TASK-1', { customerTenantId: 11 });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/msp/workbench/tasks/TASK-1/claim', {
      customerTenantId: 11,
    });
  });

  it('completeWorkbenchTask 透传流程变量', async () => {
    mockPost.mockResolvedValue({ taskId: 'TASK-1' });
    await completeWorkbenchTask('TASK-1', { customerTenantId: 11, variables: { approved: true } });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/msp/workbench/tasks/TASK-1/complete', {
      customerTenantId: 11,
      variables: { approved: true },
    });
  });
});

describe('msp-records-api · 三类单据读面', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('记录类型 → 路径 slug 映射锁定', () => {
    expect(MSP_RECORD_SLUG.change).toBe('changes');
    expect(MSP_RECORD_SLUG.problem).toBe('problems');
    expect(MSP_RECORD_SLUG.service_request).toBe('service-requests');
  });

  it('listWorkbenchRecords 按类型与筛选序列化', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    await listWorkbenchRecords('service_request', {
      customerTenantIds: [11],
      status: 'provisioning',
      q: 'P215',
      limit: 50,
      cursor: 'abc',
    });
    expect(mockGet).toHaveBeenCalledWith(
      '/api/v1/msp/workbench/service-requests?customerTenantIds=11&status=provisioning&q=P215&limit=50&cursor=abc'
    );
  });

  it('getWorkbenchRecord 锁定声明租户参数', async () => {
    mockGet.mockResolvedValue({ id: 26, recordType: 'change' });
    await getWorkbenchRecord('change', 26, 11);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/msp/changes/26?customerTenantId=11');
  });
});

describe('msp-records-api · 写面（状态推进）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('变更：POST /msp/changes/:id/status（comment 透传）', async () => {
    mockPost.mockResolvedValue({ id: 26, status: 'scheduled' });
    await updateWorkbenchRecordStatus('change', 26, {
      customerTenantId: 11,
      status: 'scheduled',
      comment: '排期至周五',
    });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/msp/changes/26/status', {
      customerTenantId: 11,
      status: 'scheduled',
      comment: '排期至周五',
    });
  });

  it('问题：resolution 透传', async () => {
    mockPost.mockResolvedValue({ id: 3, status: 'resolved' });
    await updateWorkbenchRecordStatus('problem', 3, {
      customerTenantId: 11,
      status: 'resolved',
      resolution: '已修复',
    });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/msp/problems/3/status', {
      customerTenantId: 11,
      status: 'resolved',
      resolution: '已修复',
    });
  });

  it('服务请求：仅履约状态（无审批类）', async () => {
    mockPost.mockResolvedValue({ id: 5, status: 'delivered' });
    await updateWorkbenchRecordStatus('service_request', 5, {
      customerTenantId: 11,
      status: 'delivered',
    });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/msp/service-requests/5/status', {
      customerTenantId: 11,
      status: 'delivered',
    });
  });
});
