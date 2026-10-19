/**
 * V0-04 试点对照报告卡（挂 `/ai/observability`）。
 *
 * 内容：本期 vs 基线（前一窗口）逐指标对照 + §11.5 放量门槛判定 + Markdown 导出（复用 I2-02 下载工具）。
 * 降级：埋点服务未装配（503/404）→ 提示未启用；其余错误 → 错误提示。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Space, Table, Tag, Tooltip, Typography } from 'antd';
import { Download } from 'lucide-react';
import type { ColumnsType } from 'antd/es/table';

import { aiGetPilotReport, type PilotMetric, type PilotReport } from '@/lib/api/ai-api';
import { buildPilotReportMarkdown } from '@/lib/ai/pilot-report-markdown';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { downloadTextFile } from '@/lib/utils/download-text-file';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const PILOT_REPORT_CARD_TESTID = 'pilot-report-card';

interface PilotReportCardProps {
  /** 对照窗口天数（本期=其前同长基线）。 */
  days: number;
  pageKey?: string;
}

const fmtRate = (value: number): string => `${(value * 100).toFixed(1)}%`;
const fmtMetric = (metric: PilotMetric, which: 'current' | 'baseline'): string => {
  const value = which === 'current' ? metric.current : metric.baseline;
  return metric.unit === 'rate' ? fmtRate(value) : `${Math.round(value)} ms`;
};
const fmtImprovement = (metric: PilotMetric): string => {
  if (!metric.comparable) return '—';
  const sign = metric.improvement >= 0 ? '+' : '';
  return `${sign}${(metric.improvement * 100).toFixed(1)}%`;
};

type MetricRow = PilotMetric & { pilot?: string; rowKey?: string };

