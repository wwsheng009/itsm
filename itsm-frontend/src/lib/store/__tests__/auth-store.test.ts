/**
 * 认证状态 Store 测试
 * 测试 useAuthStore 的核心功能
 */

// 全局测试 API（describe/it/expect/jest）由 @types/jest 提供。
// 这里不再 import '@jest/globals'：该包未安装（jest 29 + @types/jest 方案），
// 缺失的模块会让 `npm run type-check`（`tsc --noEmit`）整包失败。
import { act } from '@testing-library/react';

// Mock 依赖
jest.mock('@/lib/auth/token-storage', () => ({
  clearAuthStorage: jest.fn(),
}));

jest.mock('@/lib/auth/tenant-context', () => ({
  setTenant: jest.fn(),
  clearTenant: jest.fn(),
}));

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    setTenantId: jest.fn(),
    setTenantCode: jest.fn(),
    clearToken: jest.fn(),
    get: jest.fn(),
  },
  abortAllRequests: jest.fn(),
}));

jest.mock('@/lib/api/tenant-api', () => ({
  TenantAPI: { switchTenant: jest.fn() },
}));

jest.mock('@/lib/providers/QueryProvider', () => {
  const clear = jest.fn();
  return { getQueryClient: jest.fn(() => ({ clear })), __clear: clear };
});

