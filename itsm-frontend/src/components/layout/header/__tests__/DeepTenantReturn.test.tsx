/**
 * DeepTenantReturn 组件测试（MSW-4 / WB-A6 功能缺口回归）。
 *
 * 背景：深度态返回入口原先位于 CustomerFilter 内部，而 CustomerFilter 在客户
 * 租户下被 Header 的 provider 门禁整体卸载，导致「进入客户后无法返回」。
 * 本套件锁定独立返回入口的可见性与切回语义。
 */
import React from 'react';
import { cleanup, render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';

const mockNavigate = jest.fn();

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useNavigate: () => mockNavigate,
}));

jest.mock('@/lib/api/msp-workbench-api', () => ({
  MSP_WORKBENCH_PATH: '/msp/workbench',
}));

import { useAuthStore } from '@/lib/store/auth-store';
import { DeepTenantReturn } from '../DeepTenantReturn';

const CUSTOMER_TENANT = {
  id: 2,
  name: 'Beta LLC',
  code: 'BETA',
  type: 'msp_customer',
  status: 'active',
  createdAt: '',
  updatedAt: '',
};

const PROVIDER_TENANT = {
  id: 1,
  name: 'Provider Inc',
  code: 'PROV',
  type: 'msp_provider',
  status: 'active',
  createdAt: '',
  updatedAt: '',
};

function setAuthState(input: {
  currentTenant?: typeof CUSTOMER_TENANT | typeof PROVIDER_TENANT | null;
  homeTenantId?: number;
  mspRole?: string;
  switchTenant?: jest.Mock;
}) {
  useAuthStore.setState({
    currentTenant: input.currentTenant ?? null,
    user: {
      id: 1,
      username: 'msp',
      email: '',
      name: '',
      tenantId: input.currentTenant?.id,
      homeTenantId: input.homeTenantId,
      mspRole: input.mspRole,
      createdAt: '',
      updatedAt: '',
    },
    switchTenant: input.switchTenant ?? jest.fn(),
  } as never);
}

describe('DeepTenantReturn', () => {
  afterEach(() => {
    cleanup();
    useAuthStore.setState({ currentTenant: null, user: null } as never);
  });

  it('深度态：显示客户指示与返回按钮，点击后切回 home 再导航工作台', async () => {
    const switchTenant = jest.fn().mockResolvedValue(undefined);
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 1,
      mspRole: 'provider_agent',
      switchTenant,
    });

    render(<DeepTenantReturn />);

    expect(screen.getByTestId('deep-tenant-indicator')).toHaveTextContent(
      '当前客户：Beta LLC（深度操作中）'
    );

    await userEvent.click(screen.getByTestId('back-to-workbench'));

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(1));
    expect(mockNavigate).toHaveBeenCalledWith('/msp/workbench');
  });

  it('切回失败时不导航、保留会话并提示（MSW-A5）', async () => {
    const switchTenant = jest.fn().mockRejectedValue(new Error('租户已被暂停'));
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 1,
      mspRole: 'provider_agent',
      switchTenant,
    });

    render(<DeepTenantReturn />);
    await userEvent.click(screen.getByTestId('back-to-workbench'));

    await waitFor(() => expect(switchTenant).toHaveBeenCalledWith(1));
    expect(mockNavigate).not.toHaveBeenCalled();
    expect(useAuthStore.getState().currentTenant?.id).toBe(2);
    expect(await screen.findByText('租户已被暂停')).toBeInTheDocument();
    expect(screen.getByTestId('back-to-workbench')).toBeEnabled();
  });

  it('原生客户用户（当前租户=归属租户）不显示返回入口', () => {
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 2,
      mspRole: 'customer_user',
    });

    render(<DeepTenantReturn />);

    expect(screen.queryByTestId('deep-tenant-indicator')).not.toBeInTheDocument();
    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });

  it('provider 租户内（非深度态）不显示返回入口', () => {
    setAuthState({
      currentTenant: PROVIDER_TENANT,
      homeTenantId: 1,
      mspRole: 'provider_admin',
    });

    render(<DeepTenantReturn />);

    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });

  it('缺少 homeTenantId 时 fail-closed（不渲染，避免切错租户）', () => {
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: undefined,
      mspRole: 'provider_admin',
    });

    render(<DeepTenantReturn />);

    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });

  it('非服务商员工角色不显示返回入口', () => {
    setAuthState({
      currentTenant: CUSTOMER_TENANT,
      homeTenantId: 1,
      mspRole: 'customer_user',
    });

    render(<DeepTenantReturn />);

    expect(screen.queryByTestId('back-to-workbench')).not.toBeInTheDocument();
  });
});
