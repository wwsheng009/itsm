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