describe('useAuthStore', () => {
  beforeEach(() => {
    jest.resetModules();
    // 清理 localStorage
    window.localStorage.clear();
  });

  // ============================================
  // 初始状态测试
  // ============================================
  describe('初始状态', () => {
    it('应为未认证状态', async () => {
      const { useAuthStore } = await import('../auth-store');
      const state = useAuthStore.getState();
      
      expect(state.user).toBeNull();
      expect(state.token).toBeNull();
      expect(state.currentTenant).toBeNull();
      expect(state.isAuthenticated).toBe(false);
      expect(state.isLoading).toBe(false);
    });
  });

  // ============================================
  // 登录/登出测试
  // ============================================
  describe('login', () => {
    it('应正确设置用户信息', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'admin',
        tenantId: 1,
      };

      const mockTenant = {
        id: 1,
        name: 'Test Tenant',
        code: 'test',
        type: 'standard' as const,
        status: 'active' as const,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token', mockTenant);
      });

      const state = useAuthStore.getState();

      expect(state.user).toEqual(mockUser);
      expect(state.isAuthenticated).toBe(true);
      expect(state.isLoading).toBe(false);
      expect(state.currentTenant).toEqual(mockTenant);
    });

    it('登录时无租户应设置 currentTenant 为 null', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      const state = useAuthStore.getState();
      expect(state.currentTenant).toBeNull();
    });
  });

  describe('logout', () => {
    it('应清除所有认证状态', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      // 先登录
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'admin',
        tenantId: 1,
      };
      
      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      // 验证登录状态
      expect(useAuthStore.getState().isAuthenticated).toBe(true);

      // 登出
      act(() => {
        useAuthStore.getState().logout();
      });

      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
      expect(state.token).toBeNull();
      expect(state.currentTenant).toBeNull();
      expect(state.isAuthenticated).toBe(false);

      // IP-P0-8：登出清理 = 取消在途 + 清空数据缓存
      const { abortAllRequests } = await import('@/lib/api/http-client');
      expect(abortAllRequests).toHaveBeenCalled();
      const queryProvider = (await import('@/lib/providers/QueryProvider')) as unknown as {
        __clear: jest.Mock;
      };
      expect(queryProvider.__clear).toHaveBeenCalled();
    });
  });

  describe('switchTenant（IP-P0-8 深度切换链路）', () => {
    it('切换成功后重建 user/租户并清空缓存', async () => {
      const { useAuthStore } = await import('../auth-store');
      const { TenantAPI } = await import('@/lib/api/tenant-api');
      const { httpClient } = await import('@/lib/api/http-client');
      const queryProvider = (await import('@/lib/providers/QueryProvider')) as unknown as {
        __clear: jest.Mock;
      };

      (TenantAPI.switchTenant as jest.Mock).mockResolvedValue({
        tenant: { id: 9, name: 'Customer', code: 'cust', type: 'msp_customer', status: 'active' },
      });
      (httpClient.get as jest.Mock).mockResolvedValue({
        id: 1,
        username: 'msp',
        email: 'msp@example.com',
        name: 'MSP User',
        role: 'agent',
        tenantId: 9,
        permissions: ['msp_ticket:read'],
      });

      await act(async () => {
        await useAuthStore.getState().switchTenant(9);
      });

      expect(TenantAPI.switchTenant).toHaveBeenCalledWith(9);
      expect(httpClient.get).toHaveBeenCalledWith('/api/v1/auth/me');
      const state = useAuthStore.getState();
      expect(state.currentTenant?.id).toBe(9);
      expect(state.user?.tenantId).toBe(9);
      expect(state.isSwitching).toBe(false);
      expect(queryProvider.__clear).toHaveBeenCalled();
    });
  });

  describe('updateUser', () => {
    it('应更新用户信息', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user',
        tenantId: 1,
      };
      
      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      act(() => {
        useAuthStore.getState().updateUser({ name: 'Updated Name' });
      });

      const state = useAuthStore.getState();
      expect(state.user?.name).toBe('Updated Name');
      expect(state.user?.email).toBe('test@example.com'); // 保持不变
    });

    it('用户未登录时 updateUser 应不报错', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      // 未登录状态
      act(() => {
        useAuthStore.getState().updateUser({ name: 'Updated Name' });
      });

      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
    });
  });

  describe('setLoading', () => {
    it('应设置 loading 状态', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      act(() => {
        useAuthStore.getState().setLoading(true);
      });

      expect(useAuthStore.getState().isLoading).toBe(true);

      act(() => {
        useAuthStore.getState().setLoading(false);
      });

      expect(useAuthStore.getState().isLoading).toBe(false);
    });
  });

  // ============================================
  // 租户管理测试
  // ============================================
  describe('setCurrentTenant', () => {
    it('应设置当前租户', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockTenant = {
        id: 1,
        name: 'Test Tenant',
        code: 'test',
        type: 'standard' as const,
        status: 'active' as const,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };

      act(() => {
        useAuthStore.getState().setCurrentTenant(mockTenant);
      });

      const state = useAuthStore.getState();
      expect(state.currentTenant).toEqual(mockTenant);
    });
  });

  describe('clearTenant', () => {
    it('应清除租户', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockTenant = {
        id: 1,
        name: 'Test Tenant',
        code: 'test',
        type: 'standard' as const,
        status: 'active' as const,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };

      act(() => {
        useAuthStore.getState().setCurrentTenant(mockTenant);
      });

      act(() => {
        useAuthStore.getState().clearTenant();
      });

      const state = useAuthStore.getState();
      expect(state.currentTenant).toBeNull();
    });
  });

  // ============================================
  // 权限检查测试
  // ============================================
  describe('hasPermission', () => {
    it('用户有权限时应返回 true', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'admin',
        tenantId: 1,
        permissions: ['ticket:read', 'ticket:create'],
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasPermission('ticket:read')).toBe(true);
      expect(useAuthStore.getState().hasPermission('ticket:create')).toBe(true);
    });

    it('用户无权限时应返回 false', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user',
        tenantId: 1,
        permissions: ['ticket:read'],
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasPermission('ticket:delete')).toBe(false);
    });

    it('用户为 null 时应返回 false', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      expect(useAuthStore.getState().hasPermission('ticket:read')).toBe(false);
    });

    it('super_admin 的 ["*"] 通配符应匹配任意权限码（管理菜单判定依赖）', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Super Admin',
        username: 'superadmin',
        email: 'admin@example.com',
        role: 'super_admin',
        tenantId: 1,
        // 后端 Login getUserPermissions 对 super_admin 下发 ["*"]
        permissions: ['*'],
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      // Sidebar isAdmin 判定的四个权限码都必须命中
      expect(useAuthStore.getState().hasPermission('user:write')).toBe(true);
      expect(useAuthStore.getState().hasPermission('role:write')).toBe(true);
      expect(useAuthStore.getState().hasPermission('system_config:write')).toBe(true);
      expect(useAuthStore.getState().hasPermission('ticket_type:manage')).toBe(true);
    });

    it('资源级通配符 "resource:*" 应匹配该资源下任意 action', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 2,
        name: 'Ops Admin',
        username: 'opsadmin',
        email: 'ops@example.com',
        role: 'admin',
        tenantId: 1,
        permissions: ['user:*', 'ticket:read'],
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasPermission('user:write')).toBe(true);
      expect(useAuthStore.getState().hasPermission('user:delete')).toBe(true);
      expect(useAuthStore.getState().hasPermission('ticket:read')).toBe(true);
      // 资源不通配时不得跨界
      expect(useAuthStore.getState().hasPermission('role:write')).toBe(false);
      expect(useAuthStore.getState().hasPermission('ticket:write')).toBe(false);
    });

    it('permissions 为空数组时应返回 false', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 3,
        name: 'Plain User',
        username: 'plainuser',
        email: 'plain@example.com',
        role: 'end_user',
        tenantId: 1,
        permissions: [],
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasPermission('ticket:read')).toBe(false);
    });
  });

  describe('hasRole', () => {
    it('用户有角色时应返回 true', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'admin',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasRole('admin')).toBe(true);
    });

    it('用户角色不匹配时应返回 false', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().hasRole('admin')).toBe(false);
    });
  });

  describe('isAdmin', () => {
    it('admin 角色应返回 true', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'admin',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().isAdmin()).toBe(true);
    });

    it('super_admin 角色应返回 true', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'super_admin',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().isAdmin()).toBe(true);
    });

    it('普通用户应返回 false', async () => {
      const { useAuthStore } = await import('../auth-store');
      
      const mockUser = {
        id: 1,
        name: 'Test User',
        username: 'testuser',
        email: 'test@example.com',
        role: 'user',
        tenantId: 1,
      };

      act(() => {
        useAuthStore.getState().login(mockUser, 'mock-token');
      });

      expect(useAuthStore.getState().isAdmin()).toBe(false);
    });
  });
});

