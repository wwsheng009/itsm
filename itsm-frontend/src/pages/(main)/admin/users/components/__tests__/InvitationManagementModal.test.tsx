/**
 * InvitationManagementModal 测试（IP-P1-4c 管理侧）。
 *
 * 覆盖：
 * - 列表加载：邮箱/状态/角色/邀请人渲染 + 查询参数（tenantId/分页）；
 * - 撤销：Popconfirm 确认后调用 revokeInvitation 并重新拉取；
 * - 创建：提交载荷正确，SMTP 未配置时回显 inviteUrl 与提示文案；
 * - 过滤：状态切换后携带 status 重新拉取。
 */
import React from 'react';
import { configure, fireEvent, render, screen, waitFor, within } from '@/lib/test-utils';
import InvitationManagementModal from '../InvitationManagementModal';
import type { InvitationListItem } from '@/lib/api/invitation-api';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

const baseItem = (overrides: Partial<InvitationListItem> = {}): InvitationListItem => ({
  id: 1,
  tenantId: 7,
  email: 'pending@example.com',
  roleId: 5,
  roleCode: 'agent',
  roleName: '服务台坐席',
  status: 'pending',
  invitedBy: 3,
  inviterName: 'admin',
  expiresAt: '2026-10-06T10:00:00Z',
  createdAt: '2026-10-03T10:00:00Z',
  ...overrides,
});

const makeApi = () => ({
  listInvitations: jest.fn(),
  createInvitation: jest.fn(),
  revokeInvitation: jest.fn(),
});

const roles = [{ id: 5, name: '服务台坐席', code: 'agent' }];

describe('InvitationManagementModal', () => {
  it('加载并渲染列表（状态/角色/邀请人/过期时间）', async () => {
    const api = makeApi();
    api.listInvitations.mockResolvedValue({
      invitations: [baseItem(), baseItem({ id: 2, email: 'done@example.com', status: 'accepted' })],
      total: 2,
      limit: 10,
      offset: 0,
    });

    render(
      <InvitationManagementModal open onClose={jest.fn()} tenantId={7} roles={roles} api={api} />
    );

    expect(await screen.findByText('pending@example.com')).toBeInTheDocument();
    expect(screen.getByText('done@example.com')).toBeInTheDocument();
    expect(screen.getByText('待接受')).toBeInTheDocument();
    expect(screen.getByText('已接受')).toBeInTheDocument();
    expect(screen.getAllByText('服务台坐席').length).toBeGreaterThan(0);
    expect(screen.getAllByText('admin').length).toBe(2);
    expect(api.listInvitations).toHaveBeenCalledWith({
      tenantId: 7,
      status: undefined,
      limit: 10,
      offset: 0,
    });
  });

  it('撤销 pending 邀请：Popconfirm 确认后调用 API 并刷新', async () => {
    const api = makeApi();
    api.listInvitations.mockResolvedValue({
      invitations: [baseItem()],
      total: 1,
      limit: 10,
      offset: 0,
    });
    api.revokeInvitation.mockResolvedValue({ id: 1, status: 'revoked' });

    render(
      <InvitationManagementModal open onClose={jest.fn()} tenantId={7} roles={roles} api={api} />
    );

    fireEvent.click(await screen.findByTestId('invite-revoke-1'));
    fireEvent.click(await screen.findByRole('button', { name: '确定撤销' }));

    await waitFor(() => expect(api.revokeInvitation).toHaveBeenCalledWith(1));
    await waitFor(() => expect(api.listInvitations).toHaveBeenCalledTimes(2));
  });

  it('创建邀请：提交后回显 inviteUrl（SMTP 未配置提示）', async () => {
    const api = makeApi();
    api.listInvitations.mockResolvedValue({ invitations: [], total: 0, limit: 10, offset: 0 });
    api.createInvitation.mockResolvedValue({
      id: 12,
      email: 'new@example.com',
      status: 'pending',
      expiresAt: '2026-10-06T10:00:00Z',
      inviteUrl: 'http://localhost:5173/invite/tok-1',
      emailSent: false,
    });

    render(
      <InvitationManagementModal open onClose={jest.fn()} tenantId={7} roles={roles} api={api} />
    );

    fireEvent.click(screen.getByTestId('invite-create-button'));
    fireEvent.change(await screen.findByTestId('invite-email-input'), {
      target: { value: 'new@example.com' },
    });

    // antd Select 在 selector 上监听 mousedown；testid 挂外层 wrapper（同 admin/tools 用例模式）。
    fireEvent.mouseDown(within(screen.getByTestId('invite-role-select')).getByRole('combobox'));
    const roleOption = await screen.findByTitle('服务台坐席（agent）');
    fireEvent.mouseDown(roleOption);
    fireEvent.click(roleOption);
    // 选中态回显（先确认选择确实生效，再断言提交链路）。
    await waitFor(() =>
      expect(
        within(screen.getByTestId('invite-role-select')).getByText('服务台坐席（agent）')
      ).toBeInTheDocument()
    );
    // 诊断：确认邮箱输入已进入表单（失败时给出可读原因）。
    await waitFor(() =>
      expect((screen.getByTestId('invite-email-input') as HTMLInputElement).value).toBe(
        'new@example.com'
      )
    );

    fireEvent.click(screen.getByTestId('invite-submit'));

    await waitFor(() => {
      const err = document.querySelector('.ant-form-item-explain-error');
      if (err?.textContent) {
        throw new Error(`表单校验失败: ${err.textContent}`);
      }
      expect(api.createInvitation).toHaveBeenCalledWith({
        tenantId: 7,
        email: 'new@example.com',
        roleId: 5,
        mspRole: undefined,
      });
    });

    const urlInput = (await screen.findByTestId('invite-url')) as HTMLInputElement;
    expect(urlInput.value).toBe('http://localhost:5173/invite/tok-1');
    expect(screen.getByText(/SMTP 未配置/)).toBeInTheDocument();
  });

  it('状态过滤：选择“已撤销”后携带 status 重新拉取', async () => {
    const api = makeApi();
    api.listInvitations.mockResolvedValue({ invitations: [], total: 0, limit: 10, offset: 0 });

    render(
      <InvitationManagementModal open onClose={jest.fn()} tenantId={7} roles={roles} api={api} />
    );

    await waitFor(() => expect(api.listInvitations).toHaveBeenCalledTimes(1));

    fireEvent.mouseDown(within(screen.getByTestId('invite-status-filter')).getByRole('combobox'));
    fireEvent.click(await screen.findByTitle('已撤销'));

    await waitFor(() =>
      expect(api.listInvitations).toHaveBeenLastCalledWith({
        tenantId: 7,
        status: 'revoked',
        limit: 10,
        offset: 0,
      })
    );
  });
});
