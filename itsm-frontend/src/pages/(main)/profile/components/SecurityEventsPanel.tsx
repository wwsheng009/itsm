import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Empty, Table, Tag, Typography, message } from 'antd';
import { PersonalAccessTokenApi, type EventPage } from '@/lib/api/personal-access-token-api';
import { useI18n } from '@/lib/i18n';

const { Text } = Typography;

/** 来源标签（复用 msp-audit 的语义：login/switch/pat/...）。 */
function SourceTag({ source, t }: { source: string; t: (k: string) => string }) {
  const map: Record<string, { color: string; key: string }> = {
    login: { color: 'blue', key: 'login' },
    switch: { color: 'geekblue', key: 'switch' },
    pat: { color: 'purple', key: 'pat' },
    header: { color: 'cyan', key: 'header' },
    workbench: { color: 'gold', key: 'workbench' },
    legacy: { color: 'default', key: 'legacy' },
  };
  const meta = map[source] ?? { color: 'default', key: 'legacy' };
  return <Tag color={meta.color}>{t(`profile.securityEvents.source.${meta.key}`)}</Tag>;
}

/** Action 中文标签（复用既有语义映射的最小子集）。 */
function actionLabel(action: string, t: (k: string) => string): string {
  const map: Record<string, string> = {
    'auth.login': t('profile.securityEvents.action.login'),
    'auth.change_password': t('profile.securityEvents.action.changePassword'),
    'tenant.switch': t('profile.securityEvents.action.switchTenant'),
    'auth.token_created': t('profile.securityEvents.action.tokenCreated'),
    'auth.token_rotated': t('profile.securityEvents.action.tokenRotated'),
    'auth.token_revoked': t('profile.securityEvents.action.tokenRevoked'),
    'auth.token_revoked_all': t('profile.securityEvents.action.tokenRevokedAll'),
  };
  return map[action] ?? action;
}

export interface SecurityEventsPanelProps {
  /** 依赖注入（测试用）。 */
  api?: Pick<typeof PersonalAccessTokenApi, 'listSecurityEvents'>;
}

/**
 * 个人中心「安全设置」→ 登录与令牌使用历史（合并本人 resource=auth 事件）。
 * 替换 profile/index.tsx 原「登录历史」disabled 占位。
 * Pattern A/C/F 遵循（requestIdRef 守卫；文案经 ref）。
 */
export function SecurityEventsPanel({ api = PersonalAccessTokenApi }: SecurityEventsPanelProps) {
  const { t } = useI18n();
  const tRef = useRef(t);
  tRef.current = t;

  const [page, setPage] = useState<EventPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [current, setCurrent] = useState(1);
  const requestIdRef = useRef(0);

  const load = useCallback(
    async (pageNo: number) => {
      const reqId = ++requestIdRef.current;
      setLoading(true);
      try {
        const res = await api.listSecurityEvents({ page: pageNo, pageSize: 10 });
        if (reqId === requestIdRef.current) {
          setPage(res);
          setCurrent(pageNo);
        }
      } catch {
        if (reqId === requestIdRef.current) {
          message.error(tRef.current('profile.securityEvents.loadFailed'));
        }
      } finally {
        if (reqId === requestIdRef.current) {
          setLoading(false);
        }
      }
    },
    [api],
  );

  useEffect(() => {
    void load(1);
  }, [load]);

  return (
    <Table
      rowKey="id"
      size="small"
      loading={loading}
      dataSource={page?.items ?? []}
      pagination={{
        total: page?.total ?? 0,
        pageSize: 10,
        current,
        onChange: (p) => void load(p),
        showTotal: (total) => `${total}`,
      }}
      columns={[
        { title: t('profile.securityEvents.col.at'), dataIndex: 'at', key: 'at', width: 160, render: (v: string) => new Date(v).toLocaleString() },
        { title: t('profile.securityEvents.col.event'), dataIndex: 'action', key: 'action', render: (v: string) => actionLabel(v, t) },
        { title: t('profile.securityEvents.col.source'), dataIndex: 'source', key: 'source', width: 100, render: (v: string) => <SourceTag source={v} t={t} /> },
        { title: t('profile.securityEvents.col.result'), dataIndex: 'statusCode', key: 'statusCode', width: 80, render: (v: number) => (v < 400 ? <Tag color="green">{v}</Tag> : <Tag color="red">{v}</Tag>) },
        { title: 'IP', dataIndex: 'ip', key: 'ip', width: 130 },
        {
          title: t('profile.securityEvents.col.token'),
          key: 'token',
          width: 160,
          render: (_: unknown, record: { source: string; tokenId?: number }) =>
            record.source === 'pat' && record.tokenId ? (
              <Text code>{`#${record.tokenId}`}</Text>
            ) : (
              <Text type="secondary">—</Text>
            ),
        },
      ]}
      locale={{ emptyText: <Empty description={t('profile.securityEvents.empty')} image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
    />
  );
}
