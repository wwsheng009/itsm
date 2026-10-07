/**
 * MSP 工作台跨租户详情 API 契约测试（P1）。
 *
 * 锁定 URL 形状与声明租户参数：后端以 customerTenantId 绑定 RLS GUC 并二次校验 allocation，
 * 前端不得省略该参数（缺省 → 400 参数错误）。
 */
import { getWorkbenchTicket } from '../msp-workbench-api';
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

describe('msp-workbench-api · 跨租户详情（P1）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('getWorkbenchTicket 锁定 URL 与声明租户参数', async () => {
    const detail = { id: 224, customerTenantId: 11 } as never;
    mockGet.mockResolvedValue(detail);

    const result = await getWorkbenchTicket(224, 11);

    expect(mockGet).toHaveBeenCalledWith('/api/v1/msp/tickets/224?customerTenantId=11');
    expect(result).toBe(detail);
  });
});
