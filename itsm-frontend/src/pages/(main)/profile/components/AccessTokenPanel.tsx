import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Drawer, Empty, Form, Input, Modal, Popconfirm, Radio, Space, Table, Tag, Tooltip, Typography, message } from 'antd';
import { Copy, KeyRound, History, RefreshCw, ShieldOff } from 'lucide-react';
import {
  PersonalAccessTokenApi,
  type PersonalAccessToken,
  type CreateTokenResponse,
  type UsagePage,
} from '@/lib/api/personal-access-token-api';
import { useI18n } from '@/lib/i18n';
import { useClipboard } from '@/lib/component-utils';

const { Text, Paragraph } = Typography;

/** 状态标签。 */
function StatusTag({ status, t }: { status: PersonalAccessToken['status']; t: (k: string) => string }) {
  const map: Record<PersonalAccessToken['status'], { color: string; label: string }> = {
    active: { color: 'green', label: t('profile.accessTokens.status.active') },
    revoked: { color: 'default', label: t('profile.accessTokens.status.revoked') },
    expired: { color: 'orange', label: t('profile.accessTokens.status.expired') },
  };
  const meta = map[status] ?? { color: 'default', label: status };
  return <Tag color={meta.color}>{meta.label}</Tag>;
}

const DAY_OPTIONS = [7, 30, 90, 180, 365];

export interface AccessTokenPanelProps {
  /** 依赖注入（测试用）；缺省 PersonalAccessTokenApi。 */
  api?: Pick<
    typeof PersonalAccessTokenApi,
    'listTokens' | 'createToken' | 'rotateToken' | 'revokeToken' | 'revokeAllTokens' | 'listUsages'
  >;
}

/**
 * 个人中心「安全设置」→ 访问令牌面板。
 *
 * 安全约束（T9）：
 *   - 明文仅存在于创建/轮换弹窗的局部 state，关闭即清除；
 *     禁止写入 React Query 缓存 / store / localStorage（AC-41）。
 *   - Pattern A：requestIdRef 守卫防 stale response；Pattern C：deps 纪律；Pattern F：文案 ref。
 */
