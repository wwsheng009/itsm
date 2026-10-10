import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';

jest.mock('@/lib/auth/session-bootstrap', () => ({
  bootstrapSession: jest.fn(),
}));

jest.mock('@/components/common/RouteLoading', () => {
  const ReactLocal = require('react');
  return {
    __esModule: true,
    default: () => ReactLocal.createElement('div', { 'data-testid': 'route-loading' }),
  };
});

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

// 守卫的拒绝语义 = 重定向到 /403；用可观测替身断言目标，避免依赖导航时序。
jest.mock('react-router', () => {
  const actual = jest.requireActual('react-router');
  const ReactLocal = require('react');
  return {
    ...actual,
    Navigate: ({ to }: { to: string }) =>
      ReactLocal.createElement('div', { 'data-testid': 'navigate-to', 'data-to': to }),
  };
});

import { useAuthStore } from '@/lib/store/auth-store';
import { RequireCapability, RequirePlatformRole } from '../guards';
import { ADMIN_AREA_PERMISSIONS } from '../area-permissions';

const setAuth = (opts: { hasPermission: (p: string) => boolean; isAdmin?: boolean }) => {
  (useAuthStore as unknown as jest.Mock).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) =>
      selector({
        hasPermission: opts.hasPermission,
        isAdmin: () => opts.isAdmin ?? false,
      })
  );
};

function renderGuarded() {
  return render(
    <MemoryRouter initialEntries={['/msp']}>
      <Routes>
        <Route path="/403" element={<div data-testid="forbidden-page" />} />
        <Route
          path="/msp"
          element={
            <RequireCapability anyOf={['msp:read', 'msp_ticket:read']}>
              <div data-testid="msp-page" />
            </RequireCapability>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

describe('RequireCapability（FE-A7 分组守卫）', () => {
  beforeEach(() => jest.clearAllMocks());

  it('命中任一能力位时放行', () => {
    setAuth({ hasPermission: p => p === 'msp_ticket:read' });
    renderGuarded();
    expect(screen.getByTestId('msp-page')).toBeInTheDocument();
  });

  it('缺少能力位时跳转 /403', () => {
    setAuth({ hasPermission: () => false });
    renderGuarded();
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('msp-page')).not.toBeInTheDocument();
  });

  it('管理员不再旁路：无码即 403（2026-10-10 治理域收口，前端门禁与后端 L2 同源）', () => {
    setAuth({ hasPermission: () => false, isAdmin: true });
    renderGuarded();
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('msp-page')).not.toBeInTheDocument();
  });
});

/**
 * B6（Sweep §4）：区域守卫并集 + 页级细化门禁。
 * 专职管理员（仅 approval:write / sla:write 等）不再被 admin 区整区拦下；
 * 不匹配页面的细码仍被页级门禁收敛到 /403。
 */
describe('RequireCapability（B6 admin 区并集 + 页级细化）', () => {
  beforeEach(() => jest.clearAllMocks());

  const renderAdminArea = (path: string) =>
    render(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/403" element={<div data-testid="forbidden-page" />} />
          <Route
            path="/admin/sla-definitions"
            element={
              <RequireCapability anyOf={ADMIN_AREA_PERMISSIONS}>
                <RequireCapability anyOf={['sla:write']}>
                  <div data-testid="sla-admin-page" />
                </RequireCapability>
              </RequireCapability>
            }
          />
          <Route
            path="/admin/bots"
            element={
              <RequireCapability anyOf={ADMIN_AREA_PERMISSIONS}>
                <RequireCapability anyOf={['ai:write']}>
                  <div data-testid="bots-admin-page" />
                </RequireCapability>
              </RequireCapability>
            }
          />
        </Routes>
      </MemoryRouter>
    );

  it('并集覆盖专职管理员细码，且不含纯读业务码', () => {
    for (const code of [
      'sla:write',
      'approval:write',
      'workflow:write',
      'ai:write',
      'mcp:admin',
      'change:approve',
      'user:write',
      'permission:read',
    ]) {
      expect(ADMIN_AREA_PERMISSIONS).toContain(code);
    }
    expect(ADMIN_AREA_PERMISSIONS).not.toContain('ticket:read');
    expect(ADMIN_AREA_PERMISSIONS).not.toContain('msp:read');
  });

  it('仅 sla:write 的专职管理员可入区域并进入 SLA 管理页', () => {
    setAuth({ hasPermission: p => p === 'sla:write' });
    renderAdminArea('/admin/sla-definitions');
    expect(screen.getByTestId('sla-admin-page')).toBeInTheDocument();
  });

  it('仅 sla:write 的专职管理员被 bots 页级门禁（ai:write）拒绝', () => {
    setAuth({ hasPermission: p => p === 'sla:write' });
    renderAdminArea('/admin/bots');
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('bots-admin-page')).not.toBeInTheDocument();
  });

  it('无任何区域细码的用户被区域守卫拒绝', () => {
    setAuth({ hasPermission: () => false });
    renderAdminArea('/admin/sla-definitions');
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('sla-admin-page')).not.toBeInTheDocument();
  });
});

/**
 * 2026-10-10 收口：租户管理（/admin/tenants）为平台专属面。
 * 判定依据 = 后端 /auth/me 下发的 isPlatformRole 标记（词表单一源=middleware.PlatformRoleCodes）；
 * 租户内 admin（含 msp_provider/msp_customer/saas_*）标记为 false，一律 /403；
 * 仅角色名（无后端标记）不放行——防前端回退到手写角色名比对（后端另有身份硬校验兜底）。
 */
describe('RequirePlatformRole（租户治理面平台守卫）', () => {
  beforeEach(() => jest.clearAllMocks());

  const setUser = (user: Record<string, unknown> | undefined) => {
    (useAuthStore as unknown as jest.Mock).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector({ user })
    );
  };

  const renderTenantAdmin = () =>
    render(
      <MemoryRouter initialEntries={['/admin/tenants']}>
        <Routes>
          <Route path="/403" element={<div data-testid="forbidden-page" />} />
          <Route
            path="/admin/tenants"
            element={
              <RequirePlatformRole>
                <div data-testid="tenant-admin-page" />
              </RequirePlatformRole>
            }
          />
        </Routes>
      </MemoryRouter>
    );

  it('平台标记为 true（super_admin/sysadmin 由后端词表判定）放行', () => {
    setUser({ role: 'super_admin', isPlatformRole: true });
    renderTenantAdmin();
    expect(screen.getByTestId('tenant-admin-page')).toBeInTheDocument();
  });

  it('租户内 admin（isPlatformRole=false）拒绝并跳 /403', () => {
    setUser({ role: 'admin', isPlatformRole: false });
    renderTenantAdmin();
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('tenant-admin-page')).not.toBeInTheDocument();
  });

  it('无用户信息 fail-closed 拒绝', () => {
    setUser(undefined);
    renderTenantAdmin();
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('tenant-admin-page')).not.toBeInTheDocument();
  });

  it('回归：仅角色名（无后端标记）不放行——前端不得按角色名比对', () => {
    setUser({ role: 'super_admin' });
    renderTenantAdmin();
    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/403');
    expect(screen.queryByTestId('tenant-admin-page')).not.toBeInTheDocument();
  });
});
