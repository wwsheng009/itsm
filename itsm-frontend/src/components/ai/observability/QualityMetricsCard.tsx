/**
 * T5-06b 质量层指标卡（挂 `/ai/observability`）。
 *
 * 数据源：`GET /api/v1/ai/telemetry/summary` 的 `quality`（T5-06a 起可算）。
 * 口径：引用覆盖率 / 无引用回答率共用「带 sources_count 的 stream_end」分母；
 * 点踩率以回答级反馈为分母。分母为 0 或旧客户端未上报时显示「—」，不伪造 0。
 */
import React from 'react';
import { Card, Col, Row, Space, Statistic, Tag, Typography } from 'antd';

import type { AssistantTelemetryQuality } from '@/lib/api/ai-api';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const QUALITY_METRICS_CARD_TESTID = 'quality-metrics-card';

export const fmtQualityRate = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

interface QualityMetricsCardProps {
  quality?: AssistantTelemetryQuality;
}

const QualityMetricsCard: React.FC<QualityMetricsCardProps> = ({ quality }) => {
  const { t } = useI18n();
  const answerCount = quality?.answerCount ?? 0;
  const citationAnswerCount = quality?.citationAnswerCount ?? 0;
  const noCitationCount = Math.max(answerCount - citationAnswerCount, 0);
  const feedbackCount = quality?.feedbackCount ?? 0;
  const downvoteCount = quality?.downvoteCount ?? 0;
  const hasAnswerSample = answerCount > 0;
  const hasFeedbackSample = feedbackCount > 0;

  return (
    <Card
      data-testid={QUALITY_METRICS_CARD_TESTID}
      title={t('aiObservability.qualityTitle')}
      extra={
        <Tag color="green" data-testid="quality-measurable" style={{ marginInlineEnd: 0 }}>
          {t('aiObservability.qualityMeasurable')}
        </Tag>
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Text type="secondary" style={{ fontSize: 12 }} data-testid="quality-description">
          {t('aiObservability.qualityDescription')}
        </Text>
        <Row gutter={[16, 16]}>
          <Col xs={24} md={8}>
            <div data-testid="quality-citation-coverage">
              <Statistic
                title={t('aiObservability.qualityCitationCoverage')}
                value={fmtQualityRate(quality?.citationCoverage)}
              />
            </div>
            <Text type="secondary" style={{ fontSize: 12 }} data-testid="quality-citation-sample">
              {t('aiObservability.qualityCitationSample', {
                cited: citationAnswerCount,
                total: answerCount,
              })}
            </Text>
          </Col>
          <Col xs={24} md={8}>
            <div data-testid="quality-no-citation-rate">
              <Statistic
                title={t('aiObservability.qualityNoCitationRate')}
                value={fmtQualityRate(quality?.noCitationRate)}
              />
            </div>
            <Text type="secondary" style={{ fontSize: 12 }} data-testid="quality-no-citation-sample">
              {t('aiObservability.qualityNoCitationSample', {
                uncited: noCitationCount,
                total: answerCount,
              })}
            </Text>
          </Col>
          <Col xs={24} md={8}>
            <div data-testid="quality-downvote-rate">
              <Statistic
                title={t('aiObservability.qualityDownvoteRate')}
                value={fmtQualityRate(quality?.downvoteRate)}
              />
            </div>
            <Text type="secondary" style={{ fontSize: 12 }} data-testid="quality-downvote-sample">
              {t('aiObservability.qualityDownvoteSample', {
                downvotes: downvoteCount,
                total: feedbackCount,
              })}
            </Text>
          </Col>
        </Row>
        {!hasAnswerSample && !hasFeedbackSample ? (
          <Text type="secondary" style={{ fontSize: 12 }} data-testid="quality-empty">
            {t('aiObservability.qualityEmpty')}
          </Text>
        ) : null}
      </Space>
    </Card>
  );
};

export default QualityMetricsCard;
