/**
 * ProvisionUserModal 测试（IP-P0-5 建号通道的 UI 入口）。
 *
 * 覆盖：
 * - 平台模式：目标提示 + 提交四字段载荷（customerId=undefined）+ 成功后关闭；
 * - 必填校验：空表提交不发请求；
 * - MSP 模式：单客户自动选中，提交携带 customerId；
 * - 失败路径：展示后端错误且不关闭弹窗。
 */
import React from 'react';
import { configure, fireEvent, render, screen, waitFor } from '@/lib/test-utils';
import ProvisionUserModal from '../ProvisionUserModal';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

const fillBase = (overrides: Partial<Record<'username' | 'name' | 'email' | 'password', string>> = {}) => {
  fireEvent.change(screen.getByTestId('provision-username-input'), {
    target: { value: overrides.username ?? 'new.user' },
  });
  fireEvent.change(screen.getByTestId('provision-name-input'), {
    target: { value: overrides.name ?? '张三' },
  });
  fireEvent.change(screen.getByTestId('provision-email-input'), {
    target: { value: overrides.email ?? 'new.user@example.com' },
  });
  fireEvent.change(screen.getByTestId('provision-password-input'), {
    target: { value: overrides.password ?? 'Init@12345' },
  });
};

describe('ProvisionUserModal', () => {
  it('平台模式：提交四字段载荷并关闭弹窗', async () => {
    const submit = jest.fn().mockResolvedValue({ id: 9, username: 'new.user' });
    const onClose = jest.fn();

    render(
      <ProvisionUserModal
        open
        title="平台建号"
        targetLabel="MSP001（MSP Provider）"
        onClose={onClose}
        submit={submit}
      />
    );

    expect(screen.getByText('目标：MSP001（MSP Provider）')).toBeInTheDocument();
    fillBase();
    // antd v6 会在两个中文字符间插入空格（可访问名 "建 号"）。
    fireEvent.click(screen.getByRole('button', { name: /建\s*号/ }));

    await waitFor(() =>
      expect(submit).toHaveBeenCalledWith(
        {
          username: 'new.user',
          name: '张三',
          email: 'new.user@example.com',
          password: 'Init@12345',
        },
        undefined
      )
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('必填校验：空表提交不发请求', async () => {
    const submit = jest.fn();

    render(<ProvisionUserModal open title="平台建号" onClose={jest.fn()} submit={submit} />);
    fireEvent.click(screen.getByRole('button', { name: /建\s*号/ }));

    expect(await screen.findByText('请输入用户名')).toBeInTheDocument();
    expect(screen.getByText('请输入姓名')).toBeInTheDocument();
    expect(screen.getByText('请输入邮箱')).toBeInTheDocument();
    expect(screen.getByText('请输入初始密码')).toBeInTheDocument();
    expect(submit).not.toHaveBeenCalled();
  });

  it('MSP 模式：单客户自动选中并随载荷提交 customerId', async () => {
    const submit = jest.fn().mockResolvedValue({ id: 10, username: 'cust.user' });
    const onClose = jest.fn();

    render(
      <ProvisionUserModal
        open
        title="为客户建号"
        customerOptions={[{ id: 42, code: 'MSPCUSTA', name: 'Customer A' }]}
        onClose={onClose}
        submit={submit}
      />
    );

    fillBase({ username: 'cust.user' });
    fireEvent.click(screen.getByRole('button', { name: /建\s*号/ }));

    await waitFor(() =>
      expect(submit).toHaveBeenCalledWith(expect.objectContaining({ username: 'cust.user' }), 42)
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('失败路径：展示后端错误且不关闭弹窗', async () => {
    const submit = jest.fn().mockRejectedValue(new Error('该租户已存在同名用户'));
    const onClose = jest.fn();

    render(<ProvisionUserModal open title="平台建号" onClose={onClose} submit={submit} />);
    fillBase();
    fireEvent.click(screen.getByRole('button', { name: /建\s*号/ }));

    expect(await screen.findByTestId('provision-user-error')).toHaveTextContent(
      '该租户已存在同名用户'
    );
    expect(onClose).not.toHaveBeenCalled();
  });
});
