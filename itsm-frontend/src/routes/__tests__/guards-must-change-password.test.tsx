/**
 * IP-P1-5 首登强制改密守卫用例（方案 §IP-P1-5 / TUM-4 依赖）：
 * - 收敛：mustChangePassword 用户访问其它受保护路由 → 一律回 /change-password；
 * - 自锁豁免：改密页本身放行（否则用户无法完成改密）；
 * - 放行：标志清除后正常渲染；
 * - 未登录：引导登录（既有行为回归）。
 */
import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
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

// 用可观测替身断言导航目标，避免依赖真实导航时序。
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
import { bootstrapSession } from '@/lib/auth/session-bootstrap';
import { RequireAuth } from '../guards';

const setSession = (opts: { authenticated: boolean; user?: Record<string, unknown> | null }) => {
  // 具名形状断言：jest.Mock 类型本身不含 getState 属性（TS2339）。
  (useAuthStore as unknown as { getState: jest.Mock }).getState = jest.fn(() => ({
    isAuthenticated: opts.authenticated,
    user: opts.user ?? null,
  }));
};

function renderAuth(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/change-password" element={<RequireAuth />}>
          <Route index element={<div data-testid="change-password-page" />} />
        </Route>
        <Route path="/dashboard" element={<RequireAuth />}>
          <Route index element={<div data-testid="dashboard-page" />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe('RequireAuth 首登强制改密（IP-P1-5）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (bootstrapSession as jest.Mock).mockResolvedValue('anonymous');
  });

  it('mustChangePassword 用户访问 /dashboard → 收敛到 /change-password', () => {
    setSession({ authenticated: true, user: { mustChangePassword: true } });
    renderAuth('/dashboard');

    expect(screen.getByTestId('navigate-to')).toHaveAttribute('data-to', '/change-password');
    expect(screen.queryByTestId('dashboard-page')).not.toBeInTheDocument();
  });

  it('改密页自身放行（防自锁）', () => {
    setSession({ authenticated: true, user: { mustChangePassword: true } });
    renderAuth('/change-password');

    expect(screen.getByTestId('change-password-page')).toBeInTheDocument();
    expect(screen.queryByTestId('navigate-to')).not.toBeInTheDocument();
  });

  it('标志清除后正常放行', () => {
    setSession({ authenticated: true, user: { mustChangePassword: false } });
    renderAuth('/dashboard');

    expect(screen.getByTestId('dashboard-page')).toBeInTheDocument();
  });

  it('未登录 → 引导登录并携带 redirect', async () => {
    setSession({ authenticated: false, user: null });
    renderAuth('/dashboard');

    await waitFor(() =>
      expect(screen.getByTestId('navigate-to')).toHaveAttribute(
        'data-to',
        '/login?redirect=%2Fdashboard'
      )
    );
  });
});
