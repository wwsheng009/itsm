/**
 * TenantOnboardingModal 测试：租户开通闭环。
 *
 * 覆盖：
 * - 未开通（items 全 0 标“缺失”）→ 点击「开通模板」调用 provision 并用返回结构刷新 items；
 * - 创建首管成功且 generated=true → 展示一次性密码 + “仅显示一次”提示；
 * - bootstrapAdmins=1 / createAdmin 抛 409 → 已创建态；
 * - readiness 加载失败 → 错误 + 重试；
 * - 关闭后重开重新拉取（不复用旧数据）。
 */
import React from 'react';
import { configure, fireEvent, render, screen, waitFor } from '@/lib/test-utils';
import TenantOnboardingModal, { isBootstrapConflict } from '../TenantOnboardingModal';
import type { TenantReadinessResponse } from '@/lib/api/api-config';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

const tenant = { id: 7, name: 'Acme', code: 'acme', domain: 'acme.example.com' };

const ITEM_KEYS = [
  'roles',
  'permissions',
  'role_permissions',
  'menus',
  'groups',
  'sla_definitions',
  'ci_types',
] as const;

const readiness = (
  overrides: Partial<TenantReadinessResponse> = {},
  count = 0
): TenantReadinessResponse => ({
  tenantId: 7,
  templateVersion: 'v1.0.0',
  ready: false,
  bootstrapAdmins: 0,
  items: ITEM_KEYS.map(key => ({ key, label: `资源-${key}`, count, required: true })),
  ...overrides,
});

