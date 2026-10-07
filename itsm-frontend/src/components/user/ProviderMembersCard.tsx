/**
 * Q2=B / MSW-2：客户侧成员列表的「服务方成员」只读区块。
 *
 * - 数据来自 `GET /api/v1/users` 的 `providerMembers`（account_kind=provider 的存活映射）；
 * - 只读展示（用户名/姓名/邮箱/服务方角色/状态 + 「服务方」标签），不提供任何管理动作
 *   （客户侧不可管理服务方成员；撤销面在平台治理通道，见 MSW-3）；
 * - provider 租户无此区块（后端不返回）。
 */
import React from 'react';
import { Card, Space, Table, Tag, Typography } from 'antd';
import type { User } from '@/lib/api/user-api';

const { Text } = Typography;

export interface ProviderMembersCardProps {
  members: User[];
}

export const ProviderMembersCard: React.FC<ProviderMembersCardProps> = ({ members }) => {
  if (!members || members.length === 0) return null;

  return (
    <Card
      size="small"
      style={{ marginTop: 16 }}
      title="服务方成员（只读）"
      extra={
        <Text type="secondary" style={{ fontSize: 12 }}>
          由服务商分配/邀请产生，客户侧不可管理
        </Text>
      }
      data-testid="provider-members-card"
    >
      <Table<User>
        rowKey="id"
        size="small"
        dataSource={members}
        pagination={false}
        data-testid="provider-members-table"
        columns={[
          {
            title: '用户名',
            dataIndex: 'username',
            render: (value: string, row: User) => (
              <Space size={6}>
                <span>{value}</span>
                <Tag color="blue" data-testid={`provider-member-tag-${row.id}`}>
                  服务方
                </Tag>
              </Space>
            ),
          },
          { title: '姓名', dataIndex: 'name' },
          { title: '邮箱', dataIndex: 'email' },
          {
            title: '服务方角色',
            dataIndex: 'role',
            render: (value: string) => value || '服务方成员',
          },
          {
            title: '状态',
            dataIndex: 'active',
            render: (active: boolean) =>
              active ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>,
          },
        ]}
      />
    </Card>
  );
};

export default ProviderMembersCard;
