/**
 * 邀请管理（IP-P1-4c 管理侧）。
 *
 * 补齐后端 IP-P1-4b 已上线、前端此前只有“接受侧”落地页的缺口：
 * - 列表：`GET /api/v1/users/invitations`（status 过滤 + 分页；不含 token）；
 * - 创建：`POST /api/v1/users/invitations` → 回显 `inviteUrl`（SMTP 未配置时线下传递）并可复制；
 * - 撤销：`POST /api/v1/users/invitations/:id/revoke`（仅 pending，Popconfirm 二次确认）。
 *
 * 目标租户沿用“当前会话租户”（与页面右上角上下文一致）；跨客户邀请仍走深度切换后进入。
 */
import React, { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Copy, MailPlus, RefreshCw, Undo2 } from 'lucide-react';
import dayjs from 'dayjs';

import {
  InvitationAPI,
  type CreateInvitationResponse,
  type InvitationListItem,
  type InvitationStatus,
} from '@/lib/api/invitation-api';

const { Text } = Typography;

export interface InvitationRoleOption {
  id: number;
  name: string;
  code?: string;
}

export interface InvitationManagementModalProps {
  open: boolean;
  onClose: () => void;
  /** 目标租户；省略时后端取当前会话租户 */
  tenantId?: number;
  /** 创建邀请可选角色（复用用户页已加载的 RBAC 角色） */
  roles?: InvitationRoleOption[];
  /** 注入点（测试用）；默认走 InvitationAPI */
  api?: Pick<typeof InvitationAPI, 'listInvitations' | 'createInvitation' | 'revokeInvitation'>;
}

const STATUS_META: Record<InvitationStatus, { label: string; color: string }> = {
  pending: { label: '待接受', color: 'processing' },
  accepted: { label: '已接受', color: 'success' },
  revoked: { label: '已撤销', color: 'default' },
  expired: { label: '已过期', color: 'warning' },
};

const STATUS_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待接受' },
  { value: 'accepted', label: '已接受' },
  { value: 'revoked', label: '已撤销' },
  { value: 'expired', label: '已过期' },
];

const MSG_ROLE_OPTIONS = [
  { value: 'provider_admin', label: 'provider_admin' },
  { value: 'provider_agent', label: 'provider_agent' },
];

const PAGE_SIZE = 10;

