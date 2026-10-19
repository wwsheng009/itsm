/**
 * H1-04 帮助覆盖率看板（管理页）。
 *
 * 数据源：
 *  - `GET /ai/help/coverage`（账本：覆盖率/总计/页面清单/orphan 文档；H0-02 生成物，只读）；
 *  - `GET /ai/help/todos`（H1-01：账本缺口 ∪ 遥测 `help_gap` 聚类的文档待办队列）。
 * 降级：待办接口不可用 → 顶部提示 + 账本仍展示（看板主数据不依赖遥测）；账本失败 → 错误提示。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Input,
  Row,
  Segmented,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import {
  aiExecuteTool,
  aiGetHelpCoverage,
  aiGetHelpTodos,
  aiListHelpDrafts,
  aiSetHelpTodoState,
  type HelpCoverageResult,
  type HelpDraftReviewItem,
  type HelpDraftReviewResult,
  type HelpTodoItem,
  type HelpTodoState,
  type HelpTodosResult,
} from '@/lib/api/ai-api';
import HelpDraftReviewCard from './HelpDraftReviewCard';
import { usePermissions } from '@/lib/hooks/use-permissions';

const { Title, Text } = Typography;

export const HELP_STATUS_META: Record<string, { color: string; label: string }> = {
  ok: { color: 'green', label: '已覆盖' },
  missing: { color: 'red', label: '缺失' },
  stale: { color: 'orange', label: '需复核' },
  unknown: { color: 'default', label: '未注册' },
};

export const HELP_TODO_REASON_LABEL: Record<string, string> = {
  doc_missing: '文档缺失',
  doc_stale: '文档过时',
  page_unregistered: '未注册页面',
  user_help_gaps: '用户求助缺口',
};

// H1-01/H1-03/H1-04 条件项：待办运营状态（认领/忽略持久化的看板口径）。
export const HELP_TODO_STATE_META: Record<string, { color: string; label: string }> = {
  open: { color: 'default', label: '待处理' },
  claimed: { color: 'blue', label: '已认领' },
  ignored: { color: 'orange', label: '已忽略' },
};

export const formatHelpCoveragePercent = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

const statusTag = (status: string) => {
  const meta = HELP_STATUS_META[status] ?? HELP_STATUS_META.unknown;
  return <Tag color={meta.color}>{meta.label}</Tag>;
};

const HelpCoveragePage: React.FC = () => {
  const [coverage, setCoverage] = useState<HelpCoverageResult | null>(null);
  const [todos, setTodos] = useState<HelpTodosResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<'all' | 'missing' | 'stale' | 'ok'>('all');
  const [keyword, setKeyword] = useState('');
  // H1-04 条件项：待办状态筛选/写面在途与失败提示。
  const [stateFilter, setStateFilter] = useState<'all' | 'open' | 'claimed' | 'ignored'>('all');
  const [statePending, setStatePending] = useState<string | null>(null);
  const [todoActionError, setTodoActionError] = useState<string | null>(null);
  // T5-09a：帮助草稿审阅队列（独立降级，不影响覆盖率主数据）。
  const [drafts, setDrafts] = useState<HelpDraftReviewResult | null>(null);
  const [draftLoading, setDraftLoading] = useState(false);
  const [draftError, setDraftError] = useState<string | null>(null);
  const [draftActionError, setDraftActionError] = useState<string | null>(null);
  const [draftNotice, setDraftNotice] = useState<string | null>(null);
  const [draftPendingId, setDraftPendingId] = useState<number | null>(null);
  const { hasPermission } = usePermissions();
  const canPublishHelpDraft = hasPermission('knowledge', 'admin');

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [coverageResult, todosResult, draftsResult] = await Promise.all([
        aiGetHelpCoverage(),
        // 待办（H1-01）失败不影响看板主数据。
        aiGetHelpTodos(30, 100).catch(() => null),
        // 草稿审阅（T5-09a）失败不影响覆盖率主数据。
        aiListHelpDrafts()
          .then(result => ({ ok: true as const, result }))
          .catch((err: Error) => ({ ok: false as const, error: err })),
      ]);
      setCoverage(coverageResult);
      setTodos(todosResult);
      if (draftsResult.ok) {
        setDrafts(draftsResult.result);
        setDraftError(null);
      } else {
        setDrafts(null);
        setDraftError(draftsResult.error?.message || '帮助草稿审阅队列不可用（需要知识库写权限）');
      }
    } catch (err) {
      setError((err as Error)?.message || '加载帮助覆盖率失败');
      setCoverage(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const refreshTodos = useCallback(async () => {
    const todosResult = await aiGetHelpTodos(30, 100).catch(() => null);
    setTodos(todosResult);
  }, []);

  // 认领/忽略/恢复：写成功后仅刷新待办（账本主数据不动）；失败提示且不乐观改状态（诚实口径）。
  const changeTodoState = useCallback(
    async (pageKey: string, state: HelpTodoState) => {
      setStatePending(pageKey);
      setTodoActionError(null);
      try {
        await aiSetHelpTodoState(pageKey, state);
        await refreshTodos();
      } catch (err) {
        setTodoActionError((err as Error)?.message || '更新待办状态失败');
      } finally {
        setStatePending(null);
      }
    },
    [refreshTodos]
  );

  const statesAvailable = todos ? todos.states?.available !== false : true;

  const refreshDrafts = useCallback(async () => {
    setDraftLoading(true);
    try {
      const result = await aiListHelpDrafts();
      setDrafts(result);
      setDraftError(null);
    } catch (err) {
      setDrafts(null);
      setDraftError((err as Error)?.message || '帮助草稿审阅队列不可用（需要知识库写权限）');
    } finally {
      setDraftLoading(false);
    }
  }, []);

  const runDraftAction = useCallback(
    async (item: HelpDraftReviewItem, tool: 'help_submit_review' | 'help_publish') => {
      setDraftPendingId(item.artifactId);
      setDraftActionError(null);
      setDraftNotice(null);
      try {
        const result = await aiExecuteTool(tool, { artifactId: item.artifactId });
        if (result.status === 'pending') {
          setDraftNotice('已提交审批；通过后请刷新草稿队列查看状态。');
        } else {
          setDraftNotice(result.summary || '操作完成。');
        }
        await refreshDrafts();
      } catch (err) {
        setDraftActionError((err as Error)?.message || '操作失败');
      } finally {
        setDraftPendingId(null);
      }
    },
    [refreshDrafts]
  );

  const filteredTodos = useMemo(() => {
    const items = todos?.items ?? [];
    if (stateFilter === 'all') return items;
    return items.filter(item => (item.state ?? 'open') === stateFilter);
  }, [todos, stateFilter]);

  const filteredPages = useMemo(() => {
    const pages = coverage?.pages ?? [];
    const kw = keyword.trim().toLowerCase();
    return pages.filter(page => {
      if (statusFilter !== 'all' && page.status !== statusFilter) return false;
      if (!kw) return true;
      return `${page.pageKey} ${page.title} ${page.pattern}`.toLowerCase().includes(kw);
    });
  }, [coverage, statusFilter, keyword]);

  const pageColumns = useMemo(
    () => [
      {
        title: '页面键',
        dataIndex: 'pageKey',
        key: 'pageKey',
        width: 190,
        render: (value: string) => <Text code>{value}</Text>,
      },
      { title: '页面', dataIndex: 'title', key: 'title' },
      {
        title: '路由',
        dataIndex: 'pattern',
        key: 'pattern',
        render: (value: string) => <Text type="secondary">{value}</Text>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 90,
        render: (value: string) => statusTag(value),
      },
      {
        title: '文档',
        dataIndex: 'docs',
        key: 'docs',
        render: (docs: string[]) =>
          docs.length > 0 ? (
            docs.map(path => (
              <div key={path}>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {path}
                </Text>
              </div>
            ))
          ) : (
            <Text type="secondary">—</Text>
          ),
      },
    ],
    []
  );

  const todoColumns = useMemo(
    () => [
      {
        title: '页面键',
        dataIndex: 'pageKey',
        key: 'pageKey',
        width: 190,
        render: (value: string) => <Text code>{value}</Text>,
      },
      {
        title: '文档状态',
        dataIndex: 'docStatus',
        key: 'docStatus',
        width: 100,
        render: (value: string) => statusTag(value),
      },
      {
        title: '原因',
        dataIndex: 'reason',
        key: 'reason',
        render: (value: string) => HELP_TODO_REASON_LABEL[value] ?? value,
      },
      { title: '缺口数', dataIndex: 'gapCount', key: 'gapCount', width: 80 },
      { title: '出稿', dataIndex: 'draftCount', key: 'draftCount', width: 70 },
      {
        title: '最近',
        dataIndex: 'lastSeen',
        key: 'lastSeen',
        width: 180,
        render: (value?: string) => value || '—',
      },
      {
        title: '状态',
        dataIndex: 'state',
        key: 'state',
        width: 96,
        render: (value: string | undefined) => {
          const meta = HELP_TODO_STATE_META[value ?? 'open'] ?? HELP_TODO_STATE_META.open;
          return (
            <Tag color={meta.color} data-testid={`help-todo-state-${value ?? 'open'}`}>
              {meta.label}
            </Tag>
          );
        },
      },
      {
        title: '操作',
        key: 'actions',
        width: 168,
        render: (_: unknown, record: HelpTodoItem) => {
          const current = record.state ?? 'open';
          const busy = statePending === record.pageKey;
          if (current === 'open') {
            return (
              <Space size={4}>
                <Button
                  size="small"
                  type="link"
                  data-testid={`help-todo-claim-${record.pageKey}`}
                  disabled={!statesAvailable || busy}
                  onClick={() => void changeTodoState(record.pageKey, 'claimed')}
                >
                  认领
                </Button>
                <Button
                  size="small"
                  type="link"
                  danger
                  data-testid={`help-todo-ignore-${record.pageKey}`}
                  disabled={!statesAvailable || busy}
                  onClick={() => void changeTodoState(record.pageKey, 'ignored')}
                >
                  忽略
                </Button>
              </Space>
            );
          }
          return (
            <Button
              size="small"
              type="link"
              data-testid={`help-todo-reopen-${record.pageKey}`}
              disabled={!statesAvailable || busy}
              onClick={() => void changeTodoState(record.pageKey, 'open')}
            >
              恢复待办
            </Button>
          );
        },
      },
    ],
    [changeTodoState, statePending, statesAvailable]
  );

  return (
    <div data-testid="help-coverage-page" style={{ padding: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>
          帮助覆盖率
        </Title>
        <Text type="secondary" style={{ fontSize: 12 }}>
          docs-as-code 账本（H0-02）+ 遥测缺口待办（H1-01）
          {coverage?.generatedAt ? ` · 生成于 ${coverage.generatedAt}` : ''}
        </Text>
        <span style={{ flex: 1 }} />
        <Button
          data-testid="help-coverage-refresh"
          icon={<ReloadOutlined />}
          onClick={() => void load()}
          loading={loading}
        >
          刷新
        </Button>
      </div>

      {error ? <Alert type="error" showIcon message={error} style={{ marginBottom: 12 }} /> : null}
      {coverage && coverage.orphanDocs.length > 0 ? (
        <Alert
          type="warning"
          showIcon
          data-testid="help-orphan-alert"
          style={{ marginBottom: 12 }}
          message="存在 orphan 文档（声明的 page_key 不在页面注册表内）"
          description={coverage.orphanDocs
            .map(item => `${item.doc} → ${item.unknownKeys.join(', ')}`)
            .join('；')}
        />
      ) : null}

      <Row gutter={16} style={{ marginBottom: 16 }} data-testid="help-coverage-stats">
        <Col xs={12} md={5}>
          <Card size="small">
            <Statistic title="覆盖率" value={formatHelpCoveragePercent(coverage?.coverage)} />
          </Card>
        </Col>
        <Col xs={12} md={5}>
          <Card size="small">
            <Statistic title="页面总数" value={coverage?.totals.pages ?? 0} />
          </Card>
        </Col>
        <Col xs={12} md={5}>
          <Card size="small">
            <Statistic title="缺失" value={coverage?.totals.missing ?? 0} valueStyle={{ color: '#cf1322' }} />
          </Card>
        </Col>
        <Col xs={12} md={5}>
          <Card size="small">
            <Statistic title="需复核" value={coverage?.totals.stale ?? 0} valueStyle={{ color: '#d46b08' }} />
          </Card>
        </Col>
        <Col xs={12} md={4}>
          <Card size="small">
            <Statistic title="孤立文档" value={coverage?.totals.orphan ?? 0} />
          </Card>
        </Col>
      </Row>

      <Card
        size="small"
        title="文档待办队列（近 30 天遥测缺口 ∪ 账本缺口）"
        style={{ marginBottom: 16 }}
      >
        {todos ? (
          <>
            {!todos.telemetry.available ? (
              <Alert
                type="info"
                showIcon
                data-testid="help-todos-telemetry-degraded"
                style={{ marginBottom: 8 }}
                message="埋点服务未启用：仅展示账本缺口（遥测聚类不可用）"
              />
            ) : null}
            {todos.states && !todos.states.available ? (
              <Alert
                type="info"
                showIcon
                data-testid="help-todos-states-degraded"
                style={{ marginBottom: 8 }}
                message="待办状态服务未启用：认领/忽略不可用（状态按待处理渲染）"
              />
            ) : null}
            {todoActionError ? (
              <Alert
                type="error"
                showIcon
                data-testid="help-todos-action-error"
                style={{ marginBottom: 8 }}
                message={todoActionError}
              />
            ) : null}
            <Space style={{ marginBottom: 8 }} wrap>
              <Segmented
                data-testid="help-todo-state-filter"
                value={stateFilter}
                onChange={value => setStateFilter(value as typeof stateFilter)}
                options={[
                  { label: '全部', value: 'all' },
                  { label: '待处理', value: 'open' },
                  { label: '已认领', value: 'claimed' },
                  { label: '已忽略', value: 'ignored' },
                ]}
              />
              <Text type="secondary" style={{ fontSize: 12 }}>
                共 {filteredTodos.length} 条待办
              </Text>
            </Space>
            <Table
              data-testid="help-todos-table"
              rowKey="pageKey"
              size="small"
              columns={todoColumns}
              dataSource={filteredTodos}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
            />
          </>
        ) : (
          <Alert
            type="warning"
            showIcon
            data-testid="help-todos-unavailable"
            message="待办队列不可用（埋点服务降级或接口失败），仅显示账本覆盖"
          />
        )}
      </Card>

      <HelpDraftReviewCard
        items={drafts?.items ?? []}
        loading={draftLoading}
        error={draftError}
        actionError={draftActionError}
        notice={draftNotice}
        actionPendingId={draftPendingId}
        canPublish={canPublishHelpDraft}
        onSubmitReview={item => void runDraftAction(item, 'help_submit_review')}
        onPublish={item => void runDraftAction(item, 'help_publish')}
        onRefresh={() => void refreshDrafts()}
      />

      <Card size="small" title="页面覆盖清单">
        <Space style={{ marginBottom: 12 }} wrap>
          <Segmented
            data-testid="help-status-filter"
            value={statusFilter}
            onChange={value => setStatusFilter(value as typeof statusFilter)}
            options={[
              { label: '全部', value: 'all' },
              { label: '缺失', value: 'missing' },
              { label: '需复核', value: 'stale' },
              { label: '已覆盖', value: 'ok' },
            ]}
          />
          <Input.Search
            placeholder="搜索页面键 / 标题 / 路由"
            allowClear
            style={{ width: 260 }}
            onChange={event => setKeyword(event.target.value)}
          />
          <Text type="secondary" style={{ fontSize: 12 }}>
            共 {filteredPages.length} 个页面
          </Text>
        </Space>
        <Table
          data-testid="help-pages-table"
          rowKey="pageKey"
          size="small"
          loading={loading}
          columns={pageColumns}
          dataSource={filteredPages}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
        />
      </Card>
    </div>
  );
};

export default HelpCoveragePage;