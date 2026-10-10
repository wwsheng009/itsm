import { checkNotificationTarget, parseNotificationTarget } from '@/lib/utils/notification-target';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;

describe('parseNotificationTarget', () => {
  it('解析工单/变更/服务请求/MSP 工作台深链', () => {
    expect(parseNotificationTarget('/tickets/225')).toEqual({ kind: 'ticket', id: 225 });
    expect(parseNotificationTarget('/tickets/225?from=x')).toEqual({ kind: 'ticket', id: 225 });
    expect(parseNotificationTarget('/changes/12')).toEqual({ kind: 'change', id: 12 });
    expect(parseNotificationTarget('/service-requests/7')).toEqual({ kind: 'service_request', id: 7 });
    expect(parseNotificationTarget('/msp/workbench?ticket=225&customerTenantId=11')).toEqual({
      kind: 'msp_ticket',
      id: 225,
      customerTenantId: 11,
    });
    expect(parseNotificationTarget('/msp/workbench?customerTenantId=11&ticket=225')).toEqual({
      kind: 'msp_ticket',
      id: 225,
      customerTenantId: 11,
    });
  });

  it('非单据链接/非法协议返回 null', () => {
    expect(parseNotificationTarget('/dashboard')).toBeNull();
    // 非 msp/workbench 路径的 ticket= 查询参数不误判为单据深链
    expect(parseNotificationTarget('/dashboard?ticket=5')).toBeNull();
    expect(parseNotificationTarget('https://evil.com/tickets/1')).toBeNull();
    expect(parseNotificationTarget('//evil.com/tickets/1')).toBeNull();
  });
});

describe('checkNotificationTarget', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('目标 404 → missing（阻止跳转）', async () => {
    mockGet.mockRejectedValueOnce({ httpStatus: 404 });
    await expect(checkNotificationTarget('/tickets/225')).resolves.toBe('missing');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/tickets/225');
  });

  it('目标存在 → ok', async () => {
    mockGet.mockResolvedValueOnce({});
    await expect(checkNotificationTarget('/changes/12')).resolves.toBe('ok');
  });

  it('403/网络异常 → unknown（不阻断，由目标页处理）', async () => {
    mockGet.mockRejectedValueOnce({ httpStatus: 403 });
    await expect(checkNotificationTarget('/tickets/225')).resolves.toBe('unknown');

    mockGet.mockRejectedValueOnce(new Error('network'));
    await expect(checkNotificationTarget('/service-requests/7')).resolves.toBe('unknown');
  });

  it('MSP 深链缺少 customerTenantId → unknown，不发请求', async () => {
    await expect(checkNotificationTarget('/msp/workbench?ticket=225')).resolves.toBe('unknown');
    expect(mockGet).not.toHaveBeenCalled();
  });

  it('MSP 深链携带声明租户 → 命中跨租户详情端点', async () => {
    mockGet.mockResolvedValueOnce({});
    await expect(
      checkNotificationTarget('/msp/workbench?ticket=225&customerTenantId=11')
    ).resolves.toBe('ok');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/msp/tickets/225?customerTenantId=11');
  });

  it('普通站内链接 → unknown，不发请求', async () => {
    await expect(checkNotificationTarget('/dashboard')).resolves.toBe('unknown');
    expect(mockGet).not.toHaveBeenCalled();
  });
});
