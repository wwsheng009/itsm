/**
 * MspProviderGate 测试：跨租户数据面入口的双条件门禁。
 *
 * 覆盖客户可见性缺陷回归（2026-10-13）：客户租户角色克隆残留 msp_ticket:read 时，
 * 仅凭权限码会误放行；本门禁要求「服务商租户上下文 + 服务商角色 + 操作码」多条件
 * fail-closed（2026-10-18 起与后端 IsMSP = 租户类型 + msp_role 同向）。
 */
import React from 'react';
import { render, screen } from '@/lib/test-utils';
import MspProviderGate from '../MspProviderGate';

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';

/**
 * 与门禁消费方式对齐：canUseMspWorkbench 需「userId + 服务商租户类型 +
 * msp_role + msp_ticket:read」四条件 fail-closed（2026-10-18 起与后端 IsMSP 同向）。
 */
function setAuth(
  permissions: string[],
  tenantType?: string,
  mspRole: string | null = 'provider_admin'
) {
  (useAuthStore as unknown as jest.Mock).mockImplementation(
    (selector: (state: {
      user: { id: number; permissions: string[]; mspRole?: string | null };
      currentTenant: { type?: string } | null;
      hasPermission: (permission: string) => boolean;
    }) => unknown) =>
      selector({
        user: { id: 1, permissions, mspRole },
        currentTenant: tenantType ? { type: tenantType } : null,
        hasPermission: (permission: string) =>
          permissions.includes(permission) || permissions.includes('*'),
      })
  );
}

function renderGated(
  permissions: string[],
  tenantType?: string,
  mspRole: string | null = 'provider_admin'
) {
  setAuth(permissions, tenantType, mspRole);
  render(
    <MspProviderGate>
      <div data-testid='msp-child' />
    </MspProviderGate>
  );
}

describe('MspProviderGate', () => {
  it('服务商租户 + msp_ticket:read → 渲染', () => {
    renderGated(['msp_ticket:read'], 'msp_provider');
    expect(screen.getByTestId('msp-child')).toBeInTheDocument();
  });

  it('历史别名 msp 租户类型同样放行', () => {
    renderGated(['msp_ticket:read'], 'msp');
    expect(screen.getByTestId('msp-child')).toBeInTheDocument();
  });

  it('客户租户即使残留 msp_ticket:read → 不渲染（回归：角色克隆过度授权）', () => {
    renderGated(['msp_ticket:read', 'ticket:read'], 'msp_customer');
    expect(screen.queryByTestId('msp-child')).not.toBeInTheDocument();
  });

  it('服务商租户但无操作码（仅 msp:read）→ 不渲染', () => {
    renderGated(['msp:read'], 'msp_provider');
    expect(screen.queryByTestId('msp-child')).not.toBeInTheDocument();
  });

  it('租户类型缺失 → 不渲染（fail-closed）', () => {
    renderGated(['msp_ticket:read']);
    expect(screen.queryByTestId('msp-child')).not.toBeInTheDocument();
  });

  it('服务商租户 + 操作码但账户无服务商角色（msp_role 缺失）→ 不渲染（2026-10-18 回归）', () => {
    renderGated(['msp_ticket:read'], 'msp_provider', null);
    expect(screen.queryByTestId('msp-child')).not.toBeInTheDocument();
  });
});
