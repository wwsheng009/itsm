import { TenantAPI } from '@/lib/api/tenant-api';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
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
const mockDelete = httpClient.delete as jest.Mock;
const mockRequest = httpClient.request as jest.Mock;

describe('TenantAPI', () => {
  beforeEach(() => { jest.clearAllMocks(); });

  describe('getTenants', () => {
    it('should get tenants with params', async () => {
      const expected = { tenants: [], total: 0 };
      mockGet.mockResolvedValue(expected);
      const res = await TenantAPI.getTenants({ page: 1, pageSize: 10 } as any);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants', { page: 1, pageSize: 10 });
      expect(res).toEqual(expected);
    });
  });

  describe('getTenant', () => {
    it('should get tenant by id', async () => {
      const expected = { id: 1, name: 'Tenant 1' };
      mockGet.mockResolvedValue(expected);
      const res = await TenantAPI.getTenant(1);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/1');
      expect(res).toEqual(expected);
    });
  });

  describe('createTenant', () => {
    it('should create tenant', async () => {
      const data = { name: 'New Tenant', code: 'new-tenant' };
      const expected = { id: 2, ...data };
      mockPost.mockResolvedValue(expected);
      const res = await TenantAPI.createTenant(data as any);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/tenants', data);
      expect(res).toEqual(expected);
    });
  });

  describe('updateTenant', () => {
    it('should update tenant', async () => {
      const data = { name: 'Updated Tenant' };
      const expected = { id: 1, name: 'Updated Tenant' };
      mockPut.mockResolvedValue(expected);
      const res = await TenantAPI.updateTenant(1, data as any);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/tenants/1', data);
      expect(res).toEqual(expected);
    });

    // IP-P2-6：quota 对象原样透传（空 {} = 清空 → 不限）。
    it('should pass tenant quota through unchanged', async () => {
      const data = { quota: { maxUsers: 5, maxTicketsPerMonth: 100 } };
      mockPut.mockResolvedValue({ id: 1, ...data });
      await TenantAPI.updateTenant(1, data as any);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/tenants/1', data);

      mockPut.mockClear();
      await TenantAPI.updateTenant(1, { quota: {} } as any);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/tenants/1', { quota: {} });
    });
  });

  describe('getTenantReadiness', () => {
    it('reads GET /api/v1/tenants/:id/readiness', async () => {
      const expected = {
        tenantId: 1,
        templateVersion: 'v1',
        ready: false,
        bootstrapAdmins: 0,
        items: [{ key: 'roles', label: '角色', count: 0, required: true }],
      };
      mockGet.mockResolvedValue(expected);

      const res = await TenantAPI.getTenantReadiness(1);

      expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/1/readiness');
      expect(res).toEqual(expected);
    });
  });

  describe('provisionTenant', () => {
    it('POSTs templateVersion with a 120s timeout', async () => {
      const expected = {
        tenantId: 1,
        templateVersion: 'v2',
        ready: true,
        bootstrapAdmins: 0,
        items: [],
      };
      mockRequest.mockResolvedValue(expected);

      const res = await TenantAPI.provisionTenant(1, 'v2');

      expect(mockRequest).toHaveBeenCalledWith('/api/v1/tenants/1/provision', {
        method: 'POST',
        body: JSON.stringify({ templateVersion: 'v2' }),
        timeout: 120000,
      });
      expect(res).toEqual(expected);
    });

    it('omits templateVersion when not provided (backend default template)', async () => {
      mockRequest.mockResolvedValue({ tenantId: 2, ready: true, bootstrapAdmins: 0, items: [] });

      await TenantAPI.provisionTenant(2);

      expect(mockRequest).toHaveBeenCalledWith('/api/v1/tenants/2/provision', {
        method: 'POST',
        body: JSON.stringify({}),
        timeout: 120000,
      });
    });
  });

  describe('createBootstrapAdmin', () => {
    it('POSTs payload with a 60s timeout', async () => {
      const payload = { username: 'admin-acme', email: 'admin@acme.test' };
      const expected = {
        userId: 9,
        username: 'admin-acme',
        email: 'admin@acme.test',
        generated: false,
        mustChangePassword: true,
      };
      mockRequest.mockResolvedValue(expected);

      const res = await TenantAPI.createBootstrapAdmin(1, payload);

      expect(mockRequest).toHaveBeenCalledWith('/api/v1/tenants/1/bootstrap-admin', {
        method: 'POST',
        body: JSON.stringify(payload),
        timeout: 60000,
      });
      expect(res).toEqual(expected);
    });

    it('defaults to an empty payload (server generates password / default username)', async () => {
      mockRequest.mockResolvedValue({
        userId: 9,
        username: 'admin-acme',
        email: '',
        password: 'generated-once',
        generated: true,
        mustChangePassword: true,
      });

      await TenantAPI.createBootstrapAdmin(3);

      expect(mockRequest).toHaveBeenCalledWith('/api/v1/tenants/3/bootstrap-admin', {
        method: 'POST',
        body: JSON.stringify({}),
        timeout: 60000,
      });
    });
  });

  describe('deleteTenant', () => {
    it('should delete tenant', async () => {
      mockDelete.mockResolvedValue(undefined);
      await TenantAPI.deleteTenant(1);
      expect(mockDelete).toHaveBeenCalledWith('/api/v1/tenants/1');
    });
  });

  describe('getCurrentTenant', () => {
    it('should get current tenant', async () => {
      const expected = { id: 1, name: 'Current' };
      mockGet.mockResolvedValue(expected);
      const res = await TenantAPI.getCurrentTenant();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/tenants/current');
      expect(res).toEqual(expected);
    });
  });

  describe('switchTenant', () => {
    it('uses the auth switch-tenant endpoint (IP-P0-6 contract)', async () => {
      const resp = { tenant: { id: 2, name: 'Customer', code: 'cust', type: 'msp_customer', status: 'active' } };
      mockPost.mockResolvedValue(resp);
      const res = await TenantAPI.switchTenant(2);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/switch-tenant', { tenantId: 2 });
      expect(res).toEqual(resp);
    });
  });

  describe('getMyTenants', () => {
    it('reads candidates from /api/v1/auth/tenants', async () => {
      const resp = { tenants: [{ id: 1, name: 'Home', code: 'home' }] };
      mockGet.mockResolvedValue(resp);
      const res = await TenantAPI.getMyTenants();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/auth/tenants');
      expect(res).toEqual(resp);
    });
  });
});
