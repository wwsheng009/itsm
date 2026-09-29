import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Card, Empty, Input, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Copy, RefreshCw } from 'lucide-react';
import { PageContainer } from '@/components/common/PageContainer';
import { useI18n } from '@/lib/i18n/useI18n';
import { aiListToolCatalog, type ToolCatalogItem, type ToolCatalogParams } from '@/lib/api/ai-api';

const { Text } = Typography;

/** i18n 函数签名（与 useI18n 的 t 对齐）。 */
type TFunc = (key: string, params?: Record<string, string | number>) => string;

/** 风险分级（与后端 ToolCatalogItem.risk 枚举一致）。 */
const RISK_KEYS = ['read', 'plan', 'act_low', 'act_medium', 'act_high'] as const;
type RiskKey = (typeof RISK_KEYS)[number];

/** 风险 Tag 配色：只读/规划偏冷色，写操作随风险升级转暖色。 */
const RISK_COLORS: Record<RiskKey, string> = {
  read: 'green',
  plan: 'blue',
  act_low: 'gold',
  act_medium: 'orange',
  act_high: 'red',
};

/** 单次请求条数（后端契约 1..500，默认 200）。 */
const CATALOG_LIMIT = 200;

/** 搜索防抖窗口：服务端过滤，避免逐键请求。 */
const SEARCH_DEBOUNCE_MS = 300;

/**
 * 工具目录页（挂载 `/admin/tools`）。
 *
 * 统一查询内置工具与 MCP 外部工具：`GET /api/v1/agent/tools/catalog`（ai:read，
 * 后端按当前角色 RBAC 过滤）。筛选（q / source / readOnly / risk）全部走服务端，
 * 前端只负责防抖与展示。
 *
 * 竞态与卸载：每次请求分配自增 requestId，仅最新一次请求可写状态；卸载时使在途
 * 请求全部失效，避免竞态覆盖或卸载后 setState。
 */
