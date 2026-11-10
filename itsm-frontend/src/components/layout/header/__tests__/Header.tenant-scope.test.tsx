/**
 * Header 租户作用域入口编排测试（2026-11 功能缺口回归）。
 *
 * 锁定编排契约：
 * - provider 租户 → 渲染 CustomerFilter（数据面过滤器）；
 * - 客户租户（深度态）→ 不渲染 CustomerFilter（避免 /msp/* 403），但必须渲染
 *   DeepTenantReturn，保证「返回工作台」入口存在；
 * - 原生客户用户（当前租户=归属租户）→ 两者都不渲染。
 */
import React from 'react';
import { MemoryRouter, useLocation } from 'react-router';
import { cleanup, render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';

// 覆盖 jest.setup.js 的全局 react-router mock：本套件需要真实 MemoryRouter 上下文，
// 以断言「返回工作台」的导航落点。
jest.mock('react-router', () => jest.requireActual('react-router'));

jest.mock('@/lib/store/auth-store', () => {
  const actual = jest.requireActual('@/lib/store/auth-store');
  return {
    ...actual,
    useAuthStoreHydration: () => undefined,
  };
});

jest.mock('@/lib/api/msp-workbench-api', () => ({
  MSP_WORKBENCH_PATH: '/msp/workbench',
}));

jest.mock('@/lib/api/ticket-notification-api', () => ({
  TicketNotificationApi: {
    getUserNotifications: jest.fn().mockResolvedValue({ notifications: [] }),
    getUnreadCount: jest.fn().mockResolvedValue({ count: 0 }),
    markNotificationRead: jest.fn().mockResolvedValue(undefined),
    markAllNotificationsRead: jest.fn().mockResolvedValue(undefined),
  },
  toTicketNotification: (notification: unknown) => notification,
}));

jest.mock('@/lib/services/notification-ws', () => ({
  notificationWS: {
    connect: jest.fn().mockResolvedValue(undefined),
    disconnect: jest.fn(),
    onNotification: jest.fn(() => jest.fn()),
  },
}));

jest.mock('@/lib/services/auth-service', () => ({
  AuthService: { logout: jest.fn() },
}));

jest.mock('@/lib/design-system/theme', () => {
  const actual = jest.requireActual('@/lib/design-system/theme');
  return {
    ...actual,
    useTheme: () => ({ isDark: false, toggleTheme: jest.fn() }),
  };
});

jest.mock('@/lib/i18n', () => ({
  useI18n: () => ({ language: 'zh-CN', changeLanguage: jest.fn() }),
}));

jest.mock('../GlobalSearch', () => ({
  GlobalSearch: () => null,
  SearchInput: () => null,
}));

jest.mock('../NotificationDrawer', () => ({
  NotificationDrawer: () => null,
}));

jest.mock('../UserMenuDropdown', () => ({
  UserMenuDropdown: () => null,
}));

jest.mock('../CustomerFilter', () => ({
  CustomerFilter: () => <div data-testid="customer-filter" />,
}));

jest.mock('../breadcrumb-utils', () => ({
  buildBreadcrumb: () => [],
  useBuildBreadcrumb: () => [],
}));

import { useAuthStore } from '@/lib/store/auth-store';
import { Header } from '../Header';

const PROVIDER_TENANT = {
  id: 1,
  name: 'Provider Inc',
  code: 'PROV',
  type: 'msp_provider',
  status: 'active',
  createdAt: '',
  updatedAt: '',
};

const CUSTOMER_TENANT = {
  id: 2,
  name: 'Beta LLC',
  code: 'BETA',
  type: 'msp_customer',
  status: 'active',
  createdAt: '',
  updatedAt: '',
};

const LocationProbe: React.FC = () => {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
};

function setAuthState(input: {
  currentTenant: typeof PROVIDER_TENANT | typeof CUSTOMER_TENANT;
  homeTenantId: number;
  mspRole: string;
  switchTenant?: jest.Mock;
}) {
  useAuthStore.setState({
    user: {
      id: 1,
      username: 'msp',
      email: '',
      name: '',
      tenantId: input.currentTenant.id,
      homeTenantId: input.homeTenantId,
      mspRole: input.mspRole,
      permissions: ['msp_ticket:read'],
      createdAt: '',
      updatedAt: '',
    },
    currentTenant: input.currentTenant,
    switchTenant: input.switchTenant ?? jest.fn(),
    isAuthenticated: true,
  } as never);
}

function renderHeader() {
  return render(
    <MemoryRouter initialEntries={['/dashboard']}>
      <Header collapsed={false} onCollapse={jest.fn()} showBreadcrumb={false} />
      <LocationProbe />
    </MemoryRouter>
  );
}

describe('Header 租户作用域入口编排', () => {
  afterEach(() => {
    cleanup();
    useAuthStore.setState({ currentTenant: null, user: null } as never);
  });

  it('provider 租户：渲染客户过滤器，不渲染返回入口', async () => {
    setAuthState({
      currentTenant: PROVIDER_TENANT,
      homeTenantId: 1,
      mspRole: 'provider_admin',
    });

    renderHeader();

    expect(await screen.findByTestId('customer-filter')).toBeInTheDocument();
    expect(screen.queryByTestId('deep-tenant-indicator')).not.toBeInTheDocument();
    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });

  it('客户租户深度态：隐藏客户过滤器，但保留返回入口并可切回工作台', async () => {
    const switchTenant = jest.fn().mockResolvedValue(undefined);
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 1,
      mspRole: 'provider_agent',
      switchTenant,
    });

    renderHeader();

    expect(await screen.findByTestId('deep-tenant-indicator')).toHaveTextContent(
      '当前客户：Beta LLC（深度操作中）'
    );
    expect(screen.queryByTestId('customer-filter')).not.toBeInTheDocument();

    await userEvent.click(screen.getByTestId('back-to-workbench'));

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(1));
    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('/msp/workbench')
    );
  });

  it('原生客户用户（当前租户=归属租户）：过滤器与返回入口都不渲染', async () => {
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 2,
      mspRole: 'customer_user',
    });

    renderHeader();

    // 等待 isClient 副作用完成后断言
    await waitFor(() => expect(screen.getByTestId('location')).toBeInTheDocument());
    expect(screen.queryByTestId('customer-filter')).not.toBeInTheDocument();
    expect(screen.queryByTestId('deep-tenant-indicator')).not.toBeInTheDocument();
    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });
});