const fmt = (value?: string) => (value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—');

const InvitationManagementModal: React.FC<InvitationManagementModalProps> = ({
  open,
  onClose,
  tenantId,
  roles = [],
  api = InvitationAPI,
}) => {
  const { message } = App.useApp();
  const [status, setStatus] = useState<InvitationStatus | ''>('');
  const [page, setPage] = useState(1);
  const [reloadKey, setReloadKey] = useState(0);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState<InvitationListItem[]>([]);
  const [total, setTotal] = useState(0);

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreateInvitationResponse | null>(null);
  const [form] = Form.useForm<{ email: string; roleId: number; mspRole?: string }>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listInvitations({
        tenantId,
        status: status || undefined,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      });
      setItems(res.invitations || []);
      setTotal(res.total || 0);
    } catch (err) {
      message.error(err instanceof Error ? err.message : '邀请列表加载失败');
    } finally {
      setLoading(false);
    }
  }, [api, message, page, status, tenantId]);

  useEffect(() => {
    if (open) void load();
  }, [open, load, reloadKey]);

  const refresh = () => {
    if (page !== 1) {
      setPage(1);
    } else {
      setReloadKey(k => k + 1);
    }
  };

  const handleCreate = async (values: { email: string; roleId: number; mspRole?: string }) => {
    setCreating(true);
    try {
      const res = await api.createInvitation({
        tenantId,
        email: values.email,
        roleId: values.roleId,
        mspRole: values.mspRole || undefined,
      });
      setCreated(res);
      setCreateOpen(false);
      form.resetFields();
      message.success(res.emailSent ? '邀请邮件已发送' : '邀请已创建');
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : '创建邀请失败');
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (record: InvitationListItem) => {
    try {
      await api.revokeInvitation(record.id);
      message.success('邀请已撤销');
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : '撤销邀请失败');
    }
  };

  const handleCopy = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url);
      message.success('邀请链接已复制');
    } catch {
      message.info('复制失败，请手动复制链接');
    }
  };

  const columns: ColumnsType<InvitationListItem> = [
    {
      title: '被邀请邮箱',
      dataIndex: 'email',
      key: 'email',
      render: value => <Text strong>{value}</Text>,
    },
    {
      title: '角色',
      key: 'role',
      render: (_, record) => (
        <Space size={4} wrap>
          <Tag color="blue">{record.roleName || record.roleCode || `#${record.roleId}`}</Tag>
          {record.mspRole ? <Tag color="purple">{record.mspRole}</Tag> : null}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: InvitationStatus) => (
        <Tag color={STATUS_META[value]?.color || 'default'}>{STATUS_META[value]?.label || value}</Tag>
      ),
    },
    {
      title: '邀请人',
      key: 'inviter',
      width: 130,
      render: (_, record) => record.inviterName || (record.invitedBy ? `#${record.invitedBy}` : '—'),
    },
    {
      title: '过期时间',
      dataIndex: 'expiresAt',
      key: 'expiresAt',
      width: 150,
      render: fmt,
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 150,
      render: fmt,
    },
    {
      title: '操作',
      key: 'action',
      width: 100,
      render: (_, record) =>
        record.status === 'pending' ? (
          <Popconfirm
            title="撤销该邀请？"
            description="撤销后链接立即失效，且不可恢复。"
            okText="确定撤销"
            cancelText="取消"
            onConfirm={() => handleRevoke(record)}
          >
            <Button type="link" danger size="small" icon={<Undo2 size={14} />} data-testid={`invite-revoke-${record.id}`}>
              撤销
            </Button>
          </Popconfirm>
        ) : (
          <Text type="secondary">—</Text>
        ),
    },
  ];

  return (
    <>
      <Modal
        title="邀请管理"
        open={open}
        onCancel={onClose}
        width={920}
        footer={null}
        destroyOnClose
      >
        <div className="mb-3 flex items-center justify-between" data-testid="invitation-toolbar">
          <Space>
            <span data-testid="invite-status-filter">
              <Select
                value={status}
                options={STATUS_OPTIONS}
                style={{ width: 140 }}
                onChange={value => {
                  setStatus(value as InvitationStatus | '');
                  setPage(1);
                }}
              />
            </span>
            <Button icon={<RefreshCw size={14} />} onClick={refresh} loading={loading}>
              刷新
            </Button>
          </Space>
          <Button
            type="primary"
            icon={<MailPlus size={16} />}
            onClick={() => setCreateOpen(true)}
            data-testid="invite-create-button"
          >
            创建邀请
          </Button>
        </div>

        <Table<InvitationListItem>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={items}
          loading={loading}
          locale={{ emptyText: <Empty description="暂无邀请记录" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total,
            showSizeChanger: false,
            showTotal: t => `共 ${t} 条`,
            onChange: next => setPage(next),
          }}
        />
      </Modal>

      <Modal
        title="创建邀请"
        open={createOpen}
        onCancel={() => {
          setCreateOpen(false);
          form.resetFields();
        }}
        footer={null}
        width={520}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={handleCreate}>
          <Form.Item
            name="email"
            label="被邀请邮箱"
            rules={[
              { required: true, message: '请输入被邀请邮箱' },
              { type: 'email', message: '邮箱格式不正确' },
            ]}
          >
            <Input placeholder="new.user@example.com" data-testid="invite-email-input" />
          </Form.Item>
          {/* 注意：wrapper 必须在 Form.Item 外层——Form.Item 只会把 value/onChange 注入直接子元素。 */}
          <span data-testid="invite-role-select">
            <Form.Item name="roleId" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
              <Select
                placeholder={roles.length ? '选择租户内角色' : '当前租户暂无可用角色，请先创建角色'}
                disabled={!roles.length}
                options={roles.map(r => ({
                  value: r.id,
                  label: r.code ? `${r.name}（${r.code}）` : r.name,
                }))}
              />
            </Form.Item>
          </span>
          <Form.Item
            name="mspRole"
            label="服务方角色（可选）"
            tooltip="仅服务方租户 / 平台通道使用；白名单 provider_admin、provider_agent"
          >
            <Select allowClear placeholder="不设置" options={MSG_ROLE_OPTIONS} data-testid="invite-msp-role-select" />
          </Form.Item>
          <Form.Item className="!mb-0">
            <div className="flex justify-end gap-2">
              <Button
                onClick={() => {
                  setCreateOpen(false);
                  form.resetFields();
                }}
              >
                取消
              </Button>
              <Button
                type="primary"
                loading={creating}
                onClick={() => form.submit()}
                data-testid="invite-submit"
              >
                创建
              </Button>
            </div>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="邀请已创建"
        open={!!created}
        onCancel={() => setCreated(null)}
        width={640}
        footer={[
          <Button key="done" type="primary" onClick={() => setCreated(null)}>
            完成
          </Button>,
        ]}
      >
        {created ? (
          <>
            <Alert
              showIcon
              type={created.emailSent ? 'success' : 'info'}
              message={
                created.emailSent
                  ? `邀请邮件已发送至 ${created.email}`
                  : 'SMTP 未配置：请复制下方链接，线下发送给被邀请人'
              }
            />
            <div className="mt-3 flex gap-2">
              <Input readOnly value={created.inviteUrl} data-testid="invite-url" />
              <Button icon={<Copy size={14} />} onClick={() => handleCopy(created.inviteUrl)}>
                复制链接
              </Button>
            </div>
            <Text type="secondary" className="mt-2 block">
              链接有效期至 {fmt(created.expiresAt)}；一次性使用，重发或撤销后即失效。
            </Text>
          </>
        ) : null}
      </Modal>
    </>
  );
};

export default InvitationManagementModal;
