/**
 * ProviderMembersCard 用例（Q2=B / MSW-2）：
 * - 空集合不渲染（provider 租户/无服务方成员时零占位）；
 * - 渲染只读行：服务方标签、角色、状态，且**不出现任何管理动作按钮**。
 */
import React from 'react';
import { render, screen, within } from '@/lib/test-utils';
import { ProviderMembersCard } from '../ProviderMembersCard';
import type { User } from '@/lib/api/user-api';

const makeMember = (overrides: Partial<User> = {}): User => ({
  id: 101,
  username: 'msp-staff',
  email: 'staff@provider.example',
  name: '服务工程师',
  department: '',
  phone: '',
  active: true,
  tenantId: 2,
  role: 'provider_agent',
  createdAt: '2026-10-07T00:00:00Z',
  updatedAt: '2026-10-07T00:00:00Z',
  accountKind: 'provider',
  readOnly: true,
  ...overrides,
});

describe('ProviderMembersCard', () => {
  it('空集合不渲染（零占位）', () => {
    render(<ProviderMembersCard members={[]} />);
    expect(screen.queryByTestId('provider-members-card')).not.toBeInTheDocument();
  });

  it('渲染只读服务方成员：标签/角色/状态，且无管理动作', () => {
    render(
      <ProviderMembersCard
        members={[makeMember(), makeMember({ id: 102, username: 'msp-backup', active: false })]}
      />
    );

    const card = screen.getByTestId('provider-members-card');
    expect(within(card).getByText('服务方成员（只读）')).toBeInTheDocument();
    expect(within(card).getByText('由服务商分配/邀请产生，客户侧不可管理')).toBeInTheDocument();
    expect(screen.getByTestId('provider-member-tag-101')).toHaveTextContent('服务方');
    expect(screen.getByTestId('provider-member-tag-102')).toHaveTextContent('服务方');
    expect(within(card).getByText('msp-staff')).toBeInTheDocument();
    expect(within(card).getAllByText('provider_agent').length).toBe(2);
    expect(within(card).getByText('启用')).toBeInTheDocument();
    expect(within(card).getByText('停用')).toBeInTheDocument();

    // 只读：区块内不得出现按钮/链接等管理动作。
    expect(within(card).queryAllByRole('button')).toHaveLength(0);
    expect(within(card).queryAllByRole('link')).toHaveLength(0);
  });
});