const PilotReportCard: React.FC<PilotReportCardProps> = ({ days, pageKey = 'ai.observability' }) => {
  const { t } = useI18n();
  const [report, setReport] = useState<PilotReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [disabled, setDisabled] = useState(false);

  const load = useCallback(
    async (windowDays: number) => {
      setLoading(true);
      setError(null);
      try {
        setReport(await aiGetPilotReport({ currentDays: windowDays, baselineDays: windowDays }));
        setDisabled(false);
      } catch (err) {
        const status = (err as { response?: { status?: number }; status?: number })?.response?.status
          ?? (err as { status?: number })?.status;
        if (status === 503 || status === 404) {
          setDisabled(true);
          setReport(null);
        } else {
          setReport(null);
          setError(t('aiObservability.pilotLoadFailed'));
        }
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
    trackAssistantEvent('handoff', { pageKey, source: 'pilot_report_export' });
    downloadTextFile(`ai-pilot-report-${report.current.days}d.md`, buildPilotReportMarkdown(report));
  }, [report, pageKey]);

  const metricRows = useMemo(() => {
    if (!report) return [];
    return report.sections.flatMap(section =>
      section.metrics.map(metric => ({
        ...metric,
        rowKey: `${section.key}:${metric.key}`,
        pilot: section.pilot,
      }))
    );
  }, [report]);

  const metricColumns = useMemo<ColumnsType<MetricRow>>(
    () => [
      {
        title: t('aiObservability.pilotMetricCol'),
        key: 'metric',
        render: (_: unknown, row: MetricRow) => (
          <Space size={4}>
            {row.pilot ? (
              <Tag color="blue" style={{ marginInlineEnd: 0 }}>
                {row.pilot}
              </Tag>
            ) : null}
            <Text style={{ fontSize: 12 }}>{row.title}</Text>
            {row.note ? (
              <Tooltip title={row.note}>
                <Text type="secondary" style={{ fontSize: 11 }}>
                  ⓘ
                </Text>
              </Tooltip>
            ) : null}
          </Space>
        ),
      },
      {
        title: t('aiObservability.pilotBaselineCol'),
        key: 'baseline',
        width: 110,
        render: (_: unknown, row: MetricRow) => fmtMetric(row, 'baseline'),
      },
      {
        title: t('aiObservability.pilotCurrentCol'),
        key: 'current',
        width: 110,
        render: (_: unknown, row: MetricRow) => fmtMetric(row, 'current'),
      },
      {
        title: t('aiObservability.pilotImprovementCol'),
        key: 'improvement',
        width: 110,
        render: (_: unknown, row: MetricRow) => fmtImprovement(row),
      },
      {
        title: t('aiObservability.pilotVerdictCol'),
        key: 'verdict',
        width: 130,
        render: (_: unknown, row: MetricRow) =>
          row.comparable ? (
            <Tag color={row.improved ? 'green' : 'red'}>
              {row.improved ? t('aiObservability.pilotMet') : t('aiObservability.pilotNotMet')}
            </Tag>
          ) : (
            <Tag>{t('aiObservability.pilotInsufficient')}</Tag>
          ),
      },
    ],
    [t]
  );

  return (
    <Card
      data-testid={PILOT_REPORT_CARD_TESTID}
      title={t('aiObservability.pilotTitle')}
      extra={
        <Space>
          {report ? (
            <Tag
              data-testid="pilot-report-gate"
              color={report.gate.passed ? 'green' : 'red'}
              style={{ marginInlineEnd: 0 }}
            >
              {report.gate.passed ? t('aiObservability.pilotGatePassed') : t('aiObservability.pilotGateBlocked')}
              （{t('aiObservability.pilotPilotsMet')} {report.gate.pilotsMet}/{report.gate.required}）
            </Tag>
          ) : null}
          <Button
            size="small"
            icon={<Download size={14} />}
            disabled={!report}
            onClick={handleExport}
            data-testid="pilot-report-export"
          >
            {t('aiObservability.pilotExport')}
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }} data-testid="pilot-report-body">
        {disabled ? (
          <Alert
            type="info"
            showIcon
            data-testid="pilot-report-disabled"
            message={t('aiObservability.disabledTitle')}
          />
        ) : null}
        {error ? (
          <Alert type="warning" showIcon data-testid="pilot-report-error" message={error} />
        ) : null}
        {report ? (
          <>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('aiObservability.pilotWindowNote', {
                current: report.current.days,
                baseline: report.baseline.days,
                generated: report.generatedAt,
              })}
            </Text>
            <Space wrap>
              {report.sections.map(section => (
                <Tooltip
                  key={section.key}
                  title={section.reasons?.join('；')}
                >
                  <Tag color={section.met ? 'green' : 'red'} data-testid={`pilot-section-${section.key}`}>
                    {section.pilot} {section.met ? t('aiObservability.pilotMet') : t('aiObservability.pilotNotMet')}
                  </Tag>
                </Tooltip>
              ))}
            </Space>
            <Table<MetricRow>
              data-testid="pilot-report-table"
              rowKey="rowKey"
              size="small"
              loading={loading}
              dataSource={metricRows}
              columns={metricColumns}
              pagination={false}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('aiObservability.pilotNegativeTitle')}
            </Text>
            <Table<MetricRow>
              data-testid="pilot-report-negative"
              rowKey="key"
              size="small"
              dataSource={report.negative}
              columns={metricColumns}
              pagination={false}
            />
            {report.gate.reasons.length > 0 ? (
              <Alert
                type={report.gate.passed ? 'success' : 'warning'}
                showIcon
                data-testid="pilot-report-gate-reasons"
                message={report.gate.reasons.join('；')}
              />
            ) : null}
            {report.gate.thresholds.length > 0 ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="pilot-report-thresholds">
                {report.gate.thresholds.join('；')}
              </Text>
            ) : null}
            {report.notes?.length ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="pilot-report-notes">
                {report.notes.join('；')}
              </Text>
            ) : null}
          </>
        ) : !disabled && !error ? (
          <Text type="secondary">{`${t('aiObservability.refresh')}…`}</Text>
        ) : null}
      </Space>
    </Card>
  );
};

export default PilotReportCard;