/**
 * MspProviderGate 测试：跨租户数据面入口的双条件门禁。
 *
 * 覆盖客户可见性缺陷回归（2026-10-13）：客户租户角色克隆残留 msp_ticket:read 时，
 * 仅凭权限码会误放行；本门禁要求「服务商租户上下文 + 操作码」双条件。
 */
import React from 'react';
import { render, screen } from '@/lib/test-utils';
import MspProviderGate from '../MspProviderGate';

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';

/** 与两个消费点（MspProviderGate 取租户类型 / PermissionGate 取权限）对齐。 */
function setAuth(permissions: string[], tenantType?: string) {
  (useAuthStore as unknown as jest.Mock).mockImplementation(
    (selector: (state: { user: { permissions: string[] }; currentTenant: { type?: string } | null }) => unknown) =>
      selector({ user: { permissions }, currentTenant: tenantType ? { type: tenantType } : null })
  );
}

function renderGated(permissions: string[], tenantType?: string) {
  setAuth(permissions, tenantType);
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
});