// ============================================
// useTenantStore 测试
// ============================================
describe('useTenantStore', () => {
  beforeEach(() => {
    jest.resetModules();
  });

  describe('初始状态', () => {
    it('应有初始空状态', async () => {
      const { useTenantStore } = await import('../auth-store');
      const state = useTenantStore.getState();
      
      expect(state.tenants).toEqual([]);
      expect(state.loading).toBe(false);
      expect(state.error).toBeNull();
    });
  });

  describe('setTenants', () => {
    it('应设置租户列表', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      const mockTenants = [
        { id: 1, name: 'Tenant 1', code: 't1', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
        { id: 2, name: 'Tenant 2', code: 't2', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
      ];

      act(() => {
        useTenantStore.getState().setTenants(mockTenants);
      });

      expect(useTenantStore.getState().tenants).toEqual(mockTenants);
    });
  });

  describe('addTenant', () => {
    it('应添加租户到列表', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      const mockTenant = { id: 1, name: 'New Tenant', code: 'new', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };

      act(() => {
        useTenantStore.getState().addTenant(mockTenant);
      });

      expect(useTenantStore.getState().tenants).toContainEqual(mockTenant);
    });
  });

  describe('updateTenant', () => {
    it('应更新指定租户', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      const mockTenants = [
        { id: 1, name: 'Tenant 1', code: 't1', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
        { id: 2, name: 'Tenant 2', code: 't2', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
      ];

      act(() => {
        useTenantStore.getState().setTenants(mockTenants);
      });

      act(() => {
        useTenantStore.getState().updateTenant(1, { name: 'Updated Tenant' });
      });

      const tenants = useTenantStore.getState().tenants;
      expect(tenants[0].name).toBe('Updated Tenant');
      expect(tenants[1].name).toBe('Tenant 2'); // 未更新的应保持不变
    });
  });

  describe('removeTenant', () => {
    it('应从列表中移除租户', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      const mockTenants = [
        { id: 1, name: 'Tenant 1', code: 't1', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
        { id: 2, name: 'Tenant 2', code: 't2', type: 'standard' as const, status: 'active' as const, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
      ];

      act(() => {
        useTenantStore.getState().setTenants(mockTenants);
      });

      act(() => {
        useTenantStore.getState().removeTenant(1);
      });

      const tenants = useTenantStore.getState().tenants;
      expect(tenants).toHaveLength(1);
      expect(tenants[0].id).toBe(2);
    });
  });

  describe('setLoading / setError', () => {
    it('应正确设置 loading 状态', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      act(() => {
        useTenantStore.getState().setLoading(true);
      });

      expect(useTenantStore.getState().loading).toBe(true);
    });

    it('应正确设置 error 状态', async () => {
      const { useTenantStore } = await import('../auth-store');
      
      act(() => {
        useTenantStore.getState().setError('Some error');
      });

      expect(useTenantStore.getState().error).toBe('Some error');
    });
  });
});

// ============================================
// PERMISSIONS 常量测试
// ============================================
describe('权限常量', () => {
  it('PERMISSIONS 应包含所有预期权限', async () => {
    const { PERMISSIONS } = await import('../auth-store');
    
    expect(PERMISSIONS.TICKET_VIEW).toBe('ticket:read');
    expect(PERMISSIONS.TICKET_CREATE).toBe('ticket:create');
    expect(PERMISSIONS.TICKET_DELETE).toBe('ticket:delete');
    expect(PERMISSIONS.USER_VIEW).toBe('user:read');
    expect(PERMISSIONS.SYSTEM_CONFIG).toBe('system:config');
  });

  it('ROLES 应包含所有预期角色', async () => {
    const { ROLES } = await import('../auth-store');
    
    expect(ROLES.SUPER_ADMIN).toBe('super_admin');
    expect(ROLES.ADMIN).toBe('admin');
    expect(ROLES.MANAGER).toBe('manager');
    expect(ROLES.AGENT).toBe('agent');
    expect(ROLES.END_USER).toBe('end_user');
  });
});