const ToolsCatalogPage: React.FC = () => {
  const { t } = useI18n();
  const tt = t as TFunc;
  const { message } = App.useApp();

  const [items, setItems] = useState<ToolCatalogItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const [searchText, setSearchText] = useState('');
  const [query, setQuery] = useState('');
  const [source, setSource] = useState('');
  const [readOnly, setReadOnly] = useState('');
  const [risk, setRisk] = useState('');

  /** 竞态/卸载守卫：自增 id，只有最新一次请求可以写入状态。 */
  const requestSeqRef = useRef(0);

  useEffect(
    () => () => {
      // 卸载：使所有在途请求失效（其 setState 被守卫丢弃）。
      requestSeqRef.current += 1;
    },
    []
  );

  // 搜索防抖：停止输入 300ms 后才把关键字并入查询条件。
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchText.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [searchText]);

  const loadCatalog = useCallback(async () => {
    const requestId = (requestSeqRef.current += 1);
    const params: ToolCatalogParams = { limit: CATALOG_LIMIT };
    if (query) params.q = query;
    if (source) params.source = source as 'builtin' | 'mcp';
    if (readOnly) params.readOnly = readOnly === 'true';
    if (risk) params.risk = risk;

    setLoading(true);
    try {
      const res = await aiListToolCatalog(params);
      if (requestId !== requestSeqRef.current) return;
      setItems(Array.isArray(res?.items) ? res.items : []);
      setTotal(res?.total ?? 0);
    } catch {
      if (requestId !== requestSeqRef.current) return;
      setItems([]);
      setTotal(0);
      message.error(tt('toolsCatalog.loadFailed'));
    } finally {
      if (requestId === requestSeqRef.current) setLoading(false);
    }
  }, [message, query, readOnly, risk, source, tt]);

  useEffect(() => {
    void loadCatalog();
  }, [loadCatalog]);

  const copyName = useCallback(
    async (name: string) => {
      try {
        // jsdom/非安全上下文可能没有 clipboard：静默降级，不打断列表使用。
        if (!navigator.clipboard?.writeText) return;
        await navigator.clipboard.writeText(name);
        message.success(tt('toolsCatalog.copied'));
      } catch {
        // 剪贴板权限被拒：忽略（无专用失败文案）。
      }
    },
    [message, tt]
  );

  const riskLabel = useCallback(
    (value: string) => (RISK_KEYS.includes(value as RiskKey) ? tt(`botsAdmin.risk.${value}`) : value),
    [tt]
  );

  const sourceLabel = useCallback(
    (provider?: string) => (provider === 'mcp' ? tt('toolsCatalog.sourceMCP') : tt('toolsCatalog.sourceBuiltin')),
    [tt]
  );

  const columns: ColumnsType<ToolCatalogItem> = [
    {
      title: tt('toolsCatalog.columns.name'),
      dataIndex: 'name',
      key: 'name',
      render: (_, record) => (
        <Space orientation="vertical" size={0}>
          <Space size={4}>
            <Text strong>{record.name}</Text>
            {record.provider === 'mcp' ? <Tag color="geekblue">{tt('toolsCatalog.mcpTag')}</Tag> : null}
          </Space>
          {record.serverName ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {tt('toolsCatalog.server', { server: record.serverName })}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: tt('toolsCatalog.columns.source'),
      dataIndex: 'provider',
      key: 'source',
      width: 110,
      render: (value: string) => <Tag>{sourceLabel(value)}</Tag>,
    },
    {
      title: tt('toolsCatalog.columns.readOnly'),
      dataIndex: 'readOnly',
      key: 'readOnly',
      width: 90,
      render: (value: boolean) => (
        <Tag color={value ? 'green' : 'orange'}>{value ? tt('toolsCatalog.yes') : tt('toolsCatalog.no')}</Tag>
      ),
    },
    {
      title: tt('toolsCatalog.columns.risk'),
      dataIndex: 'risk',
      key: 'risk',
      width: 110,
      render: (value?: string) =>
        value ? (
          <Tag color={RISK_COLORS[value as RiskKey]}>{riskLabel(value)}</Tag>
        ) : (
          <Text type="secondary">{tt('toolsCatalog.riskUnknown')}</Text>
        ),
    },
    {
      title: tt('toolsCatalog.columns.category'),
      dataIndex: 'category',
      key: 'category',
      width: 130,
      render: (value?: string) => value || '-',
    },
    {
      title: tt('toolsCatalog.columns.description'),
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
      render: (value?: string) =>
        value ? (
          <Tooltip title={value}>
            <Text ellipsis style={{ display: 'inline-block', maxWidth: '100%' }}>
              {value}
            </Text>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      title: tt('toolsCatalog.columns.actions'),
      key: 'actions',
      width: 130,
      render: (_, record) => (
        <Button size="small" type="link" icon={<Copy size={14} />} onClick={() => void copyName(record.name)}>
          {tt('toolsCatalog.copy')}
        </Button>
      ),
    },
  ];

  return (
    <PageContainer
      header={{
        title: tt('toolsCatalog.title'),
        breadcrumb: {
          items: [
            { title: tt('toolsCatalog.breadcrumbHome') },
            { title: tt('toolsCatalog.breadcrumbCurrent') },
          ],
        },
      }}
    >
      <Card
        title={
          <Space orientation="vertical" size={0}>
            <Text strong>{tt('toolsCatalog.title')}</Text>
            <Text type="secondary" style={{ fontSize: 12, fontWeight: 'normal' }}>
              {tt('toolsCatalog.subtitle')}
            </Text>
          </Space>
        }
        extra={
          <Button icon={<RefreshCw size={14} />} loading={loading} onClick={() => void loadCatalog()}>
            {tt('toolsCatalog.refresh')}
          </Button>
        }
      >
        <Alert type="info" showIcon title={tt('toolsCatalog.rbacHint')} style={{ marginBottom: 16 }} />

        <Space wrap style={{ marginBottom: 12 }}>
          <Input.Search
            allowClear
            value={searchText}
            placeholder={tt('toolsCatalog.searchPlaceholder')}
            style={{ width: 280 }}
            onChange={event => setSearchText(event.target.value)}
            onSearch={value => setQuery(value.trim())}
          />
          <span data-testid="tool-catalog-source-filter">
            <Select
              value={source}
              style={{ width: 150 }}
              onChange={setSource}
              options={[
                { value: '', label: tt('toolsCatalog.sourceAll') },
                { value: 'builtin', label: tt('toolsCatalog.sourceBuiltin') },
                { value: 'mcp', label: tt('toolsCatalog.sourceMCP') },
              ]}
            />
          </span>
          <span data-testid="tool-catalog-readonly-filter">
            <Select
              value={readOnly}
              style={{ width: 150 }}
              onChange={setReadOnly}
              options={[
                { value: '', label: tt('toolsCatalog.readOnlyAll') },
                { value: 'true', label: tt('toolsCatalog.readOnlyYes') },
                { value: 'false', label: tt('toolsCatalog.readOnlyNo') },
              ]}
            />
          </span>
          <span data-testid="tool-catalog-risk-filter">
            <Select
              value={risk}
              style={{ width: 150 }}
              onChange={setRisk}
              options={[
                { value: '', label: tt('toolsCatalog.riskAll') },
                ...RISK_KEYS.map(value => ({ value, label: riskLabel(value) })),
              ]}
            />
          </span>
        </Space>

        <div style={{ marginBottom: 8 }}>
          <Text type="secondary">{tt('toolsCatalog.total', { total })}</Text>
        </div>

        <Table<ToolCatalogItem>
          rowKey="name"
          size="small"
          loading={loading}
          dataSource={items}
          columns={columns}
          pagination={false}
          locale={{ emptyText: <Empty description={tt('toolsCatalog.empty')} /> }}
        />
      </Card>
    </PageContainer>
  );
};

export default ToolsCatalogPage;
