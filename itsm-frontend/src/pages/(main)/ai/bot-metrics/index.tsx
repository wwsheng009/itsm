/**
 * Bot 运行维度看板（B4-02）
 *
 * 数据来源：`GET /api/v1/ai/bot-metrics`（service/bot/metrics.go 聚合 bot_runs/bot_steps/tool_invocations）。
 * 口径：成功/确认/verify/工具错误/时延 + 成本代理（token 计量未接线，页面显式提示）。
 * `bot.enabled=false` 时后端返回 503 → 页面展示「能力未启用」提示，不报错。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Row, Select, Space, Statistic, Table, Tag, Tooltip, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  aiGetBotMetrics,
  type BotMetrics,
  type BotMetricsBreakdown,
} from '@/lib/api/ai-api';

const { Title, Text } = Typography;

const ENTRYPOINTS = ['chat', 'ticket_detail', 'ticket_list', 'incident_detail', 'incident_create', 'ci_detail'];

export const pickErrStatus = (err: unknown): number | undefined => {
  const e = err as { response?: { status?: number }; status?: number; code?: number } | undefined;
  return e?.response?.status ?? e?.status ?? (typeof e?.code === 'number' ? e.code : undefined);
};

export const fmtRate = (v: number | undefined): string =>
  typeof v === 'number' && Number.isFinite(v) ? `${(v * 100).toFixed(1)}%` : '—';

export const fmtMs = (v: number | undefined): string =>
  typeof v === 'number' && Number.isFinite(v) ? `${Math.round(v)} ms` : '—';

const BotMetricsPage: React.FC = () => {
  const { t } = useI18n();
  const [days, setDays] = useState(7);
  const [entrypoint, setEntrypoint] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<BotMetrics | null>(null);
  const [disabled, setDisabled] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await aiGetBotMetrics({ days, entrypoint });
      setData(result);
      setDisabled(false);
    } catch (err) {
      const status = pickErrStatus(err);
      if (status === 503 || status === 404) {
        setDisabled(true);
        setData(null);
      } else {
        setError((err as Error)?.message || t('aiBotMetrics.loadFailed'));
      }
    } finally {
      setLoading(false);
    }
  }, [days, entrypoint, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const breakdownColumns = useMemo(
    () => [
      { title: t('aiBotMetrics.colKey'), dataIndex: 'key', key: 'key' },
      { title: t('aiBotMetrics.colRuns'), dataIndex: 'runs', key: 'runs' },
      { title: t('aiBotMetrics.colFailed'), dataIndex: 'failed', key: 'failed' },
      {
        title: t('aiBotMetrics.colSuccessRate'),
        dataIndex: 'successRate',
        key: 'successRate',
        render: (v: number) => fmtRate(v),
      },
      {
        title: t('aiBotMetrics.colAvgDuration'),
        dataIndex: 'avgDurationMs',
        key: 'avgDurationMs',
        render: (v: number) => fmtMs(v),
      },
      { title: t('aiBotMetrics.colToolCalls'), dataIndex: 'toolCalls', key: 'toolCalls' },
      { title: t('aiBotMetrics.colToolErrors'), dataIndex: 'toolErrors', key: 'toolErrors' },
    ],
    [t],
  );

  if (disabled) {
    return (
      <div data-testid="bot-metrics-page" style={{ padding: 24 }}>
        <Alert
          type="info"
          showIcon
          message={t('aiBotMetrics.disabledTitle')}
          description={t('aiBotMetrics.disabledHint')}
          data-testid="bot-metrics-disabled"
        />
      </div>
    );
  }

  return (
    <div data-testid="bot-metrics-page" style={{ padding: 24 }}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Row justify="space-between" align="middle">
          <Col>
            <Title level={4} style={{ margin: 0 }}>
              {t('aiBotMetrics.title')}
            </Title>
            <Text type="secondary">{t('aiBotMetrics.description')}</Text>
          </Col>
          <Col>
            <Space>
              <Select
                data-testid="bot-metrics-days"
                value={days}
                onChange={setDays}
                options={[7, 14, 30].map((d) => ({ value: d, label: t('aiBotMetrics.lastDays', { days: d }) }))}
                style={{ width: 140 }}
              />
              <Select
                data-testid="bot-metrics-entrypoint"
                allowClear
                placeholder={t('aiBotMetrics.allEntrypoints')}
                value={entrypoint}
                onChange={(v) => setEntrypoint(v)}
                options={ENTRYPOINTS.map((e) => ({ value: e, label: e }))}
                style={{ width: 180 }}
              />
              <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading} data-testid="bot-metrics-refresh">
                {t('aiBotMetrics.refresh')}
              </Button>
            </Space>
          </Col>
        </Row>

        {error && <Alert type="error" showIcon message={error} data-testid="bot-metrics-error" />}

        {data?.tokensRecorded === false && (
          <Alert
            type="warning"
            showIcon
            message={t('aiBotMetrics.tokensNotRecorded')}
            data-testid="bot-metrics-token-note"
          />
        )}

        <Row gutter={16}>
          <Col span={6}>
            <Card>
              <Statistic
                title={t('aiBotMetrics.runSuccessRate')}
                value={fmtRate(data?.runs.successRate)}
                data-testid="kpi-run-success"
              />
              <Text type="secondary">
                {t('aiBotMetrics.runCounts', {
                  completed: data?.runs.completed ?? 0,
                  failed: data?.runs.failed ?? 0,
                  running: data?.runs.running ?? 0,
                })}
              </Text>
            </Card>
          </Col>
          <Col span={6}>
            <Card>
              <Statistic
                title={t('aiBotMetrics.approvalRate')}
                value={fmtRate(data?.confirmations.approvalRate)}
                data-testid="kpi-approval-rate"
              />
              <Text type="secondary">
                {t('aiBotMetrics.confirmCounts', {
                  approved: data?.confirmations.approved ?? 0,
                  rejected: data?.confirmations.rejected ?? 0,
                  expired: data?.confirmations.expired ?? 0,
                })}
              </Text>
            </Card>
          </Col>
          <Col span={6}>
            <Card>
              <Statistic
                title={t('aiBotMetrics.verifyFailRate')}
                value={fmtRate(data?.verify.failRate)}
                data-testid="kpi-verify-fail"
              />
              <Text type="secondary">
                {t('aiBotMetrics.verifyCounts', {
                  verified: data?.verify.verified ?? 0,
                  failed: data?.verify.failed ?? 0,
                })}
              </Text>
            </Card>
          </Col>
          <Col span={6}>
            <Card>
              <Statistic
                title={t('aiBotMetrics.toolErrorRate')}
                value={fmtRate(data?.tools.errorRate)}
                data-testid="kpi-tool-error"
              />
              <Text type="secondary">
                {t('aiBotMetrics.toolCounts', { total: data?.tools.total ?? 0, errors: data?.tools.errors ?? 0 })}
              </Text>
            </Card>
          </Col>
        </Row>

        <Card
          title={t('aiBotMetrics.costTitle')}
          extra={
            data?.tokensRecorded === false ? (
              <Tooltip title={t('aiBotMetrics.tokensNotRecorded')}>
                <Tag color="orange" data-testid="cost-proxy-tag">
                  {t('aiBotMetrics.costProxy')}
                </Tag>
              </Tooltip>
            ) : null
          }
        >
          <Row gutter={16}>
            <Col span={6}>
              <Statistic title={t('aiBotMetrics.llmCalls')} value={data?.cost.llmCalls ?? 0} />
            </Col>
            <Col span={6}>
              <Statistic title={t('aiBotMetrics.toolCalls')} value={data?.cost.toolCalls ?? 0} />
            </Col>
            <Col span={6}>
              <Statistic title={t('aiBotMetrics.avgStepsPerRun')} value={data?.cost.avgStepsPerRun ?? 0} precision={2} />
            </Col>
            <Col span={6}>
              <Statistic
                title={t('aiBotMetrics.avgRunDuration')}
                value={fmtMs(data?.runs.avgDurationMs)}
              />
            </Col>
          </Row>
        </Card>

        <Card title={t('aiBotMetrics.byEntrypoint')}>
          <Table<BotMetricsBreakdown>
            rowKey="key"
            size="small"
            loading={loading}
            dataSource={data?.byEntrypoint ?? []}
            columns={breakdownColumns}
            pagination={false}
            data-testid="bot-metrics-by-entrypoint"
            locale={{ emptyText: t('aiBotMetrics.empty') }}
          />
        </Card>

        <Card title={t('aiBotMetrics.byBot')}>
          <Table<BotMetricsBreakdown>
            rowKey="key"
            size="small"
            loading={loading}
            dataSource={data?.byBot ?? []}
            columns={breakdownColumns}
            pagination={false}
            data-testid="bot-metrics-by-bot"
            locale={{ emptyText: t('aiBotMetrics.empty') }}
          />
        </Card>
      </Space>
    </div>
  );
};

export default BotMetricsPage;