export function AccessTokenPanel({ api = PersonalAccessTokenApi }: AccessTokenPanelProps) {
  const { t } = useI18n();
  const tRef = useRef(t);
  tRef.current = t;
  const { copied, copyToClipboard } = useClipboard();

  const [tokens, setTokens] = useState<PersonalAccessToken[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [createLoading, setCreateLoading] = useState(false);
  const [reveal, setReveal] = useState<CreateTokenResponse | null>(null);
  const [revealSaved, setRevealSaved] = useState(false);
  const [usageOpen, setUsageOpen] = useState(false);
  const [usage, setUsage] = useState<UsagePage | null>(null);
  const [usageToken, setUsageToken] = useState<PersonalAccessToken | null>(null);
  const [form] = Form.useForm();

  // Pattern A：requestId 守卫
  const requestIdRef = useRef(0);
  const load = useCallback(async () => {
    const reqId = ++requestIdRef.current;
    setLoading(true);
    try {
      const list = await api.listTokens();
      if (reqId === requestIdRef.current) {
        setTokens(list);
      }
    } catch {
      if (reqId === requestIdRef.current) {
        message.error(tRef.current('profile.accessTokens.loadFailed'));
      }
    } finally {
      if (reqId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const closeReveal = useCallback(() => {
    // 明文生命周期：关闭即从 state 清除（AC-41）
    setReveal(null);
    setRevealSaved(false);
    form.resetFields();
  }, [form]);

  const handleCreate = useCallback(async () => {
    const values = await form.validateFields();
    setCreateLoading(true);
    try {
      const res = await api.createToken({
        name: values.name,
        description: values.description,
        expiresInDays: values.expiresInDays,
      });
      setCreateOpen(false);
      setReveal(res);
      setRevealSaved(false);
      void load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : tRef.current('profile.accessTokens.createFailed'));
    } finally {
      setCreateLoading(false);
    }
  }, [api, form, load]);

  const handleRotate = useCallback(async (record: PersonalAccessToken) => {
    try {
      const res = await api.rotateToken(record.id, 0);
      setReveal(res);
      setRevealSaved(false);
      void load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : tRef.current('profile.accessTokens.rotateFailed'));
    }
  }, [api, load]);

  const handleRevoke = useCallback(async (record: PersonalAccessToken) => {
    try {
      await api.revokeToken(record.id);
      message.success(tRef.current('profile.accessTokens.revokeSuccess'));
      void load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : tRef.current('profile.accessTokens.revokeFailed'));
    }
  }, [api, load]);

  const handleRevokeAll = useCallback(async () => {
    try {
      const res = await api.revokeAllTokens();
      message.success(`${tRef.current('profile.accessTokens.revokeAllSuccess')} (${res.revokedCount})`);
      void load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : tRef.current('profile.accessTokens.revokeFailed'));
    }
  }, [api, load]);

  const handleShowUsage = useCallback(async (record: PersonalAccessToken) => {
    setUsageToken(record);
    setUsageOpen(true);
    setUsage(null);
    try {
      const page = await api.listUsages(record.id, { page: 1, pageSize: 20 });
      setUsage(page);
    } catch {
      message.error(tRef.current('profile.accessTokens.loadFailed'));
    }
  }, [api]);

  const hasActive = tokens.some((tk) => tk.status === 'active');

  const columns = [
    { title: t('profile.accessTokens.col.name'), dataIndex: 'name', key: 'name', ellipsis: true },
    {
      title: t('profile.accessTokens.col.token'),
      dataIndex: 'maskedToken',
      key: 'token',
      render: (v: string) => <Text code copyable={false}>{v}</Text>,
    },
    { title: t('profile.accessTokens.col.status'), dataIndex: 'status', key: 'status', render: (v: PersonalAccessToken['status']) => <StatusTag status={v} t={t} /> },
    {
      title: t('profile.accessTokens.col.lastUsed'),
      dataIndex: 'lastUsedAt',
      key: 'lastUsed',
      render: (v?: string, record?: PersonalAccessToken) =>
        v ? (
          <Tooltip title={record?.lastUsedIp ? `IP: ${record.lastUsedIp}` : undefined}>
            <span>{new Date(v).toLocaleString()}</span>
          </Tooltip>
        ) : (
          <Text type="secondary">{t('profile.accessTokens.neverUsed')}</Text>
        ),
    },
    {
      title: t('profile.accessTokens.col.expiresAt'),
      dataIndex: 'expiresAt',
      key: 'expiresAt',
      render: (v: string) => {
        const days = Math.ceil((new Date(v).getTime() - Date.now()) / 86400000);
        return (
          <span>
            {new Date(v).toLocaleDateString()}
            {days > 0 && days <= 14 && <Tag color="orange" style={{ marginLeft: 8 }}>{`${days}d`}</Tag>}
          </span>
        );
      },
    },
    {
      title: t('profile.accessTokens.col.actions'),
      key: 'actions',
      render: (_: unknown, record: PersonalAccessToken) => (
        <Space size={0}>
          <Button type="link" size="small" icon={<History size={14} />} onClick={() => void handleShowUsage(record)} data-testid={`pat-usage-${record.id}`}>
            {t('profile.accessTokens.usage')}
          </Button>
          {record.status === 'active' && (
            <>
              <Popconfirm
                title={t('profile.accessTokens.rotateTitle')}
                description={t('profile.accessTokens.rotateDesc')}
                onConfirm={() => void handleRotate(record)}
              >
                <Button type="link" size="small" icon={<RefreshCw size={14} />} data-testid={`pat-rotate-${record.id}`}>
                  {t('profile.accessTokens.rotate')}
                </Button>
              </Popconfirm>
              <Popconfirm
                title={t('profile.accessTokens.revokeTitle')}
                description={t('profile.accessTokens.revokeDesc')}
                okText={t('profile.accessTokens.revokeConfirm')}
                okButtonProps={{ danger: true }}
                onConfirm={() => void handleRevoke(record)}
              >
                <Button type="link" danger size="small" icon={<ShieldOff size={14} />} data-testid={`pat-revoke-${record.id}`}>
                  {t('profile.accessTokens.revoke')}
                </Button>
              </Popconfirm>
            </>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <div>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>{t('profile.accessTokens.title')}</div>
          <div style={{ color: 'rgba(0,0,0,0.45)', fontSize: 13 }}>{t('profile.accessTokens.subtitle')}</div>
        </div>
        <Space>
          {hasActive && (
            <Popconfirm title={t('profile.accessTokens.revokeAllTitle')} description={t('profile.accessTokens.revokeAllDesc')} okButtonProps={{ danger: true }} onConfirm={() => void handleRevokeAll()}>
              <Button danger data-testid="pat-revoke-all">{t('profile.accessTokens.revokeAll')}</Button>
            </Popconfirm>
          )}
          <Button type="primary" icon={<KeyRound size={14} />} onClick={() => setCreateOpen(true)} data-testid="pat-create">
            {t('profile.accessTokens.create')}
          </Button>
        </Space>
      </div>

      <Table
        rowKey="id"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={tokens}
        pagination={false}
        locale={{
          emptyText: <Empty description={t('profile.accessTokens.empty')} image={Empty.PRESENTED_IMAGE_SIMPLE} />,
        }}
      />

      {/* 创建弹窗 */}
      <Modal
        title={t('profile.accessTokens.createTitle')}
        open={createOpen}
        onOk={() => void handleCreate()}
        onCancel={() => setCreateOpen(false)}
        confirmLoading={createLoading}
        okText={t('profile.accessTokens.create')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={{ expiresInDays: 90 }}>
          <Form.Item name="name" label={t('profile.accessTokens.form.name')} rules={[{ required: true, max: 64 }]}>
            <Input data-testid="pat-name-input" maxLength={64} placeholder={t('profile.accessTokens.form.namePlaceholder')} />
          </Form.Item>
          <Form.Item name="description" label={t('profile.accessTokens.form.description')}>
            <Input maxLength={255} />
          </Form.Item>
          <Form.Item name="expiresInDays" label={t('profile.accessTokens.form.expiry')} rules={[{ required: true }]}>
            <Radio.Group>
              {DAY_OPTIONS.map((d) => (
                <Radio key={d} value={d}>{`${d} ${t('profile.accessTokens.form.days')}`}</Radio>
              ))}
            </Radio.Group>
          </Form.Item>
          <Alert type="warning" showIcon message={t('profile.accessTokens.form.warn')} />
        </Form>
      </Modal>

      {/* 一次性明文展示（关闭即清除 state） */}
      <Modal
        title={t('profile.accessTokens.revealTitle')}
        open={reveal !== null}
        onCancel={revealSaved ? closeReveal : undefined}
        closable={revealSaved}
        keyboard={false}
        maskClosable={false}
        destroyOnClose
        footer={
          <Button type="primary" disabled={!revealSaved} onClick={closeReveal} data-testid="pat-reveal-done">
            {t('profile.accessTokens.revealDone')}
          </Button>
        }
      >
        {reveal && (
          <>
            <Alert
              type="warning"
              showIcon
              message={t('profile.accessTokens.revealOnce')}
              description={t('profile.accessTokens.revealOnceDesc')}
              style={{ marginBottom: 12 }}
            />
            <Paragraph code copyable={false} data-testid="pat-plaintext" style={{ wordBreak: 'break-all' }}>
              {reveal.plaintextToken}
            </Paragraph>
            <Space>
              <Button
                icon={<Copy size={14} />}
                onClick={() => void copyToClipboard(reveal.plaintextToken)}
                data-testid="pat-copy"
              >
                {copied ? t('profile.accessTokens.copied') : t('profile.accessTokens.copy')}
              </Button>
            </Space>
            <Paragraph type="secondary" style={{ marginTop: 12, fontSize: 12 }}>
              {t('profile.accessTokens.usageHint')}
            </Paragraph>
            <Paragraph code style={{ marginTop: 4, fontSize: 12, wordBreak: 'break-all' }}>
              {`curl -H "Authorization: Bearer ${reveal.plaintextToken}" ...`}
            </Paragraph>
            <label style={{ display: 'block', marginTop: 12, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={revealSaved}
                onChange={(e) => setRevealSaved(e.target.checked)}
                data-testid="pat-saved-check"
              />
              <span style={{ marginLeft: 8 }}>{t('profile.accessTokens.savedCheck')}</span>
            </label>
          </>
        )}
      </Modal>

      {/* 使用历史抽屉 */}
      <Drawer
        title={`${t('profile.accessTokens.usageTitle')}${usageToken ? ` - ${usageToken.name}` : ''}`}
        open={usageOpen}
        onClose={() => setUsageOpen(false)}
        width={640}
      >
        {usageToken && usage && (
          <>
            <Space direction="vertical" size={4} style={{ marginBottom: 12 }}>
              <Text>{`${t('profile.accessTokens.summary.useCount')}: ${usage.summary.useCount}`}</Text>
              <Text>
                {`${t('profile.accessTokens.summary.lastUsed')}: `}
                {usage.summary.lastUsedAt ? new Date(usage.summary.lastUsedAt).toLocaleString() : t('profile.accessTokens.neverUsed')}
                {usage.summary.lastUsedIp ? ` (${usage.summary.lastUsedIp})` : ''}
              </Text>
            </Space>
            <Table
              rowKey="id"
              size="small"
              dataSource={usage.items}
              pagination={{ total: usage.total, pageSize: usage.pageSize, showTotal: (total) => `${total}` }}
              columns={[
                { title: t('profile.accessTokens.usageCol.at'), dataIndex: 'at', key: 'at', render: (v: string) => new Date(v).toLocaleString(), width: 160 },
                { title: t('profile.accessTokens.usageCol.action'), dataIndex: 'action', key: 'action', width: 140 },
                { title: t('profile.accessTokens.usageCol.path'), dataIndex: 'path', key: 'path', ellipsis: true },
                { title: t('profile.accessTokens.usageCol.method'), dataIndex: 'method', key: 'method', width: 80 },
                { title: t('profile.accessTokens.usageCol.status'), dataIndex: 'statusCode', key: 'statusCode', width: 80 },
                { title: 'IP', dataIndex: 'ip', key: 'ip', width: 120 },
              ]}
              locale={{ emptyText: <Empty description={t('profile.accessTokens.usageEmpty')} image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
            />
          </>
        )}
        {usageToken && !usage && <Text type="secondary">...</Text>}
      </Drawer>
    </div>
  );
}
