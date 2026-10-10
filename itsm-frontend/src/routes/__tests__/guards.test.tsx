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
import { RequireCapability } from '../guards';
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

  it('管理员保留既有访问语义', () => {
    setAuth({ hasPermission: () => false, isAdmin: true });
    renderGuarded();
    expect(screen.getByTestId('msp-page')).toBeInTheDocument();
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
