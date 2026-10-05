import {
  TenantUserAdminApi,
  tenantUserAdminErrorMessage,
  TENANT_USER_ADMIN_ERROR_MESSAGES,
} from '../tenant-user-admin-api';
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
const mockPut = httpClient.put as jest.Mock;

describe('TenantUserAdminApi（TUM-4 契约锁定）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('listUsers 锁定 URL 与查询参数', async () => {
    const resp = { items: [], total: 0, page: 2, pageSize: 50 };
    mockGet.mockResolvedValue(resp);

    const result = await TenantUserAdminApi.listUsers(7, {
      page: 2,
      pageSize: 50,
      keyword: 'alice',
      status: 'active',
      role: 'admin',
    });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/7/users', {
      page: 2,
      pageSize: 50,
      keyword: 'alice',
      status: 'active',
      role: 'admin',
    });
    expect(result).toEqual(resp);
  });

  it('listUsers 无参默认空对象', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 });
    await TenantUserAdminApi.listUsers(9);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/9/users', {});
  });

  it('getUser 锁定 (tenant_id,user_id) 双键路径', async () => {
    mockGet.mockResolvedValue({ id: 42, username: 'b2ccust_admin' });
    const result = await TenantUserAdminApi.getUser(9, 42);
    expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/9/users/42');
    expect(result.id).toBe(42);
  });

  it('resetPassword generated 载荷与一次性口令回执', async () => {
    mockPost.mockResolvedValue({
      userId: 42,
      mode: 'generated',
      generatedPassword: 'Xk7#mQ2pL9@vR4tZ',
      mustChangePassword: true,
    });

    const result = await TenantUserAdminApi.resetPassword(9, 42, { mode: 'generated' });

    expect(mockPost).toHaveBeenCalledWith('/api/v1/tenants/9/users/42/reset-password', {
      mode: 'generated',
    });
    expect(result.generatedPassword).toBe('Xk7#mQ2pL9@vR4tZ');
    expect(result.mustChangePassword).toBe(true);
  });

  it('resetPassword specified 携带 newPassword', async () => {
    mockPost.mockResolvedValue({ userId: 42, mode: 'specified', mustChangePassword: true });
    await TenantUserAdminApi.resetPassword(9, 42, { mode: 'specified', newPassword: 'NewPass456!' });
    expect(mockPost).toHaveBeenCalledWith('/api/v1/tenants/9/users/42/reset-password', {
      mode: 'specified',
      newPassword: 'NewPass456!',
    });
  });

  it('setStatus 载荷含 active 与 confirmCode', async () => {
    mockPut.mockResolvedValue(undefined);
    await TenantUserAdminApi.setStatus(9, 42, { active: false, confirmCode: 'default' });
    expect(mockPut).toHaveBeenCalledWith('/api/v1/tenants/9/users/42/status', {
      active: false,
      confirmCode: 'default',
    });
  });

  it('forceLogout 锁定 POST 路径', async () => {
    mockPost.mockResolvedValue({ userId: 42, revokedAt: '2026-10-05T00:00:00Z' });
    const result = await TenantUserAdminApi.forceLogout(9, 42);
    expect(mockPost).toHaveBeenCalledWith('/api/v1/tenants/9/users/42/force-logout', {});
    expect(result.userId).toBe(42);
  });
});

describe('tenantUserAdminErrorMessage（§3.5 错误码映射）', () => {
  it('稳定错误码映射为可读中文', () => {
    expect(tenantUserAdminErrorMessage({ errorCode: 'LAST_ADMIN_PROTECTED' })).toContain(
      '最后一个管理员'
    );
    expect(tenantUserAdminErrorMessage({ errorCode: 'PASSWORD_POLICY_VIOLATION' })).toContain(
      '密码策略'
    );
    expect(tenantUserAdminErrorMessage({ errorCode: 'TENANT_USER_ADMIN_DISABLED' })).toContain(
      'TENANT_USER_ADMIN_ENABLED'
    );
    expect(Object.keys(TENANT_USER_ADMIN_ERROR_MESSAGES)).toEqual(
      expect.arrayContaining(['PLATFORM_SCOPE_REQUIRED', 'TENANT_CONFIRM_REQUIRED'])
    );
  });

  it('未知错误码回落后端 message，无 message 时兜底', () => {
    expect(tenantUserAdminErrorMessage({ errorCode: 'SOMETHING_NEW', message: 'boom' })).toBe('boom');
    expect(tenantUserAdminErrorMessage({})).toBe('操作失败，请稍后重试');
  });
});
