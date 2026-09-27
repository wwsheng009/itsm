import { httpClient } from '@/lib/api/http-client';
import mcpApi, {
  describeMCPError,
  isMCPFeatureDisabled,
  isMCPServiceUnavailable,
  mcpErrorCode,
} from '../mcp-api';

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

describe('mcpApi（M0-12 管理 API 客户端）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('listServers：GET 列表并兜底空数组', async () => {
    mockGet.mockResolvedValueOnce({ items: [{ id: 1, name: 'gitlab' }], summary: { total: 1 } });
    const result = await mcpApi.listServers();
    expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/mcp-servers');
    expect(result.items).toHaveLength(1);
    expect(result.summary.total).toBe(1);

    mockGet.mockResolvedValueOnce({ total: 0 });
    const empty = await mcpApi.listServers();
    expect(empty.items).toEqual([]);
  });

  it('healthSummary：GET /health（与列表同投影）', async () => {
    mockGet.mockResolvedValue({ items: [], summary: { error: 2, auth_required: 1 } });
    const result = await mcpApi.healthSummary();
    expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/health');
    expect(result.summary.error).toBe(2);
  });

  it('测试连接：POST /:id/test，缺省传空对象（用已存配置）', async () => {
    mockPost.mockResolvedValue({ ok: true, tool_count: 3, duration_ms: 12 });
    await mcpApi.testServer(7);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/7/test', {});

    await mcpApi.testServer(7, { url: 'https://x.example.com/mcp' });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/7/test', {
      url: 'https://x.example.com/mcp',
    });
  });

  it('生命周期：enable/disable/reload 均走 POST 且路径正确', async () => {
    mockPost.mockResolvedValue({ id: 3 });
    await mcpApi.enableServer(3);
    await mcpApi.disableServer(3);
    await mcpApi.reloadServer(3);
    expect(mockPost).toHaveBeenNthCalledWith(1, '/api/v1/ai/mcp-servers/3/enable', {});
    expect(mockPost).toHaveBeenNthCalledWith(2, '/api/v1/ai/mcp-servers/3/disable', {});
    expect(mockPost).toHaveBeenNthCalledWith(3, '/api/v1/ai/mcp-servers/3/reload', {});
  });

  it('工具治理：投影名必须 URL 编码（含 mcp__ 前缀与特殊字符）', async () => {
    const callable = 'mcp__gitlab__list issues';
    mockPost.mockResolvedValue({ callable_name: callable });
    mockPut.mockResolvedValue({ callable_name: callable });

    await mcpApi.setToolEnabled(5, callable, true);
    expect(mockPost).toHaveBeenCalledWith(
      `/api/v1/ai/mcp-servers/5/tools/${encodeURIComponent(callable)}/enable`,
      {},
    );
    await mcpApi.setToolEnabled(5, callable, false);
    expect(mockPost).toHaveBeenCalledWith(
      `/api/v1/ai/mcp-servers/5/tools/${encodeURIComponent(callable)}/disable`,
      {},
    );
    await mcpApi.setToolClassification(5, callable, { read_only: true, risk: 'read', category: 'issue' });
    expect(mockPut).toHaveBeenCalledWith(
      `/api/v1/ai/mcp-servers/5/tools/${encodeURIComponent(callable)}/classification`,
      { read_only: true, risk: 'read', category: 'issue' },
    );
  });

  it('批量治理与凭据轮换：载荷原样透传', async () => {
    mockPost.mockResolvedValue({ affected: 2, enabled: false });
    await mcpApi.bulkSetTools(9, { tools: ['a', 'b'], enabled: false });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/9/tools/bulk', {
      tools: ['a', 'b'],
      enabled: false,
    });

    mockPost.mockResolvedValue({ id: 9 });
    await mcpApi.rotateCredential(9, { credential_type: 'static_header', credential: { token: 't' } });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/9/rotate-credential', {
      credential_type: 'static_header',
      credential: { token: 't' },
    });
  });

  it('工具清单/事件：items 缺省兜底为空数组；删除走 DELETE', async () => {
    mockGet.mockResolvedValue({ total: 0 });
    expect(await mcpApi.listTools(1)).toEqual([]);
    expect(await mcpApi.listEvents(1)).toEqual([]);
    expect(mockGet).toHaveBeenNthCalledWith(1, '/api/v1/ai/mcp-servers/1/tools');
    expect(mockGet).toHaveBeenNthCalledWith(2, '/api/v1/ai/mcp-servers/1/events');

    mockDelete.mockResolvedValue({ deleted: true });
    await mcpApi.deleteServer(1);
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/ai/mcp-servers/1');
  });
});

describe('mcpApi 错误码与降级判定', () => {
  it('describeMCPError：错误码映射 → message → fallback', () => {
    expect(describeMCPError({ errorCode: 'ssrf_blocked' })).toContain('出站安全校验');
    expect(describeMCPError({ errorCode: 'protocol_mismatch' })).toContain('协议版本');
    expect(describeMCPError(new Error('boom'))).toBe('boom');
    expect(describeMCPError(undefined, '兜底')).toBe('兜底');
  });

  it('mcpErrorCode：仅字符串错误码透出', () => {
    expect(mcpErrorCode({ errorCode: 'conflict' })).toBe('conflict');
    expect(mcpErrorCode({ errorCode: '' })).toBeUndefined();
    expect(mcpErrorCode(null)).toBeUndefined();
  });

  it('isMCPFeatureDisabled：404 且无 errorCode 才算开关关闭', () => {
    expect(isMCPFeatureDisabled({ httpStatus: 404 })).toBe(true);
    expect(isMCPFeatureDisabled({ httpStatus: 404, errorCode: 'not_found' })).toBe(false);
    expect(isMCPFeatureDisabled({ httpStatus: 500 })).toBe(false);
    expect(isMCPFeatureDisabled(undefined)).toBe(false);
  });

  it('isMCPServiceUnavailable：unavailable 错误码或 503', () => {
    expect(isMCPServiceUnavailable({ errorCode: 'unavailable' })).toBe(true);
    expect(isMCPServiceUnavailable({ httpStatus: 503 })).toBe(true);
    expect(isMCPServiceUnavailable({ httpStatus: 403 })).toBe(false);
  });
});
