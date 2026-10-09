/**
 * TenantUsersDrawer（TUM-4）测试。
 *
 * 覆盖：
 * - 列表渲染（总数/待改密徽标/首管标记）；
 * - 403 平台面错误态；
 * - 空态三分（无用户 / 搜索无结果）；
 * - 重置密码两模式（generated 结果卡片 + 防误关勾选）；
 * - 启停乐观更新失败回滚（LAST_ADMIN_PROTECTED 文案映射）。
 */
import React from 'react';
import { MemoryRouter } from 'react-router';
import { act, fireEvent, render, screen, waitFor } from '@/lib/test-utils';
import TenantUsersDrawer from '../TenantUsersDrawer';
import { TenantUserAdminApi } from '@/lib/api/tenant-user-admin-api';
import type { TenantUserItem } from '@/lib/api/tenant-user-admin-api';

jest.mock('@/lib/api/tenant-user-admin-api', () => ({
  TenantUserAdminApi: {
    listUsers: jest.fn(),
    getUser: jest.fn(),
    resetPassword: jest.fn(),
    setStatus: jest.fn(),
    forceLogout: jest.fn(),
    offboardUser: jest.fn(),
  },
  tenantUserAdminErrorMessage: jest.fn((error: unknown) => {
    const e = error as { errorCode?: string; message?: string };
    if (e?.errorCode === 'LAST_ADMIN_PROTECTED') {
      return '该租户最后一个管理员不可停用，请先指派新管理员';
    }
    return e?.message ?? '操作失败，请稍后重试';
  }),
}));

jest.mock('@/lib/hooks/usePasswordPolicy', () => ({
  usePasswordPolicy: () => ({
    policy: {},
    hint: '至少 8 位，含大小写字母与数字',
    buildRules: () => [{ required: true, message: '请输入新密码' }],
  }),
}));

const mockListUsers = TenantUserAdminApi.listUsers as jest.Mock;
const mockSetStatus = TenantUserAdminApi.setStatus as jest.Mock;
const mockResetPassword = TenantUserAdminApi.resetPassword as jest.Mock;
const mockOffboard = TenantUserAdminApi.offboardUser as jest.Mock;

const tenant = { id: 9, name: '客户A', code: 'cust-a', type: 'saas_customer' };

const baseItem: TenantUserItem = {
  id: 42,
  username: 'alice',
  name: 'Alice',
  email: 'alice@example.com',
  role: 'end_user',
  active: true,
  mustChangePassword: true,
  isBootstrapAdmin: false,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
};

const renderDrawer = (props: Partial<React.ComponentProps<typeof TenantUsersDrawer>> = {}) =>
  render(
    <MemoryRouter initialEntries={['/admin/tenants?tenantUsers=9']}>
      <TenantUsersDrawer open tenant={tenant} onClose={jest.fn()} {...props} />
    </MemoryRouter>
  );

describe('TenantUsersDrawer', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('渲染列表：总数、用户名、待改密徽标与首管标记', async () => {
    mockListUsers.mockResolvedValue({
      items: [{ ...baseItem, isBootstrapAdmin: true }],
      total: 1,
      page: 1,
      pageSize: 20,
    });

    renderDrawer();

    expect(await screen.findByText('alice')).toBeInTheDocument();
    expect(screen.getByText('共 1 个用户')).toBeInTheDocument();
    expect(screen.getByText('待改密')).toBeInTheDocument();
    expect(screen.getByText('首管')).toBeInTheDocument();
    expect(mockListUsers).toHaveBeenCalledWith(9, {
      page: 1,
      pageSize: 20,
      keyword: undefined,
      status: undefined,
      role: undefined,
    });
  });

  it('403 PLATFORM_SCOPE_REQUIRED 渲染平台面说明', async () => {
    mockListUsers.mockRejectedValue({ errorCode: 'PLATFORM_SCOPE_REQUIRED', message: 'forbidden' });

    renderDrawer();

    expect(await screen.findByText('需要平台管理员权限')).toBeInTheDocument();
  });

  it('空态：无用户展示建号 CTA；搜索无结果显示清空筛选', async () => {
    mockListUsers.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 });
    const onProvision = jest.fn();

    renderDrawer({ onProvision });

    expect(await screen.findByText('该租户暂无用户')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /去建号/ }));
    expect(onProvision).toHaveBeenCalledTimes(1);

    // 搜索后无结果 → 清空筛选
    fireEvent.change(screen.getByPlaceholderText('搜索用户名 / 姓名 / 邮箱'), {
      target: { value: 'nobody' },
    });
    expect(
      await screen.findByText('没有匹配的用户', undefined, { timeout: 3000 })
    ).toBeInTheDocument();
  });

  it('重置密码 generated：结果卡片防误关（勾选后方可关闭）', async () => {
    mockListUsers.mockResolvedValue({ items: [baseItem], total: 1, page: 1, pageSize: 20 });
    mockResetPassword.mockResolvedValue({
      userId: 42,
      mode: 'generated',
      generatedPassword: 'Xk7#mQ2pL9@vR4tZ',
      mustChangePassword: true,
    });

    renderDrawer();
    await screen.findByText('alice');

    fireEvent.click(screen.getByRole('button', { name: '重置密码' }));
    expect(await screen.findByText('一次性口令（推荐）')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '确认重置' }));

    expect(await screen.findByText('Xk7#mQ2pL9@vR4tZ')).toBeInTheDocument();
    expect(mockResetPassword).toHaveBeenCalledWith(9, 42, { mode: 'generated', newPassword: undefined });

    const closeButton = screen.getByTestId('reset-password-close');
    expect(closeButton).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: /我已保存口令/ }));
    await waitFor(() => expect(screen.getByTestId('reset-password-close')).toBeEnabled());
  });

  it('停用失败：乐观更新回滚并展示护栏文案', async () => {
    mockListUsers.mockResolvedValue({
      items: [{ ...baseItem, role: 'agent' }],
      total: 1,
      page: 1,
      pageSize: 20,
    });
    mockSetStatus.mockRejectedValue({ errorCode: 'LAST_ADMIN_PROTECTED', message: 'conflict' });

    renderDrawer();
    await screen.findByText('alice');

    const switchEl = screen.getByRole('switch');
    expect(switchEl).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(switchEl);
    fireEvent.click(await screen.findByRole('button', { name: '确认停用' }));

    await waitFor(() => expect(mockSetStatus).toHaveBeenCalledWith(9, 42, { active: false }));
    // 失败回滚：开关恢复为启用
    await waitFor(() =>
      expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
    );
    expect(await screen.findByText('该租户最后一个管理员不可停用，请先指派新管理员')).toBeInTheDocument();
  });

  it('离职（IP-P2-8 D3=B）：二次确认后调用 offboard 并提示撤销计数', async () => {
    mockListUsers.mockResolvedValue({ items: [baseItem], total: 1, page: 1, pageSize: 20 });
    mockOffboard.mockResolvedValue({
      userId: 42,
      deactivated: true,
      alreadyInactive: false,
      allocationsRevoked: 2,
      allocationsPending: 0,
    });

    renderDrawer();
    await screen.findByText('alice');

    fireEvent.click(screen.getByRole('button', { name: '离职' }));
    fireEvent.click(await screen.findByRole('button', { name: '确认离职' }));

    await waitFor(() => expect(mockOffboard).toHaveBeenCalledWith(9, 42));
    expect(await screen.findByText(/已离职「alice」：撤销 2 个分配/)).toBeInTheDocument();
  });
});
