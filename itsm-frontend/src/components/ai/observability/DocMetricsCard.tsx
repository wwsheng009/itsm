/**
 * P2 文档命中率/解决率运营报表卡（挂 `/ai/observability`）。
 *
 * 数据源：`GET /api/v1/ai/observability/doc-metrics`（遥测聚合 + 账本覆盖率）。
 * 降级：埋点未装配（telemetry.available=false）→ 提示仅覆盖率；请求失败 → 错误提示（可刷新重试）。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Row, Space, Statistic, Table, Typography } from 'antd';
import { Download } from 'lucide-react';
import type { ColumnsType } from 'antd/es/table';

import { aiGetDocMetrics, type DocMetricsPage, type DocMetricsReport } from '@/lib/api/ai-api';
import { buildDocMetricsMarkdown } from '@/lib/ai/doc-metrics-markdown';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { downloadTextFile } from '@/lib/utils/download-text-file';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const DOC_METRICS_CARD_TESTID = 'doc-metrics-card';

/** 比率格式化：null/缺省 → 「—」（不虚报 0%）。 */
export const fmtDocMetricsRate = (value: number | null | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

interface DocMetricsCardProps {
  /** 统计窗口天数（后端缺省 30、上限 90）。 */
  days: number;
  pageKey?: string;
}

const DocMetricsCard: React.FC<DocMetricsCardProps> = ({ days, pageKey = 'ai.observability' }) => {
  const { t } = useI18n();
  const [report, setReport] = useState<DocMetricsReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (windowDays: number) => {
      setLoading(true);
      setError(null);
      try {
        setReport(await aiGetDocMetrics({ days: windowDays }));
      } catch {
        setReport(null);
        setError(t('aiObservability.docMetricsLoadFailed'));
      } finally {
        setLoading(false);
      }
    },
    [t]
  );

  useEffect(() => {
    void load(days);
  }, [days, load]);

  const handleExport = useCallback(() => {
    if (!report) return;
    // 报障/归档导出：Markdown 快照落盘（埋点先行，下载失败静默降级）。
    trackAssistantEvent('handoff', { pageKey, source: 'doc_metrics_export' });
    downloadTextFile(`ai-doc-metrics-${report.days}d.md`, buildDocMetricsMarkdown(report));
  }, [report, pageKey]);

  const columns = useMemo<ColumnsType<DocMetricsPage>>(
    () => [
      { title: t('aiObservability.docMetricsPageCol'), dataIndex: 'pageKey', key: 'pageKey' },
      { title: t('aiObservability.docMetricsOpenedCol'), dataIndex: 'opened', key: 'opened', width: 80 },
      { title: t('aiObservability.docMetricsGapsCol'), dataIndex: 'gaps', key: 'gaps', width: 80 },
      { title: t('aiObservability.docMetricsDraftsCol'), dataIndex: 'drafts', key: 'drafts', width: 80 },
      {
        title: t('aiObservability.docMetricsPublishedCol'),
        dataIndex: 'published',
        key: 'published',
        width: 80,
      },
      { title: t('aiObservability.docMetricsHelpful'), dataIndex: 'helpful', key: 'helpful', width: 90 },
      { title: t('aiObservability.docMetricsUnhelpful'), dataIndex: 'unhelpful', key: 'unhelpful', width: 90 },
    ],
    [t]
  );

  const coverage = report?.coverage;

  return (
    <Card
      title={t('aiObservability.docMetricsTitle')}
      data-testid={DOC_METRICS_CARD_TESTID}
      extra={
        report ? (
          <Button
            size="small"
            icon={<Download size={13} />}
            data-testid="doc-metrics-export"
            onClick={handleExport}
          >
            {t('aiObservability.docMetricsExport')}
          </Button>
        ) : null
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('aiObservability.docMetricsDescription')}
        </Text>
        {error ? <Alert type="error" showIcon message={error} data-testid="doc-metrics-error" /> : null}
        {report && !report.telemetry.available ? (
          <Alert
            type="warning"
            showIcon
            data-testid="doc-metrics-telemetry-disabled"
            message={t('aiObservability.docMetricsTelemetryDisabled')}
          />
        ) : null}

        {report ? (
          <>
            <Row gutter={[16, 16]}>
              <Col span={6}>
                <div data-testid="doc-metrics-hit-rate">
                  <Statistic
                    title={t('aiObservability.docMetricsHitRate')}
                    value={fmtDocMetricsRate(report.rates.hitRate)}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-resolve-rate">
                  <Statistic
                    title={t('aiObservability.docMetricsResolveRate')}
                    value={fmtDocMetricsRate(report.rates.resolveRate)}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-feedback-rate">
                  <Statistic
                    title={t('aiObservability.docMetricsFeedbackResolveRate')}
                    value={fmtDocMetricsRate(report.rates.feedbackResolveRate)}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-opened">
                  <Statistic title={t('aiObservability.docMetricsOpened')} value={report.totals.opened} />
                </div>
              </Col>
            </Row>
            <Row gutter={[16, 16]}>
              <Col span={6}>
                <div data-testid="doc-metrics-gaps">
                  <Statistic title={t('aiObservability.docMetricsGaps')} value={report.totals.gaps} />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-drafts">
                  <Statistic title={t('aiObservability.docMetricsDrafts')} value={report.totals.drafts} />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-feedback">
                  <Statistic
                    title={t('aiObservability.docMetricsFeedback')}
                    value={`${report.totals.feedbackHelpful} / ${report.totals.feedbackUnhelpful}`}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-coverage">
                  <Statistic
                    title={t('aiObservability.docMetricsCoverage')}
                    value={fmtDocMetricsRate(coverage?.coverage)}
                  />
                  <Text type="secondary" style={{ fontSize: 11 }}>
                    {t('aiObservability.docMetricsCoverageHint', {
                      pages: coverage?.totals?.pages ?? 0,
                      missing: coverage?.totals?.missing ?? 0,
                      stale: coverage?.totals?.stale ?? 0,
                    })}
                  </Text>
                </div>
              </Col>
            </Row>
            <Row gutter={[16, 16]}>
              <Col span={6}>
                <div data-testid="doc-metrics-draft-publish-rate">
                  <Statistic
                    title={t('aiObservability.docMetricsDraftPublishRate')}
                    value={fmtDocMetricsRate(report.rates.draftPublishRate)}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="doc-metrics-published">
                  <Statistic
                    title={t('aiObservability.docMetricsPublished')}
                    value={report.totals.published}
                  />
                </div>
              </Col>
              <Col span={12}>
                <Text type="secondary" style={{ fontSize: 11 }}>
                  {t('aiObservability.docMetricsDraftPublishHint')}
                </Text>
              </Col>
            </Row>
            {report.totals.opened === 0 ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="doc-metrics-empty">
                {t('aiObservability.empty')}
              </Text>
            ) : null}
            <Table<DocMetricsPage>
              rowKey="pageKey"
              size="small"
              columns={columns}
              dataSource={report.byPage}
              pagination={false}
              data-testid="doc-metrics-by-page"
              locale={{ emptyText: t('aiObservability.empty') }}
            />
          </>
        ) : loading ? (
          <Text type="secondary">{`${t('aiObservability.refresh')}…`}</Text>
        ) : null}
      </Space>
    </Card>
  );
};

export default DocMetricsCard;