describe('TenantOnboardingModal', () => {
  it('未开通：items 全 0 标“缺失”，点击开通调用 provision 并刷新 items', async () => {
    const fetchReadiness = jest.fn().mockResolvedValue(readiness());
    const provision = jest
      .fn()
      .mockResolvedValue(readiness({ ready: true }, 1));
    const createAdmin = jest.fn();

    render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={provision}
        createAdmin={createAdmin}
      />
    );

    const rolesItem = await screen.findByTestId('readiness-item-roles');
    expect(rolesItem).toHaveTextContent('缺失');
    expect(screen.getByTestId('readiness-not-ready')).toBeInTheDocument();
    expect(screen.getByText('模板版本：v1.0.0')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('provision-button'));

    await waitFor(() => expect(provision).toHaveBeenCalledWith(7));
    await waitFor(() =>
      expect(screen.getByTestId('readiness-item-roles')).not.toHaveTextContent('缺失')
    );
    expect(screen.getByTestId('readiness-item-roles')).toHaveTextContent('1');
    expect(screen.getByTestId('readiness-ready')).toBeInTheDocument();
    // 刷新来自 provision 返回值，未重复拉取 readiness。
    expect(fetchReadiness).toHaveBeenCalledTimes(1);
    expect(createAdmin).not.toHaveBeenCalled();
  });

  it('创建首管成功且 generated=true：展示一次性密码与“仅显示一次”提示', async () => {
    const fetchReadiness = jest
      .fn()
      .mockResolvedValue(readiness({ ready: true }, 1));
    const createAdmin = jest.fn().mockResolvedValue({
      userId: 21,
      username: 'admin-acme',
      email: 'admin@acme.test',
      password: 'Gen3rated-pw-12345',
      generated: true,
      mustChangePassword: true,
    });

    render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={createAdmin}
      />
    );

    await screen.findByTestId('readiness-item-roles');
    fireEvent.click(screen.getByTestId('next-step'));

    fireEvent.change(await screen.findByTestId('bootstrap-username'), {
      target: { value: 'admin-acme' },
    });
    fireEvent.change(screen.getByTestId('bootstrap-email'), {
      target: { value: 'admin@acme.test' },
    });
    fireEvent.click(screen.getByTestId('create-admin-button'));

    await waitFor(() =>
      expect(createAdmin).toHaveBeenCalledWith(7, {
        username: 'admin-acme',
        email: 'admin@acme.test',
      })
    );

    const passwordInput = await screen.findByTestId('one-time-password');
    expect(passwordInput).toHaveValue('Gen3rated-pw-12345');
    expect(screen.getByText('一次性密码仅显示一次，请立即保存')).toBeInTheDocument();

    // 已创建 → 允许进入完成步骤，展示租户编码/域名与登录提示。
    fireEvent.click(screen.getByTestId('next-step'));
    const done = await screen.findByTestId('onboarding-step-done');
    expect(done).toHaveTextContent('acme');
    expect(done).toHaveTextContent('acme.example.com');
    expect(done).toHaveTextContent('首次登录必须修改密码');
  });

  it('bootstrapAdmins=1：直接进入已创建态，不展示首管表单', async () => {
    const fetchReadiness = jest
      .fn()
      .mockResolvedValue(readiness({ ready: true, bootstrapAdmins: 1 }, 1));

    render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={jest.fn()}
      />
    );

    await screen.findByTestId('readiness-item-roles');
    fireEvent.click(screen.getByTestId('next-step'));

    expect(await screen.findByTestId('admin-created')).toHaveTextContent('首个管理员已创建');
    expect(screen.queryByTestId('create-admin-button')).not.toBeInTheDocument();
  });

  it('createAdmin 抛 409：降级为已创建态（不阻塞进入下一步）', async () => {
    const fetchReadiness = jest
      .fn()
      .mockResolvedValue(readiness({ ready: true, bootstrapAdmins: 0 }, 1));
    const conflict = Object.assign(new Error('tenant already has bootstrap admin'), {
      httpStatus: 409,
      code: 4090,
    });
    const createAdmin = jest.fn().mockRejectedValue(conflict);

    render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={createAdmin}
      />
    );

    await screen.findByTestId('readiness-item-roles');
    fireEvent.click(screen.getByTestId('next-step'));
    fireEvent.click(await screen.findByTestId('create-admin-button'));

    expect(await screen.findByTestId('admin-created')).toHaveTextContent('已存在首个管理员');
    expect(screen.getByTestId('next-step')).toBeEnabled();
  });

  it('isBootstrapConflict 识别 envelope code 4090 与 HTTP 409', () => {
    expect(isBootstrapConflict({ code: 4090 })).toBe(true);
    expect(isBootstrapConflict({ httpStatus: 409 })).toBe(true);
    expect(isBootstrapConflict(new Error('network'))).toBe(false);
    expect(isBootstrapConflict(null)).toBe(false);
  });

  it('readiness 加载失败：展示错误并可重试', async () => {
    const fetchReadiness = jest
      .fn()
      .mockRejectedValueOnce(new Error('readiness 500'))
      .mockResolvedValueOnce(readiness({ ready: true }, 1));

    render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={jest.fn()}
      />
    );

    expect(await screen.findByTestId('readiness-error')).toHaveTextContent('readiness 500');

    fireEvent.click(screen.getByTestId('readiness-retry'));

    expect(await screen.findByTestId('readiness-item-roles')).toBeInTheDocument();
    expect(fetchReadiness).toHaveBeenCalledTimes(2);
  });

  it('关闭后重新打开重新拉取 readiness（不复用旧数据）', async () => {
    const fetchReadiness = jest.fn().mockResolvedValue(readiness({ ready: true }, 1));
    const { rerender } = render(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={jest.fn()}
      />
    );
    await waitFor(() => expect(fetchReadiness).toHaveBeenCalledTimes(1));

    rerender(
      <TenantOnboardingModal
        open={false}
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={jest.fn()}
      />
    );
    rerender(
      <TenantOnboardingModal
        open
        tenant={tenant}
        onClose={jest.fn()}
        fetchReadiness={fetchReadiness}
        provision={jest.fn()}
        createAdmin={jest.fn()}
      />
    );

    await waitFor(() => expect(fetchReadiness).toHaveBeenCalledTimes(2));
  });
});
