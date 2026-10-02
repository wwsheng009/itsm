/**
 * 邀请落地页测试（IP-P1-4c）。
 *
 * 覆盖：
 * - 路径式 token（后端 inviteUrl 形态 `/invite/<token>`）回显 + 设置密码 + 激活成功引导登录；
 * - 查询式 token 兼容（`/invite?token=`）；
 * - 缺 token / 状态非 pending（accepted、revoked）→ 不可用提示；
 * - 两次密码不一致 → 表单校验拦截，不发起 accept。
 */
import React from 'react';
import { fireEvent } from '@testing-library/react';
import { render, screen, waitFor } from '@/lib/test-utils';

const mockNavigate = jest.fn();
let mockRouteParams: Record<string, string | undefined> = {};
const mockSearchParams = new URLSearchParams();

jest.mock('react-router', () => {
  const React = require('react');
  return {
    Link: (props: { to: string; children: React.ReactNode }) =>
      React.createElement('a', { href: props.to }, props.children),
    useNavigate: () => mockNavigate,
    useParams: () => mockRouteParams,
    useSearchParams: () => [mockSearchParams, jest.fn()],
  };
});

jest.mock('@/lib/services/auth-service', () => ({
  __esModule: true,
  default: {
    inspectInvitation: jest.fn(),
    acceptInvitation: jest.fn(),
  },
}));

import AuthService from '@/lib/services/auth-service';
import InviteLandingPage from '../index';

const pendingInfo = {
  status: 'pending',
  emailMasked: 'a***@example.com',
  tenantName: 'Acme',
  roleCode: 'agent',
  expiresAt: '2026-10-07T00:00:00Z',
  hasTargetUser: false,
};

function fillPasswordPair(password: string, confirm: string) {
  fireEvent.change(screen.getByLabelText('设置密码'), { target: { value: password } });
  fireEvent.change(screen.getByLabelText('确认密码'), { target: { value: confirm } });
}

describe('InviteLandingPage', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockRouteParams = {};
    Array.from(mockSearchParams.keys()).forEach(key => mockSearchParams.delete(key));
    (AuthService.inspectInvitation as jest.Mock).mockResolvedValue({ ...pendingInfo });
    (AuthService.acceptInvitation as jest.Mock).mockResolvedValue({ userId: 9, username: 'newbie' });
  });

  it('路径式 token：回显 → 设置密码 → 激活成功 → 立即登录', async () => {
    mockRouteParams = { token: 'tok-path-1' };

    render(<InviteLandingPage />);

    expect(await screen.findByText('加入 Acme')).toBeInTheDocument();
    expect(screen.getByText(/a\*\*\*@example\.com/)).toBeInTheDocument();
    expect(AuthService.inspectInvitation).toHaveBeenCalledWith('tok-path-1');

    fillPasswordPair('Str0ng!Pass', 'Str0ng!Pass');
    fireEvent.click(screen.getByRole('button', { name: '设置密码并激活账号' }));

    await waitFor(() =>
      expect(AuthService.acceptInvitation).toHaveBeenCalledWith({
        token: 'tok-path-1',
        password: 'Str0ng!Pass',
        name: undefined,
      })
    );
    expect(await screen.findByText('账号已激活')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '立即登录' }));
    expect(mockNavigate).toHaveBeenCalledWith('/login');
  });

  it('查询式 token 兼容：/invite?token=...', async () => {
    mockSearchParams.set('token', 'tok-query-1');

    render(<InviteLandingPage />);

    expect(await screen.findByText('加入 Acme')).toBeInTheDocument();
    expect(AuthService.inspectInvitation).toHaveBeenCalledWith('tok-query-1');
  });

  it('缺 token：提示不可用并可返回登录', async () => {
    render(<InviteLandingPage />);

    expect(await screen.findByText('邀请不可用')).toBeInTheDocument();
    expect(screen.getByText(/缺少 token/)).toBeInTheDocument();
    expect(AuthService.inspectInvitation).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: '返回登录' }));
    expect(mockNavigate).toHaveBeenCalledWith('/login');
  });

  it('状态非 pending（accepted）：展示"已被接受"提示', async () => {
    mockRouteParams = { token: 'tok-accepted' };
    (AuthService.inspectInvitation as jest.Mock).mockResolvedValue({
      ...pendingInfo,
      status: 'accepted',
    });

    render(<InviteLandingPage />);

    expect(await screen.findByText(/该邀请已被接受/)).toBeInTheDocument();
  });

  it('两次密码不一致：表单拦截且不发起 accept', async () => {
    mockRouteParams = { token: 'tok-path-2' };

    render(<InviteLandingPage />);
    await screen.findByText('加入 Acme');

    fillPasswordPair('Str0ng!Pass', 'Different!Pass');
    fireEvent.click(screen.getByRole('button', { name: '设置密码并激活账号' }));

    // 全量并跑 + 覆盖率插桩下异步校验信息可能 >1s 才渲染，显式放宽等待。
    expect(
      await screen.findByText('两次输入的密码不一致', {}, { timeout: 15000 })
    ).toBeInTheDocument();
    expect(AuthService.acceptInvitation).not.toHaveBeenCalled();
  });
});
