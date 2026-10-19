/**
 * LLM 调用历史与错误观测卡（租户级）。
 *
 * 数据源：`GET /api/v1/ai/llm-calls` 与 `/llm-calls/summary`。
 * 边界：只展示调用元数据与错误摘要，不展示 prompt/请求体；token 为 prompt 估算值，
 * 不产生预算、金额或计费口径。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Input,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';

import {
  aiGetLLMCallSummary,
  aiGetLLMCalls,
  type LLMCallBucket,
  type LLMCallList,
  type LLMCallRecord,
  type LLMCallSummary,
} from '@/lib/api/ai-api';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const LLM_CALL_OBSERVABILITY_CARD_TESTID = 'llm-call-observability-card';

type StatusFilter = 'all' | 'success' | 'failed';

interface CommittedFilters {
  status: StatusFilter;
  providerKey: string;
  taskType: string;
  errorClass: string;
}

interface LLMCallObservabilityCardProps {
  days: number;
}

const fmtRate = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

const fmtLatency = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value)} ms` : '—';

const LLMCallObservabilityCard: React.FC<LLMCallObservabilityCardProps> = ({ days }) => {
  const { t } = useI18n();
  const [filters, setFilters] = useState<CommittedFilters>({
    status: 'all',
    providerKey: '',
    taskType: '',
    errorClass: '',
  });
  const [providerDraft, setProviderDraft] = useState('');
  const [taskDraft, setTaskDraft] = useState('');
  const [errorDraft, setErrorDraft] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [summary, setSummary] = useState<LLMCallSummary | null>(null);
  const [list, setList] = useState<LLMCallList | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);

  useEffect(() => {
    setPage(1);
  }, [days, filters.status, filters.providerKey, filters.taskType, filters.errorClass]);

  const queryBase = useMemo(
    () => ({
      days,
      providerKey: filters.providerKey || undefined,
      taskType: filters.taskType || undefined,
      errorClass: filters.errorClass || undefined,
      success: filters.status === 'all' ? undefined : filters.status === 'success',
    }),
    [days, filters]
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [nextSummary, nextList] = await Promise.all([
        aiGetLLMCallSummary(queryBase),
        aiGetLLMCalls({ ...queryBase, page, pageSize }),
      ]);
      setSummary(nextSummary);
      setList(nextList);
    } catch (err) {
      setSummary(null);
      setList(null);
      setError((err as Error)?.message || t('aiObservability.llmCallsLoadFailed'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, queryBase, t]);

  useEffect(() => {
    void load();
  }, [load, refreshToken]);

  const commitDrafts = useCallback(() => {
    setFilters(prev => ({
      ...prev,
      providerKey: providerDraft.trim(),
      taskType: taskDraft.trim(),
      errorClass: errorDraft.trim(),
    }));
    setPage(1);
  }, [errorDraft, providerDraft, taskDraft]);

  const retry = useCallback(() => {
    setRefreshToken(value => value + 1);
  }, []);

  const columns = useMemo<ColumnsType<LLMCallRecord>>(
    () => [
      {
        title: t('aiObservability.llmCallsColTime'),
        dataIndex: 'createdAt',
        key: 'createdAt',
        width: 170,
        render: value => <Text style={{ fontSize: 12 }}>{new Date(value).toLocaleString()}</Text>,
      },
      {
        title: t('aiObservability.llmCallsColProvider'),
        key: 'provider',
        width: 150,
        render: (_, row) => (
          <Tooltip title={`${row.provider || '—'} · ${row.providerSource || '—'}`}>
            <Text style={{ fontSize: 12 }}>{row.providerKey || row.provider || '—'}</Text>
          </Tooltip>
        ),
      },
      {
        title: t('aiObservability.llmCallsColModel'),
        dataIndex: 'model',
        key: 'model',
        width: 140,
        render: value => <Text style={{ fontSize: 12 }}>{value || '—'}</Text>,
      },
      {
        title: t('aiObservability.llmCallsColTask'),
        dataIndex: 'taskType',
        key: 'taskType',
        width: 130,
        render: value => <Tag>{value || 'unknown'}</Tag>,
      },
      {
        title: t('aiObservability.llmCallsColStatus'),
        dataIndex: 'success',
        key: 'success',
        width: 90,
        render: success => (
          <Tag color={success ? 'green' : 'red'} data-testid={`llm-call-status-${success ? 'ok' : 'error'}`}>
            {success
              ? t('aiObservability.llmCallsSuccessTag')
              : t('aiObservability.llmCallsFailedTag')}
          </Tag>
        ),
      },
      {
        title: t('aiObservability.llmCallsColLatency'),
        dataIndex: 'latencyMs',
        key: 'latencyMs',
        width: 100,
        render: value => `${Math.round(value || 0)} ms`,
      },
      {
        title: t('aiObservability.llmCallsColTokens'),
        dataIndex: 'tokens',
        key: 'tokens',
        width: 120,
        render: (value, row) => (
          <Tooltip title={t('aiObservability.llmCallsEstimatedHint')}>
            <span data-testid="llm-call-tokens">
              {row.tokensEstimated ? '~' : ''}
              {value ?? 0}
            </span>
          </Tooltip>
        ),
      },
      {
        title: t('aiObservability.llmCallsColError'),
        key: 'error',
        width: 220,
        render: (_, row) =>
          row.success ? (
            '—'
          ) : (
            <Tooltip title={row.errorMessage || row.errorClass || 'unknown'}>
              <Space size={4} wrap>
                <Tag color="red" data-testid="llm-call-error-class">
                  {row.errorClass || 'unknown'}
                </Tag>
                {row.httpStatus ? <Text type="secondary">{row.httpStatus}</Text> : null}
              </Space>
            </Tooltip>
          ),
      },
    ],
    [t]
  );

  const errorBuckets = summary?.byErrorClass ?? [];
  const hasData = (summary?.total ?? 0) > 0;

  return (
    <Card
      title={t('aiObservability.llmCallsTitle')}
      data-testid={LLM_CALL_OBSERVABILITY_CARD_TESTID}
      extra={
        <Button size="small" onClick={retry} loading={loading} data-testid="llm-calls-refresh">
          {t('aiObservability.llmCallsRefresh')}
        </Button>
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('aiObservability.llmCallsDescription')}
        </Text>

        <Space wrap>
          <Select<StatusFilter>
            data-testid="llm-calls-status"
            value={filters.status}
            onChange={status => setFilters(prev => ({ ...prev, status }))}
            options={[
              { value: 'all', label: t('aiObservability.llmCallsStatusAll') },
              { value: 'success', label: t('aiObservability.llmCallsStatusSuccess') },
              { value: 'failed', label: t('aiObservability.llmCallsStatusFailed') },
            ]}
            style={{ width: 130 }}
          />
          <Input
            data-testid="llm-calls-provider"
            value={providerDraft}
            onChange={event => setProviderDraft(event.target.value)}
            onPressEnter={commitDrafts}
            onBlur={commitDrafts}
            allowClear
            placeholder={t('aiObservability.llmCallsProviderPlaceholder')}
            style={{ width: 170 }}
          />
          <Input
            data-testid="llm-calls-task"
            value={taskDraft}
            onChange={event => setTaskDraft(event.target.value)}
            onPressEnter={commitDrafts}
            onBlur={commitDrafts}
            allowClear
            placeholder={t('aiObservability.llmCallsTaskPlaceholder')}
            style={{ width: 170 }}
          />
          <Input
            data-testid="llm-calls-error-class"
            value={errorDraft}
            onChange={event => setErrorDraft(event.target.value)}
            onPressEnter={commitDrafts}
            onBlur={commitDrafts}
            allowClear
            placeholder={t('aiObservability.llmCallsErrorClassPlaceholder')}
            style={{ width: 170 }}
          />
        </Space>

        {error ? (
          <Alert
            type="error"
            showIcon
            message={error}
            data-testid="llm-calls-error"
            action={
              <Button size="small" onClick={retry}>
                {t('aiObservability.llmCallsRefresh')}
              </Button>
            }
          />
        ) : null}

        <Row gutter={[16, 16]}>
          <Col xs={12} md={6}>
            <div data-testid="llm-calls-total">
              <Statistic title={t('aiObservability.llmCallsTotal')} value={summary?.total ?? 0} />
            </div>
          </Col>
          <Col xs={12} md={6}>
            <div data-testid="llm-calls-success-rate">
              <Statistic
                title={t('aiObservability.llmCallsSuccessRate')}
                value={fmtRate(summary?.successRate)}
              />
            </div>
          </Col>
          <Col xs={12} md={6}>
            <div data-testid="llm-calls-failed">
              <Statistic title={t('aiObservability.llmCallsFailed')} value={summary?.failed ?? 0} />
            </div>
          </Col>
          <Col xs={12} md={6}>
            <div data-testid="llm-calls-avg-latency">
              <Statistic
                title={t('aiObservability.llmCallsAvgLatency')}
                value={fmtLatency(summary?.avgLatencyMs)}
              />
            </div>
          </Col>
        </Row>

        <div data-testid="llm-calls-error-distribution">
          <Text strong style={{ marginInlineEnd: 8 }}>
            {t('aiObservability.llmCallsErrorDistribution')}
          </Text>
          {errorBuckets.length > 0 ? (
            <Space wrap size={[6, 6]}>
              {errorBuckets.map((bucket: LLMCallBucket) => (
                <Tag key={bucket.key} color="red">
                  {bucket.key} · {bucket.count}
                </Tag>
              ))}
            </Space>
          ) : (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('aiObservability.llmCallsNoError')}
            </Text>
          )}
        </div>

        {!loading && !error && !hasData ? (
          <Text type="secondary" data-testid="llm-calls-empty">
            {t('aiObservability.llmCallsEmpty')}
          </Text>
        ) : null}

        <Table<LLMCallRecord>
          rowKey="id"
          size="small"
          loading={loading}
          columns={columns}
          dataSource={list?.items ?? []}
          data-testid="llm-calls-table"
          scroll={{ x: 1100 }}
          pagination={{
            current: page,
            pageSize,
            total: list?.total ?? 0,
            showSizeChanger: false,
            onChange: setPage,
          }}
        />
      </Space>
    </Card>
  );
};

export default LLMCallObservabilityCard;
